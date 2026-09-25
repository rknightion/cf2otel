package r2

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

const (
	testBandwidthDataset          = "r2BandwidthUsageAdaptiveGroups"
	testCatalogDataDataset        = "r2CatalogDataOperationsAdaptiveGroups"
	testCatalogMaintenanceDataset = "r2CatalogTableMaintenanceAdaptiveGroups"
	testOperationsDataset         = "r2OperationsAdaptiveGroups"
	testStorageDataset            = "r2StorageAdaptiveGroups"
	testSQLDataset                = "r2sqlOperationsAdaptiveGroups"
)

type r2QueryCall struct {
	request cfapi.GraphQLRequest
	err     error
}

type r2TestAPI struct {
	settings    map[string]cfapi.DatasetSettings
	settingsErr error
	query       func(cfapi.GraphQLRequest) ([]map[string]any, error)
	calls       []r2QueryCall
}

func (*r2TestAPI) Get(context.Context, string, url.Values, any) error {
	return errors.New("unexpected REST request")
}

func (f *r2TestAPI) Query(_ context.Context, request cfapi.GraphQLRequest, out any) error {
	var rows []map[string]any
	var err error
	if f.query != nil {
		rows, err = f.query(request)
	}
	f.calls = append(f.calls, r2QueryCall{request: request, err: err})
	if err != nil {
		return err
	}
	b, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (f *r2TestAPI) DatasetSettings(_ context.Context, scope cfapi.Scope, scopeID, dataset string) (cfapi.DatasetSettings, error) {
	if scope != cfapi.AccountScope || scopeID != "synthetic-account" {
		return cfapi.DatasetSettings{}, errors.New("unexpected R2 settings scope")
	}
	if f.settingsErr != nil {
		return cfapi.DatasetSettings{}, f.settingsErr
	}
	settings, ok := f.settings[dataset]
	if !ok {
		return cfapi.DatasetSettings{}, fmt.Errorf("unexpected R2 dataset %q", dataset)
	}
	return settings, nil
}

func (*r2TestAPI) Accounts(context.Context) ([]cfapi.Account, error) {
	return nil, errors.New("unexpected account discovery")
}

func (*r2TestAPI) Zones(context.Context) ([]cfapi.Zone, error) {
	return nil, errors.New("unexpected zone discovery")
}

func (*r2TestAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) {
	return nil, errors.New("unexpected gateway discovery")
}

func r2TestSettings(dataset string) cfapi.DatasetSettings {
	fields := []string{"dimensions_datetimeFiveMinutes"}
	switch dataset {
	case testBandwidthDataset:
		fields = append(fields, "sum_bytesDownload", "sum_bytesUpload", "dimensions_bucketName")
	case testCatalogDataDataset, testCatalogMaintenanceDataset:
		fields = append(fields, "count", "dimensions_namespaceName")
	case testOperationsDataset:
		fields = append(fields, "sum_requests", "dimensions_bucketName")
	case testStorageDataset:
		fields = append(fields, "max_payloadSize", "max_objectCount", "dimensions_bucketName")
	case testSQLDataset:
		fields = append(fields, "count", "dimensions_bucket")
	}
	return cfapi.DatasetSettings{
		Enabled:           true,
		AvailableFields:   fields,
		MaxNumberOfFields: 20,
		MaxDuration:       3600,
		NotOlderThan:      31 * 24 * 60 * 60,
		MaxPageSize:       100,
	}
}

func r2TestConfig(enabled ...string) *config.Config {
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "synthetic-account"
	for name, collectorConfig := range cfg.Collectors {
		collectorConfig.Enabled = false
		cfg.Collectors[name] = collectorConfig
	}
	for _, name := range enabled {
		collectorConfig := cfg.Collectors[name]
		collectorConfig.Enabled = true
		cfg.Collectors[name] = collectorConfig
	}
	return &cfg
}

func newR2TestAPI(dataset string) *r2TestAPI {
	return &r2TestAPI{settings: map[string]cfapi.DatasetSettings{dataset: r2TestSettings(dataset)}}
}

func r2Register(t *testing.T, cfg *config.Config, api cfapi.Client) *collector.Registry {
	t.Helper()
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: cfg, API: api, Registry: registry})
	return registry
}

func r2WindowCollector(t *testing.T, cfg *config.Config, api cfapi.Client, name string) collector.WindowCollector {
	t.Helper()
	for _, entry := range r2Register(t, cfg, api).Entries() {
		if entry.Collector.Name() != name {
			continue
		}
		window, ok := entry.Collector.(collector.WindowCollector)
		if !ok {
			t.Fatalf("collector %q is not a window collector", name)
		}
		return window
	}
	t.Fatalf("collector %q was not registered", name)
	return nil
}

