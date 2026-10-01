package rum

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

const testAccountID = "account-test"

type fakeAPI struct {
	settings map[string]cfapi.DatasetSettings
	rows     map[string][]map[string]any
	queries  []cfapi.GraphQLRequest
	reads    []string
}

func (f *fakeAPI) Get(context.Context, string, url.Values, any) error {
	return errors.New("unexpected REST request")
}

func (f *fakeAPI) Query(_ context.Context, req cfapi.GraphQLRequest, out any) error {
	f.queries = append(f.queries, req)
	data, err := json.Marshal(f.rows[req.ScopeID+"/"+req.Dataset])
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
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

func (f *fakeAPI) DatasetSettings(_ context.Context, scope cfapi.Scope, scopeID, dataset string) (cfapi.DatasetSettings, error) {
	if scope != cfapi.AccountScope {
		return cfapi.DatasetSettings{}, errors.New("RUM settings were not requested at account scope")
	}
	key := scopeID + "/" + dataset
	f.reads = append(f.reads, key)
	settings, ok := f.settings[key]
	if !ok {
		return cfapi.DatasetSettings{}, errors.New("unexpected dataset settings request")
	}
	return settings, nil
}

func rumSettings(maxFields int, fields ...string) cfapi.DatasetSettings {
	return cfapi.DatasetSettings{Enabled: true, AvailableFields: fields, MaxNumberOfFields: maxFields}
}

func testConfig() *config.Config {
	return &config.Config{Cloudflare: config.CloudflareConfig{AccountID: testAccountID}}
}

func TestPageloadGroupsEmitCountsByCountryAndDeviceWithinAccountFieldLimit(t *testing.T) {
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute)
	api := &fakeAPI{
		settings: map[string]cfapi.DatasetSettings{
			testAccountID + "/rumPageloadEventsAdaptiveGroups": rumSettings(4,
				"count", "sum_visits", "dimensions_countryName", "dimensions_deviceType", "dimensions_siteTag"),
		},
		rows: map[string][]map[string]any{
			testAccountID + "/rumPageloadEventsAdaptiveGroups": {{
				"count": 17,
				"sum":   map[string]any{"visits": 9},
				"dimensions": map[string]any{
					"countryName": "GB", "deviceType": "desktop", "siteTag": "example.test",
					"requestPath": "/private-path", "refererPath": "/private-referrer",
				},
			}},
		},
	}
	out := &telemetry.Buffer{}
	c := NewPageloads(testConfig(), api)
	mark, err := c.CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) {
		t.Fatalf("mark = %s, want %s", mark, to)
	}
	if c.Lag() != 10*time.Minute {
		t.Fatalf("lag = %s, want 10m ingestion holdback", c.Lag())
	}
	if len(out.Metrics) != 2 || len(out.Records) != 0 {
		t.Fatalf("metrics=%d records=%d, want two counters and no logs", len(out.Metrics), len(out.Records))
	}
	if out.Metrics[0].Name != semconv.MetricRUMPageViews || out.Metrics[0].Kind != "counter" || out.Metrics[0].Value != 17 {
		t.Fatalf("page views = %+v", out.Metrics[0])
	}
	if out.Metrics[1].Name != semconv.MetricRUMSessions || out.Metrics[1].Kind != "counter" || out.Metrics[1].Value != 9 {
		t.Fatalf("sessions = %+v", out.Metrics[1])
	}
	for _, metric := range out.Metrics {
		if !hasRUMAttr(metric.Attrs, semconv.AttrRUMCountry, "GB") || !hasRUMAttr(metric.Attrs, semconv.AttrRUMDeviceType, "desktop") {
			t.Errorf("metric lacks country/device dimensions: %+v", metric.Attrs)
		}
		for _, attr := range metric.Attrs {
			if attr.Key != semconv.AttrRUMCountry && attr.Key != semconv.AttrRUMDeviceType && attr.Key != semconv.AttrRUMSiteTag {
				t.Errorf("unexpected high-cardinality metric attribute %q", attr.Key)
			}
		}
		if hasRUMAttr(metric.Attrs, semconv.AttrRUMSiteTag, "example.test") {
			t.Errorf("optional site tag exceeded maxNumberOfFields: %+v", metric.Attrs)
		}
	}
	if len(api.queries) != 1 {
		t.Fatalf("queries = %d, want one account query", len(api.queries))
	}
	got := api.queries[0]
	if got.Scope != cfapi.AccountScope || got.ScopeID != testAccountID || got.Dataset != "rumPageloadEventsAdaptiveGroups" || !got.From.Equal(from) || !got.To.Equal(to) {
		t.Fatalf("query = %+v", got)
	}
	wantFields := []string{"count", "sum.visits", "dimensions.countryName", "dimensions.deviceType"}
	if !reflect.DeepEqual(got.WantedFields, wantFields) {
		t.Fatalf("fields = %v, want available and field-limited selection %v", got.WantedFields, wantFields)
	}
	if !reflect.DeepEqual(api.reads, []string{testAccountID + "/rumPageloadEventsAdaptiveGroups"}) {
		t.Fatalf("settings reads = %v", api.reads)
	}
}

