package workersai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/collectors/workersai"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	otellog "go.opentelemetry.io/otel/log"
)

type emitter struct{ values map[string]float64 }

func (e *emitter) Counter(_ context.Context, name string, value float64, attrs ...telemetry.Attr) error {
	if strings.HasPrefix(name, "cloudflare.") {
		if len(attrs) != 0 {
			panic("identifier attributes on aggregate metric")
		}
		e.values[name] += value
	}
	return nil
}
func (*emitter) Gauge(context.Context, string, float64, ...telemetry.Attr) error     { return nil }
func (*emitter) Histogram(context.Context, string, float64, ...telemetry.Attr) error { return nil }
func (*emitter) LogEvent(context.Context, string, string, time.Time, otellog.Severity, ...telemetry.Attr) error {
	return nil
}
func (*emitter) Span(context.Context, telemetry.SpanSpec) error { return nil }

var bounds = regexp.MustCompile(`datetime_(?:geq|lt):"([^"]+)"`)

func setup(t *testing.T, optional bool, limit int, rows func(time.Time, time.Time) []any) (collector.Entry, *collector.Scheduler, *emitter, *collector.FileStore, *[]string) {
	t.Helper()
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Query string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		response := map[string]any{}
		if strings.Contains(body.Query, "settings{") {
			advertised := []string{"count", "dimensions_datetimeFiveMinutes"}
			if optional {
				advertised = append(advertised, "sum_totalInputTokens", "sum_totalOutputTokens", "sum_totalInferenceTimeMs")
			}
			response["settings"] = map[string]any{"aiInferenceAdaptiveGroups": cfapi.DatasetSettings{Enabled: true, AvailableFields: advertised, MaxNumberOfFields: 30, MaxDuration: 2764800, NotOlderThan: 2764800, MaxPageSize: limit}}
		} else {
			queries = append(queries, body.Query)
			matches := bounds.FindAllStringSubmatch(body.Query, -1)
			if len(matches) != 2 {
				t.Errorf("missing half-open bounds: %s", body.Query)
				return
			}
			a, _ := time.Parse(time.RFC3339, matches[0][1])
			b, _ := time.Parse(time.RFC3339, matches[1][1])
			response["ai"] = rows(a, b)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"accounts": []any{response}}}})
	}))
	t.Cleanup(srv.Close)
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "account-fixture"
	cfg.Cloudflare.APIBase = srv.URL
	cfg.Collectors[semconv.CollectorNameWorkersAIMetrics] = config.CollectorConfig{Enabled: true, Interval: 5 * time.Minute, InitialLookback: 30 * time.Minute, MaxWindow: time.Hour}
	registry := collector.NewRegistry()
	workersai.Register(collector.Deps{Config: &cfg, API: cfapi.New(cfg.Cloudflare), Registry: registry})
	if len(registry.Entries()) != 1 {
		t.Fatal("missing Workers AI collector")
	}
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "checkpoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	out := &emitter{values: map[string]float64{}}
	scheduler := collector.NewScheduler(nil, out, store)
	return registry.Entries()[0], scheduler, out, store, &queries
}
func row(at time.Time, count any, sum map[string]any) any {
	return map[string]any{"count": count, "dimensions": map[string]any{"datetimeFiveMinutes": at.Format(time.RFC3339)}, "sum": sum}
}