func TestNoCompleteBucketFailsWithoutProgress(t *testing.T) {
	api := &r2TestAPI{settings: map[string]cfapi.DatasetSettings{testOperationsDataset: r2TestSettings(testOperationsDataset)}}
	window := r2WindowCollector(t, r2TestConfig("r2.operations"), api, "r2.operations")
	from := time.Date(2026, 9, 24, 10, 1, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	mark, err := window.CollectWindow(context.Background(), from, to, &telemetry.Buffer{})
	if err == nil || !mark.Equal(from) || len(api.calls) != 0 {
		t.Fatalf("no complete bucket advanced or queried: mark=%s err=%v calls=%d", mark, err, len(api.calls))
	}
}

func r2UTC(hour, minute int) time.Time {
	return time.Date(2026, 9, 25, hour, minute, 0, 0, time.UTC)
}

func r2Row(at time.Time, group map[string]any, dimensions map[string]any) map[string]any {
	dimensions = cloneR2Map(dimensions)
	dimensions["datetimeFiveMinutes"] = at.UTC().Format(time.RFC3339)
	row := map[string]any{"dimensions": dimensions}
	for key, value := range group {
		row[key] = value
	}
	return row
}

func cloneR2Map(values map[string]any) map[string]any {
	copy := make(map[string]any, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}

func r2MetricAttrs(attrs []telemetry.Attr) map[string]string {
	values := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		values[attr.Key] = attr.Value
	}
	return values
}

func TestRegisterInstallsAllSixR2CollectorsByDefault(t *testing.T) {
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "synthetic-account"
	registry := r2Register(t, &cfg, &r2TestAPI{})
	want := map[string]bool{
		"r2.bandwidth":           true,
		"r2.catalog_data":        true,
		"r2.catalog_maintenance": true,
		"r2.operations":          true,
		"r2.storage":             true,
		"r2.sql":                 true,
	}
	entries := registry.Entries()
	if len(entries) != len(want) {
		t.Fatalf("registered collector count = %d, want %d", len(entries), len(want))
	}
	for _, entry := range entries {
		name := entry.Collector.Name()
		if !want[name] {
			t.Errorf("unexpected collector %q", name)
		}
		delete(want, name)
		window, ok := entry.Collector.(collector.WindowCollector)
		if !ok {
			t.Errorf("collector %q is not windowed", name)
			continue
		}
		if entry.Interval != 5*time.Minute || entry.InitialLookback != 30*time.Minute || entry.MaxWindow != time.Hour || window.Lag() != 10*time.Minute {
			t.Errorf("collector %q has unexpected default timing: interval=%s lookback=%s maxWindow=%s lag=%s", name, entry.Interval, entry.InitialLookback, entry.MaxWindow, window.Lag())
		}
	}
	if len(want) > 0 {
		t.Errorf("collectors not registered: %v", sortedR2Keys(want))
	}
}

func TestFrozenR2MetricMappingsAndResourceAttributes(t *testing.T) {
	from := r2UTC(10, 0)
	to := from.Add(5 * time.Minute)
	type expectedMetric struct {
		name, kind string
		value      float64
	}
	tests := []struct {
		name, collectorName, dataset, attrKey, attrValue string
		group, dimensions                                map[string]any
		metrics                                          []expectedMetric
	}{
		{name: "bandwidth", collectorName: "r2.bandwidth", dataset: testBandwidthDataset, attrKey: semconv.AttrR2BucketName, attrValue: "bucket-a", group: map[string]any{"sum": map[string]any{"bytesDownload": 9, "bytesUpload": 4}}, dimensions: map[string]any{"bucketName": "bucket-a"}, metrics: []expectedMetric{{semconv.MetricR2BandwidthDownloadBytes, "counter", 9}, {semconv.MetricR2BandwidthUploadBytes, "counter", 4}}},
		{name: "catalog data", collectorName: "r2.catalog_data", dataset: testCatalogDataDataset, attrKey: semconv.AttrR2CatalogNamespaceName, attrValue: "namespace-a", group: map[string]any{"count": 5}, dimensions: map[string]any{"namespaceName": "namespace-a"}, metrics: []expectedMetric{{semconv.MetricR2CatalogDataOperations, "counter", 5}}},
		{name: "catalog maintenance", collectorName: "r2.catalog_maintenance", dataset: testCatalogMaintenanceDataset, attrKey: semconv.AttrR2CatalogNamespaceName, attrValue: "namespace-a", group: map[string]any{"count": 6}, dimensions: map[string]any{"namespaceName": "namespace-a"}, metrics: []expectedMetric{{semconv.MetricR2CatalogMaintenanceJobs, "counter", 6}}},
		{name: "operations", collectorName: "r2.operations", dataset: testOperationsDataset, attrKey: semconv.AttrR2BucketName, attrValue: "bucket-a", group: map[string]any{"sum": map[string]any{"requests": 7}}, dimensions: map[string]any{"bucketName": "bucket-a"}, metrics: []expectedMetric{{semconv.MetricR2Requests, "counter", 7}}},
		{name: "sql", collectorName: "r2.sql", dataset: testSQLDataset, attrKey: semconv.AttrR2SQLBucketName, attrValue: "bucket-a", group: map[string]any{"count": 8}, dimensions: map[string]any{"bucket": "bucket-a"}, metrics: []expectedMetric{{semconv.MetricR2SQLQueries, "counter", 8}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := r2TestSettings(test.dataset)
			api := &r2TestAPI{settings: map[string]cfapi.DatasetSettings{test.dataset: settings}}
			row := r2Row(from, test.group, test.dimensions)
			row["objectName"] = "must-not-be-an-attribute"
			row["tableName"] = "must-not-be-an-attribute"
			row["queryText"] = "must-not-be-an-attribute"
			row["dimensions"].(map[string]any)["objectName"] = "must-not-be-an-attribute"
			row["dimensions"].(map[string]any)["tableName"] = "must-not-be-an-attribute"
			api.query = func(cfapi.GraphQLRequest) ([]map[string]any, error) { return []map[string]any{row}, nil }
			window := r2WindowCollector(t, r2TestConfig(test.collectorName), api, test.collectorName)
			out := &telemetry.Buffer{}
			mark, err := window.CollectWindow(context.Background(), from, to, out)
			if err != nil {
				t.Fatal(err)
			}
			if !mark.Equal(to) {
				t.Fatalf("mark = %s, want %s", mark, to)
			}
			if len(out.Metrics) != len(test.metrics) {
				t.Fatalf("metric count = %d, want %d: %#v", len(out.Metrics), len(test.metrics), out.Metrics)
			}
			for _, want := range test.metrics {
				found := false
				for _, metric := range out.Metrics {
					if metric.Name != want.name {
						continue
					}
					found = true
					attrs := r2MetricAttrs(metric.Attrs)
					if metric.Kind != want.kind || metric.Value != want.value || len(attrs) != 1 || attrs[test.attrKey] != test.attrValue {
						t.Errorf("metric = %#v, want %s %s=%v with only %s=%q", metric, want.kind, want.name, want.value, test.attrKey, test.attrValue)
					}
				}
				if !found {
					t.Errorf("missing metric %s", want.name)
				}
			}
			request := api.calls[0].request
			if request.Scope != cfapi.AccountScope || request.ScopeID != "synthetic-account" || request.Dataset != test.dataset {
				t.Fatalf("query used unexpected account or dataset: %#v", request)
			}
			for _, field := range request.WantedFields {
				if strings.Contains(strings.ToLower(field), "object") || strings.Contains(strings.ToLower(field), "table") || strings.Contains(strings.ToLower(field), "querytext") {
					t.Errorf("query selected forbidden dimension %q", field)
				}
			}
		})
	}
}

func TestStorageUsesLatestCompleteBucketPerResourceAndOmitsObjectNames(t *testing.T) {
	from := r2UTC(10, 0)
	to := from.Add(20 * time.Minute)
	settings := r2TestSettings(testStorageDataset)
	api := &r2TestAPI{settings: map[string]cfapi.DatasetSettings{testStorageDataset: settings}}
	api.query = func(cfapi.GraphQLRequest) ([]map[string]any, error) {
		rows := []map[string]any{
			r2Row(from, map[string]any{"max": map[string]any{"payloadSize": 10, "objectCount": 1}}, map[string]any{"bucketName": "bucket-a", "objectName": "private-object-a", "tableName": "private-table-a"}),
			r2Row(from.Add(5*time.Minute), map[string]any{"max": map[string]any{"payloadSize": 20, "objectCount": 2}}, map[string]any{"bucketName": "bucket-a", "objectName": "private-object-b", "tableName": "private-table-b"}),
			r2Row(from.Add(10*time.Minute), map[string]any{"max": map[string]any{"payloadSize": 30, "objectCount": 3}}, map[string]any{"bucketName": "bucket-a", "objectName": "private-object-c", "tableName": "private-table-c"}),
			r2Row(from.Add(5*time.Minute), map[string]any{"max": map[string]any{"payloadSize": 90, "objectCount": 9}}, map[string]any{"bucketName": "bucket-b", "objectName": "private-object-d", "tableName": "private-table-d"}),
			r2Row(from.Add(15*time.Minute), map[string]any{"max": map[string]any{"payloadSize": 80, "objectCount": 8}}, map[string]any{"bucketName": "bucket-b", "objectName": "private-object-e", "tableName": "private-table-e"}),
		}
		return rows, nil
	}
	window := r2WindowCollector(t, r2TestConfig("r2.storage"), api, "r2.storage")
	out := &telemetry.Buffer{}
	mark, err := window.CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) {
		t.Fatalf("mark = %s, want %s", mark, to)
	}
	want := map[string]float64{
		semconv.MetricR2StoragePayloadBytes + "/bucket-a": 30,
		semconv.MetricR2StorageObjects + "/bucket-a":      3,
		semconv.MetricR2StoragePayloadBytes + "/bucket-b": 80,
		semconv.MetricR2StorageObjects + "/bucket-b":      8,
	}
	if len(out.Metrics) != len(want) {
		t.Fatalf("metric count = %d, want %d: %#v", len(out.Metrics), len(want), out.Metrics)
	}
	for _, metric := range out.Metrics {
		attrs := r2MetricAttrs(metric.Attrs)
		key := metric.Name + "/" + attrs[semconv.AttrR2BucketName]
		if metric.Kind != "gauge" || metric.Value != want[key] || len(attrs) != 1 {
			t.Errorf("metric = %#v, want latest gauge %s=%v with one bucket attribute", metric, key, want[key])
		}
		delete(want, key)
	}
	if len(want) > 0 {
		t.Errorf("missing storage gauges: %v", want)
	}
	wanted := api.calls[0].request.WantedFields
	for _, field := range []string{"max.payloadSize", "max.objectCount", "dimensions.datetimeFiveMinutes", "dimensions.bucketName"} {
		if !r2HasString(wanted, field) {
			t.Errorf("storage query omitted required/allowed field %q: %v", field, wanted)
		}
	}
	for _, field := range wanted {
		if strings.Contains(strings.ToLower(field), "objectname") || strings.Contains(strings.ToLower(field), "tablename") {
			t.Errorf("storage query selected private object/table field %q", field)
		}
	}
}

