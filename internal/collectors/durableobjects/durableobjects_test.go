package durableobjects

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	otellog "go.opentelemetry.io/otel/log"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

const testAccountID = "00000000000000000000000000000001"

type queryHandler func(context.Context, cfapi.GraphQLRequest, any) error

type durableObjectsTestAPI struct {
	settings map[string]cfapi.DatasetSettings
	rows     map[string][]map[string]any
	query    queryHandler
	requests []cfapi.GraphQLRequest
}

func (a *durableObjectsTestAPI) Get(context.Context, string, url.Values, any) error { return nil }
func (a *durableObjectsTestAPI) Accounts(context.Context) ([]cfapi.Account, error) {
	return nil, nil
}
func (a *durableObjectsTestAPI) Zones(context.Context) ([]cfapi.Zone, error) { return nil, nil }
func (a *durableObjectsTestAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) {
	return nil, nil
}
func (a *durableObjectsTestAPI) DatasetSettings(_ context.Context, _ cfapi.Scope, _ string, dataset string) (cfapi.DatasetSettings, error) {
	settings, ok := a.settings[dataset]
	if !ok {
		return cfapi.DatasetSettings{}, errors.New("missing test settings")
	}
	return settings, nil
}
func (a *durableObjectsTestAPI) Query(ctx context.Context, request cfapi.GraphQLRequest, out any) error {
	a.requests = append(a.requests, request)
	if a.query != nil {
		return a.query(ctx, request, out)
	}
	rows := a.rows[request.Dataset]
	encoded, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, out)
}

type recordedMetric struct {
	kind  string
	name  string
	value float64
	attrs []telemetry.Attr
}

type recordingEmitter struct{ metrics []recordedMetric }

func (e *recordingEmitter) Gauge(_ context.Context, name string, value float64, attrs ...telemetry.Attr) error {
	e.metrics = append(e.metrics, recordedMetric{"gauge", name, value, append([]telemetry.Attr(nil), attrs...)})
	return nil
}
func (e *recordingEmitter) Counter(_ context.Context, name string, value float64, attrs ...telemetry.Attr) error {
	e.metrics = append(e.metrics, recordedMetric{"counter", name, value, append([]telemetry.Attr(nil), attrs...)})
	return nil
}
func (*recordingEmitter) Histogram(context.Context, string, float64, ...telemetry.Attr) error {
	return nil
}
func (*recordingEmitter) LogEvent(context.Context, string, string, time.Time, otellog.Severity, ...telemetry.Attr) error {
	return nil
}
func (*recordingEmitter) Span(context.Context, telemetry.SpanSpec) error { return nil }

func platformConfig() *config.Config {
	cfg := config.Default()
	cfg.Cloudflare.AccountID = testAccountID
	return &cfg
}

func registerFamily(cfg *config.Config, api cfapi.Client) *collector.Registry {
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: cfg, API: api, Registry: registry})
	return registry
}

func collectorEntry(t *testing.T, registry *collector.Registry, name string) collector.Entry {
	t.Helper()
	for _, entry := range registry.Entries() {
		if entry.Collector.Name() == name {
			return entry
		}
	}
	t.Fatalf("collector %q was not registered", name)
	return collector.Entry{}
}

func runWindow(t *testing.T, cfg *config.Config, api *durableObjectsTestAPI, name string, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	t.Helper()
	entry := collectorEntry(t, registerFamily(cfg, api), name)
	window, ok := entry.Collector.(collector.WindowCollector)
	if !ok {
		t.Fatalf("collector %q is not a window collector", name)
	}
	return window.CollectWindow(context.Background(), from, to, out)
}

func standardSettings(required ...string) cfapi.DatasetSettings {
	return cfapi.DatasetSettings{
		Enabled:           true,
		AvailableFields:   append([]string(nil), required...),
		MaxNumberOfFields: 30,
		MaxDuration:       3600,
		NotOlderThan:      90 * 24 * 3600,
		MaxPageSize:       100,
	}
}

