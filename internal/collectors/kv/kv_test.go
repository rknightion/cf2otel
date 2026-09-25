package kv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

const fixtureAccountID = "account-fixture"

var fixtureFrom = time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)

type fakeAPI struct {
	settings      map[string]cfapi.DatasetSettings
	rows          map[string][]map[string]any
	requests      []cfapi.GraphQLRequest
	settingsCalls []cfapi.GraphQLRequest
	queryFn       func(cfapi.GraphQLRequest) ([]map[string]any, error)
}

func (f *fakeAPI) Get(context.Context, string, url.Values, any) error {
	return errors.New("unexpected REST request")
}
func (f *fakeAPI) Query(_ context.Context, request cfapi.GraphQLRequest, out any) error {
	f.requests = append(f.requests, request)
	rows := f.rows[request.Dataset]
	var err error
	if f.queryFn != nil {
		rows, err = f.queryFn(request)
		if err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, out)
}
func (f *fakeAPI) DatasetSettings(_ context.Context, scope cfapi.Scope, scopeID, dataset string) (cfapi.DatasetSettings, error) {
	f.settingsCalls = append(f.settingsCalls, cfapi.GraphQLRequest{Scope: scope, ScopeID: scopeID, Dataset: dataset})
	if scope != cfapi.AccountScope || scopeID != fixtureAccountID {
		return cfapi.DatasetSettings{}, fmt.Errorf("unexpected settings scope %q %q", scope, scopeID)
	}
	settings, ok := f.settings[dataset]
	if !ok {
		return cfapi.DatasetSettings{}, fmt.Errorf("unexpected dataset settings request %q", dataset)
	}
	return settings, nil
}
func (*fakeAPI) Accounts(context.Context) ([]cfapi.Account, error) {
	return nil, errors.New("unexpected account discovery")
}
func (*fakeAPI) Zones(context.Context) ([]cfapi.Zone, error) {
	return nil, errors.New("unexpected zone discovery")
}
func (*fakeAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) {
	return nil, errors.New("unexpected gateway discovery")
}

func testSettings(fields ...string) cfapi.DatasetSettings {
	return cfapi.DatasetSettings{
		Enabled:           true,
		AvailableFields:   fields,
		MaxNumberOfFields: 20,
		MaxDuration:       3600,
		NotOlderThan:      31 * 24 * 60 * 60,
		MaxPageSize:       10000,
	}
}
func kvSettings() map[string]cfapi.DatasetSettings {
	return map[string]cfapi.DatasetSettings{
		"kvOperationsAdaptiveGroups": testSettings("sum_requests", "dimensions_datetimeFiveMinutes", "dimensions_fixtureOnly"),
		"kvStorageAdaptiveGroups":    testSettings("max_byteCount", "max_keyCount", "dimensions_datetimeFiveMinutes", "dimensions_fixtureOnly"),
	}
}
func testConfig() *config.Config {
	cfg := config.Default()
	cfg.Cloudflare.AccountID = fixtureAccountID
	return &cfg
}
func registered(t *testing.T, api *fakeAPI, cfg *config.Config, name string) collector.WindowCollector {
	t.Helper()
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: cfg, API: api, Registry: registry})
	for _, entry := range registry.Entries() {
		if entry.Collector.Name() == name {
			window, ok := entry.Collector.(collector.WindowCollector)
			if !ok {
				t.Fatalf("collector %q is not windowed", name)
			}
			return window
		}
	}
	t.Fatalf("collector %q was not registered", name)
	return nil
}
func metricRows(metrics []telemetry.BufferedMetric) map[string]telemetry.BufferedMetric {
	out := make(map[string]telemetry.BufferedMetric, len(metrics))
	for _, metric := range metrics {
		out[metric.Name] = metric
	}
	return out
}

func TestRegisterInstallsEnabledKVDatasets(t *testing.T) {
	api := &fakeAPI{settings: kvSettings()}
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: testConfig(), API: api, Registry: registry})
	entries := registry.Entries()
	if len(entries) != 2 {
		t.Fatalf("registered %d collectors, want two", len(entries))
	}
	want := map[string]bool{"kv.operations": true, "kv.storage": true}
	for _, entry := range entries {
		if !want[entry.Collector.Name()] {
			t.Errorf("unexpected collector %q", entry.Collector.Name())
		}
		delete(want, entry.Collector.Name())
		if entry.Interval != 5*time.Minute || entry.InitialLookback != 30*time.Minute || entry.MaxWindow != time.Hour {
			t.Errorf("entry %q has unexpected schedule: %+v", entry.Collector.Name(), entry)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing collectors: %v", want)
	}
}

