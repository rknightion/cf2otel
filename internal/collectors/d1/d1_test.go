package d1

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

func d1Settings() map[string]cfapi.DatasetSettings {
	return map[string]cfapi.DatasetSettings{
		"d1AnalyticsAdaptiveGroups": testSettings("sum_readQueries", "sum_writeQueries", "dimensions_datetimeFiveMinutes", "dimensions_fixtureOnly"),
		"d1QueriesAdaptiveGroups":   testSettings("count", "dimensions_datetimeFiveMinutes"),
		"d1StorageAdaptiveGroups":   testSettings("max_databaseSizeBytes", "dimensions_datetimeFiveMinutes"),
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

func TestRegisterInstallsEnabledD1Datasets(t *testing.T) {
	api := &fakeAPI{settings: d1Settings()}
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: testConfig(), API: api, Registry: registry})
	entries := registry.Entries()
	if len(entries) != 3 {
		t.Fatalf("registered %d collectors, want three", len(entries))
	}
	want := map[string]bool{"d1.analytics": true, "d1.queries": true, "d1.storage": true}
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

func TestAnalyticsUsesAccountOnlyRequiredFieldsAndSumsGroups(t *testing.T) {
	settings := d1Settings()
	analyticsSettings := settings["d1AnalyticsAdaptiveGroups"]
	analyticsSettings.MaxPageSize = 20000
	settings["d1AnalyticsAdaptiveGroups"] = analyticsSettings
	api := &fakeAPI{
		settings: settings,
		rows: map[string][]map[string]any{
			"d1AnalyticsAdaptiveGroups": {
				{"sum": map[string]any{"readQueries": 1, "writeQueries": 2}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Format(time.RFC3339), "resourceFixture": "synthetic-db-1"}},
				{"sum": map[string]any{"readQueries": 3, "writeQueries": 4}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Add(5 * time.Minute).Format(time.RFC3339), "resourceFixture": "synthetic-db-2"}},
			},
		},
	}
	collector := registered(t, api, testConfig(), "d1.analytics")
	out := &telemetry.Buffer{}
	to := fixtureFrom.Add(10 * time.Minute)
	mark, err := collector.CollectWindow(context.Background(), fixtureFrom, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) {
		t.Fatalf("mark = %s, want %s", mark, to)
	}
	if len(api.requests) != 1 {
		t.Fatalf("query count = %d, want one", len(api.requests))
	}
	request := api.requests[0]
	if request.Scope != cfapi.AccountScope || request.ScopeID != fixtureAccountID || request.Dataset != "d1AnalyticsAdaptiveGroups" {
		t.Fatalf("request did not use configured account and dataset: %+v", request)
	}
	if !request.From.Equal(fixtureFrom) || !request.To.Equal(to) || request.Limit != 10000 {
		t.Fatalf("request window/limit = %s..%s/%d", request.From, request.To, request.Limit)
	}
	wantFields := []string{"sum.readQueries", "sum.writeQueries", "dimensions.datetimeFiveMinutes"}
	if fmt.Sprint(request.WantedFields) != fmt.Sprint(wantFields) {
		t.Fatalf("wanted fields = %v, want %v", request.WantedFields, wantFields)
	}
	got := metricRows(out.Metrics)
	if len(got) != 2 || got[semconv.MetricD1ReadQueries].Value != 4 || got[semconv.MetricD1WriteQueries].Value != 6 {
		t.Fatalf("account aggregates = %+v, want read=4 and write=6", got)
	}
	for _, metric := range out.Metrics {
		if metric.Kind != "counter" || len(metric.Attrs) != 0 {
			t.Errorf("metric has unexpected kind or resource attributes: %+v", metric)
		}
	}
}