func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func TestRegisterInstallsFourWindowCollectors(t *testing.T) {
	cfg := platformConfig()
	registry := registerFamily(cfg, &durableObjectsTestAPI{})
	entries := registry.Entries()
	if len(entries) != 4 {
		t.Fatalf("registered %d collectors, want four datasets", len(entries))
	}
	want := map[string]bool{
		"durableobjects.invocations": true,
		"durableobjects.periodic":    true,
		"durableobjects.sql_storage": true,
		"durableobjects.subrequests": true,
	}
	for _, entry := range entries {
		name := entry.Collector.Name()
		if !want[name] {
			t.Errorf("unexpected collector %q", name)
		}
		delete(want, name)
		if entry.Interval != 5*time.Minute || entry.InitialLookback != 30*time.Minute || entry.MaxWindow != time.Hour {
			t.Errorf("%s registration = interval %s lookback %s max %s", name, entry.Interval, entry.InitialLookback, entry.MaxWindow)
		}
		if entry.Collector.DefaultInterval() != 5*time.Minute {
			t.Errorf("%s default interval = %s", name, entry.Collector.DefaultInterval())
		}
		if entry.Collector.(collector.WindowCollector).Lag() != 10*time.Minute {
			t.Errorf("%s lag = %s, want 10m", name, entry.Collector.(collector.WindowCollector).Lag())
		}
	}
	if len(want) != 0 {
		t.Errorf("missing collectors: %v", want)
	}

	cfg.Collectors["durableobjects.periodic"] = config.CollectorConfig{Enabled: false}
	registry = registerFamily(cfg, &durableObjectsTestAPI{})
	if len(registry.Entries()) != 3 {
		t.Fatalf("disabled dataset left %d registrations, want three", len(registry.Entries()))
	}
}

func TestRequiredSettingsFieldsFailClosedAndSQLNamespaceIsOptional(t *testing.T) {
	definitions := []struct {
		name, dataset, metricField string
	}{
		{"durableobjects.invocations", "durableObjectsInvocationsAdaptiveGroups", "sum.requests"},
		{"durableobjects.periodic", "durableObjectsPeriodicGroups", "sum.subrequests"},
		{"durableobjects.sql_storage", "durableObjectsSqlStorageGroups", "max.storedBytes"},
		{"durableobjects.subrequests", "durableObjectsSubrequestsAdaptiveGroups", "sum.requestBodySizeUncached"},
	}
	from := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	to := from.Add(10 * time.Minute)
	for _, definition := range definitions {
		for _, missing := range []string{definition.metricField, "dimensions.datetimeFiveMinutes"} {
			t.Run(definition.name+" missing "+missing, func(t *testing.T) {
				fields := []string{"dimensions.datetimeFiveMinutes", definition.metricField}
				available := fields[:0]
				for _, field := range fields {
					if field != missing {
						available = append(available, field)
					}
				}
				api := &durableObjectsTestAPI{settings: map[string]cfapi.DatasetSettings{definition.dataset: standardSettings(available...)}}
				mark, err := runWindow(t, platformConfig(), api, definition.name, from, to, &recordingEmitter{})
				if err == nil || !strings.Contains(err.Error(), "required field") {
					t.Fatalf("missing required %s returned mark %s, error %v", missing, mark, err)
				}
				if len(api.requests) != 0 {
					t.Fatalf("queried after required field %s was unavailable", missing)
				}
			})
		}
	}
}