func TestRequiredFieldsFailClosedAndOptionalNamesBecomeAccountAggregate(t *testing.T) {
	from := r2UTC(10, 0)
	to := from.Add(5 * time.Minute)
	for _, missing := range []string{"sum_requests", "dimensions_datetimeFiveMinutes"} {
		t.Run("missing "+missing, func(t *testing.T) {
			settings := r2TestSettings(testOperationsDataset)
			settings.AvailableFields = withoutR2String(settings.AvailableFields, missing)
			api := &r2TestAPI{settings: map[string]cfapi.DatasetSettings{testOperationsDataset: settings}}
			window := r2WindowCollector(t, r2TestConfig("r2.operations"), api, "r2.operations")
			out := &telemetry.Buffer{}
			mark, err := window.CollectWindow(context.Background(), from, to, out)
			if err == nil || !mark.Equal(from) || len(api.calls) != 0 || len(out.Metrics) != 0 {
				t.Fatalf("missing required field %q was not rejected before query: mark=%s err=%v calls=%d metrics=%d", missing, mark, err, len(api.calls), len(out.Metrics))
			}
		})
	}
	t.Run("required fields exceed field cap", func(t *testing.T) {
		settings := r2TestSettings(testOperationsDataset)
		settings.MaxNumberOfFields = 1
		api := &r2TestAPI{settings: map[string]cfapi.DatasetSettings{testOperationsDataset: settings}}
		window := r2WindowCollector(t, r2TestConfig("r2.operations"), api, "r2.operations")
		out := &telemetry.Buffer{}
		mark, err := window.CollectWindow(context.Background(), from, to, out)
		if err == nil || !mark.Equal(from) || len(api.calls) != 0 || len(out.Metrics) != 0 {
			t.Fatalf("field cap below required value/time fields was not rejected: mark=%s err=%v calls=%d metrics=%d", mark, err, len(api.calls), len(out.Metrics))
		}
	})
	for _, tc := range []struct {
		name       string
		available  []string
		maxFields  int
		wantFields []string
	}{
		{name: "optional not advertised", available: []string{"sum_requests", "dimensions_datetimeFiveMinutes"}, maxFields: 20, wantFields: []string{"sum.requests", "dimensions.datetimeFiveMinutes"}},
		{name: "optional excluded by field cap", available: []string{"sum_requests", "dimensions_datetimeFiveMinutes", "dimensions_bucketName"}, maxFields: 2, wantFields: []string{"sum.requests", "dimensions.datetimeFiveMinutes"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := r2TestSettings(testOperationsDataset)
			settings.AvailableFields = tc.available
			settings.MaxNumberOfFields = tc.maxFields
			api := &r2TestAPI{settings: map[string]cfapi.DatasetSettings{testOperationsDataset: settings}}
			api.query = func(cfapi.GraphQLRequest) ([]map[string]any, error) {
				return []map[string]any{r2Row(from, map[string]any{"sum": map[string]any{"requests": 12}}, map[string]any{"bucketName": "must-be-ignored"})}, nil
			}
			window := r2WindowCollector(t, r2TestConfig("r2.operations"), api, "r2.operations")
			out := &telemetry.Buffer{}
			mark, err := window.CollectWindow(context.Background(), from, to, out)
			if err != nil {
				t.Fatal(err)
			}
			if !mark.Equal(to) || len(api.calls) != 1 || !equalR2Strings(api.calls[0].request.WantedFields, tc.wantFields) {
				t.Fatalf("query fields = %v, want %v", api.calls[0].request.WantedFields, tc.wantFields)
			}
			if len(out.Metrics) != 1 || out.Metrics[0].Value != 12 || len(out.Metrics[0].Attrs) != 0 {
				t.Fatalf("optional field absence did not produce an account aggregate: %#v", out.Metrics)
			}
		})
	}
}