func TestPublicConfigRejectsWindowShorterThanSourceBucket(t *testing.T) {
	t.Setenv("CF2OTEL_CLOUDFLARE__API_TOKEN", "fixture-token")
	t.Setenv("CF2OTEL_OTLP__ENDPOINT", "http://127.0.0.1:4318")
	t.Setenv("CF2OTEL_OTLP__GRAFANA_CLOUD__INSTANCE_ID", "fixture-instance")
	t.Setenv("CF2OTEL_OTLP__GRAFANA_CLOUD__TOKEN", "fixture-token")
	var dataQueries int
	var fetched [][2]time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Query string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		response := map[string]any{}
		if strings.Contains(body.Query, "settings{") {
			response["settings"] = map[string]any{"aiInferenceAdaptiveGroups": cfapi.DatasetSettings{Enabled: true, AvailableFields: []string{"count", "dimensions_datetimeFiveMinutes"}, MaxNumberOfFields: 2, MaxDuration: 3600, NotOlderThan: 3600, MaxPageSize: 100}}
		} else {
			dataQueries++
			matches := bounds.FindAllStringSubmatch(body.Query, -1)
			if len(matches) != 2 {
				t.Error("missing half-open bounds")
				return
			}
			a, _ := time.Parse(time.RFC3339, matches[0][1])
			b, _ := time.Parse(time.RFC3339, matches[1][1])
			fetched = append(fetched, [2]time.Time{a, b})
			response["ai"] = []any{row(a, 1, nil)}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"accounts": []any{response}}}})
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := "cloudflare:\n  account_id: account-fixture\n  api_base: " + srv.URL + "\ncollectors:\n  workersai.metrics:\n    enabled: true\n    interval: 5m\n    initial_lookback: 30m\n    max_window: 1m\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	entryConfig := cfg.Collector(semconv.CollectorNameWorkersAIMetrics)
	registry := collector.NewRegistry()
	workersai.Register(collector.Deps{Config: cfg, API: cfapi.New(cfg.Cloudflare), Registry: registry})
	entry := registry.Entries()[0]
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "checkpoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Add(-time.Hour).Truncate(5 * time.Minute)
	if err := store.Set(entry.Collector.Name(), start); err != nil {
		t.Fatal(err)
	}
	scheduler := collector.NewScheduler(nil, &emitter{values: map[string]float64{}}, store)
	scheduler.Now = func() time.Time { return start.Add(15 * time.Minute) }
	if err := scheduler.RunOnce(context.Background(), entry); err == nil || !strings.Contains(err.Error(), "no complete bucket") {
		t.Fatalf("one-minute scheduler window did not reproduce source-bucket failure: %v", err)
	}
	if dataQueries != 0 {
		t.Fatalf("scheduler issued %d partial-bucket data queries", dataQueries)
	}
	validation := cfg.Validate()
	if validation == nil || !strings.Contains(validation.Error(), "workersai.metrics.max_window") {
		t.Fatalf("public config validation did not reject the sub-bucket window: %v (loaded max_window %s)", validation, entryConfig.MaxWindow)
	}

	for _, window := range []string{"5m", "6m", "10m"} {
		t.Run(window+"-fresh-31m-lookback", func(t *testing.T) {
			publicYAML := strings.ReplaceAll(strings.ReplaceAll(yaml, "max_window: 1m", "max_window: "+window), "initial_lookback: 30m", "initial_lookback: 31m")
			if err := os.WriteFile(path, []byte(publicYAML), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			registry := collector.NewRegistry()
			workersai.Register(collector.Deps{Config: cfg, API: cfapi.New(cfg.Cloudflare), Registry: registry})
			entry := registry.Entries()[0]
			checkpointPath := filepath.Join(t.TempDir(), "checkpoints.json")
			store, err := collector.NewFileStore(checkpointPath)
			if err != nil {
				t.Fatal(err)
			}
			out := &emitter{values: map[string]float64{}}
			scheduler := collector.NewScheduler(nil, out, store)
			// With the source lag, the initial cursor is one minute before a
			// bucket boundary. No checkpoint seed may mask startup alignment.
			now := start.Add(time.Hour)
			scheduler.Now = func() time.Time { return now }
			if window != "10m" {
				if window == "5m" {
					if err := scheduler.RunOnce(context.Background(), entry); err == nil || !strings.Contains(err.Error(), "no complete bucket") {
						t.Fatalf("fresh unaligned startup did not reproduce failure: %v", err)
					}
				}
				if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "workersai.metrics.max_window") {
					t.Fatalf("public config validation did not reject unsafe %s window with 31m lookback: %v", window, err)
				}
				return
			}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("supported 10m minimum rejected: %v", err)
			}
			firstQuery := len(fetched)
			previous := now.Add(-41 * time.Minute) // source holdback plus public lookback
			for cycle := 0; cycle < 3; cycle++ {
				if cycle == 2 {
					store, err = collector.NewFileStore(checkpointPath)
					if err != nil {
						t.Fatal(err)
					}
					scheduler = collector.NewScheduler(nil, out, store)
					scheduler.Now = func() time.Time { return now }
				}
				if err := scheduler.RunOnce(context.Background(), entry); err != nil {
					t.Fatalf("supported minimum failed cycle %d: %v", cycle, err)
				}
				mark, ok := store.Get(entry.Collector.Name())
				if !ok || !mark.After(previous) || !mark.Equal(mark.Truncate(5*time.Minute)) {
					t.Fatalf("cycle %d checkpoint did not advance on complete buckets: previous=%s mark=%s", cycle, previous, mark)
				}
				previous = mark
				now = now.Add(5 * time.Minute)
			}
			if len(fetched) == firstQuery || out.values[semconv.MetricWorkersAIInferences] == 0 {
				t.Fatal("supported minimum exported no complete buckets")
			}
			for i, interval := range fetched[firstQuery:] {
				if !interval[0].Equal(interval[0].Truncate(5*time.Minute)) || !interval[1].Equal(interval[1].Truncate(5*time.Minute)) || !interval[1].After(interval[0]) {
					t.Fatalf("partial bucket query: %v", interval)
				}
				if i > 0 && !interval[0].Equal(fetched[firstQuery+i-1][1]) {
					t.Fatalf("overlap or gap after committed bucket: %v", interval)
				}
			}
		})
	}
}