func TestInvocationsAggregateAccountRowsWithoutAttributes(t *testing.T) {
	from := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	to := from.Add(15 * time.Minute)
	api := &durableObjectsTestAPI{
		settings: map[string]cfapi.DatasetSettings{
			"durableObjectsInvocationsAdaptiveGroups": {
				Enabled: true, AvailableFields: []string{"dimensions_datetimeFiveMinutes", "sum_requests"},
				MaxNumberOfFields: 2, MaxDuration: 600, NotOlderThan: 86400, MaxPageSize: 20000,
			},
		},
		rows: map[string][]map[string]any{
			"durableObjectsInvocationsAdaptiveGroups": {
				{"dimensions": map[string]any{"datetimeFiveMinutes": timestamp(from)}, "sum": map[string]any{"requests": 2.0}},
				{"dimensions": map[string]any{"datetimeFiveMinutes": timestamp(from.Add(5 * time.Minute))}, "sum": map[string]any{"requests": 3.0}},
				{"dimensions": map[string]any{"datetimeFiveMinutes": timestamp(from.Add(10 * time.Minute))}, "sum": map[string]any{"requests": 5.0}},
				{"dimensions": map[string]any{"datetimeFiveMinutes": timestamp(to)}, "sum": map[string]any{"requests": 100.0}},
			},
		},
	}
	emitter := &recordingEmitter{}
	mark, err := runWindow(t, platformConfig(), api, "durableobjects.invocations", from, to, emitter)
	if err != nil || !mark.Equal(to) {
		t.Fatalf("CollectWindow returned mark %s, error %v", mark, err)
	}
	if len(emitter.metrics) != 1 || emitter.metrics[0].kind != "counter" || emitter.metrics[0].name != semconv.MetricDurableObjectsRequests || emitter.metrics[0].value != 10 {
		t.Fatalf("account aggregate = %#v, want requests counter 10", emitter.metrics)
	}
	if len(emitter.metrics[0].attrs) != 0 {
		t.Fatalf("account aggregate emitted labels: %#v", emitter.metrics[0].attrs)
	}
	if len(api.requests) != 1 {
		t.Fatalf("sent %d queries, want one", len(api.requests))
	}
	request := api.requests[0]
	if request.Scope != cfapi.AccountScope || request.ScopeID != testAccountID || request.Dataset != "durableObjectsInvocationsAdaptiveGroups" {
		t.Fatalf("wrong request scope: %#v", request)
	}
	if request.Limit != 10000 {
		t.Fatalf("query limit = %d, want min(10000, maxPageSize) = 10000", request.Limit)
	}
	if len(request.WantedFields) != 2 || !containsField(request.WantedFields, "sum.requests") || !containsField(request.WantedFields, "dimensions.datetimeFiveMinutes") {
		t.Fatalf("wanted fields = %v", request.WantedFields)
	}
}

func TestAllCountersRespectAccountSeriesCapAndNeverEmitResourceLabels(t *testing.T) {
	from := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name, dataset, sourceField, metric string
	}{
		{"durableobjects.invocations", "durableObjectsInvocationsAdaptiveGroups", "requests", semconv.MetricDurableObjectsRequests},
		{"durableobjects.periodic", "durableObjectsPeriodicGroups", "subrequests", semconv.MetricDurableObjectsSubrequests},
		{"durableobjects.subrequests", "durableObjectsSubrequestsAdaptiveGroups", "requestBodySizeUncached", semconv.MetricDurableObjectsRequestBodyBytes},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := platformConfig()
			cfg.Platform.MaxMetricSeriesPerWindow = 1
			api := &durableObjectsTestAPI{
				settings: map[string]cfapi.DatasetSettings{test.dataset: standardSettings("dimensions.datetimeFiveMinutes", "sum."+test.sourceField)},
				rows: map[string][]map[string]any{test.dataset: {
					{"dimensions": map[string]any{"datetimeFiveMinutes": timestamp(from), "namespaceName": "example-namespace", "objectName": "example-object"}, "sum": map[string]any{test.sourceField: 2.0}},
					{"dimensions": map[string]any{"datetimeFiveMinutes": timestamp(from.Add(5 * time.Minute)), "namespaceName": "another-namespace", "objectName": "another-object"}, "sum": map[string]any{test.sourceField: 3.0}},
				},
				},
			}
			emitter := &recordingEmitter{}
			_, err := runWindow(t, cfg, api, test.name, from, from.Add(10*time.Minute), emitter)
			if err != nil {
				t.Fatalf("CollectWindow failed: %v", err)
			}
			if len(emitter.metrics) != 1 || emitter.metrics[0].kind != "counter" || emitter.metrics[0].name != test.metric || emitter.metrics[0].value != 5 {
				t.Fatalf("account-level series = %#v, want %s counter 5", emitter.metrics, test.metric)
			}
			if len(emitter.metrics[0].attrs) != 0 {
				t.Fatalf("resource/object identifiers became metric labels: %#v", emitter.metrics[0].attrs)
			}
		})
	}
}

