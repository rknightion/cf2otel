package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

const workersTestDataset = "workersOverviewRequestsAdaptiveGroups"

type workersTestAPI struct {
	settings    cfapi.DatasetSettings
	settingsErr error
	query       func(cfapi.GraphQLRequest) ([]map[string]any, error)
	requests    []cfapi.GraphQLRequest
}

func (*workersTestAPI) Get(context.Context, string, url.Values, any) error {
	return errors.New("unexpected REST request")
}
func (f *workersTestAPI) Query(_ context.Context, request cfapi.GraphQLRequest, out any) error {
	f.requests = append(f.requests, request)
	if f.query == nil {
		return errors.New("unexpected GraphQL request")
	}
	rows, err := f.query(request)
	if err != nil {
		return err
	}
	data, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
func (f *workersTestAPI) DatasetSettings(_ context.Context, scope cfapi.Scope, _ string, dataset string) (cfapi.DatasetSettings, error) {
	if scope != cfapi.AccountScope || dataset != workersTestDataset {
		return cfapi.DatasetSettings{}, errors.New("unexpected dataset settings request")
	}
	return f.settings, f.settingsErr
}
func (*workersTestAPI) Accounts(context.Context) ([]cfapi.Account, error) {
	return nil, errors.New("unexpected account discovery")
}
func (*workersTestAPI) Zones(context.Context) ([]cfapi.Zone, error) {
	return nil, errors.New("unexpected zone discovery")
}
func (*workersTestAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) {
	return nil, errors.New("unexpected gateway discovery")
}

func workersTestSettings() cfapi.DatasetSettings {
	return cfapi.DatasetSettings{
		Enabled:           true,
		AvailableFields:   []string{"count", "dimensions_datetimeFiveMinutes", "dimensions_scriptName"},
		MaxNumberOfFields: 3,
		MaxDuration:       3600,
		NotOlderThan:      7 * 24 * 60 * 60,
		MaxPageSize:       50,
	}
}

func workersTestConfig() *config.Config {
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "synthetic-account"
	return &cfg
}

func workersTestRow(bucket time.Time, count any, scriptName any) map[string]any {
	dimensions := map[string]any{"datetimeFiveMinutes": bucket.UTC().Format(time.RFC3339)}
	if scriptName != nil {
		dimensions["scriptName"] = scriptName
	}
	return map[string]any{"count": count, "dimensions": dimensions}
}

func TestNoCompleteBucketDoesNotAdvance(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 2, 0, 0, time.UTC)
	to := time.Date(2026, 9, 24, 10, 5, 0, 0, time.UTC)
	api := &workersTestAPI{settings: workersTestSettings()}
	worker := NewOverviewMetrics(workersTestConfig(), api)
	mark, err := worker.CollectWindow(context.Background(), from, to, &telemetry.Buffer{})
	if err == nil || !mark.Equal(from) || len(api.requests) != 0 {
		t.Fatalf("empty complete-bucket window advanced or queried: mark=%s err=%v requests=%d", mark, err, len(api.requests))
	}
}

func TestWorkersOverviewUsesSourceFieldsAndOnlyBoundedScriptAttribute(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(10 * time.Minute)
	rows := []map[string]any{
		workersTestRow(from, 2, "worker-a"),
		workersTestRow(from.Add(5*time.Minute), 3, "worker-a"),
		workersTestRow(from, 4, strings.Repeat("x", 129)),
		workersTestRow(from, 6, "   "),
	}
	rows[0]["email"] = "private@example.com"
	rows[0]["dimensions"].(map[string]any)["path"] = "/private/path"
	api := &workersTestAPI{
		settings: workersTestSettings(),
		query:    func(cfapi.GraphQLRequest) ([]map[string]any, error) { return rows, nil },
	}
	out := &telemetry.Buffer{}
	mark, err := NewOverviewMetrics(workersTestConfig(), api).CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) {
		t.Fatalf("mark = %s, want %s", mark, to)
	}
	if len(api.requests) != 1 {
		t.Fatalf("queries = %d, want 1", len(api.requests))
	}
	request := api.requests[0]
	if request.Scope != cfapi.AccountScope || request.ScopeID != "synthetic-account" || request.Dataset != workersTestDataset {
		t.Fatalf("unexpected request scope or dataset: %+v", request)
	}
	if got, want := request.WantedFields, []string{"count", "dimensions.datetimeFiveMinutes", "dimensions.scriptName"}; !sameWorkersStrings(got, want) {
		t.Fatalf("wanted fields = %v, want %v", got, want)
	}
	if request.Limit != 50 || !request.From.Equal(from) || !request.To.Equal(to) {
		t.Fatalf("request limits/window = limit %d %s..%s", request.Limit, request.From, request.To)
	}
	if len(out.Metrics) != 2 {
		t.Fatalf("metrics = %d, want named and unlabeled series", len(out.Metrics))
	}
	values := map[string]float64{}
	for _, metric := range out.Metrics {
		if metric.Kind != "counter" || metric.Name != semconv.MetricWorkersRequests {
			t.Fatalf("unexpected metric: %+v", metric)
		}
		if len(metric.Attrs) == 0 {
			values[""] = metric.Value
			continue
		}
		if len(metric.Attrs) != 1 || metric.Attrs[0].Key != semconv.AttrWorkersScriptName {
			t.Fatalf("unexpected metric attributes: %+v", metric.Attrs)
		}
		values[metric.Attrs[0].Value] = metric.Value
	}
	if values["worker-a"] != 5 || values[""] != 10 {
		t.Fatalf("series values = %v, want worker-a=5 and unlabeled=10", values)
	}
}

