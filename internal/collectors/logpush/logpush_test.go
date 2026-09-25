package logpush

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

const logpushTestDataset = "logpushHealthAdaptiveGroups"

type logpushTestAPI struct {
	settings cfapi.DatasetSettings
	query    func(cfapi.GraphQLRequest) ([]map[string]any, error)
	requests []cfapi.GraphQLRequest
}

func (*logpushTestAPI) Get(context.Context, string, url.Values, any) error {
	return errors.New("unexpected REST request")
}
func (f *logpushTestAPI) Query(_ context.Context, request cfapi.GraphQLRequest, out any) error {
	f.requests = append(f.requests, request)
	rows, err := f.query(request)
	if err != nil {
		return err
	}
	b, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
func (f *logpushTestAPI) DatasetSettings(_ context.Context, scope cfapi.Scope, _ string, dataset string) (cfapi.DatasetSettings, error) {
	if scope != cfapi.AccountScope || dataset != logpushTestDataset {
		return cfapi.DatasetSettings{}, errors.New("unexpected dataset settings request")
	}
	return f.settings, nil
}
func (*logpushTestAPI) Accounts(context.Context) ([]cfapi.Account, error) {
	return nil, errors.New("unexpected account discovery")
}
func (*logpushTestAPI) Zones(context.Context) ([]cfapi.Zone, error) {
	return nil, errors.New("unexpected zone discovery")
}
func (*logpushTestAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) {
	return nil, errors.New("unexpected gateway discovery")
}

func logpushTestSettings() cfapi.DatasetSettings {
	return cfapi.DatasetSettings{
		Enabled:           true,
		AvailableFields:   []string{"sum_uploads", "sum_records", "dimensions_datetimeFiveMinutes", "dimensions_destination"},
		MaxNumberOfFields: 4,
		MaxDuration:       3600,
		NotOlderThan:      7 * 24 * 60 * 60,
		MaxPageSize:       30,
	}
}
func logpushTestConfig() *config.Config {
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "synthetic-account"
	return &cfg
}
func logpushTestRow(bucket time.Time, uploads, records any) map[string]any {
	return map[string]any{
		"sum":        map[string]any{"uploads": uploads, "records": records},
		"dimensions": map[string]any{"datetimeFiveMinutes": bucket.UTC().Format(time.RFC3339), "destination": "private-destination"},
	}
}

func TestLogpushHealthSumsAdvertisedSourceFieldsWithoutAttributes(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(10 * time.Minute)
	api := &logpushTestAPI{
		settings: logpushTestSettings(),
		query: func(cfapi.GraphQLRequest) ([]map[string]any, error) {
			return []map[string]any{logpushTestRow(from, 2, 30), logpushTestRow(from.Add(5*time.Minute), 3, 50)}, nil
		},
	}
	out := &telemetry.Buffer{}
	mark, err := NewHealthMetrics(logpushTestConfig(), api).CollectWindow(context.Background(), from, to, out)
	if err != nil || !mark.Equal(to) {
		t.Fatalf("collection returned mark=%s err=%v", mark, err)
	}
	if len(api.requests) != 1 {
		t.Fatalf("queries = %d, want 1", len(api.requests))
	}
	request := api.requests[0]
	if request.Scope != cfapi.AccountScope || request.ScopeID != "synthetic-account" || request.Dataset != logpushTestDataset {
		t.Fatalf("request = %+v", request)
	}
	if !equalLogpushStrings(request.WantedFields, []string{"sum.uploads", "sum.records", "dimensions.datetimeFiveMinutes"}) {
		t.Fatalf("selected fields = %v", request.WantedFields)
	}
	if request.Limit != 30 || !request.From.Equal(from) || !request.To.Equal(to) {
		t.Fatalf("request limit/window = %d %s..%s", request.Limit, request.From, request.To)
	}
	if len(out.Metrics) != 2 {
		t.Fatalf("metrics = %+v, want uploads and records", out.Metrics)
	}
	values := map[string]float64{}
	for _, metric := range out.Metrics {
		if metric.Kind != "counter" || len(metric.Attrs) != 0 {
			t.Fatalf("unexpected metric kind or attributes: %+v", metric)
		}
		values[metric.Name] = metric.Value
	}
	if values[semconv.MetricLogpushUploads] != 5 || values[semconv.MetricLogpushRecords] != 80 {
		t.Fatalf("metric totals = %v, want uploads=5 records=80", values)
	}
}

func TestLogpushHealthRequiresAllValueAndTimeFields(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute)
	settings := logpushTestSettings()
	settings.AvailableFields = []string{"sum_uploads", "dimensions_datetimeFiveMinutes"}
	api := &logpushTestAPI{settings: settings, query: func(cfapi.GraphQLRequest) ([]map[string]any, error) { return nil, nil }}
	out := &telemetry.Buffer{}
	mark, err := NewHealthMetrics(logpushTestConfig(), api).CollectWindow(context.Background(), from, to, out)
	if err == nil || !mark.Equal(from) || len(out.Metrics) != 0 {
		t.Fatalf("missing records field returned mark=%s err=%v metrics=%d", mark, err, len(out.Metrics))
	}
}