func TestAdjacentWindowsOwnHalfOpenFiveMinuteBuckets(t *testing.T) {
	from := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	rows := make([]map[string]any, 0, 5)
	for i, value := range []float64{1, 2, 4, 8, 100} {
		rows = append(rows, map[string]any{
			"dimensions": map[string]any{"datetimeFiveMinutes": timestamp(from.Add(time.Duration(i*5) * time.Minute))},
			"sum":        map[string]any{"requests": value},
		})
	}
	api := &durableObjectsTestAPI{
		settings: map[string]cfapi.DatasetSettings{"durableObjectsInvocationsAdaptiveGroups": standardSettings("sum.requests", "dimensions.datetimeFiveMinutes")},
		rows:     map[string][]map[string]any{"durableObjectsInvocationsAdaptiveGroups": rows},
	}
	first, second := &recordingEmitter{}, &recordingEmitter{}
	mark1, err := runWindow(t, platformConfig(), api, "durableobjects.invocations", from, from.Add(10*time.Minute), first)
	if err != nil || !mark1.Equal(from.Add(10*time.Minute)) {
		t.Fatalf("first window mark=%s error=%v", mark1, err)
	}
	mark2, err := runWindow(t, platformConfig(), api, "durableobjects.invocations", mark1, from.Add(20*time.Minute), second)
	if err != nil || !mark2.Equal(from.Add(20*time.Minute)) {
		t.Fatalf("second window mark=%s error=%v", mark2, err)
	}
	if len(first.metrics) != 1 || first.metrics[0].value != 3 {
		t.Errorf("first half-open window aggregate = %#v, want 3", first.metrics)
	}
	if len(second.metrics) != 1 || second.metrics[0].value != 12 {
		t.Errorf("second half-open window aggregate = %#v, want 12", second.metrics)
	}
}

func TestPartialWindowEdgesUseOnlyCompleteBuckets(t *testing.T) {
	from := time.Date(2026, 9, 25, 12, 2, 0, 0, time.UTC)
	to := time.Date(2026, 9, 25, 12, 14, 0, 0, time.UTC)
	rows := []map[string]any{
		{"dimensions": map[string]any{"datetimeFiveMinutes": timestamp(from.Truncate(5 * time.Minute))}, "sum": map[string]any{"requests": 100.0}},
		{"dimensions": map[string]any{"datetimeFiveMinutes": timestamp(time.Date(2026, 9, 25, 12, 5, 0, 0, time.UTC))}, "sum": map[string]any{"requests": 7.0}},
		{"dimensions": map[string]any{"datetimeFiveMinutes": timestamp(time.Date(2026, 9, 25, 12, 10, 0, 0, time.UTC))}, "sum": map[string]any{"requests": 200.0}},
	}
	api := &durableObjectsTestAPI{
		settings: map[string]cfapi.DatasetSettings{"durableObjectsInvocationsAdaptiveGroups": standardSettings("sum.requests", "dimensions.datetimeFiveMinutes")},
		rows:     map[string][]map[string]any{"durableObjectsInvocationsAdaptiveGroups": rows},
	}
	emitter := &recordingEmitter{}
	mark, err := runWindow(t, platformConfig(), api, "durableobjects.invocations", from, to, emitter)
	if err != nil || !mark.Equal(time.Date(2026, 9, 25, 12, 10, 0, 0, time.UTC)) {
		t.Fatalf("partial window mark=%s error=%v, want 12:10", mark, err)
	}
	if len(api.requests) != 1 || !api.requests[0].From.Equal(time.Date(2026, 9, 25, 12, 5, 0, 0, time.UTC)) || !api.requests[0].To.Equal(mark) {
		t.Fatalf("partial bucket query range = %#v", api.requests)
	}
	if len(emitter.metrics) != 1 || emitter.metrics[0].value != 7 {
		t.Fatalf("partial edge rows included an incomplete bucket: %#v", emitter.metrics)
	}
}