func TestWorkersOverviewFallsBackWhenOptionalNameIsNotAdvertised(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute)
	settings := workersTestSettings()
	settings.AvailableFields = []string{"count", "dimensions_datetimeFiveMinutes"}
	settings.MaxNumberOfFields = 2
	api := &workersTestAPI{
		settings: settings,
		query: func(cfapi.GraphQLRequest) ([]map[string]any, error) {
			return []map[string]any{workersTestRow(from, 7, nil)}, nil
		},
	}
	out := &telemetry.Buffer{}
	mark, err := NewOverviewMetrics(workersTestConfig(), api).CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) || len(api.requests) != 1 || !sameWorkersStrings(api.requests[0].WantedFields, []string{"count", "dimensions.datetimeFiveMinutes"}) {
		t.Fatalf("optional-field fallback request = %+v, mark = %s", api.requests, mark)
	}
	if len(out.Metrics) != 1 || out.Metrics[0].Value != 7 || len(out.Metrics[0].Attrs) != 0 {
		t.Fatalf("fallback metrics = %+v, want one unlabeled count of 7", out.Metrics)
	}
}

func TestWorkersOverviewMarksOnlyCompleteBucketsAtUnalignedEdges(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 2, 0, 0, time.UTC)
	to := from.Add(10 * time.Minute)
	api := &workersTestAPI{
		settings: workersTestSettings(),
		query: func(cfapi.GraphQLRequest) ([]map[string]any, error) {
			return []map[string]any{
				workersTestRow(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 1, "worker-a"),
				workersTestRow(time.Date(2026, 9, 24, 10, 5, 0, 0, time.UTC), 2, "worker-a"),
				workersTestRow(time.Date(2026, 9, 24, 10, 10, 0, 0, time.UTC), 4, "worker-a"),
			}, nil
		},
	}
	out := &telemetry.Buffer{}
	mark, err := NewOverviewMetrics(workersTestConfig(), api).CollectWindow(context.Background(), from, to, out)
	wantMark := time.Date(2026, 9, 24, 10, 10, 0, 0, time.UTC)
	if err != nil || !mark.Equal(wantMark) || len(out.Metrics) != 1 || out.Metrics[0].Value != 2 {
		t.Fatalf("unaligned edge result mark=%s err=%v metrics=%+v", mark, err, out.Metrics)
	}
}

func TestWorkersOverviewRequiresEveryRequiredSourceField(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute)
	settings := workersTestSettings()
	settings.AvailableFields = []string{"dimensions_datetimeFiveMinutes", "dimensions_scriptName"}
	api := &workersTestAPI{settings: settings, query: func(cfapi.GraphQLRequest) ([]map[string]any, error) { return nil, nil }}
	out := &telemetry.Buffer{}
	mark, err := NewOverviewMetrics(workersTestConfig(), api).CollectWindow(context.Background(), from, to, out)
	if err == nil || !mark.Equal(from) || len(out.Metrics) != 0 {
		t.Fatalf("missing required count field returned mark=%s err=%v metrics=%d", mark, err, len(out.Metrics))
	}
}