func TestInvalidResourceNamesFoldIntoUnlabeledAggregate(t *testing.T) {
	from := r2UTC(10, 0)
	to := from.Add(20 * time.Minute)
	api := newR2TestAPI(testOperationsDataset)
	api.query = func(cfapi.GraphQLRequest) ([]map[string]any, error) {
		return []map[string]any{
			r2Row(from, map[string]any{"sum": map[string]any{"requests": 1}}, map[string]any{"bucketName": ""}),
			r2Row(from.Add(5*time.Minute), map[string]any{"sum": map[string]any{"requests": 2}}, map[string]any{"bucketName": " \t "}),
			r2Row(from.Add(10*time.Minute), map[string]any{"sum": map[string]any{"requests": 3}}, map[string]any{"bucketName": strings.Repeat("x", 129)}),
			r2Row(from.Add(15*time.Minute), map[string]any{"sum": map[string]any{"requests": 4}}, map[string]any{"bucketName": "valid-bucket"}),
		}, nil
	}
	window := r2WindowCollector(t, r2TestConfig("r2.operations"), api, "r2.operations")
	out := &telemetry.Buffer{}
	_, err := window.CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Metrics) != 2 {
		t.Fatalf("metrics = %#v, want one folded aggregate and one valid bucket", out.Metrics)
	}
	for _, metric := range out.Metrics {
		attrs := r2MetricAttrs(metric.Attrs)
		switch {
		case len(attrs) == 0 && metric.Value == 6:
		case len(attrs) == 1 && attrs[semconv.AttrR2BucketName] == "valid-bucket" && metric.Value == 4:
		default:
			t.Errorf("unexpected folded metric: %#v", metric)
		}
	}
}