func TestPageloadsFailClosedWhenRequiredAccountFieldIsMissing(t *testing.T) {
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	api := &fakeAPI{settings: map[string]cfapi.DatasetSettings{
		testAccountID + "/rumPageloadEventsAdaptiveGroups": rumSettings(30,
			"count", "sum_visits", "dimensions_countryName"),
	}}
	out := &telemetry.Buffer{}
	mark, err := NewPageloads(testConfig(), api).CollectWindow(context.Background(), from, to, out)
	if err == nil {
		t.Fatal("missing device dimension was accepted")
	}
	if !mark.Equal(from) || len(api.queries) != 0 || len(out.Metrics) != 0 {
		t.Fatalf("mark=%s queries=%d metrics=%d; want no query, output, or cursor advance", mark, len(api.queries), len(out.Metrics))
	}
}

func TestWebVitalsUseRollingAccountGroupsAndExcludeNoDataSentinel(t *testing.T) {
	// These invented source values exercise conversion from GraphQL timing
	// quantiles to seconds without using live account values.
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute)
	fields := []string{
		"dimensions_deviceType", "dimensions_siteTag",
		"quantiles_largestContentfulPaintP75", "quantiles_interactionToNextPaintP75",
		"quantiles_firstInputDelayP75", "quantiles_firstContentfulPaintP75",
		"quantiles_timeToFirstByteP75", "quantiles_cumulativeLayoutShiftP75",
	}
	api := &fakeAPI{
		settings: map[string]cfapi.DatasetSettings{
			testAccountID + "/rumWebVitalsEventsAdaptiveGroups": rumSettings(7, fields...),
		},
		rows: map[string][]map[string]any{
			testAccountID + "/rumWebVitalsEventsAdaptiveGroups": {{
				"dimensions": map[string]any{"deviceType": "desktop", "siteTag": "example.test", "requestPath": "/private-path"},
				"quantiles": map[string]any{
					"largestContentfulPaintP75": 123456, "interactionToNextPaintP75": 45678,
					"firstInputDelayP75": -1, "firstContentfulPaintP75": 76543,
					"timeToFirstByteP75": 12345, "cumulativeLayoutShiftP75": 0.08,
				},
			}},
		},
	}
	out := &telemetry.Buffer{}
	c := NewWebVitals(testConfig(), api)
	mark, err := c.CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) || c.Lag() != 10*time.Minute {
		t.Fatalf("mark=%s lag=%s", mark, c.Lag())
	}
	if len(out.Metrics) != 5 {
		t.Fatalf("metrics = %d, want five values with the -1 FID sentinel omitted", len(out.Metrics))
	}
	wantNames := map[string]bool{
		semconv.MetricRUMLCPP75: true, semconv.MetricRUMINPP75: true,
		semconv.MetricRUMFCPP75: true, semconv.MetricRUMTTFBP75: true,
		semconv.MetricRUMCLSP75: true,
	}
	values := map[string]float64{}
	for i, metric := range out.Metrics {
		if !wantNames[metric.Name] || metric.Kind != "gauge" {
			t.Errorf("metric[%d] = %+v, want one of the five non-sentinel gauges", i, metric)
		}
		delete(wantNames, metric.Name)
		values[metric.Name] = metric.Value
		if !hasRUMAttr(metric.Attrs, semconv.AttrRUMDeviceType, "desktop") {
			t.Errorf("metric[%d] lacks device dimension: %+v", i, metric.Attrs)
		}
		if hasRUMAttr(metric.Attrs, semconv.AttrRUMSiteTag, "example.test") {
			t.Errorf("optional site tag exceeded maxNumberOfFields: %+v", metric.Attrs)
		}
		for _, attr := range metric.Attrs {
			if attr.Key != semconv.AttrRUMDeviceType && attr.Key != semconv.AttrRUMSiteTag {
				t.Errorf("unexpected web-vitals metric attribute %q", attr.Key)
			}
		}
	}
	if len(wantNames) != 0 || math.Abs(values[semconv.MetricRUMLCPP75]-0.123456) > 1e-9 || math.Abs(values[semconv.MetricRUMINPP75]-0.045678) > 1e-9 || math.Abs(values[semconv.MetricRUMFCPP75]-0.076543) > 1e-9 || math.Abs(values[semconv.MetricRUMTTFBP75]-0.012345) > 1e-9 || values[semconv.MetricRUMCLSP75] != 0.08 {
		t.Fatalf("quantile names or values were not preserved: missing=%v values=%v", wantNames, values)
	}
	if len(api.queries) != 1 {
		t.Fatalf("queries = %d, want one account query", len(api.queries))
	}
	got := api.queries[0]
	if got.Scope != cfapi.AccountScope || got.ScopeID != testAccountID || got.Dataset != "rumWebVitalsEventsAdaptiveGroups" || !got.From.Equal(to.Add(-webVitalsRollingWindow)) || !got.To.Equal(to) {
		t.Fatalf("rolling query = %+v", got)
	}
	wantFields := []string{
		"dimensions.deviceType", "quantiles.largestContentfulPaintP75", "quantiles.interactionToNextPaintP75",
		"quantiles.firstInputDelayP75", "quantiles.firstContentfulPaintP75", "quantiles.timeToFirstByteP75",
		"quantiles.cumulativeLayoutShiftP75",
	}
	if !reflect.DeepEqual(got.WantedFields, wantFields) {
		t.Fatalf("fields = %v, want maxNumberOfFields-limited %v", got.WantedFields, wantFields)
	}
}