func TestWorkersOverviewSeriesCapDisclosesDeterministicDropCount(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute)
	cfg := workersTestConfig()
	cfg.Platform.MaxMetricSeriesPerWindow = 2
	api := &workersTestAPI{
		settings: workersTestSettings(),
		query: func(cfapi.GraphQLRequest) ([]map[string]any, error) {
			return []map[string]any{
				workersTestRow(from, 1, "zeta"),
				workersTestRow(from, 1, "beta"),
				workersTestRow(from, 1, "alpha"),
			}, nil
		},
	}
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	out := &telemetry.Buffer{}
	mark, err := NewOverviewMetrics(cfg, api).CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) || len(out.Metrics) != 2 {
		t.Fatalf("cap result mark=%s metrics=%d", mark, len(out.Metrics))
	}
	names := make([]string, 0, len(out.Metrics))
	for _, metric := range out.Metrics {
		if len(metric.Attrs) != 1 || metric.Attrs[0].Key != semconv.AttrWorkersScriptName {
			t.Fatalf("unexpected capped attributes: %+v", metric.Attrs)
		}
		names = append(names, metric.Attrs[0].Value)
	}
	sort.Strings(names)
	if !sameWorkersStrings(names, []string{"alpha", "beta"}) {
		t.Fatalf("retained names = %v, want stable first two series", names)
	}
	if got := logs.String(); strings.Count(got, "platform metric series dropped") != 1 || !strings.Contains(got, "dropped_series=1") || strings.Contains(got, "alpha") || strings.Contains(got, "beta") || strings.Contains(got, "zeta") {
		t.Fatalf("drop disclosure = %q, want one count-only warning without resource values", got)
	}
}

func TestWorkersOverviewSplitsSaturatedQueriesOnMinuteBoundaries(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(20 * time.Minute)
	api := &workersTestAPI{
		settings: workersTestSettings(),
		query: func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
			if request.To.Sub(request.From) > 5*time.Minute {
				return nil, fmt.Errorf("GraphQL dataset %s window saturated limit %d", request.Dataset, request.Limit)
			}
			var rows []map[string]any
			for bucket := request.From; bucket.Before(request.To); bucket = bucket.Add(5 * time.Minute) {
				rows = append(rows, workersTestRow(bucket, 1, "worker-a"))
			}
			return rows, nil
		},
	}
	out := &telemetry.Buffer{}
	mark, err := NewOverviewMetrics(workersTestConfig(), api).CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) || len(api.requests) != 7 {
		t.Fatalf("split result mark=%s queries=%d, want mark %s and 7 requests", mark, len(api.requests), to)
	}
	leaves := make([]cfapi.GraphQLRequest, 0, 4)
	for _, request := range api.requests {
		if request.From.Second() != 0 || request.From.Nanosecond() != 0 || request.To.Second() != 0 || request.To.Nanosecond() != 0 {
			t.Fatalf("split request is not minute aligned: %s..%s", request.From, request.To)
		}
		if request.To.Sub(request.From) <= 5*time.Minute {
			leaves = append(leaves, request)
		}
	}
	sort.Slice(leaves, func(i, j int) bool { return leaves[i].From.Before(leaves[j].From) })
	if len(leaves) != 4 {
		t.Fatalf("leaf query count = %d, want 4", len(leaves))
	}
	for i, request := range leaves {
		wantFrom := from.Add(time.Duration(i) * 5 * time.Minute)
		if !request.From.Equal(wantFrom) || !request.To.Equal(wantFrom.Add(5*time.Minute)) {
			t.Fatalf("leaf %d = %s..%s, want adjacent half-open window %s..%s", i, request.From, request.To, wantFrom, wantFrom.Add(5*time.Minute))
		}
	}
	if len(out.Metrics) != 1 || out.Metrics[0].Value != 4 {
		t.Fatalf("split metrics = %+v, want one counter of 4", out.Metrics)
	}
}

func TestWorkersOverviewFifteenMinuteSplitKeepsWholeBuckets(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(15 * time.Minute)
	api := &workersTestAPI{
		settings: workersTestSettings(),
		query: func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
			if !request.From.Equal(request.From.Truncate(5*time.Minute)) || !request.To.Equal(request.To.Truncate(5*time.Minute)) {
				return nil, errors.New("query split cut a five-minute bucket")
			}
			if request.To.Sub(request.From) > 5*time.Minute {
				return nil, fmt.Errorf("GraphQL dataset %s window saturated limit %d", request.Dataset, request.Limit)
			}
			return []map[string]any{workersTestRow(request.From, 1, "worker-a")}, nil
		},
	}
	out := &telemetry.Buffer{}
	mark, err := NewOverviewMetrics(workersTestConfig(), api).CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) || len(out.Metrics) != 1 || out.Metrics[0].Value != 3 {
		t.Fatalf("15-minute split: mark=%s metrics=%#v, want three complete buckets", mark, out.Metrics)
	}
}