func TestSeriesCapKeepsSortedKeysAndDisclosesOnlyDropCount(t *testing.T) {
	from := r2UTC(10, 0)
	to := from.Add(15 * time.Minute)
	api := newR2TestAPI(testOperationsDataset)
	api.query = func(cfapi.GraphQLRequest) ([]map[string]any, error) {
		return []map[string]any{
			r2Row(from, map[string]any{"sum": map[string]any{"requests": 1}}, map[string]any{"bucketName": "private-series-c"}),
			r2Row(from.Add(5*time.Minute), map[string]any{"sum": map[string]any{"requests": 1}}, map[string]any{"bucketName": "private-series-a"}),
			r2Row(from.Add(10*time.Minute), map[string]any{"sum": map[string]any{"requests": 1}}, map[string]any{"bucketName": "private-series-b"}),
		}, nil
	}
	cfg := r2TestConfig("r2.operations")
	cfg.Platform.MaxMetricSeriesPerWindow = 2
	window := r2WindowCollector(t, cfg, api, "r2.operations")
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	out := &telemetry.Buffer{}
	_, err := window.CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Metrics) != 2 {
		t.Fatalf("emitted %d metric series, want cap 2", len(out.Metrics))
	}
	got := []string{r2MetricAttrs(out.Metrics[0].Attrs)[semconv.AttrR2BucketName], r2MetricAttrs(out.Metrics[1].Attrs)[semconv.AttrR2BucketName]}
	if !equalR2Strings(got, []string{"private-series-a", "private-series-b"}) {
		t.Fatalf("retained series = %v, want the first two sorted keys", got)
	}
	logText := logs.String()
	if !strings.Contains(logText, "dropped=1") {
		t.Fatalf("drop count was not disclosed once in the log: %s", logText)
	}
	if strings.Count(logText, "platform metric series dropped") != 1 {
		t.Fatalf("expected one drop disclosure, got log: %s", logText)
	}
	for _, value := range []string{"private-series-a", "private-series-b", "private-series-c"} {
		if strings.Contains(logText, value) {
			t.Errorf("drop disclosure exposed resource value %q", value)
		}
	}
}