func TestRegisteredWebVitalsConvertMicrosecondsToSeconds(t *testing.T) {
	from := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute)
	key := testAccountID + "/rumWebVitalsEventsAdaptiveGroups"
	api := &fakeAPI{
		settings: map[string]cfapi.DatasetSettings{key: rumSettings(7,
			"dimensions_deviceType", "quantiles_largestContentfulPaintP75", "quantiles_interactionToNextPaintP75",
			"quantiles_firstInputDelayP75", "quantiles_firstContentfulPaintP75", "quantiles_timeToFirstByteP75",
			"quantiles_cumulativeLayoutShiftP75")},
		rows: map[string][]map[string]any{key: {{
			"dimensions": map[string]any{"deviceType": "desktop"},
			"quantiles": map[string]any{
				"largestContentfulPaintP75": 2_500_000, "interactionToNextPaintP75": 2_500_000,
				"firstInputDelayP75": 2_500_000, "firstContentfulPaintP75": 2_500_000,
				"timeToFirstByteP75": 2_500_000, "cumulativeLayoutShiftP75": 0.08,
			},
		}}},
	}
	cfg := testConfig()
	cfg.Collectors = map[string]config.CollectorConfig{"rum.web_vitals": {Enabled: true}}
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: cfg, API: api, Registry: registry})
	out := &telemetry.Buffer{}
	found := false
	for _, entry := range registry.Entries() {
		if entry.Collector.Name() != "rum.web_vitals" {
			continue
		}
		found = true
		window, ok := entry.Collector.(collector.WindowCollector)
		if !ok {
			t.Fatal("web-vitals collector does not support windows")
		}
		mark, err := window.CollectWindow(context.Background(), from, to, out)
		if err != nil {
			t.Fatal(err)
		}
		if !mark.Equal(to) {
			t.Fatalf("mark = %s, want %s", mark, to)
		}
	}
	if !found {
		t.Fatal("web-vitals collector was not registered")
	}
	want := map[string]float64{
		semconv.MetricRUMLCPP75: 2.5, semconv.MetricRUMINPP75: 2.5,
		semconv.MetricRUMFIDP75: 2.5, semconv.MetricRUMFCPP75: 2.5,
		semconv.MetricRUMTTFBP75: 2.5, semconv.MetricRUMCLSP75: 0.08,
	}
	for _, metric := range out.Metrics {
		value, ok := want[metric.Name]
		if !ok {
			t.Errorf("unexpected or duplicate metric %q", metric.Name)
			continue
		}
		if metric.Kind != "gauge" || metric.Value != value {
			t.Errorf("%s = %g (%s), want %g gauge (timings in seconds, CLS unchanged)", metric.Name, metric.Value, metric.Kind, value)
		}
		delete(want, metric.Name)
	}
	if len(want) != 0 {
		t.Errorf("missing metrics: %v", want)
	}
}

