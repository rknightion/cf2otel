package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

const turnstileTestDataset = "turnstileAdaptiveGroups"

type turnstileTestAPI struct {
	settings cfapi.DatasetSettings
	query    func(cfapi.GraphQLRequest) ([]map[string]any, error)
	requests []cfapi.GraphQLRequest
}

func (*turnstileTestAPI) Get(context.Context, string, url.Values, any) error {
	return errors.New("unexpected REST request")
}
func (f *turnstileTestAPI) Query(_ context.Context, request cfapi.GraphQLRequest, out any) error {
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
func (f *turnstileTestAPI) DatasetSettings(_ context.Context, scope cfapi.Scope, _ string, dataset string) (cfapi.DatasetSettings, error) {
	if scope != cfapi.AccountScope || dataset != turnstileTestDataset {
		return cfapi.DatasetSettings{}, errors.New("unexpected dataset settings request")
	}
	return f.settings, nil
}
func (*turnstileTestAPI) Accounts(context.Context) ([]cfapi.Account, error) {
	return nil, errors.New("unexpected account discovery")
}
func (*turnstileTestAPI) Zones(context.Context) ([]cfapi.Zone, error) {
	return nil, errors.New("unexpected zone discovery")
}
func (*turnstileTestAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) {
	return nil, errors.New("unexpected gateway discovery")
}

func turnstileTestSettings() cfapi.DatasetSettings {
	return cfapi.DatasetSettings{
		Enabled:           true,
		AvailableFields:   []string{"count", "dimensions_datetimeFiveMinutes", "dimensions_sitekey", "dimensions_success"},
		MaxNumberOfFields: 4,
		MaxDuration:       3600,
		NotOlderThan:      7 * 24 * 60 * 60,
		MaxPageSize:       20,
	}
}
func turnstileTestConfig() *config.Config {
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "synthetic-account"
	return &cfg
}
func turnstileTestRow(bucket time.Time, count any) map[string]any {
	return map[string]any{
		"count":      count,
		"dimensions": map[string]any{"datetimeFiveMinutes": bucket.UTC().Format(time.RFC3339), "sitekey": "synthetic-site-key", "email": "private@example.com"},
	}
}

func TestTurnstileUsesGroupsCountAndOnlyTimeField(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(10 * time.Minute)
	api := &turnstileTestAPI{
		settings: turnstileTestSettings(),
		query: func(cfapi.GraphQLRequest) ([]map[string]any, error) {
			return []map[string]any{turnstileTestRow(from, 5), turnstileTestRow(from.Add(5*time.Minute), 7)}, nil
		},
	}
	out := &telemetry.Buffer{}
	mark, err := NewEventMetrics(turnstileTestConfig(), api).CollectWindow(context.Background(), from, to, out)
	if err != nil || !mark.Equal(to) {
		t.Fatalf("collection returned mark=%s err=%v", mark, err)
	}
	if len(api.requests) != 1 {
		t.Fatalf("query count = %d, want 1", len(api.requests))
	}
	request := api.requests[0]
	if request.Scope != cfapi.AccountScope || request.ScopeID != "synthetic-account" || request.Dataset != turnstileTestDataset {
		t.Fatalf("request = %+v", request)
	}
	if !equalTurnstileStrings(request.WantedFields, []string{"count", "dimensions.datetimeFiveMinutes"}) {
		t.Fatalf("selected fields = %v, want count and datetimeFiveMinutes only", request.WantedFields)
	}
	if request.Limit != 20 || !request.From.Equal(from) || !request.To.Equal(to) {
		t.Fatalf("request limit/window = %d %s..%s", request.Limit, request.From, request.To)
	}
	if len(out.Metrics) != 1 || out.Metrics[0].Kind != "counter" || out.Metrics[0].Name != semconv.MetricTurnstileEvents || out.Metrics[0].Value != 12 || len(out.Metrics[0].Attrs) != 0 {
		t.Fatalf("metrics = %+v, want one unlabeled Turnstile counter of 12", out.Metrics)
	}
}

func TestTurnstileUsesOnlyCompleteFiveMinuteBuckets(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 2, 0, 0, time.UTC)
	to := from.Add(10 * time.Minute)
	api := &turnstileTestAPI{
		settings: turnstileTestSettings(),
		query: func(cfapi.GraphQLRequest) ([]map[string]any, error) {
			bucket := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
			return []map[string]any{turnstileTestRow(bucket, 1), turnstileTestRow(bucket.Add(5*time.Minute), 2), turnstileTestRow(bucket.Add(10*time.Minute), 4)}, nil
		},
	}
	out := &telemetry.Buffer{}
	mark, err := NewEventMetrics(turnstileTestConfig(), api).CollectWindow(context.Background(), from, to, out)
	wantMark := time.Date(2026, 9, 24, 10, 10, 0, 0, time.UTC)
	if err != nil || !mark.Equal(wantMark) || len(out.Metrics) != 1 || out.Metrics[0].Value != 2 {
		t.Fatalf("incomplete bucket result mark=%s err=%v metrics=%+v", mark, err, out.Metrics)
	}
}