func TestLogpushHealthUsesOnlyCompleteFiveMinuteBuckets(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 2, 0, 0, time.UTC)
	to := from.Add(10 * time.Minute)
	api := &logpushTestAPI{
		settings: logpushTestSettings(),
		query: func(cfapi.GraphQLRequest) ([]map[string]any, error) {
			bucket := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
			return []map[string]any{logpushTestRow(bucket, 1, 10), logpushTestRow(bucket.Add(5*time.Minute), 2, 20), logpushTestRow(bucket.Add(10*time.Minute), 4, 40)}, nil
		},
	}
	out := &telemetry.Buffer{}
	mark, err := NewHealthMetrics(logpushTestConfig(), api).CollectWindow(context.Background(), from, to, out)
	wantMark := time.Date(2026, 9, 24, 10, 10, 0, 0, time.UTC)
	if err != nil || !mark.Equal(wantMark) || len(out.Metrics) != 2 {
		t.Fatalf("incomplete bucket result mark=%s err=%v metrics=%+v", mark, err, out.Metrics)
	}
	values := map[string]float64{}
	for _, metric := range out.Metrics {
		values[metric.Name] = metric.Value
	}
	if values[semconv.MetricLogpushUploads] != 2 || values[semconv.MetricLogpushRecords] != 20 {
		t.Fatalf("complete bucket sums = %v, want uploads=2 records=20", values)
	}
}

func TestLogpushHealthCapIsDeterministicAndDisclosesDroppedSeries(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute)
	cfg := logpushTestConfig()
	cfg.Platform.MaxMetricSeriesPerWindow = 1
	api := &logpushTestAPI{settings: logpushTestSettings(), query: func(cfapi.GraphQLRequest) ([]map[string]any, error) {
		return []map[string]any{logpushTestRow(from, 1, 10)}, nil
	}}
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	out := &telemetry.Buffer{}
	mark, err := NewHealthMetrics(cfg, api).CollectWindow(context.Background(), from, to, out)
	if err != nil || !mark.Equal(to) || len(out.Metrics) != 1 {
		t.Fatalf("cap result mark=%s err=%v metrics=%+v", mark, err, out.Metrics)
	}
	if out.Metrics[0].Name != semconv.MetricLogpushRecords || len(out.Metrics[0].Attrs) != 0 {
		t.Fatalf("retained metric = %+v, want deterministic first metric and no attributes", out.Metrics[0])
	}
	if got := logs.String(); strings.Count(got, "platform metric series dropped") != 1 || !strings.Contains(got, "dropped_series=1") || strings.Contains(got, "private-destination") {
		t.Fatalf("drop disclosure = %q, want one count-only warning", got)
	}
}