func TestSaturatedQueryBisectsAtMinuteBoundariesWithoutGaps(t *testing.T) {
	from := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	to := from.Add(20 * time.Minute)
	var leaves []cfapi.GraphQLRequest
	api := &durableObjectsTestAPI{
		settings: map[string]cfapi.DatasetSettings{"durableObjectsInvocationsAdaptiveGroups": standardSettings("sum.requests", "dimensions.datetimeFiveMinutes")},
		query: func(_ context.Context, request cfapi.GraphQLRequest, out any) error {
			if request.To.Sub(request.From) > 5*time.Minute {
				return saturatedError(request)
			}
			leaves = append(leaves, request)
			rows := []map[string]any{{
				"dimensions": map[string]any{"datetimeFiveMinutes": timestamp(request.From)},
				"sum":        map[string]any{"requests": 1.0},
			}}
			encoded, _ := json.Marshal(rows)
			return json.Unmarshal(encoded, out)
		},
	}
	emitter := &recordingEmitter{}
	mark, err := runWindow(t, platformConfig(), api, "durableobjects.invocations", from, to, emitter)
	if err != nil || !mark.Equal(to) {
		t.Fatalf("split window mark=%s error=%v", mark, err)
	}
	if len(leaves) != 4 {
		t.Fatalf("successful query leaves = %d, want four", len(leaves))
	}
	sort.Slice(leaves, func(i, j int) bool { return leaves[i].From.Before(leaves[j].From) })
	cursor := from
	for _, leaf := range leaves {
		if !leaf.From.Equal(cursor) || leaf.From.Second() != 0 || leaf.From.Nanosecond() != 0 || leaf.To.Second() != 0 || leaf.To.Nanosecond() != 0 {
			t.Fatalf("leaf %s..%s does not continue minute-boundary cursor %s", leaf.From, leaf.To, cursor)
		}
		cursor = leaf.To
	}
	if !cursor.Equal(to) {
		t.Fatalf("covered through %s, want %s", cursor, to)
	}
	if len(emitter.metrics) != 1 || emitter.metrics[0].value != 4 {
		t.Fatalf("split aggregate = %#v, want counter 4", emitter.metrics)
	}
}

func TestFifteenMinuteSaturationKeepsFiveMinuteBucketBoundaries(t *testing.T) {
	from := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	to := from.Add(15 * time.Minute)
	api := &durableObjectsTestAPI{
		settings: map[string]cfapi.DatasetSettings{"durableObjectsInvocationsAdaptiveGroups": standardSettings("sum.requests", "dimensions.datetimeFiveMinutes")},
		query: func(_ context.Context, request cfapi.GraphQLRequest, out any) error {
			if !request.From.Equal(request.From.Truncate(5*time.Minute)) || !request.To.Equal(request.To.Truncate(5*time.Minute)) {
				return errors.New("query split cut a five-minute bucket")
			}
			if request.To.Sub(request.From) > 5*time.Minute {
				return saturatedError(request)
			}
			rows := []map[string]any{{"dimensions": map[string]any{"datetimeFiveMinutes": timestamp(request.From)}, "sum": map[string]any{"requests": 1.0}}}
			encoded, _ := json.Marshal(rows)
			return json.Unmarshal(encoded, out)
		},
	}
	emitter := &recordingEmitter{}
	mark, err := runWindow(t, platformConfig(), api, "durableobjects.invocations", from, to, emitter)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) || len(emitter.metrics) != 1 || emitter.metrics[0].value != 3 {
		t.Fatalf("15-minute split: mark=%s metrics=%#v, want three complete buckets", mark, emitter.metrics)
	}
}