func TestRegisteredWebVitalsOmitMissingQuantilesAndRetainZero(t *testing.T) {
	from := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	key := testAccountID + "/rumWebVitalsEventsAdaptiveGroups"
	for _, missing := range []string{"absent", "null", "sentinel", "negative", "absent_quantiles", "null_quantiles", "empty"} {
		t.Run(missing, func(t *testing.T) {
			api := &fakeAPI{
				settings: map[string]cfapi.DatasetSettings{key: rumSettings(7,
					"dimensions_deviceType", "quantiles_largestContentfulPaintP75", "quantiles_interactionToNextPaintP75",
					"quantiles_firstInputDelayP75", "quantiles_firstContentfulPaintP75", "quantiles_timeToFirstByteP75",
					"quantiles_cumulativeLayoutShiftP75")},
				rows: map[string][]map[string]any{key: {vitalsRow("desktop", 1, 0.1)}},
			}
			cfg := testConfig()
			cfg.Collectors = map[string]config.CollectorConfig{"rum.web_vitals": {Enabled: true}}
			registry := collector.NewRegistry()
			Register(collector.Deps{Config: cfg, API: api, Registry: registry})
			entries := registry.Entries()
			if len(entries) != 1 {
				t.Fatalf("registered collectors = %d, want one web-vitals collector", len(entries))
			}
			c := entries[0].Collector.(collector.WindowCollector)
			to := from.Add(5 * time.Minute)
			initial := &telemetry.Buffer{}
			if _, err := c.CollectWindow(context.Background(), from, to, initial); err != nil {
				t.Fatal(err)
			}
			if len(initial.Metrics) != 6 {
				t.Fatalf("initial metrics = %d, want six populated quantiles", len(initial.Metrics))
			}
			row := vitalsRow("desktop", 0, 0)
			quantiles := row["quantiles"].(map[string]any)
			switch missing {
			case "absent":
				delete(quantiles, "firstInputDelayP75")
			case "null":
				quantiles["firstInputDelayP75"] = nil
			case "sentinel":
				quantiles["firstInputDelayP75"] = -1
			case "negative":
				quantiles["firstInputDelayP75"] = -2
			case "absent_quantiles":
				delete(row, "quantiles")
			case "null_quantiles":
				row["quantiles"] = nil
			}
			api.rows[key] = []map[string]any{row}
			wantCount := 5
			if missing == "absent_quantiles" || missing == "null_quantiles" || missing == "empty" {
				wantCount = 0
			}
			if missing == "empty" {
				api.rows[key] = nil
			}
			for i := 0; i < 2; i++ {
				out := &telemetry.Buffer{}
				mark, err := c.CollectWindow(context.Background(), to, to.Add(5*time.Minute), out)
				if err != nil {
					t.Fatalf("missing quantiles must not fail collection: %v", err)
				}
				if !mark.Equal(to.Add(5 * time.Minute)) {
					t.Fatalf("mark = %s, want successful window advance", mark)
				}
				if len(out.Metrics) != wantCount {
					t.Fatalf("metrics = %d, want %d genuine zeros and no synthetic missing-data points", len(out.Metrics), wantCount)
				}
				seen := map[string]bool{}
				for _, metric := range out.Metrics {
					if metric.Name == semconv.MetricRUMFIDP75 || metric.Kind != "gauge" || metric.Value != 0 || seen[metric.Name] {
						t.Errorf("metric = %+v, want distinct genuine zero excluding missing FID", metric)
					}
					seen[metric.Name] = true
				}
				to = to.Add(5 * time.Minute)
			}
		})
	}
}

