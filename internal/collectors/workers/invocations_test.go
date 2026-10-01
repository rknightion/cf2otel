package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

func TestWorkersInvocationsAggregateStatusesAndLatestBucket(t *testing.T) {
	cfg := workersTestConfig()
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	api := &invocationTestAPI{workersTestAPI: workersTestAPI{settings: invocationSettings()}}
	api.query = func(r cfapi.GraphQLRequest) ([]map[string]any, error) {
		if strings.Contains(strings.Join(r.WantedFields, ","), "sum.requests") {
			return []map[string]any{invocationRow(from, "example-worker", "ok"), invocationRow(from, "example-worker", "exception"), invocationRow(from.Add(5*time.Minute), "example-worker", "ok")}, nil
		}
		for _, f := range r.WantedFields {
			if f == "dimensions.status" {
				t.Fatal("quantiles must aggregate all statuses, not combine status percentiles")
			}
		}
		older := invocationRow(from, "example-worker", "")
		newer := invocationRow(from.Add(5*time.Minute), "example-worker", "")
		for key := range newer["quantiles"].(map[string]any) {
			newer["quantiles"].(map[string]any)[key] = 1250000.5
		}
		// Deliberately return newest first to prove order-independent gauge selection.
		return []map[string]any{newer, older}, nil
	}
	out := &telemetry.Buffer{}
	mark, err := registeredInvocations(t, cfg, api).CollectWindow(context.Background(), from, from.Add(10*time.Minute), out)
	if err != nil || !mark.Equal(from.Add(10*time.Minute)) {
		t.Fatalf("mark=%s err=%v", mark, err)
	}
	requests := map[string]float64{}
	for _, m := range out.Metrics {
		if m.Kind == "gauge" {
			if m.Value != 1.2500005 {
				t.Fatalf("gauge must describe latest bucket, not merged quantiles: %+v", m)
			}
			continue
		}
		if m.Name == semconv.MetricWorkersInvocations {
			for _, a := range m.Attrs {
				if a.Key == semconv.AttrWorkersStatus {
					requests[a.Value] = m.Value
				}
			}
		} else if m.Name == semconv.MetricWorkersErrors && m.Value != 6 {
			t.Fatalf("errors must sum statuses and buckets: %+v", m)
		} else if m.Name == semconv.MetricWorkersSubrequests && m.Value != 33 {
			t.Fatalf("subrequests must sum statuses and buckets: %+v", m)
		}
	}
	if requests["ok"] != 14 || requests["exception"] != 7 {
		t.Fatalf("status totals=%v", requests)
	}
}

func TestWorkersInvocationsEntitlementBoundsAndSeriesCap(t *testing.T) {
	cfg := workersTestConfig()
	cfg.Platform.MaxMetricSeriesPerWindow = 2
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	settings := invocationSettings()
	settings.AvailableFields = settings.AvailableFields[:6]
	settings.MaxNumberOfFields = 6
	api := &invocationTestAPI{workersTestAPI: workersTestAPI{settings: settings}}
	api.query = func(r cfapi.GraphQLRequest) ([]map[string]any, error) {
		if len(r.WantedFields) != 6 {
			t.Fatalf("selected unavailable quantile: %v", r.WantedFields)
		}
		return []map[string]any{invocationRow(from, "example-worker", "ok"), invocationRow(from, strings.Repeat("x", 129), "ok")}, nil
	}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	out := &telemetry.Buffer{}
	_, err := registeredInvocations(t, cfg, api).CollectWindow(context.Background(), from, from.Add(5*time.Minute), out)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Metrics) != 2 || !strings.Contains(logs.String(), "dropped_series=4") {
		t.Fatalf("cap not disclosed: metrics=%+v log=%s", out.Metrics, logs.String())
	}
	for _, m := range out.Metrics {
		for _, a := range m.Attrs {
			if a.Key == semconv.AttrWorkersScriptName && len(a.Value) > 128 {
				t.Fatalf("script name was not bounded: %+v", m)
			}
		}
	}
}