func TestWorkersOverviewFailsOnIrreducibleSaturationWithoutPartialMetrics(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute)
	api := &workersTestAPI{
		settings: workersTestSettings(),
		query: func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
			return nil, fmt.Errorf("GraphQL dataset %s window saturated limit %d", request.Dataset, request.Limit)
		},
	}
	out := &telemetry.Buffer{}
	mark, err := NewOverviewMetrics(workersTestConfig(), api).CollectWindow(context.Background(), from, to, out)
	if err == nil || !strings.Contains(err.Error(), "five-minute") || !mark.Equal(from) || len(out.Metrics) != 0 {
		t.Fatalf("irreducible saturation returned mark=%s err=%v metrics=%d", mark, err, len(out.Metrics))
	}
	if len(api.requests) != 1 {
		t.Fatalf("queries = %d, want one irreducible five-minute query", len(api.requests))
	}
}

func TestWorkersOverviewPassesAdjacentHalfOpenWindowsWithoutOverlap(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute)
	api := &workersTestAPI{
		settings: workersTestSettings(),
		query: func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
			return []map[string]any{workersTestRow(request.From, 1, "worker-a")}, nil
		},
	}
	collector := NewOverviewMetrics(workersTestConfig(), api)
	for _, window := range [][2]time.Time{{from, to}, {to, to.Add(5 * time.Minute)}} {
		out := &telemetry.Buffer{}
		mark, err := collector.CollectWindow(context.Background(), window[0], window[1], out)
		if err != nil || !mark.Equal(window[1]) || len(out.Metrics) != 1 || out.Metrics[0].Value != 1 {
			t.Fatalf("window %s..%s returned mark=%s err=%v metrics=%+v", window[0], window[1], mark, err, out.Metrics)
		}
	}
	if len(api.requests) != 2 || !api.requests[0].From.Equal(from) || !api.requests[0].To.Equal(to) || !api.requests[1].From.Equal(to) || !api.requests[1].To.Equal(to.Add(5*time.Minute)) {
		t.Fatalf("adjacent requests = %+v", api.requests)
	}
}

func TestWorkersOverviewUsesLiveMaxDurationAndPageSize(t *testing.T) {
	from := time.Now().UTC().Truncate(5 * time.Minute).Add(-20 * time.Minute)
	to := from.Add(10 * time.Minute)
	var dataQueries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var request struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(request.Query, "settings{") {
			_, _ = w.Write([]byte(`{"data":{"viewer":{"accounts":[{"settings":{"workersOverviewRequestsAdaptiveGroups":{"enabled":true,"availableFields":["count","dimensions_datetimeFiveMinutes","dimensions_scriptName"],"maxNumberOfFields":3,"maxDuration":300,"notOlderThan":3600,"maxPageSize":2}}}]}}}`))
			return
		}
		dataQueries = append(dataQueries, request.Query)
		_, _ = w.Write([]byte(`{"data":{"viewer":{"accounts":[{"workersOverviewRequestsAdaptiveGroups":[]}]}}}`))
	}))
	defer server.Close()
	cfg := workersTestConfig()
	cfg.Cloudflare.APIBase = server.URL
	api := cfapi.New(cfg.Cloudflare)
	mark, err := NewOverviewMetrics(cfg, api).CollectWindow(context.Background(), from, to, &telemetry.Buffer{})
	if err != nil || !mark.Equal(to) {
		t.Fatalf("bounded GraphQL collection returned mark=%s err=%v", mark, err)
	}
	if len(dataQueries) != 2 {
		t.Fatalf("data queries = %d, want two live maxDuration windows", len(dataQueries))
	}
	for _, query := range dataQueries {
		if !strings.Contains(query, "limit:2") {
			t.Errorf("query ignored maxPageSize: %s", query)
		}
	}
}

func TestWorkersRegisterUsesConfiguredWindowWhenEnabled(t *testing.T) {
	cfg := workersTestConfig()
	cfg.Collectors["workers.overview"] = config.CollectorConfig{Enabled: true, Interval: time.Minute, InitialLookback: 15 * time.Minute, MaxWindow: 30 * time.Minute}
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: cfg, API: &workersTestAPI{}, Registry: registry})
	entries := registry.Entries()
	if len(entries) != 1 || entries[0].Collector.Name() != "workers.overview" || entries[0].Interval != time.Minute || entries[0].InitialLookback != 15*time.Minute || entries[0].MaxWindow != 30*time.Minute {
		t.Fatalf("registered entries = %+v", entries)
	}
}

func sameWorkersStrings(got, want []string) bool {
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