func TestSchedulerAdditiveTotalsAndExactSelection(t *testing.T) {
	start := time.Now().UTC().Add(-time.Hour).Truncate(5 * time.Minute)
	entry, scheduler, out, store, queries := setup(t, true, 10000, func(a, b time.Time) []any {
		return []any{row(a, 2, map[string]any{"totalInputTokens": 10, "totalOutputTokens": 3, "totalInferenceTimeMs": 1250}), row(a.Add(5*time.Minute), 4, map[string]any{"totalInputTokens": 20, "totalOutputTokens": nil, "totalInferenceTimeMs": 750})}
	})
	if err := store.Set(entry.Collector.Name(), start); err != nil {
		t.Fatal(err)
	}
	scheduler.Now = func() time.Time { return start.Add(20*time.Minute + 37*time.Second) }
	if err := scheduler.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	scheduler.Now = func() time.Time { return start.Add(30*time.Minute + 37*time.Second) }
	if err := scheduler.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{semconv.MetricWorkersAIInferences: 12, semconv.MetricWorkersAIInputTokens: 60, semconv.MetricWorkersAIOutputTokens: 6, semconv.MetricWorkersAIInferenceTime: 4}
	for name, value := range want {
		if out.values[name] != value {
			t.Errorf("%s=%v want %v", name, out.values[name], value)
		}
	}
	mark, _ := store.Get(entry.Collector.Name())
	if !mark.Equal(start.Add(20 * time.Minute)) {
		t.Errorf("unaligned checkpoint %s", mark)
	}
	for _, q := range *queries {
		for _, field := range []string{"count", "datetimeFiveMinutes", "totalInputTokens", "totalOutputTokens", "totalInferenceTimeMs"} {
			if !strings.Contains(q, field) {
				t.Errorf("missing selected %s", field)
			}
		}
		for _, forbidden := range []string{"modelId", "tag", "cost", "totalInputBytes", "totalOutputBytes"} {
			if strings.Contains(q, forbidden) {
				t.Errorf("unexpected selection %s", forbidden)
			}
		}
	}
}