func TestWebVitalsOmitStaleSeriesWhenEmittingCurrentValues(t *testing.T) {
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute)
	settings := rumSettings(30,
		"dimensions_deviceType", "quantiles_largestContentfulPaintP75", "quantiles_interactionToNextPaintP75",
		"quantiles_firstInputDelayP75", "quantiles_firstContentfulPaintP75", "quantiles_timeToFirstByteP75",
		"quantiles_cumulativeLayoutShiftP75")
	key := testAccountID + "/rumWebVitalsEventsAdaptiveGroups"
	api := &fakeAPI{
		settings: map[string]cfapi.DatasetSettings{key: settings},
		rows:     map[string][]map[string]any{key: {vitalsRow("desktop", 1, 0.1)}},
	}
	c := NewWebVitals(testConfig(), api)
	if _, err := c.CollectWindow(context.Background(), from, to, &telemetry.Buffer{}); err != nil {
		t.Fatal(err)
	}
	api.rows[key] = []map[string]any{vitalsRow("mobile", 2, 0.2)}
	out := &telemetry.Buffer{}
	if _, err := c.CollectWindow(context.Background(), to, to.Add(5*time.Minute), out); err != nil {
		t.Fatal(err)
	}
	if len(out.Metrics) != 6 {
		t.Fatalf("metrics=%d, want only six current gauges with no synthetic stale zeros", len(out.Metrics))
	}
	for i := 0; i < len(out.Metrics); i++ {
		want := 2.0
		if out.Metrics[i].Name == semconv.MetricRUMCLSP75 {
			want = 0.2
		}
		if out.Metrics[i].Value != want || !hasRUMAttr(out.Metrics[i].Attrs, semconv.AttrRUMDeviceType, "mobile") {
			t.Fatalf("current metric[%d] = %+v, want mobile source value", i, out.Metrics[i])
		}
	}
}

type vitalsFailSecondFlush struct{ calls int }

func (f *vitalsFailSecondFlush) BeginCommit() uint64 { return uint64(f.calls) }
func (f *vitalsFailSecondFlush) FlushCommit(context.Context, uint64) error {
	f.calls++
	if f.calls == 2 {
		return errors.New("synthetic OTLP failure")
	}
	return nil
}