func TestSaturatedQueryBisectionPreservesHalfOpenWindowBoundaries(t *testing.T) {
	from := r2UTC(10, 0)
	to := from.Add(20 * time.Minute)
	api := newR2TestAPI(testOperationsDataset)
	api.query = func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
		if request.To.Sub(request.From) > 5*time.Minute {
			return nil, fmt.Errorf("GraphQL dataset %s window %s..%s saturated limit %d", request.Dataset, request.From.Format(time.RFC3339), request.To.Format(time.RFC3339), request.Limit)
		}
		return []map[string]any{r2Row(request.From, map[string]any{"sum": map[string]any{"requests": 1}}, map[string]any{"bucketName": "bucket-a"})}, nil
	}
	window := r2WindowCollector(t, r2TestConfig("r2.operations"), api, "r2.operations")
	out := &telemetry.Buffer{}
	mark, err := window.CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) {
		t.Fatalf("mark = %s, want %s", mark, to)
	}
	var leaves []cfapi.GraphQLRequest
	for _, call := range api.calls {
		if call.err == nil {
			leaves = append(leaves, call.request)
		}
	}
	sort.Slice(leaves, func(i, j int) bool { return leaves[i].From.Before(leaves[j].From) })
	if len(leaves) != 4 {
		t.Fatalf("successful leaf query count = %d, want 4: %#v", len(leaves), leaves)
	}
	for i, leaf := range leaves {
		wantFrom := from.Add(time.Duration(i) * 5 * time.Minute)
		wantTo := wantFrom.Add(5 * time.Minute)
		if !leaf.From.Equal(wantFrom) || !leaf.To.Equal(wantTo) {
			t.Fatalf("leaf %d = [%s,%s), want [%s,%s)", i, leaf.From, leaf.To, wantFrom, wantTo)
		}
		if i > 0 && !leaves[i-1].To.Equal(leaf.From) {
			t.Fatalf("adjacent leaves have a gap or overlap: previous end %s, next start %s", leaves[i-1].To, leaf.From)
		}
	}
	if len(out.Metrics) != 1 || out.Metrics[0].Value != 4 {
		t.Fatalf("bisection emitted %#v, want one counter totaling the four non-overlapping buckets", out.Metrics)
	}
}

func TestFifteenMinuteSaturationKeepsFiveMinuteBucketBoundaries(t *testing.T) {
	from := r2UTC(10, 0)
	to := from.Add(15 * time.Minute)
	api := newR2TestAPI(testOperationsDataset)
	api.query = func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
		if request.From != request.From.Truncate(5*time.Minute) || request.To != request.To.Truncate(5*time.Minute) {
			return nil, fmt.Errorf("query split cut a five-minute bucket: [%s,%s)", request.From, request.To)
		}
		if request.To.Sub(request.From) > 5*time.Minute {
			return nil, fmt.Errorf("GraphQL dataset %s window %s..%s saturated limit %d", request.Dataset, request.From.Format(time.RFC3339), request.To.Format(time.RFC3339), request.Limit)
		}
		return []map[string]any{r2Row(request.From, map[string]any{"sum": map[string]any{"requests": 1}}, map[string]any{"bucketName": "bucket-a"})}, nil
	}
	window := r2WindowCollector(t, r2TestConfig("r2.operations"), api, "r2.operations")
	out := &telemetry.Buffer{}
	mark, err := window.CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) || len(out.Metrics) != 1 || out.Metrics[0].Value != 3 {
		t.Fatalf("15-minute split: mark=%s metrics=%#v, want three complete buckets", mark, out.Metrics)
	}
}

