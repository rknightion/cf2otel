package queues

import (
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

const queueFixtureAccount = "synthetic-account"

var queueFixtureFrom = time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)

type queueQueryCall struct {
	request cfapi.GraphQLRequest
	err     error
}

type queueTestAPI struct {
	settings map[string]cfapi.DatasetSettings
	query    func(cfapi.GraphQLRequest) ([]map[string]any, error)
	calls    []queueQueryCall
}

func (*queueTestAPI) Get(context.Context, string, url.Values, any) error {
	return errors.New("unexpected REST request")
}

func (f *queueTestAPI) Query(_ context.Context, request cfapi.GraphQLRequest, out any) error {
	var rows []map[string]any
	var err error
	if f.query != nil {
		rows, err = f.query(request)
	}
	f.calls = append(f.calls, queueQueryCall{request: request, err: err})
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, out)
}

func (f *queueTestAPI) DatasetSettings(_ context.Context, scope cfapi.Scope, scopeID, dataset string) (cfapi.DatasetSettings, error) {
	if scope != cfapi.AccountScope || scopeID != queueFixtureAccount {
		return cfapi.DatasetSettings{}, errors.New("unexpected Queue settings scope")
	}
	settings, ok := f.settings[dataset]
	if !ok {
		return cfapi.DatasetSettings{}, fmt.Errorf("unexpected Queue dataset %q", dataset)
	}
	return settings, nil
}

func (*queueTestAPI) Accounts(context.Context) ([]cfapi.Account, error) {
	return nil, errors.New("unexpected account discovery")
}
func (*queueTestAPI) Zones(context.Context) ([]cfapi.Zone, error) {
	return nil, errors.New("unexpected zone discovery")
}
func (*queueTestAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) {
	return nil, errors.New("unexpected gateway discovery")
}

func queueTestSettings(fields ...string) cfapi.DatasetSettings {
	return cfapi.DatasetSettings{
		Enabled:           true,
		AvailableFields:   fields,
		MaxNumberOfFields: 20,
		MaxDuration:       3600,
		NotOlderThan:      31 * 24 * 60 * 60,
		MaxPageSize:       10000,
	}
}

func queueSettings() map[string]cfapi.DatasetSettings {
	return map[string]cfapi.DatasetSettings{
		"queueBacklogAdaptiveGroups":           queueTestSettings("avg_messages", "avg_bytes", "dimensions_datetimeFiveMinutes", "dimensions_queueId", "dimensions_unselected"),
		"queueConsumerMetricsAdaptiveGroups":   queueTestSettings("avg_concurrency", "dimensions_datetimeFiveMinutes", "dimensions_queueId", "dimensions_unselected"),
		"queueDelayedBacklogAdaptiveGroups":    queueTestSettings("avg_messages", "dimensions_datetimeFiveMinutes", "dimensions_queueId", "dimensions_unselected"),
		"queueMessageOperationsAdaptiveGroups": queueTestSettings("count", "sum_billableOperations", "dimensions_datetimeFiveMinutes", "dimensions_queueId", "dimensions_unselected"),
	}
}

func queueTestConfig(enabled ...string) *config.Config {
	cfg := config.Default()
	cfg.Cloudflare.AccountID = queueFixtureAccount
	for name, value := range cfg.Collectors {
		value.Enabled = false
		cfg.Collectors[name] = value
	}
	for _, name := range enabled {
		value := cfg.Collectors[name]
		value.Enabled = true
		cfg.Collectors[name] = value
	}
	return &cfg
}

func queueRegister(t *testing.T, cfg *config.Config, api cfapi.Client) *collector.Registry {
	t.Helper()
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: cfg, API: api, Registry: registry})
	return registry
}

func queueWindow(t *testing.T, cfg *config.Config, api cfapi.Client, name string) collector.WindowCollector {
	t.Helper()
	for _, entry := range queueRegister(t, cfg, api).Entries() {
		if entry.Collector.Name() != name {
			continue
		}
		window, ok := entry.Collector.(collector.WindowCollector)
		if !ok {
			t.Fatalf("collector %q is not windowed", name)
		}
		return window
	}
	t.Fatalf("collector %q was not registered", name)
	return nil
}