func TestTurnstileFailsWhenRequiredSourceFieldIsNotAdvertised(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute)
	settings := turnstileTestSettings()
	settings.AvailableFields = []string{"dimensions_datetimeFiveMinutes", "dimensions_sitekey"}
	api := &turnstileTestAPI{settings: settings, query: func(cfapi.GraphQLRequest) ([]map[string]any, error) { return nil, nil }}
	out := &telemetry.Buffer{}
	mark, err := NewEventMetrics(turnstileTestConfig(), api).CollectWindow(context.Background(), from, to, out)
	if err == nil || !mark.Equal(from) || len(out.Metrics) != 0 {
		t.Fatalf("missing count returned mark=%s err=%v metrics=%d", mark, err, len(out.Metrics))
	}
}

func TestTurnstileSplitsSaturatedQueriesAndPreservesAdjacentWindows(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(20 * time.Minute)
	api := &turnstileTestAPI{
		settings: turnstileTestSettings(),
		query: func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
			if request.To.Sub(request.From) > 5*time.Minute {
				return nil, fmt.Errorf("GraphQL dataset %s window saturated limit %d", request.Dataset, request.Limit)
			}
			return []map[string]any{turnstileTestRow(request.From, 1)}, nil
		},
	}
	out := &telemetry.Buffer{}
	mark, err := NewEventMetrics(turnstileTestConfig(), api).CollectWindow(context.Background(), from, to, out)
	if err != nil || !mark.Equal(to) || len(api.requests) != 7 || len(out.Metrics) != 1 || out.Metrics[0].Value != 4 {
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

	adjacent := &turnstileTestAPI{
		settings: turnstileTestSettings(),
		query: func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
			return []map[string]any{turnstileTestRow(request.From, 1)}, nil
		},
	}
	collector := NewEventMetrics(turnstileTestConfig(), adjacent)
	for _, window := range [][2]time.Time{{from, from.Add(5 * time.Minute)}, {from.Add(5 * time.Minute), from.Add(10 * time.Minute)}} {
		buffer := &telemetry.Buffer{}
		mark, err := collector.CollectWindow(context.Background(), window[0], window[1], buffer)
		if err != nil || !mark.Equal(window[1]) || len(buffer.Metrics) != 1 || buffer.Metrics[0].Value != 1 {
			t.Fatalf("adjacent window %s..%s mark=%s err=%v metrics=%+v", window[0], window[1], mark, err, buffer.Metrics)
		}
	}
	if len(adjacent.requests) != 2 || !adjacent.requests[0].To.Equal(adjacent.requests[1].From) {
		t.Fatalf("adjacent half-open requests = %+v", adjacent.requests)
	}
}

func TestTurnstileFiveMinuteSaturationFailsWithoutAdvancing(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute)
	api := &turnstileTestAPI{
		settings: turnstileTestSettings(),
		query: func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
			return nil, fmt.Errorf("GraphQL dataset %s window saturated limit %d", request.Dataset, request.Limit)
		},
	}
	out := &telemetry.Buffer{}
	mark, err := NewEventMetrics(turnstileTestConfig(), api).CollectWindow(context.Background(), from, to, out)
	if err == nil || !strings.Contains(err.Error(), "five-minute") || !mark.Equal(from) || len(out.Metrics) != 0 || len(api.requests) != 1 {
		t.Fatalf("irreducible saturation mark=%s err=%v requests=%d metrics=%d", mark, err, len(api.requests), len(out.Metrics))
	}
}

func TestTurnstileRegisterUsesConfiguredWindowWhenEnabled(t *testing.T) {
	cfg := turnstileTestConfig()
	cfg.Collectors["turnstile.events"] = config.CollectorConfig{Enabled: true, Interval: time.Minute, InitialLookback: 15 * time.Minute, MaxWindow: 30 * time.Minute}
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: cfg, API: &turnstileTestAPI{}, Registry: registry})
	entries := registry.Entries()
	if len(entries) != 1 || entries[0].Collector.Name() != "turnstile.events" || entries[0].Interval != time.Minute || entries[0].InitialLookback != 15*time.Minute || entries[0].MaxWindow != 30*time.Minute {
		t.Fatalf("registered entries = %+v", entries)
	}
}

func equalTurnstileStrings(got, want []string) bool {
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