func TestOperationsUsesAccountOnlyRequiredFieldsAndSumsGroups(t *testing.T) {
	settings := kvSettings()
	operationSettings := settings["kvOperationsAdaptiveGroups"]
	operationSettings.MaxPageSize = 37
	settings["kvOperationsAdaptiveGroups"] = operationSettings
	api := &fakeAPI{
		settings: settings,
		rows: map[string][]map[string]any{
			"kvOperationsAdaptiveGroups": {
				{"sum": map[string]any{"requests": 3}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Format(time.RFC3339), "resourceFixture": "synthetic-namespace-1"}},
				{"sum": map[string]any{"requests": 5}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Add(5 * time.Minute).Format(time.RFC3339), "resourceFixture": "synthetic-namespace-2"}},
			},
		},
	}
	collector := registered(t, api, testConfig(), "kv.operations")
	out := &telemetry.Buffer{}
	to := fixtureFrom.Add(10 * time.Minute)
	mark, err := collector.CollectWindow(context.Background(), fixtureFrom, to, out)
	if err != nil || !mark.Equal(to) {
		t.Fatalf("mark=%s error=%v", mark, err)
	}
	if len(api.requests) != 1 {
		t.Fatalf("query count = %d, want one", len(api.requests))
	}
	request := api.requests[0]
	if request.Scope != cfapi.AccountScope || request.ScopeID != fixtureAccountID || request.Dataset != "kvOperationsAdaptiveGroups" {
		t.Fatalf("request did not use configured account and KV dataset: %+v", request)
	}
	if !request.From.Equal(fixtureFrom) || !request.To.Equal(to) || request.Limit != 37 {
		t.Fatalf("request window/limit = %s..%s/%d", request.From, request.To, request.Limit)
	}
	wantFields := []string{"sum.requests", "dimensions.datetimeFiveMinutes"}
	if fmt.Sprint(request.WantedFields) != fmt.Sprint(wantFields) {
		t.Fatalf("wanted fields = %v, want %v", request.WantedFields, wantFields)
	}
	if len(out.Metrics) != 1 || out.Metrics[0].Kind != "counter" || out.Metrics[0].Name != semconv.MetricKVRequests || out.Metrics[0].Value != 8 || len(out.Metrics[0].Attrs) != 0 {
		t.Fatalf("account requests metric = %+v, want count 8 and no attributes", out.Metrics)
	}
}

func TestStorageUsesLatestCompleteBucketMaximumAcrossNamespaces(t *testing.T) {
	api := &fakeAPI{
		settings: kvSettings(),
		rows: map[string][]map[string]any{
			"kvStorageAdaptiveGroups": {
				{"max": map[string]any{"byteCount": 100, "keyCount": 10}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Format(time.RFC3339), "resourceFixture": "synthetic-namespace-a"}},
				{"max": map[string]any{"byteCount": 250, "keyCount": 25}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Format(time.RFC3339), "resourceFixture": "synthetic-namespace-b"}},
				{"max": map[string]any{"byteCount": 9, "keyCount": 2}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Add(5 * time.Minute).Format(time.RFC3339), "resourceFixture": "synthetic-namespace-a"}},
				{"max": map[string]any{"byteCount": 17, "keyCount": 3}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Add(5 * time.Minute).Format(time.RFC3339), "resourceFixture": "synthetic-namespace-b"}},
				{"max": map[string]any{"byteCount": 999, "keyCount": 99}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Add(10 * time.Minute).Format(time.RFC3339), "resourceFixture": "synthetic-namespace-c"}},
			},
		},
	}
	collector := registered(t, api, testConfig(), "kv.storage")
	out := &telemetry.Buffer{}
	to := fixtureFrom.Add(10 * time.Minute)
	mark, err := collector.CollectWindow(context.Background(), fixtureFrom, to, out)
	if err != nil || !mark.Equal(to) {
		t.Fatalf("mark=%s error=%v", mark, err)
	}
	got := metricRows(out.Metrics)
	if len(got) != 2 || got[semconv.MetricKVStorageBytes].Kind != "gauge" || got[semconv.MetricKVStorageBytes].Value != 17 || got[semconv.MetricKVStorageKeys].Kind != "gauge" || got[semconv.MetricKVStorageKeys].Value != 3 {
		t.Fatalf("KV storage gauges = %+v, want latest complete bucket maxima 17 bytes and 3 keys", got)
	}
	for _, metric := range out.Metrics {
		if len(metric.Attrs) != 0 {
			t.Errorf("resource attribute escaped account aggregation: %+v", metric.Attrs)
		}
	}
	if len(api.requests) != 1 || fmt.Sprint(api.requests[0].WantedFields) != fmt.Sprint([]string{"max.byteCount", "max.keyCount", "dimensions.datetimeFiveMinutes"}) {
		t.Fatalf("KV storage selected fields = %+v, want only contracted value and time fields", api.requests)
	}
}