func TestIrreducibleFiveMinuteSaturationFailsWithoutEmissionOrAdvance(t *testing.T) {
	from := r2UTC(10, 0)
	to := from.Add(5 * time.Minute)
	api := newR2TestAPI(testOperationsDataset)
	api.query = func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
		return nil, fmt.Errorf("GraphQL dataset %s window %s..%s saturated limit %d", request.Dataset, request.From.Format(time.RFC3339), request.To.Format(time.RFC3339), request.Limit)
	}
	window := r2WindowCollector(t, r2TestConfig("r2.operations"), api, "r2.operations")
	out := &telemetry.Buffer{}
	mark, err := window.CollectWindow(context.Background(), from, to, out)
	if err == nil || !strings.Contains(err.Error(), "five-minute") {
		t.Fatalf("irreducible saturated interval returned %v, want five-minute saturation error", err)
	}
	if !mark.Equal(from) || len(out.Metrics) != 0 {
		t.Fatalf("irreducible query advanced or emitted: mark=%s metrics=%#v", mark, out.Metrics)
	}
	foundBucket := false
	for _, call := range api.calls {
		if call.request.To.Sub(call.request.From) == 5*time.Minute {
			foundBucket = true
			break
		}
	}
	if !foundBucket {
		t.Fatal("query did not reach the irreducible five-minute bucket")
	}
}

func TestAdjacentWindowsKeepExactHalfOpenBucketOwnership(t *testing.T) {
	from := r2UTC(10, 0)
	middle := from.Add(10 * time.Minute)
	to := middle.Add(10 * time.Minute)
	api := newR2TestAPI(testOperationsDataset)
	api.query = func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
		var rows []map[string]any
		for at := request.From; at.Before(request.To); at = at.Add(5 * time.Minute) {
			rows = append(rows, r2Row(at, map[string]any{"sum": map[string]any{"requests": 1}}, map[string]any{"bucketName": "bucket-a"}))
		}
		return rows, nil
	}
	window := r2WindowCollector(t, r2TestConfig("r2.operations"), api, "r2.operations")
	first := &telemetry.Buffer{}
	firstMark, err := window.CollectWindow(context.Background(), from, middle, first)
	if err != nil {
		t.Fatal(err)
	}
	second := &telemetry.Buffer{}
	secondMark, err := window.CollectWindow(context.Background(), middle, to, second)
	if err != nil {
		t.Fatal(err)
	}
	if !firstMark.Equal(middle) || !secondMark.Equal(to) || len(api.calls) != 2 {
		t.Fatalf("marks/calls = %s, %s, %d; want adjacent windows through %s", firstMark, secondMark, len(api.calls), to)
	}
	if !api.calls[0].request.From.Equal(from) || !api.calls[0].request.To.Equal(middle) || !api.calls[1].request.From.Equal(middle) || !api.calls[1].request.To.Equal(to) {
		t.Fatalf("query windows = [%s,%s), [%s,%s), want [%s,%s), [%s,%s)", api.calls[0].request.From, api.calls[0].request.To, api.calls[1].request.From, api.calls[1].request.To, from, middle, middle, to)
	}
	if len(first.Metrics) != 1 || first.Metrics[0].Value != 2 || len(second.Metrics) != 1 || second.Metrics[0].Value != 2 {
		t.Fatalf("adjacent windows emitted %#v and %#v, want two buckets each", first.Metrics, second.Metrics)
	}
}