func TestWorkersInvocationsFailClosedAndBisectSaturation(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	for _, mode := range []string{"malformed", "missing field", "irreducible", "split"} {
		t.Run(mode, func(t *testing.T) {
			settings := invocationSettings()
			if mode == "missing field" {
				settings.AvailableFields = settings.AvailableFields[1:]
			}
			api := &invocationTestAPI{workersTestAPI: workersTestAPI{settings: settings}}
			api.query = func(r cfapi.GraphQLRequest) ([]map[string]any, error) {
				if mode == "irreducible" || (mode == "split" && r.To.Sub(r.From) > workersBucket) {
					return nil, fmt.Errorf("GraphQL dataset %s window saturated limit %d", r.Dataset, r.Limit)
				}
				if !r.From.Equal(r.From.Truncate(workersBucket)) || !r.To.Equal(r.To.Truncate(workersBucket)) {
					t.Fatal("saturation split cut a bucket")
				}
				row := invocationRow(r.From, "example-worker", "ok")
				if mode == "malformed" {
					row["quantiles"].(map[string]any)["cpuTimeP50"] = -1
				}
				return []map[string]any{row}, nil
			}
			out := &telemetry.Buffer{}
			mark, err := registeredInvocations(t, workersTestConfig(), api).CollectWindow(context.Background(), from, from.Add(15*time.Minute), out)
			if mode == "split" {
				if err != nil || !mark.Equal(from.Add(15*time.Minute)) {
					t.Fatalf("split mark=%s error=%v", mark, err)
				}
				for _, m := range out.Metrics {
					if m.Name == semconv.MetricWorkersInvocations && m.Value != 21 {
						t.Fatalf("split counter=%+v", m)
					}
				}
			} else if err == nil || !mark.Equal(from) || len(out.Metrics) != 0 || len(out.Records) != 0 {
				t.Fatalf("failure advanced/emitted: mark=%s error=%v metrics=%+v", mark, err, out.Metrics)
			}
		})
	}
}

func TestWorkersInvocationsRealClientUsesEntitlementAndLimits(t *testing.T) {
	from := time.Now().UTC().Truncate(workersBucket).Add(-20 * time.Minute)
	settings := invocationSettings()
	settings.MaxDuration = 300
	settings.NotOlderThan = 3600
	// Keep the concatenated two-window response below the page cap so this
	// case isolates maxDuration; saturation is exercised separately.
	settings.MaxPageSize = 3
	// An unentitled quantile must never enter the rendered GraphQL selection.
	for i, field := range settings.AvailableFields {
		if field == "quantiles_cpuTimeP999" {
			settings.AvailableFields = append(settings.AvailableFields[:i], settings.AvailableFields[i+1:]...)
			break
		}
	}
	dataQueries := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", 400)
			return
		}
		var response any
		if strings.Contains(request.Query, "settings{") {
			response = map[string]any{"data": map[string]any{"viewer": map[string]any{"accounts": []any{map[string]any{"settings": map[string]any{invocationTestDataset: settings}}}}}}
		} else {
			dataQueries++
			if !strings.Contains(request.Query, "limit:3") || strings.Contains(request.Query, "cpuTimeP999") {
				t.Errorf("query ignored runtime limits/entitlement: %s", request.Query)
			}
			_, remaining, found := strings.Cut(request.Query, `datetime_geq:"`)
			if !found {
				t.Error("query is missing half-open start filter")
				http.Error(w, "bad filter", 400)
				return
			}
			timestamp, _, _ := strings.Cut(remaining, `"`)
			bucket, err := time.Parse(time.RFC3339, timestamp)
			if err != nil {
				t.Error(err)
				http.Error(w, "bad timestamp", 400)
				return
			}
			response = map[string]any{"data": map[string]any{"viewer": map[string]any{"accounts": []any{map[string]any{invocationTestDataset: []any{invocationRow(bucket, "example-worker", "ok")}}}}}}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	cfg := workersTestConfig()
	cfg.Cloudflare.APIBase = server.URL
	out := &telemetry.Buffer{}
	mark, err := registeredInvocations(t, cfg, cfapi.New(cfg.Cloudflare)).CollectWindow(context.Background(), from, from.Add(10*time.Minute), out)
	if err != nil || !mark.Equal(from.Add(10*time.Minute)) || dataQueries != 4 {
		t.Fatalf("real client mark=%s error=%v queries=%d", mark, err, dataQueries)
	}
	for _, m := range out.Metrics {
		if m.Name == semconv.MetricWorkersCPUTime {
			for _, a := range m.Attrs {
				if a.Key == semconv.AttrStatistic && a.Value == "p999" {
					t.Fatal("unentitled quantile exported")
				}
			}
		}
	}
	if len(out.Metrics) == 0 || len(out.Records) != 0 {
		t.Fatalf("real client aggregate output=%+v", out)
	}
}

const invocationTestDataset = "workersInvocationsAdaptive"

type invocationTestAPI struct{ workersTestAPI }