func TestIrreducibleFiveMinuteSaturationFailsWithoutAdvancing(t *testing.T) {
	from := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute)
	api := &durableObjectsTestAPI{
		settings: map[string]cfapi.DatasetSettings{"durableObjectsInvocationsAdaptiveGroups": standardSettings("sum.requests", "dimensions.datetimeFiveMinutes")},
		query: func(_ context.Context, request cfapi.GraphQLRequest, _ any) error {
			return saturatedError(request)
		},
	}
	emitter := &recordingEmitter{}
	mark, err := runWindow(t, platformConfig(), api, "durableobjects.invocations", from, to, emitter)
	if err == nil || !strings.Contains(err.Error(), "saturated") {
		t.Fatalf("irreducible saturation returned mark %s, error %v", mark, err)
	}
	if !mark.Equal(from) || len(emitter.metrics) != 0 {
		t.Fatalf("failed saturation advanced to %s or emitted metrics %#v", mark, emitter.metrics)
	}
	foundBucket := false
	for _, request := range api.requests {
		if request.To.Sub(request.From) == 5*time.Minute {
			foundBucket = true
		}
	}
	if !foundBucket {
		t.Fatal("saturated query was not reduced to an irreducible five-minute bucket")
	}
}

func TestSQLStorageUsesLatestBucketMaximumAcrossNamespaces(t *testing.T) {
	from := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	to := from.Add(15 * time.Minute)
	api := &durableObjectsTestAPI{
		settings: map[string]cfapi.DatasetSettings{
			"durableObjectsSqlStorageGroups": standardSettings("max.storedBytes", "dimensions.datetimeFiveMinutes", "dimensions.namespaceName"),
		},
		rows: map[string][]map[string]any{
			"durableObjectsSqlStorageGroups": {
				{"dimensions": map[string]any{"datetimeFiveMinutes": timestamp(from.Add(5 * time.Minute)), "namespaceName": "ns-a"}, "max": map[string]any{"storedBytes": 900.0}},
				{"dimensions": map[string]any{"datetimeFiveMinutes": timestamp(from.Add(10 * time.Minute)), "namespaceName": "ns-a"}, "max": map[string]any{"storedBytes": 12.0}},
				{"dimensions": map[string]any{"datetimeFiveMinutes": timestamp(from.Add(10 * time.Minute)), "namespaceName": "ns-b"}, "max": map[string]any{"storedBytes": 24.0}},
				{"dimensions": map[string]any{"datetimeFiveMinutes": timestamp(to), "namespaceName": "ns-c"}, "max": map[string]any{"storedBytes": 999.0}},
			},
		},
	}
	emitter := &recordingEmitter{}
	mark, err := runWindow(t, platformConfig(), api, "durableobjects.sql_storage", from, to, emitter)
	if err != nil || !mark.Equal(to) {
		t.Fatalf("SQL storage mark=%s error=%v", mark, err)
	}
	if len(emitter.metrics) != 1 || emitter.metrics[0].kind != "gauge" || emitter.metrics[0].name != semconv.MetricDurableObjectsSQLStorageBytes || emitter.metrics[0].value != 24 {
		t.Fatalf("SQL storage aggregate = %#v, want latest-bucket max 24", emitter.metrics)
	}
	if len(emitter.metrics[0].attrs) != 0 {
		t.Fatalf("SQL storage leaked namespace attributes: %#v", emitter.metrics[0].attrs)
	}
	if !containsField(api.requests[0].WantedFields, "dimensions.namespaceName") {
		t.Fatalf("advertised optional namespace grouping not selected: %v", api.requests[0].WantedFields)
	}
	if api.requests[0].Limit != 100 {
		t.Fatalf("query limit = %d, want live maxPageSize 100", api.requests[0].Limit)
	}
}