func queueRow(at time.Time, groups, dimensions map[string]any) map[string]any {
	row := map[string]any{"dimensions": map[string]any{
		"datetimeFiveMinutes": at.UTC().Format(time.RFC3339),
	}}
	for key, value := range dimensions {
		row["dimensions"].(map[string]any)[key] = value
	}
	for key, value := range groups {
		row[key] = value
	}
	return row
}

func queueMetrics(metrics []telemetry.BufferedMetric) map[string]telemetry.BufferedMetric {
	out := make(map[string]telemetry.BufferedMetric, len(metrics))
	for _, metric := range metrics {
		out[metric.Name] = metric
	}
	return out
}

func queueFieldsEqual(got, want []string) bool {
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

func TestRegisterInstallsAllFourQueueCollectorsByDefault(t *testing.T) {
	cfg := config.Default()
	cfg.Cloudflare.AccountID = queueFixtureAccount
	registry := queueRegister(t, &cfg, &queueTestAPI{})
	want := map[string]bool{
		"queues.backlog":            true,
		"queues.consumer":           true,
		"queues.delayed_backlog":    true,
		"queues.message_operations": true,
	}
	entries := registry.Entries()
	if len(entries) != len(want) {
		t.Fatalf("registered %d Queue collectors, want %d", len(entries), len(want))
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
			t.Errorf("collector %q has unexpected schedule: entry=%+v lag=%s", name, entry, window.Lag())
		}
	}
	if len(want) != 0 {
		t.Errorf("missing Queue collectors: %v", want)
	}

	cfg.Collectors["queues.consumer"] = config.CollectorConfig{Enabled: false}
	registry = queueRegister(t, &cfg, &queueTestAPI{})
	if len(registry.Entries()) != 3 {
		t.Fatalf("disabling one Queue dataset left %d collectors, want 3", len(registry.Entries()))
	}
}