func TestStorageFailsClosedWhenRequiredFieldIsMissing(t *testing.T) {
	settings := kvSettings()
	settings["kvStorageAdaptiveGroups"] = testSettings("max_byteCount", "dimensions_datetimeFiveMinutes")
	api := &fakeAPI{settings: settings}
	collector := registered(t, api, testConfig(), "kv.storage")
	out := &telemetry.Buffer{}
	mark, err := collector.CollectWindow(context.Background(), fixtureFrom, fixtureFrom.Add(5*time.Minute), out)
	if err == nil || !strings.Contains(err.Error(), "max.keyCount") {
		t.Fatalf("error = %v, want missing required max.keyCount", err)
	}
	if !mark.Equal(fixtureFrom) || len(api.requests) != 0 || len(out.Metrics) != 0 {
		t.Fatalf("missing field advanced or emitted: mark=%s requests=%d metrics=%d", mark, len(api.requests), len(out.Metrics))
	}
}

func TestMoreThan500SyntheticNamespacesCollapseToFrozenAccountSeries(t *testing.T) {
	rows := make([]map[string]any, 501)
	for i := range rows {
		rows[i] = map[string]any{
			"sum":        map[string]any{"requests": 1},
			"dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Format(time.RFC3339), "resourceFixture": fmt.Sprintf("fixture-namespace-%03d", i)},
		}
	}
	api := &fakeAPI{settings: kvSettings(), rows: map[string][]map[string]any{"kvOperationsAdaptiveGroups": rows}}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	collector := registered(t, api, testConfig(), "kv.operations")
	out := &telemetry.Buffer{}
	mark, err := collector.CollectWindow(context.Background(), fixtureFrom, fixtureFrom.Add(5*time.Minute), out)
	if err != nil || !mark.Equal(fixtureFrom.Add(5*time.Minute)) {
		t.Fatalf("mark=%s error=%v", mark, err)
	}
	if len(out.Metrics) != 1 || out.Metrics[0].Name != semconv.MetricKVRequests || out.Metrics[0].Value != 501 || len(out.Metrics[0].Attrs) != 0 {
		t.Fatalf("501 resource rows did not collapse to one account series: %+v", out.Metrics)
	}
	if strings.Contains(logs.String(), "dropped_series") {
		t.Fatalf("the 500-series cap is unreachable after account aggregation: %q", logs.String())
	}
}

func TestConfiguredSeriesDropDisclosureContainsOnlyCount(t *testing.T) {
	cfg := testConfig()
	cfg.Platform.MaxMetricSeriesPerWindow = 1
	api := &fakeAPI{
		settings: kvSettings(),
		rows: map[string][]map[string]any{
			"kvStorageAdaptiveGroups": {{
				"max":        map[string]any{"byteCount": 50, "keyCount": 5},
				"dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Format(time.RFC3339), "resourceFixture": "fixture-namespace-secret-value"},
			}},
		},
	}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	collector := registered(t, api, cfg, "kv.storage")
	out := &telemetry.Buffer{}
	if _, err := collector.CollectWindow(context.Background(), fixtureFrom, fixtureFrom.Add(5*time.Minute), out); err != nil {
		t.Fatal(err)
	}
	if len(out.Metrics) != 1 || out.Metrics[0].Name != semconv.MetricKVStorageBytes {
		t.Fatalf("series cap retained %v, want first sorted metric %q", out.Metrics, semconv.MetricKVStorageBytes)
	}
	logText := logs.String()
	if strings.Count(logText, "dropped_series=1") != 1 || strings.Contains(logText, "fixture-namespace-secret-value") {
		t.Fatalf("drop disclosure is missing its count or leaks a resource: %q", logText)
	}
}