func TestSQLStorageFallsBackToAccountAggregationWhenOptionalFieldUnavailableOrOverFieldLimit(t *testing.T) {
	from := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	to := from.Add(10 * time.Minute)
	for _, test := range []struct {
		name      string
		available []string
		maxFields int
	}{
		{"absent", []string{"max.storedBytes", "dimensions.datetimeFiveMinutes"}, 30},
		{"field budget", []string{"max.storedBytes", "dimensions.datetimeFiveMinutes", "dimensions.namespaceName"}, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := standardSettings(test.available...)
			settings.MaxNumberOfFields = test.maxFields
			api := &durableObjectsTestAPI{
				settings: map[string]cfapi.DatasetSettings{"durableObjectsSqlStorageGroups": settings},
				rows: map[string][]map[string]any{"durableObjectsSqlStorageGroups": {
					{"dimensions": map[string]any{"datetimeFiveMinutes": timestamp(from)}, "max": map[string]any{"storedBytes": 14.0}},
				}},
			}
			emitter := &recordingEmitter{}
			mark, err := runWindow(t, platformConfig(), api, "durableobjects.sql_storage", from, to, emitter)
			if err != nil || !mark.Equal(to) {
				t.Fatalf("account fallback mark=%s error=%v", mark, err)
			}
			if containsField(api.requests[0].WantedFields, "dimensions.namespaceName") {
				t.Fatalf("selected unavailable/over-budget optional field: %v", api.requests[0].WantedFields)
			}
			if len(emitter.metrics) != 1 || emitter.metrics[0].value != 14 || len(emitter.metrics[0].attrs) != 0 {
				t.Fatalf("account fallback = %#v", emitter.metrics)
			}
		})
	}
}

func TestSeriesCapDropsDeterministicallyAndDisclosesOnlyCount(t *testing.T) {
	previous := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	points := []metricPoint{
		{name: semconv.MetricDurableObjectsRequestBodyBytes, kind: counterMetric, value: 4},
		{name: semconv.MetricDurableObjectsSQLStorageBytes, kind: gaugeMetric, value: 8},
		{name: semconv.MetricDurableObjectsSubrequests, kind: counterMetric, value: 3},
		{name: semconv.MetricDurableObjectsRequests, kind: counterMetric, value: 1},
	}
	got := capMetricSeries("durableobjects.synthetic", points, 2, slog.Default())
	if len(got) != 2 || got[0].name != semconv.MetricDurableObjectsRequests || got[1].name != semconv.MetricDurableObjectsSQLStorageBytes {
		t.Fatalf("retained metric names = %#v, want sorted first two frozen names", got)
	}
	log := logs.String()
	if strings.Count(log, "platform metric series dropped") != 1 || !strings.Contains(log, "dropped=2") || !strings.Contains(log, "collector=durableobjects.synthetic") {
		t.Fatalf("drop disclosure = %q, want one collector and count-only warning", log)
	}
	for _, forbidden := range []string{"example-namespace", "example-object", "storedBytes", "requestBodySizeUncached"} {
		if strings.Contains(log, forbidden) {
			t.Fatalf("drop log disclosed a resource or source value %q: %s", forbidden, log)
		}
	}
}

func containsField(fields []string, want string) bool {
	for _, field := range fields {
		if strings.EqualFold(field, want) {
			return true
		}
		if prefix, suffix, ok := strings.Cut(want, "."); ok && strings.EqualFold(field, prefix+"_"+suffix) {
			return true
		}
	}
	return false
}

var _ cfapi.Client = (*durableObjectsTestAPI)(nil)
var _ telemetry.Emitter = (*recordingEmitter)(nil)