func TestGaugeDatasetsTakeMaximumQueueAverageFromLatestCompleteBucket(t *testing.T) {
	tests := []struct {
		collector string
		dataset   string
		fields    []string
		metrics   map[string]float64
		oldRows   []map[string]any
		newRows   []map[string]any
	}{
		{
			collector: "queues.backlog",
			dataset:   "queueBacklogAdaptiveGroups",
			fields:    []string{"avg.messages", "avg.bytes", "dimensions.datetimeFiveMinutes", "dimensions.queueId"},
			metrics:   map[string]float64{semconv.MetricQueuesBacklogMessages: 11, semconv.MetricQueuesBacklogBytes: 110},
			oldRows: []map[string]any{
				queueRow(queueFixtureFrom, map[string]any{"avg": map[string]any{"messages": 8, "bytes": 80}}, map[string]any{"queueId": "queue-fixture-alpha"}),
				queueRow(queueFixtureFrom, map[string]any{"avg": map[string]any{"messages": 14, "bytes": 140}}, map[string]any{"queueId": "queue-fixture-beta"}),
			},
			newRows: []map[string]any{
				queueRow(queueFixtureFrom.Add(5*time.Minute), map[string]any{"avg": map[string]any{"messages": 4, "bytes": 40}}, map[string]any{"queueId": "queue-fixture-alpha"}),
				queueRow(queueFixtureFrom.Add(5*time.Minute), map[string]any{"avg": map[string]any{"messages": 11, "bytes": 110}}, map[string]any{"queueId": "queue-fixture-beta"}),
			},
		},
		{
			collector: "queues.consumer",
			dataset:   "queueConsumerMetricsAdaptiveGroups",
			fields:    []string{"avg.concurrency", "dimensions.datetimeFiveMinutes", "dimensions.queueId"},
			metrics:   map[string]float64{semconv.MetricQueuesConsumerConcurrency: 5},
			oldRows: []map[string]any{
				queueRow(queueFixtureFrom, map[string]any{"avg": map[string]any{"concurrency": 2}}, map[string]any{"queueId": "queue-fixture-alpha"}),
				queueRow(queueFixtureFrom, map[string]any{"avg": map[string]any{"concurrency": 8}}, map[string]any{"queueId": "queue-fixture-beta"}),
			},
			newRows: []map[string]any{
				queueRow(queueFixtureFrom.Add(5*time.Minute), map[string]any{"avg": map[string]any{"concurrency": 3}}, map[string]any{"queueId": "queue-fixture-alpha"}),
				queueRow(queueFixtureFrom.Add(5*time.Minute), map[string]any{"avg": map[string]any{"concurrency": 5}}, map[string]any{"queueId": "queue-fixture-beta"}),
			},
		},
		{
			collector: "queues.delayed_backlog",
			dataset:   "queueDelayedBacklogAdaptiveGroups",
			fields:    []string{"avg.messages", "dimensions.datetimeFiveMinutes", "dimensions.queueId"},
			metrics:   map[string]float64{semconv.MetricQueuesDelayedBacklogMessages: 6},
			oldRows: []map[string]any{
				queueRow(queueFixtureFrom, map[string]any{"avg": map[string]any{"messages": 3}}, map[string]any{"queueId": "queue-fixture-alpha"}),
				queueRow(queueFixtureFrom, map[string]any{"avg": map[string]any{"messages": 9}}, map[string]any{"queueId": "queue-fixture-beta"}),
			},
			newRows: []map[string]any{
				queueRow(queueFixtureFrom.Add(5*time.Minute), map[string]any{"avg": map[string]any{"messages": 1}}, map[string]any{"queueId": "queue-fixture-alpha"}),
				queueRow(queueFixtureFrom.Add(5*time.Minute), map[string]any{"avg": map[string]any{"messages": 6}}, map[string]any{"queueId": "queue-fixture-beta"}),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.collector, func(t *testing.T) {
			api := &queueTestAPI{settings: queueSettings(), query: func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
				return append(append([]map[string]any{}, test.oldRows...), test.newRows...), nil
			}}
			window := queueWindow(t, queueTestConfig(test.collector), api, test.collector)
			out := &telemetry.Buffer{}
			to := queueFixtureFrom.Add(10 * time.Minute)
			mark, err := window.CollectWindow(context.Background(), queueFixtureFrom, to, out)
			if err != nil {
				t.Fatal(err)
			}
			if !mark.Equal(to) {
				t.Fatalf("mark = %s, want %s", mark, to)
			}
			if len(api.calls) != 1 {
				t.Fatalf("query count = %d, want one", len(api.calls))
			}
			request := api.calls[0].request
			if request.Scope != cfapi.AccountScope || request.ScopeID != queueFixtureAccount || request.Dataset != test.dataset {
				t.Fatalf("request did not use only configured account and frozen dataset: %+v", request)
			}
			if !request.From.Equal(queueFixtureFrom) || !request.To.Equal(to) {
				t.Fatalf("query window = [%s,%s), want [%s,%s)", request.From, request.To, queueFixtureFrom, to)
			}
			if !queueFieldsEqual(request.WantedFields, test.fields) {
				t.Fatalf("selected fields = %v, want %v", request.WantedFields, test.fields)
			}
			got := queueMetrics(out.Metrics)
			if len(got) != len(test.metrics) {
				t.Fatalf("emitted metrics = %+v, want %d gauges", got, len(test.metrics))
			}
			for name, value := range test.metrics {
				metric, ok := got[name]
				if !ok || metric.Kind != "gauge" || metric.Value != value || len(metric.Attrs) != 0 {
					t.Errorf("metric %s = %+v, want unlabeled gauge %v from latest bucket maximum", name, metric, value)
				}
			}
			for _, metric := range out.Metrics {
				if metric.Name == "queueId" || strings.Contains(metric.Name, "queue-fixture") {
					t.Errorf("queue identity leaked into metric name %q", metric.Name)
				}
				if len(metric.Attrs) != 0 {
					t.Errorf("Queue metric has resource attributes: %+v", metric)
				}
			}
		})
	}
}