func TestOptionalSumsOmitted(t *testing.T) {
	for _, advertised := range []bool{false, true} {
		t.Run(map[bool]string{false: "unavailable", true: "null"}[advertised], func(t *testing.T) {
			entry, scheduler, out, _, queries := setup(t, advertised, 10000, func(a, b time.Time) []any { return []any{row(a, 3, map[string]any{"totalInputTokens": nil})} })
			a := time.Now().UTC().Add(-time.Hour).Truncate(5 * time.Minute)
			if err := scheduler.CollectRange(context.Background(), entry.Collector.(collector.WindowCollector), a, a.Add(5*time.Minute)); err != nil {
				t.Fatal(err)
			}
			if len(out.values) != 1 || out.values[semconv.MetricWorkersAIInferences] != 3 {
				t.Fatalf("fabricated optional sums: %v", out.values)
			}
			if !advertised && strings.Contains((*queries)[0], "sum{") {
				t.Fatal("selected unavailable sums")
			}
		})
	}
}

func TestSchedulerSkipsIncompleteBoundaryThenCollectsCompleteBucket(t *testing.T) {
	entry, scheduler, out, store, queries := setup(t, false, 10000, func(a, b time.Time) []any { return []any{row(a, 1, nil)} })
	start := time.Now().UTC().Add(-time.Hour).Truncate(5 * time.Minute)
	if err := store.Set(entry.Collector.Name(), start); err != nil {
		t.Fatal(err)
	}
	scheduler.Now = func() time.Time { return start.Add(14*time.Minute + 59*time.Second) }
	if err := scheduler.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	if len(*queries) != 0 || len(out.values) != 0 {
		t.Fatal("incomplete bucket fetched")
	}
	scheduler.Now = func() time.Time { return start.Add(15 * time.Minute) }
	if err := scheduler.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	mark, _ := store.Get(entry.Collector.Name())
	if !mark.Equal(start.Add(5*time.Minute)) || out.values[semconv.MetricWorkersAIInferences] != 1 {
		t.Fatalf("complete bucket mark=%s values=%v", mark, out.values)
	}
}

func TestSaturationBisectsAndSingleBucketFailureIsAtomic(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "bisect", true: "atomic-failure"}[fail], func(t *testing.T) {
			entry, scheduler, out, store, queries := setup(t, false, 2, func(a, b time.Time) []any {
				rows := []any{row(a, 1, nil)}
				if b.Sub(a) > 5*time.Minute || fail {
					rows = append(rows, row(a, 1, nil))
				}
				return rows
			})
			start := time.Now().UTC().Add(-time.Hour).Truncate(5 * time.Minute)
			if err := store.Set(entry.Collector.Name(), start); err != nil {
				t.Fatal(err)
			}
			scheduler.Now = func() time.Time { return start.Add(20 * time.Minute) }
			err := scheduler.RunOnce(context.Background(), entry)
			mark, _ := store.Get(entry.Collector.Name())
			if fail {
				if err == nil || len(out.values) != 0 || !mark.Equal(start) {
					t.Fatalf("saturation lost atomicity: err=%v mark=%s values=%v", err, mark, out.values)
				}
			} else {
				if err != nil || out.values[semconv.MetricWorkersAIInferences] != 2 || len(*queries) != 3 {
					t.Fatalf("bisect err=%v queries=%d values=%v", err, len(*queries), out.values)
				}
			}
		})
	}
}

func TestMalformedNumericWindowIsAtomic(t *testing.T) {
	for _, bad := range []any{-1, "7", nil} {
		t.Run("numeric", func(t *testing.T) {
			entry, scheduler, out, _, _ := setup(t, true, 10000, func(a, b time.Time) []any {
				return []any{row(a, 1, map[string]any{"totalInputTokens": 5}), row(a.Add(5*time.Minute), bad, nil)}
			})
			a := time.Now().UTC().Add(-time.Hour).Truncate(5 * time.Minute)
			err := scheduler.CollectRange(context.Background(), entry.Collector.(collector.WindowCollector), a, a.Add(10*time.Minute))
			if err == nil || len(out.values) != 0 {
				t.Fatalf("malformed count committed: err=%v values=%v", err, out.values)
			}
		})
	}
}