func TestLogpushHealthSplitsSaturatedQueriesIntoAdjacentWindows(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(20 * time.Minute)
	api := &logpushTestAPI{
		settings: logpushTestSettings(),
		query: func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
			if request.To.Sub(request.From) > 5*time.Minute {
				return nil, fmt.Errorf("GraphQL dataset %s window saturated limit %d", request.Dataset, request.Limit)
			}
			return []map[string]any{logpushTestRow(request.From, 1, 10)}, nil
		},
	}
	out := &telemetry.Buffer{}
	mark, err := NewHealthMetrics(logpushTestConfig(), api).CollectWindow(context.Background(), from, to, out)
	if err != nil || !mark.Equal(to) || len(api.requests) != 7 || len(out.Metrics) != 2 {
		t.Fatalf("split result mark=%s err=%v requests=%d metrics=%+v", mark, err, len(api.requests), out.Metrics)
	}
	leaves := make([]cfapi.GraphQLRequest, 0, 4)
	for _, request := range api.requests {
		if request.From.Second() != 0 || request.From.Nanosecond() != 0 || request.To.Second() != 0 || request.To.Nanosecond() != 0 {
			t.Fatalf("request not minute aligned: %s..%s", request.From, request.To)
		}
		if request.To.Sub(request.From) <= 5*time.Minute {
			leaves = append(leaves, request)
		}
	}
	sort.Slice(leaves, func(i, j int) bool { return leaves[i].From.Before(leaves[j].From) })
	if len(leaves) != 4 {
		t.Fatalf("leaf requests = %d, want 4", len(leaves))
	}
	for i, request := range leaves {
		wantFrom := from.Add(time.Duration(i) * 5 * time.Minute)
		if !request.From.Equal(wantFrom) || !request.To.Equal(wantFrom.Add(5*time.Minute)) {
			t.Fatalf("leaf %d = %s..%s, want %s..%s", i, request.From, request.To, wantFrom, wantFrom.Add(5*time.Minute))
		}
	}
	values := map[string]float64{}
	for _, metric := range out.Metrics {
		values[metric.Name] = metric.Value
	}
	if values[semconv.MetricLogpushUploads] != 4 || values[semconv.MetricLogpushRecords] != 40 {
		t.Fatalf("split metric values = %v", values)
	}
}

func TestLogpushHealthFiveMinuteSaturationFailsWithoutAdvancing(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute)
	api := &logpushTestAPI{
		settings: logpushTestSettings(),
		query: func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
			return nil, fmt.Errorf("GraphQL dataset %s window saturated limit %d", request.Dataset, request.Limit)
		},
	}
	out := &telemetry.Buffer{}
	mark, err := NewHealthMetrics(logpushTestConfig(), api).CollectWindow(context.Background(), from, to, out)
	if err == nil || !strings.Contains(err.Error(), "five-minute") || !mark.Equal(from) || len(out.Metrics) != 0 || len(api.requests) != 1 {
		t.Fatalf("irreducible saturation mark=%s err=%v requests=%d metrics=%d", mark, err, len(api.requests), len(out.Metrics))
	}
}

func TestLogpushHealthAdjacentWindowsHaveNoOverlap(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	api := &logpushTestAPI{
		settings: logpushTestSettings(),
		query: func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
			return []map[string]any{logpushTestRow(request.From, 1, 10)}, nil
		},
	}
	collector := NewHealthMetrics(logpushTestConfig(), api)
	for _, window := range [][2]time.Time{{from, from.Add(5 * time.Minute)}, {from.Add(5 * time.Minute), from.Add(10 * time.Minute)}} {
		out := &telemetry.Buffer{}
		mark, err := collector.CollectWindow(context.Background(), window[0], window[1], out)
		if err != nil || !mark.Equal(window[1]) || len(out.Metrics) != 2 {
			t.Fatalf("window %s..%s mark=%s err=%v metrics=%+v", window[0], window[1], mark, err, out.Metrics)
		}
	}
	if len(api.requests) != 2 || !api.requests[0].To.Equal(api.requests[1].From) || !api.requests[0].From.Equal(from) || !api.requests[1].To.Equal(from.Add(10*time.Minute)) {
		t.Fatalf("adjacent half-open requests = %+v", api.requests)
	}
}

func TestLogpushHealthRegisterUsesConfiguredWindowWhenEnabled(t *testing.T) {
	cfg := logpushTestConfig()
	cfg.Collectors["logpush.health"] = config.CollectorConfig{Enabled: true, Interval: time.Minute, InitialLookback: 15 * time.Minute, MaxWindow: 30 * time.Minute}
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: cfg, API: &logpushTestAPI{}, Registry: registry})
	entries := registry.Entries()
	if len(entries) != 1 || entries[0].Collector.Name() != "logpush.health" || entries[0].Interval != time.Minute || entries[0].InitialLookback != 15*time.Minute || entries[0].MaxWindow != 30*time.Minute {
		t.Fatalf("registered entries = %+v", entries)
	}
}

func equalLogpushStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