func TestGaugeSettingsRequireQueueIDAndAllRequiredValues(t *testing.T) {
	for _, test := range []struct {
		collector string
		dataset   string
		missing   string
	}{
		{"queues.backlog", "queueBacklogAdaptiveGroups", "dimensions.queueId"},
		{"queues.backlog", "queueBacklogAdaptiveGroups", "avg.bytes"},
		{"queues.consumer", "queueConsumerMetricsAdaptiveGroups", "dimensions.datetimeFiveMinutes"},
		{"queues.delayed_backlog", "queueDelayedBacklogAdaptiveGroups", "avg.messages"},
	} {
		t.Run(test.collector+"/"+test.missing, func(t *testing.T) {
			settings := queueSettings()
			item := settings[test.dataset]
			missing := strings.ReplaceAll(test.missing, ".", "_")
			fields := item.AvailableFields[:0]
			for _, field := range item.AvailableFields {
				if !strings.EqualFold(field, missing) {
					fields = append(fields, field)
				}
			}
			item.AvailableFields = fields
			settings[test.dataset] = item
			api := &queueTestAPI{settings: settings}
			window := queueWindow(t, queueTestConfig(test.collector), api, test.collector)
			out := &telemetry.Buffer{}
			mark, err := window.CollectWindow(context.Background(), queueFixtureFrom, queueFixtureFrom.Add(5*time.Minute), out)
			if err == nil || !strings.Contains(err.Error(), "required field") {
				t.Fatalf("missing required field returned mark=%s error=%v", mark, err)
			}
			if !mark.Equal(queueFixtureFrom) || len(api.calls) != 0 || len(out.Metrics) != 0 {
				t.Fatalf("missing required field queried, emitted, or advanced: mark=%s calls=%d metrics=%d", mark, len(api.calls), len(out.Metrics))
			}
		})
	}
}

func TestMessageOperationsUseOptionalQueueIDAndSumOnlyCountAndBillableOperations(t *testing.T) {
	settings := queueSettings()
	operations := settings["queueMessageOperationsAdaptiveGroups"]
	operations.AvailableFields = []string{"count", "sum_billableOperations", "dimensions_datetimeFiveMinutes", "dimensions_unselected"}
	operations.MaxNumberOfFields = 3
	operations.MaxPageSize = 37
	settings["queueMessageOperationsAdaptiveGroups"] = operations
	api := &queueTestAPI{
		settings: settings,
		query: func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
			return []map[string]any{
				queueRow(request.From, map[string]any{"count": 2, "sum": map[string]any{"billableOperations": 7}}, nil),
				queueRow(request.From.Add(5*time.Minute), map[string]any{"count": 3, "sum": map[string]any{"billableOperations": 11}}, nil),
			}, nil
		},
	}
	window := queueWindow(t, queueTestConfig("queues.message_operations"), api, "queues.message_operations")
	out := &telemetry.Buffer{}
	to := queueFixtureFrom.Add(10 * time.Minute)
	mark, err := window.CollectWindow(context.Background(), queueFixtureFrom, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) {
		t.Fatalf("mark = %s, want %s", mark, to)
	}
	if len(api.calls) != 1 {
		t.Fatalf("query count = %d, want one", len(api.calls))
	}
	request := api.calls[0].request
	wantFields := []string{"count", "sum.billableOperations", "dimensions.datetimeFiveMinutes"}
	if request.Scope != cfapi.AccountScope || request.ScopeID != queueFixtureAccount || request.Dataset != "queueMessageOperationsAdaptiveGroups" || request.Limit != 37 {
		t.Fatalf("message operations request = %+v", request)
	}
	if !queueFieldsEqual(request.WantedFields, wantFields) {
		t.Fatalf("selected fields = %v, want only count, billable sum, and bucket time %v", request.WantedFields, wantFields)
	}
	got := queueMetrics(out.Metrics)
	want := map[string]float64{semconv.MetricQueuesMessageOperations: 5, semconv.MetricQueuesBillableOperations: 18}
	if len(got) != len(want) {
		t.Fatalf("metrics = %+v, want exactly count and billable operations", got)
	}
	for name, value := range want {
		metric, ok := got[name]
		if !ok || metric.Kind != "counter" || metric.Value != value || len(metric.Attrs) != 0 {
			t.Errorf("metric %s = %+v, want account-only counter %v", name, metric, value)
		}
	}
}