func (f *invocationTestAPI) DatasetSettings(_ context.Context, scope cfapi.Scope, account, dataset string) (cfapi.DatasetSettings, error) {
	if scope != cfapi.AccountScope || account != "synthetic-account" || dataset != invocationTestDataset {
		return cfapi.DatasetSettings{}, errors.New("unexpected invocation settings scope")
	}
	return f.settings, f.settingsErr
}
func invocationSettings() cfapi.DatasetSettings {
	fields := []string{"dimensions_scriptName", "dimensions_status", "dimensions_datetimeFiveMinutes", "sum_requests", "sum_errors", "sum_subrequests"}
	for _, name := range []string{"cpuTime", "wallTime", "requestDuration"} {
		for _, statistic := range []string{"P50", "P75", "P99", "P999"} {
			fields = append(fields, "quantiles_"+name+statistic)
		}
	}
	return cfapi.DatasetSettings{Enabled: true, AvailableFields: fields, MaxNumberOfFields: 35, MaxPageSize: 50, MaxDuration: 2764800, NotOlderThan: 7776000}
}
func registeredInvocations(t *testing.T, cfg *config.Config, api cfapi.Client) collector.WindowCollector {
	t.Helper()
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: cfg, API: api, Registry: registry})
	for _, entry := range registry.Entries() {
		if entry.Collector.Name() == "workers.invocations" {
			window, ok := entry.Collector.(collector.WindowCollector)
			if !ok || entry.Interval != cfg.Collector("workers.invocations").Interval {
				t.Fatal("invocations must register its configured window collector")
			}
			return window
		}
	}
	t.Fatal("default-enabled workers.invocations was not registered")
	return nil
}
func invocationRow(bucket time.Time, script, status string) map[string]any {
	q := map[string]any{}
	for _, name := range []string{"cpuTime", "wallTime", "requestDuration"} {
		for i, statistic := range []string{"P50", "P75", "P99", "P999"} {
			q[name+statistic] = float64(i+1) * 2500000
		}
	}
	return map[string]any{"dimensions": map[string]any{"datetimeFiveMinutes": bucket.Format(time.RFC3339), "scriptName": script, "status": status}, "sum": map[string]any{"requests": 7, "errors": 2, "subrequests": 11}, "quantiles": q}
}
func TestWorkersInvocationsRegisterAndExportAggregates(t *testing.T) {
	cfg := workersTestConfig()
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	api := &invocationTestAPI{workersTestAPI: workersTestAPI{settings: invocationSettings()}}
	api.query = func(r cfapi.GraphQLRequest) ([]map[string]any, error) {
		if r.Scope != cfapi.AccountScope || r.ScopeID != "synthetic-account" || r.Dataset != invocationTestDataset || len(r.WantedFields) > 35 {
			t.Fatalf("invalid aggregate request: %+v", r)
		}
		for _, f := range r.WantedFields {
			if strings.Contains(f, "duration") || f == "count" {
				t.Fatalf("raw or GB*s field selected: %s", f)
			}
		}
		return []map[string]any{invocationRow(from, "example-worker", "ok")}, nil
	}
	out := &telemetry.Buffer{}
	mark, err := registeredInvocations(t, cfg, api).CollectWindow(context.Background(), from, from.Add(5*time.Minute), out)
	if err != nil || !mark.Equal(from.Add(5*time.Minute)) {
		t.Fatalf("collect mark=%s error=%v", mark, err)
	}
	if len(out.Records) != 0 {
		t.Fatalf("raw invocation events emitted: %+v", out.Records)
	}
	counters := map[string]float64{semconv.MetricWorkersInvocations: 7, semconv.MetricWorkersErrors: 2, semconv.MetricWorkersSubrequests: 11}
	gauges := map[string]bool{semconv.MetricWorkersCPUTime: true, semconv.MetricWorkersWallTime: true, semconv.MetricWorkersRequestDuration: true}
	seen := map[string]bool{}
	for _, m := range out.Metrics {
		attrs := map[string]string{}
		for _, a := range m.Attrs {
			attrs[a.Key] = a.Value
		}
		if attrs[semconv.AttrWorkersScriptName] != "example-worker" {
			t.Fatalf("unbounded or missing script: %+v", m)
		}
		if value, ok := counters[m.Name]; ok {
			if m.Kind != "counter" || m.Value != value {
				t.Fatalf("counter %+v, want %v", m, value)
			}
			if m.Name == semconv.MetricWorkersInvocations {
				if attrs[semconv.AttrWorkersStatus] != "ok" || len(attrs) != 2 {
					t.Fatalf("status missing: %+v", m)
				}
			} else if len(attrs) != 1 {
				t.Fatalf("error/subrequest attributes are script-only: %+v", m)
			}
			seen[m.Name] = true
		} else if gauges[m.Name] {
			values := map[string]float64{"p50": 2.5, "p75": 5, "p99": 7.5, "p999": 10}
			want, ok := values[attrs[semconv.AttrStatistic]]
			if !ok || m.Kind != "gauge" || m.Value != want || len(attrs) != 2 {
				t.Fatalf("microseconds conversion/quantile attributes: %+v", m)
			}
			seen[m.Name+attrs[semconv.AttrStatistic]] = true
		} else {
			t.Fatalf("unexpected signal: %+v", m)
		}
	}
	for name := range counters {
		if !seen[name] {
			t.Errorf("missing %s", name)
		}
	}
	for name := range gauges {
		for _, s := range []string{"p50", "p75", "p99", "p999"} {
			if !seen[name+s] {
				t.Errorf("missing %s %s", name, s)
			}
		}
	}
}