func TestAnalyticsFailsClosedWhenRequiredFieldIsMissing(t *testing.T) {
	for _, test := range []struct {
		field   string
		missing string
	}{
		{field: "sum_writeQueries", missing: "sum.writeQueries"},
		{field: "dimensions_datetimeFiveMinutes", missing: "dimensions.datetimeFiveMinutes"},
	} {
		t.Run(test.missing, func(t *testing.T) {
			available := []string{"sum_readQueries", "sum_writeQueries", "dimensions_datetimeFiveMinutes", "dimensions_fixtureOnly"}
			filtered := available[:0]
			for _, field := range available {
				if field != test.field {
					filtered = append(filtered, field)
				}
			}
			settings := d1Settings()
			settings["d1AnalyticsAdaptiveGroups"] = testSettings(filtered...)
			api := &fakeAPI{settings: settings}
			collector := registered(t, api, testConfig(), "d1.analytics")
			out := &telemetry.Buffer{}
			mark, err := collector.CollectWindow(context.Background(), fixtureFrom, fixtureFrom.Add(5*time.Minute), out)
			if err == nil || !strings.Contains(err.Error(), test.missing) {
				t.Fatalf("error = %v, want missing required %s", err, test.missing)
			}
			if !mark.Equal(fixtureFrom) || len(api.requests) != 0 || len(out.Metrics) != 0 {
				t.Fatalf("missing field advanced or emitted: mark=%s requests=%d metrics=%d", mark, len(api.requests), len(out.Metrics))
			}
		})
	}
}

func TestAnalyticsRequiredFieldsMustFitAvailableFieldLimit(t *testing.T) {
	settings := d1Settings()
	analyticsSettings := settings["d1AnalyticsAdaptiveGroups"]
	analyticsSettings.MaxNumberOfFields = 2
	settings["d1AnalyticsAdaptiveGroups"] = analyticsSettings
	api := &fakeAPI{settings: settings}
	collector := registered(t, api, testConfig(), "d1.analytics")
	out := &telemetry.Buffer{}
	mark, err := collector.CollectWindow(context.Background(), fixtureFrom, fixtureFrom.Add(5*time.Minute), out)
	if err == nil || !strings.Contains(err.Error(), "requires 3 fields") {
		t.Fatalf("error = %v, want required field limit failure", err)
	}
	if !mark.Equal(fixtureFrom) || len(api.requests) != 0 || len(out.Metrics) != 0 {
		t.Fatalf("field limit advanced or emitted: mark=%s requests=%d metrics=%d", mark, len(api.requests), len(out.Metrics))
	}
}

func TestStorageUsesLatestCompleteBucketAndMaximumAcrossRows(t *testing.T) {
	api := &fakeAPI{
		settings: d1Settings(),
		rows: map[string][]map[string]any{
			"d1StorageAdaptiveGroups": {
				{"max": map[string]any{"databaseSizeBytes": 100}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Format(time.RFC3339), "resourceFixture": "synthetic-db-a"}},
				{"max": map[string]any{"databaseSizeBytes": 250}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Format(time.RFC3339), "resourceFixture": "synthetic-db-b"}},
				{"max": map[string]any{"databaseSizeBytes": 9}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Add(5 * time.Minute).Format(time.RFC3339), "resourceFixture": "synthetic-db-a"}},
				{"max": map[string]any{"databaseSizeBytes": 17}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Add(5 * time.Minute).Format(time.RFC3339), "resourceFixture": "synthetic-db-b"}},
				{"max": map[string]any{"databaseSizeBytes": 999}, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Add(10 * time.Minute).Format(time.RFC3339), "resourceFixture": "synthetic-db-a"}},
			},
		},
	}
	collector := registered(t, api, testConfig(), "d1.storage")
	out := &telemetry.Buffer{}
	to := fixtureFrom.Add(10 * time.Minute)
	mark, err := collector.CollectWindow(context.Background(), fixtureFrom, to, out)
	if err != nil || !mark.Equal(to) {
		t.Fatalf("mark=%s error=%v", mark, err)
	}
	if len(out.Metrics) != 1 || out.Metrics[0].Kind != "gauge" || out.Metrics[0].Name != semconv.MetricD1StorageBytes || out.Metrics[0].Value != 17 || len(out.Metrics[0].Attrs) != 0 {
		t.Fatalf("storage gauge = %+v, want latest complete bucket max 17 without attributes", out.Metrics)
	}
	if len(api.requests) != 1 || fmt.Sprint(api.requests[0].WantedFields) != fmt.Sprint([]string{"max.databaseSizeBytes", "dimensions.datetimeFiveMinutes"}) {
		t.Fatalf("D1 storage selected fields = %+v, want only contracted value and time fields", api.requests)
	}
}