func TestIncompleteAndInitiallyUnalignedWindowsHoldCheckpointUntilFullBucket(t *testing.T) {
	api := &queueTestAPI{settings: queueSettings(), query: func(cfapi.GraphQLRequest) ([]map[string]any, error) { return nil, nil }}
	window := queueWindow(t, queueTestConfig("queues.message_operations"), api, "queues.message_operations")
	from := queueFixtureFrom.Add(2 * time.Minute)
	mark, err := window.CollectWindow(context.Background(), from, queueFixtureFrom.Add(9*time.Minute), &telemetry.Buffer{})
	if err == nil || !mark.Equal(from) || len(api.calls) != 0 {
		t.Fatalf("incomplete interval advanced or queried: mark=%s err=%v calls=%d", mark, err, len(api.calls))
	}

	to := queueFixtureFrom.Add(17 * time.Minute)
	api.query = func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
		return []map[string]any{
			queueRow(request.From, map[string]any{"count": 1, "sum": map[string]any{"billableOperations": 2}}, nil),
			queueRow(request.From.Add(5*time.Minute), map[string]any{"count": 1, "sum": map[string]any{"billableOperations": 2}}, nil),
		}, nil
	}
	first := &telemetry.Buffer{}
	firstMark, err := window.CollectWindow(context.Background(), from, to, first)
	if err != nil {
		t.Fatal(err)
	}
	if want := queueFixtureFrom.Add(15 * time.Minute); !firstMark.Equal(want) {
		t.Fatalf("unaligned high-water = %s, want floor(to) %s", firstMark, want)
	}
	secondTo := queueFixtureFrom.Add(30 * time.Minute)
	second := &telemetry.Buffer{}
	secondMark, err := window.CollectWindow(context.Background(), firstMark, secondTo, second)
	if err != nil {
		t.Fatal(err)
	}
	if !secondMark.Equal(secondTo) || len(api.calls) != 2 {
		t.Fatalf("adjacent high-water/calls = %s/%d, want %s/2", secondMark, len(api.calls), secondTo)
	}
	firstRequest, secondRequest := api.calls[0].request, api.calls[1].request
	if !firstRequest.From.Equal(queueFixtureFrom.Add(5*time.Minute)) || !firstRequest.To.Equal(firstMark) || !secondRequest.From.Equal(firstMark) || !secondRequest.To.Equal(secondTo) {
		t.Fatalf("adjacent requests have a gap or overlap: [%s,%s), [%s,%s)", firstRequest.From, firstRequest.To, secondRequest.From, secondRequest.To)
	}
}