func TestGraphQLUsesLivePageDurationAndRetentionLimits(t *testing.T) {
	from := time.Now().UTC().Truncate(5 * time.Minute).Add(-time.Hour)
	to := from.Add(20 * time.Minute)
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode GraphQL request: %v", err)
			return
		}
		queries = append(queries, body.Query)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(body.Query, "settings{") {
			_, _ = io.WriteString(w, `{"data":{"viewer":{"accounts":[{"settings":{"r2OperationsAdaptiveGroups":{"enabled":true,"availableFields":["sum_requests","dimensions_datetimeFiveMinutes","dimensions_bucketName"],"maxNumberOfFields":10,"maxDuration":600,"notOlderThan":86400,"maxPageSize":23}}}]}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"viewer":{"accounts":[{"r2OperationsAdaptiveGroups":[]}]}}}`)
	}))
	defer server.Close()
	api := cfapi.New(config.CloudflareConfig{APIBase: server.URL, Timeout: time.Second, MaxResponseBytes: 4096})
	window := r2WindowCollector(t, r2TestConfig("r2.operations"), api, "r2.operations")
	out := &telemetry.Buffer{}
	mark, err := window.CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) || len(out.Metrics) != 0 {
		t.Fatalf("GraphQL window mark/metrics = %s/%#v, want %s/no metrics", mark, out.Metrics, to)
	}
	if len(queries) != 3 {
		t.Fatalf("GraphQL query count = %d, want settings plus two maxDuration windows", len(queries))
	}
	expected := [][2]time.Time{{from, from.Add(10 * time.Minute)}, {from.Add(10 * time.Minute), to}}
	for i, query := range queries[1:] {
		start, end := expected[i][0], expected[i][1]
		if !strings.Contains(query, fmt.Sprintf(`datetime_geq:%q`, start.Format(time.RFC3339))) || !strings.Contains(query, fmt.Sprintf(`datetime_lt:%q`, end.Format(time.RFC3339))) {
			t.Errorf("GraphQL query %d does not preserve half-open bounded window [%s,%s): %s", i, start, end, query)
		}
		if !strings.Contains(query, "limit:23") {
			t.Errorf("GraphQL query %d did not cap at live maxPageSize: %s", i, query)
		}
	}
	if !strings.Contains(queries[1], "dimensions{bucketName datetimeFiveMinutes}") && !strings.Contains(queries[1], "dimensions{datetimeFiveMinutes bucketName}") {
		t.Errorf("data query did not select the advertised safe dimensions: %s", queries[1])
	}

	queries = nil
	from = time.Now().UTC().Truncate(5 * time.Minute).Add(-time.Hour)
	to = from.Add(5 * time.Minute)
	retentionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode GraphQL retention request: %v", err)
			return
		}
		queries = append(queries, body.Query)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"viewer":{"accounts":[{"settings":{"r2OperationsAdaptiveGroups":{"enabled":true,"availableFields":["sum_requests","dimensions_datetimeFiveMinutes"],"maxNumberOfFields":10,"maxDuration":600,"notOlderThan":60,"maxPageSize":23}}}]}}}`)
	}))
	defer retentionServer.Close()
	retentionAPI := cfapi.New(config.CloudflareConfig{APIBase: retentionServer.URL, Timeout: time.Second, MaxResponseBytes: 4096})
	retentionWindow := r2WindowCollector(t, r2TestConfig("r2.operations"), retentionAPI, "r2.operations")
	retentionMark, retentionErr := retentionWindow.CollectWindow(context.Background(), from, to, &telemetry.Buffer{})
	var gap *cfapi.RetentionGapError
	if !errors.As(retentionErr, &gap) || !retentionMark.Equal(from) || len(queries) != 1 {
		t.Fatalf("retention result = mark %s, err %v, queries %d; want retention failure before data query", retentionMark, retentionErr, len(queries))
	}
}

func TestGraphQLPageLimitNeverExceedsTenThousand(t *testing.T) {
	from := time.Now().UTC().Truncate(5 * time.Minute).Add(-10 * time.Minute)
	to := from.Add(5 * time.Minute)
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode GraphQL request: %v", err)
			return
		}
		queries = append(queries, body.Query)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(body.Query, "settings{") {
			_, _ = io.WriteString(w, `{"data":{"viewer":{"accounts":[{"settings":{"r2OperationsAdaptiveGroups":{"enabled":true,"availableFields":["sum_requests","dimensions_datetimeFiveMinutes"],"maxNumberOfFields":10,"maxDuration":3600,"notOlderThan":86400,"maxPageSize":20000}}}]}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"viewer":{"accounts":[{"r2OperationsAdaptiveGroups":[]}]}}}`)
	}))
	defer server.Close()
	api := cfapi.New(config.CloudflareConfig{APIBase: server.URL, Timeout: time.Second, MaxResponseBytes: 4096})
	window := r2WindowCollector(t, r2TestConfig("r2.operations"), api, "r2.operations")
	mark, err := window.CollectWindow(context.Background(), from, to, &telemetry.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) || len(queries) != 2 || !strings.Contains(queries[1], "limit:10000") {
		t.Fatalf("mark/queries = %s/%v, want one data query capped at 10000", mark, queries)
	}
}

func sortedR2Keys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func r2HasString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func withoutR2String(values []string, removed string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if !strings.EqualFold(value, removed) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func equalR2Strings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