func TestAdjacentWindowsOwnFiveMinuteBoundaryOnce(t *testing.T) {
	api := &fakeAPI{
		settings: d1Settings(),
		rows: map[string][]map[string]any{
			"d1QueriesAdaptiveGroups": {
				{"count": 1, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Format(time.RFC3339), "queryText": "synthetic-query-text"}},
				{"count": 2, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Add(5 * time.Minute).Format(time.RFC3339)}},
				{"count": 4, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Add(10 * time.Minute).Format(time.RFC3339)}},
			},
		},
	}
	collector := registered(t, api, testConfig(), "d1.queries")
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
	wantFields := []string{"count", "dimensions.datetimeFiveMinutes"}
	if len(api.requests) != 2 || fmt.Sprint(api.requests[0].WantedFields) != fmt.Sprint(wantFields) {
		t.Fatalf("D1 query selection = %+v, want count and timestamp only", api.requests)
	}
	for _, metric := range append(first.Metrics, second.Metrics...) {
		if len(metric.Attrs) != 0 {
			t.Errorf("D1 query metric emitted forbidden attributes: %+v", metric.Attrs)
		}
	}
}

func TestUnalignedWindowsAdvanceOnlyThroughCompleteBuckets(t *testing.T) {
	api := &fakeAPI{
		settings: d1Settings(),
		rows: map[string][]map[string]any{
			"d1QueriesAdaptiveGroups": {
				{"count": 1, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Format(time.RFC3339)}},
				{"count": 2, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Add(5 * time.Minute).Format(time.RFC3339)}},
				{"count": 4, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Add(10 * time.Minute).Format(time.RFC3339)}},
				{"count": 8, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Add(15 * time.Minute).Format(time.RFC3339)}},
			},
		},
	}
	collector := registered(t, api, testConfig(), "d1.queries")
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

func TestMoreThan500SyntheticResourcesCollapseToFrozenAccountSeries(t *testing.T) {
	rows := make([]map[string]any, 501)
	for i := range rows {
		rows[i] = map[string]any{
			"sum":        map[string]any{"readQueries": 1, "writeQueries": 2},
			"dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Format(time.RFC3339), "resourceFixture": fmt.Sprintf("fixture-resource-%03d", i)},
		}
	}
	api := &fakeAPI{settings: d1Settings(), rows: map[string][]map[string]any{"d1AnalyticsAdaptiveGroups": rows}}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	collector := registered(t, api, testConfig(), "d1.analytics")
	out := &telemetry.Buffer{}
	mark, err := collector.CollectWindow(context.Background(), fixtureFrom, fixtureFrom.Add(5*time.Minute), out)
	if err != nil || !mark.Equal(fixtureFrom.Add(5*time.Minute)) {
		t.Fatalf("mark=%s error=%v", mark, err)
	}
	got := metricRows(out.Metrics)
	if len(got) != 2 || got[semconv.MetricD1ReadQueries].Value != 501 || got[semconv.MetricD1WriteQueries].Value != 1002 {
		t.Fatalf("501 resource rows did not collapse to the two account series: %+v", got)
	}
	for _, metric := range out.Metrics {
		if len(metric.Attrs) != 0 {
			t.Errorf("resource attribute escaped account aggregation: %+v", metric.Attrs)
		}
	}
	if strings.Contains(logs.String(), "dropped_series") {
		t.Fatalf("the 500-series cap is unreachable after account aggregation: %q", logs.String())
	}
}