func TestSaturatedQueryBisectsOnFiveMinuteBoundariesWithoutGaps(t *testing.T) {
	from := queueFixtureFrom
	to := from.Add(20 * time.Minute)
	api := &queueTestAPI{settings: queueSettings()}
	api.query = func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
		if !request.From.Equal(request.From.Truncate(5*time.Minute)) || !request.To.Equal(request.To.Truncate(5*time.Minute)) {
			return nil, fmt.Errorf("split cut a five-minute bucket: [%s,%s)", request.From, request.To)
		}
		if request.To.Sub(request.From) > 5*time.Minute {
			return nil, fmt.Errorf("GraphQL dataset %s window %s..%s saturated limit %d", request.Dataset, request.From.Format(time.RFC3339), request.To.Format(time.RFC3339), request.Limit)
		}
		return []map[string]any{queueRow(request.From, map[string]any{"count": 1, "sum": map[string]any{"billableOperations": 2}}, nil)}, nil
	}
	window := queueWindow(t, queueTestConfig("queues.message_operations"), api, "queues.message_operations")
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
		t.Fatalf("successful query leaves = %d, want 4: %+v", len(leaves), leaves)
	}
	for i, leaf := range leaves {
		wantFrom := from.Add(time.Duration(i) * 5 * time.Minute)
		wantTo := wantFrom.Add(5 * time.Minute)
		if !leaf.From.Equal(wantFrom) || !leaf.To.Equal(wantTo) {
			t.Fatalf("leaf %d = [%s,%s), want [%s,%s)", i, leaf.From, leaf.To, wantFrom, wantTo)
		}
		if i > 0 && !leaves[i-1].To.Equal(leaf.From) {
			t.Fatalf("adjacent leaves have a gap or overlap: %s then %s", leaves[i-1].To, leaf.From)
		}
	}
	got := queueMetrics(out.Metrics)
	if got[semconv.MetricQueuesMessageOperations].Value != 4 || got[semconv.MetricQueuesBillableOperations].Value != 8 {
		t.Fatalf("bisection emitted counters %+v, want 4 operations and 8 billable operations", got)
	}
}

func TestIrreducibleFiveMinuteSaturationFailsWithoutEmissionOrAdvance(t *testing.T) {
	from := queueFixtureFrom
	to := from.Add(5 * time.Minute)
	api := &queueTestAPI{settings: queueSettings(), query: func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
		return nil, fmt.Errorf("GraphQL dataset %s window %s..%s saturated limit %d", request.Dataset, request.From.Format(time.RFC3339), request.To.Format(time.RFC3339), request.Limit)
	}}
	window := queueWindow(t, queueTestConfig("queues.message_operations"), api, "queues.message_operations")
	out := &telemetry.Buffer{}
	mark, err := window.CollectWindow(context.Background(), from, to, out)
	if err == nil || !strings.Contains(err.Error(), "five-minute") {
		t.Fatalf("irreducible saturation returned mark=%s err=%v, want five-minute saturation error", mark, err)
	}
	if !mark.Equal(from) || len(api.calls) != 1 || len(out.Metrics) != 0 {
		t.Fatalf("irreducible saturation advanced or emitted: mark=%s calls=%d metrics=%d", mark, len(api.calls), len(out.Metrics))
	}
}

func TestSeriesCapRetainsSortedMetricsAndDisclosesOnlyDropCount(t *testing.T) {
	cfg := queueTestConfig("queues.backlog")
	cfg.Platform.MaxMetricSeriesPerWindow = 1
	api := &queueTestAPI{settings: queueSettings(), query: func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
		return []map[string]any{
			queueRow(request.From, map[string]any{"avg": map[string]any{"messages": 2, "bytes": 20}}, map[string]any{"queueId": "queue-fixture-alpha"}),
			queueRow(request.From, map[string]any{"avg": map[string]any{"messages": 5, "bytes": 50}}, map[string]any{"queueId": "queue-fixture-beta"}),
		}, nil
	}}
	window := queueWindow(t, cfg, api, "queues.backlog")
	var logs strings.Builder
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	out := &telemetry.Buffer{}
	_, err := window.CollectWindow(context.Background(), queueFixtureFrom, queueFixtureFrom.Add(5*time.Minute), out)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Metrics) != 1 || out.Metrics[0].Name != semconv.MetricQueuesBacklogBytes || out.Metrics[0].Value != 50 || len(out.Metrics[0].Attrs) != 0 {
		t.Fatalf("capped metric = %+v, want first sorted unlabeled metric %s=50", out.Metrics, semconv.MetricQueuesBacklogBytes)
	}
	logText := logs.String()
	if !strings.Contains(logText, "dropped=1") || strings.Count(logText, "platform metric series dropped") != 1 {
		t.Fatalf("drop count was not disclosed once: %s", logText)
	}
	for _, value := range []string{"queue-fixture-alpha", "queue-fixture-beta"} {
		if strings.Contains(logText, value) {
			t.Errorf("drop disclosure exposed queue identity %q", value)
		}
	}
}