func TestAdjacentWindowsOwnFiveMinuteBoundaryOnce(t *testing.T) {
	api := &fakeAPI{
		settings: kvSettings(),
		rows: map[string][]map[string]any{
			"kvOperationsAdaptiveGroups": {
				{"sum": map[string]any{"requests": 1}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Format(time.RFC3339)}},
				{"sum": map[string]any{"requests": 2}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Add(5 * time.Minute).Format(time.RFC3339)}},
				{"sum": map[string]any{"requests": 4}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Add(10 * time.Minute).Format(time.RFC3339)}},
			},
		},
	}
	collector := registered(t, api, testConfig(), "kv.operations")
	first := &telemetry.Buffer{}
	firstTo := fixtureFrom.Add(5 * time.Minute)
	if mark, err := collector.CollectWindow(context.Background(), fixtureFrom, firstTo, first); err != nil || !mark.Equal(firstTo) {
		t.Fatalf("first window mark=%s error=%v", mark, err)
	}
	second := &telemetry.Buffer{}
	secondTo := firstTo.Add(5 * time.Minute)
	if mark, err := collector.CollectWindow(context.Background(), firstTo, secondTo, second); err != nil || !mark.Equal(secondTo) {
		t.Fatalf("second window mark=%s error=%v", mark, err)
	}
	if len(first.Metrics) != 1 || first.Metrics[0].Value != 1 || len(second.Metrics) != 1 || second.Metrics[0].Value != 2 {
		t.Fatalf("adjacent windows emitted first=%+v second=%+v", first.Metrics, second.Metrics)
	}
}

func TestUnalignedWindowsAdvanceOnlyThroughCompleteBuckets(t *testing.T) {
	api := &fakeAPI{
		settings: kvSettings(),
		rows: map[string][]map[string]any{
			"kvOperationsAdaptiveGroups": {
				{"sum": map[string]any{"requests": 1}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Format(time.RFC3339)}},
				{"sum": map[string]any{"requests": 2}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Add(5 * time.Minute).Format(time.RFC3339)}},
				{"sum": map[string]any{"requests": 4}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Add(10 * time.Minute).Format(time.RFC3339)}},
				{"sum": map[string]any{"requests": 8}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Add(15 * time.Minute).Format(time.RFC3339)}},
			},
		},
	}
	collector := registered(t, api, testConfig(), "kv.operations")
	firstFrom := fixtureFrom.Add(30 * time.Second)
	firstTo := fixtureFrom.Add(10*time.Minute + 30*time.Second)
	first := &telemetry.Buffer{}
	mark, err := collector.CollectWindow(context.Background(), firstFrom, firstTo, first)
	if err != nil || !mark.Equal(fixtureFrom.Add(10*time.Minute)) {
		t.Fatalf("first unaligned window mark=%s error=%v, want complete high-water 10:10", mark, err)
	}
	if len(api.requests) != 1 || !api.requests[0].From.Equal(fixtureFrom.Add(5*time.Minute)) || !api.requests[0].To.Equal(mark) {
		t.Fatalf("first query interval = %+v, want [10:05, 10:10)", api.requests)
	}
	if len(first.Metrics) != 1 || first.Metrics[0].Value != 2 {
		t.Fatalf("first unaligned window emitted %+v, want only the complete 10:05 bucket", first.Metrics)
	}

	secondTo := fixtureFrom.Add(15*time.Minute + 30*time.Second)
	second := &telemetry.Buffer{}
	secondMark, err := collector.CollectWindow(context.Background(), mark, secondTo, second)
	if err != nil || !secondMark.Equal(fixtureFrom.Add(15*time.Minute)) {
		t.Fatalf("second unaligned window mark=%s error=%v, want complete high-water 10:15", secondMark, err)
	}
	if len(api.requests) != 2 || !api.requests[1].From.Equal(mark) || !api.requests[1].To.Equal(secondMark) {
		t.Fatalf("second query interval = %+v, want [10:10, 10:15)", api.requests)
	}
	if len(second.Metrics) != 1 || second.Metrics[0].Value != 4 {
		t.Fatalf("second unaligned window emitted %+v, want the 10:10 bucket exactly once", second.Metrics)
	}
}