func TestWebVitalsRetryEmitsOnlySourceValuesAfterFailedFlush(t *testing.T) {
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	settings := rumSettings(30,
		"dimensions_deviceType", "quantiles_largestContentfulPaintP75", "quantiles_interactionToNextPaintP75",
		"quantiles_firstInputDelayP75", "quantiles_firstContentfulPaintP75", "quantiles_timeToFirstByteP75",
		"quantiles_cumulativeLayoutShiftP75")
	key := testAccountID + "/rumWebVitalsEventsAdaptiveGroups"
	api := &fakeAPI{settings: map[string]cfapi.DatasetSettings{key: settings}, rows: map[string][]map[string]any{key: {vitalsRow("desktop", 1, 0.1)}}}
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "checkpoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	window := 5 * time.Minute
	now := from.Add(window + rumLag)
	out := &telemetry.Buffer{}
	c := NewWebVitals(testConfig(), api)
	entry := collector.Entry{Collector: c, Interval: window, InitialLookback: window, MaxWindow: window}
	scheduler := collector.NewScheduler(collector.NewRegistry(), out, store)
	scheduler.Now = func() time.Time { return now }
	scheduler.Flusher = &vitalsFailSecondFlush{}
	if err := scheduler.RunOnce(context.Background(), entry); err != nil {
		t.Fatalf("initial desktop commit: %v", err)
	}
	now = now.Add(window)
	api.rows[key] = []map[string]any{vitalsRow("mobile", 2, 0.2)}
	if err := scheduler.RunOnce(context.Background(), entry); err == nil || err.Error() != "synthetic OTLP failure" {
		t.Fatalf("second flush = %v, want synthetic failure", err)
	}
	if mark, _ := store.Get(c.Name()); !mark.Equal(from.Add(window)) {
		t.Fatalf("checkpoint after failed flush = %s, want %s", mark, from.Add(window))
	}
	out = &telemetry.Buffer{}
	scheduler.Emitter = out
	if err := scheduler.RunOnce(context.Background(), entry); err != nil {
		t.Fatalf("retry flush: %v", err)
	}
	if len(out.Metrics) != 6 {
		t.Fatalf("metrics after retry = %d, want six current source values", len(out.Metrics))
	}
	for _, metric := range out.Metrics {
		want := 2.0
		if metric.Name == semconv.MetricRUMCLSP75 {
			want = 0.2
		}
		if metric.Kind != "gauge" || metric.Value != want || !hasRUMAttr(metric.Attrs, semconv.AttrRUMDeviceType, "mobile") {
			t.Errorf("retry metric = %+v, want current mobile source value and no stale zero", metric)
		}
	}
}

func TestRegisterHonorsExplicitDisableAndRegistersBothWindowsWhenEnabled(t *testing.T) {
	cfg := config.Default()
	for _, name := range []string{"rum.pageloads", "rum.web_vitals"} {
		entry := cfg.Collectors[name]
		entry.Enabled = false
		cfg.Collectors[name] = entry
	}
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: &cfg, API: &fakeAPI{}, Registry: registry})
	if entries := registry.Entries(); len(entries) != 0 {
		t.Fatalf("explicitly disabled RUM entries = %d, want zero", len(entries))
	}
	cfg.Collectors["rum.pageloads"] = config.CollectorConfig{Enabled: true, Interval: 2 * time.Minute, InitialLookback: time.Hour, MaxWindow: 3 * time.Hour}
	cfg.Collectors["rum.web_vitals"] = config.CollectorConfig{Enabled: true, Interval: 3 * time.Minute, InitialLookback: time.Hour, MaxWindow: 3 * time.Hour}
	registry = collector.NewRegistry()
	Register(collector.Deps{Config: &cfg, API: &fakeAPI{}, Registry: registry})
	entries := registry.Entries()
	if len(entries) != 2 {
		t.Fatalf("registered entries = %d, want two", len(entries))
	}
	if entries[0].Collector.Name() != "rum.pageloads" || entries[1].Collector.Name() != "rum.web_vitals" {
		t.Fatalf("registered names = %q, %q", entries[0].Collector.Name(), entries[1].Collector.Name())
	}
	if entries[0].Interval != 2*time.Minute || entries[0].InitialLookback != time.Hour || entries[0].MaxWindow != 3*time.Hour || entries[1].Interval != 3*time.Minute || entries[1].InitialLookback != time.Hour || entries[1].MaxWindow != 3*time.Hour {
		t.Fatalf("registered entries did not preserve configuration: %+v", entries)
	}
}

func vitalsRow(device string, timingSeconds, cls float64) map[string]any {
	return map[string]any{
		"dimensions": map[string]any{"deviceType": device},
		"quantiles": map[string]any{
			"largestContentfulPaintP75": timingSeconds * 1_000_000, "interactionToNextPaintP75": timingSeconds * 1_000_000,
			"firstInputDelayP75": timingSeconds * 1_000_000, "firstContentfulPaintP75": timingSeconds * 1_000_000,
			"timeToFirstByteP75": timingSeconds * 1_000_000, "cumulativeLayoutShiftP75": cls,
		},
	}
}

func hasRUMAttr(attrs []telemetry.Attr, key, value string) bool {
	for _, attr := range attrs {
		if attr.Key == key && attr.Value == value {
			return true
		}
	}
	return false
}