func TestSeriesDropUsesSortedMetricsAndCountOnlyDisclosure(t *testing.T) {
	cfg := testConfig()
	cfg.Platform.MaxMetricSeriesPerWindow = 1
	api := &fakeAPI{
		settings: d1Settings(),
		rows: map[string][]map[string]any{
			"d1AnalyticsAdaptiveGroups": {{
				"sum":        map[string]any{"readQueries": 1, "writeQueries": 2},
				"dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Format(time.RFC3339), "resourceFixture": "fixture-resource-secret-value"},
			}},
		},
	}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	collector := registered(t, api, cfg, "d1.analytics")
	out := &telemetry.Buffer{}
	if _, err := collector.CollectWindow(context.Background(), fixtureFrom, fixtureFrom.Add(5*time.Minute), out); err != nil {
		t.Fatal(err)
	}
	if len(out.Metrics) != 1 || out.Metrics[0].Name != semconv.MetricD1ReadQueries {
		t.Fatalf("series cap retained %v, want first sorted metric %q", out.Metrics, semconv.MetricD1ReadQueries)
	}
	logText := logs.String()
	if strings.Count(logText, "dropped_series=1") != 1 || strings.Contains(logText, "fixture-resource-secret-value") {
		t.Fatalf("drop disclosure is missing its count or leaks a resource: %q", logText)
	}
}

func TestSaturatedQueryBisectsWithoutGapsOrOverlap(t *testing.T) {
	api := &fakeAPI{settings: d1Settings()}
	api.queryFn = func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
		if request.To.Sub(request.From) > 5*time.Minute {
			return nil, fmt.Errorf("GraphQL dataset %s window %s..%s saturated limit %d", request.Dataset, request.From.Format(time.RFC3339), request.To.Format(time.RFC3339), request.Limit)
		}
		if request.From.Equal(fixtureFrom) {
			return []map[string]any{{"count": 7, "dimensions": map[string]any{"datetimeFiveMinutes": fixtureFrom.Format(time.RFC3339)}}}, nil
		}
		return nil, nil
	}
	collector := registered(t, api, testConfig(), "d1.queries")
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
		t.Fatalf("split query metric = %+v, want count 7", out.Metrics)
	}
}

func TestSaturatedQueryKeepsFiveMinuteBucketsWhole(t *testing.T) {
	api := &fakeAPI{settings: d1Settings()}
	api.queryFn = func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
		if request.From.Second() != 0 || request.From.Minute()%5 != 0 || request.To.Second() != 0 || request.To.Minute()%5 != 0 {
			return nil, errors.New("query split cut a five-minute bucket")
		}
		if request.To.Sub(request.From) > 5*time.Minute {
			return nil, fmt.Errorf("GraphQL dataset %s window %s..%s saturated limit %d", request.Dataset, request.From.Format(time.RFC3339), request.To.Format(time.RFC3339), request.Limit)
		}
		return []map[string]any{{"count": 1, "dimensions": map[string]any{"datetimeFiveMinutes": request.From.Format(time.RFC3339)}}}, nil
	}
	collector := registered(t, api, testConfig(), "d1.queries")
	out := &telemetry.Buffer{}
	to := fixtureFrom.Add(15 * time.Minute)
	mark, err := collector.CollectWindow(context.Background(), fixtureFrom, to, out)
	if err != nil || !mark.Equal(to) || len(out.Metrics) != 1 || out.Metrics[0].Value != 3 {
		t.Fatalf("15-minute split: mark=%s err=%v metrics=%#v, want three complete buckets", mark, err, out.Metrics)
	}
}

func TestIrreducibleSaturatedBucketFailsWithoutProgress(t *testing.T) {
	api := &fakeAPI{settings: d1Settings()}
	api.queryFn = func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
		return nil, fmt.Errorf("GraphQL dataset %s window %s..%s saturated limit %d", request.Dataset, request.From.Format(time.RFC3339), request.To.Format(time.RFC3339), request.Limit)
	}
	collector := registered(t, api, testConfig(), "d1.queries")
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