func TestSaturatedQueryBisectsWithoutGapsOrOverlap(t *testing.T) {
	api := &fakeAPI{settings: kvSettings()}
	api.queryFn = func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
		if request.To.Sub(request.From) > 5*time.Minute {
			return nil, fmt.Errorf("GraphQL dataset %s window %s..%s saturated limit %d", request.Dataset, request.From.Format(time.RFC3339), request.To.Format(time.RFC3339), request.Limit)
		}
		if request.From.Equal(fixtureFrom) {
			return []map[string]any{{"sum": map[string]any{"requests": 7}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Format(time.RFC3339)}}}, nil
		}
		return nil, nil
	}
	collector := registered(t, api, testConfig(), "kv.operations")
	out := &telemetry.Buffer{}
	to := fixtureFrom.Add(15 * time.Minute)
	mark, err := collector.CollectWindow(context.Background(), fixtureFrom, to, out)
	if err != nil || !mark.Equal(to) {
		t.Fatalf("mark=%s error=%v", mark, err)
	}
	var leaves []cfapi.GraphQLRequest
	for _, request := range api.requests {
		if request.To.Sub(request.From) == 5*time.Minute {
			leaves = append(leaves, request)
		}
	}
	if len(leaves) != 3 {
		t.Fatalf("five-minute leaf count = %d, want three; queries=%d", len(leaves), len(api.requests))
	}
	for i, leaf := range leaves {
		if i == 0 && !leaf.From.Equal(fixtureFrom) {
			t.Fatalf("first leaf begins at %s, want %s", leaf.From, fixtureFrom)
		}
		if i > 0 && !leaves[i-1].To.Equal(leaf.From) {
			t.Fatalf("adjacent leaves have a gap or overlap: %s..%s then %s..%s", leaves[i-1].From, leaves[i-1].To, leaf.From, leaf.To)
		}
	}
	if !leaves[len(leaves)-1].To.Equal(to) {
		t.Fatalf("last leaf ends at %s, want %s", leaves[len(leaves)-1].To, to)
	}
	if len(out.Metrics) != 1 || out.Metrics[0].Value != 7 {
		t.Fatalf("split query metric = %+v, want requests 7", out.Metrics)
	}
}

func TestSaturatedQueryKeepsFiveMinuteBucketsWhole(t *testing.T) {
	api := &fakeAPI{settings: kvSettings()}
	api.queryFn = func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
		if request.From.Second() != 0 || request.From.Minute()%5 != 0 || request.To.Second() != 0 || request.To.Minute()%5 != 0 {
			return nil, errors.New("query split cut a five-minute bucket")
		}
		if request.To.Sub(request.From) > 5*time.Minute {
			return nil, fmt.Errorf("GraphQL dataset %s window %s..%s saturated limit %d", request.Dataset, request.From.Format(time.RFC3339), request.To.Format(time.RFC3339), request.Limit)
		}
		return []map[string]any{{"sum": map[string]any{"requests": 1}, "dimensions": map[string]any{"datetimeFiveMinutes": request.From.Format(time.RFC3339)}}}, nil
	}
	collector := registered(t, api, testConfig(), "kv.operations")
	out := &telemetry.Buffer{}
	to := fixtureFrom.Add(15 * time.Minute)
	mark, err := collector.CollectWindow(context.Background(), fixtureFrom, to, out)
	if err != nil || !mark.Equal(to) || len(out.Metrics) != 1 || out.Metrics[0].Value != 3 {
		t.Fatalf("15-minute split: mark=%s err=%v metrics=%#v, want three complete buckets", mark, err, out.Metrics)
	}
}

func TestIrreducibleSaturatedBucketFailsWithoutProgress(t *testing.T) {
	api := &fakeAPI{settings: kvSettings()}
	api.queryFn = func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
		return nil, fmt.Errorf("GraphQL dataset %s window %s..%s saturated limit %d", request.Dataset, request.From.Format(time.RFC3339), request.To.Format(time.RFC3339), request.Limit)
	}
	collector := registered(t, api, testConfig(), "kv.operations")
	out := &telemetry.Buffer{}
	to := fixtureFrom.Add(5 * time.Minute)
	mark, err := collector.CollectWindow(context.Background(), fixtureFrom, to, out)
	if err == nil || !strings.Contains(err.Error(), "five-minute") {
		t.Fatalf("error = %v, want irreducible five-minute saturation", err)
	}
	if !mark.Equal(fixtureFrom) || len(api.requests) != 1 || len(out.Metrics) != 0 {
		t.Fatalf("irreducible saturation advanced or emitted: mark=%s requests=%d metrics=%d", mark, len(api.requests), len(out.Metrics))
	}
}
