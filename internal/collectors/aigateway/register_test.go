package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	colLog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colTrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/proto"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

func TestMain(m *testing.M) {
	// Real loopback fixtures retain caller deadlines, using explicit process pacing.
	if err := cfapi.ConfigureProcessRateLimit(config.RateLimitConfig{RequestsPerSecond: 10000, Burst: 1}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestRegisterCapsWindowForBoundedExport(t *testing.T) {
	for _, tc := range []struct {
		configured, want time.Duration
	}{
		{3 * time.Hour, 15 * time.Minute},
		{5 * time.Minute, 5 * time.Minute},
	} {
		cfg := &config.Config{Collectors: map[string]config.CollectorConfig{
			"aigateway.logs": {Enabled: true, Interval: time.Minute, InitialLookback: 3 * time.Hour, MaxWindow: tc.configured},
		}}
		registry := collector.NewRegistry()
		Register(collector.Deps{Config: cfg, Registry: registry})
		entries := registry.Entries()
		if len(entries) != 1 || entries[0].MaxWindow != tc.want || entries[0].InitialLookback != 3*time.Hour {
			t.Fatalf("configured %s: entries = %#v, want window %s and preserved lookback", tc.configured, entries, tc.want)
		}
	}
}

// Exercise the production 90-second budget with invented, not live, traffic.
// Fifty large records exceed it; two halvings partition complete timestamp
// groups into successful commits without reducing the configured capture cap.
const deliveryEdgeDelay = time.Second

type deliveryMetrics struct {
	telemetry.Emitter
	requests float64
}

func (e *deliveryMetrics) Counter(ctx context.Context, name string, value float64, attrs ...telemetry.Attr) error {
	if name == semconv.MetricAIGatewayRequests {
		e.requests += value
	}
	return e.Emitter.Counter(ctx, name, value, attrs...)
}
func deliveryAttr(attrs []*common.KeyValue, key string) string {
	for _, a := range attrs {
		if a.Key == key {
			return a.Value.GetStringValue()
		}
	}
	return ""
}

func TestRegisterIndividualPOSTTimeoutDoesNotSubdivide(t *testing.T) {
	// Only the exporter deadline expires. The global 90-second budget must not
	// be mistaken for exhausted pressure, and three timeouts must never drop.
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_TIMEOUT", "20")
	from := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/logs"):
			if r.URL.Query().Get("page") == "1" {
				_, _ = io.WriteString(w, `{"success":true,"result":[{"id":"fixture","created_at":"2026-09-30T12:00:00.5Z","duration":1,"model":"fixture"}]}`)
			} else {
				_, _ = io.WriteString(w, `{"success":true,"result":[]}`)
			}
		case strings.HasSuffix(r.URL.Path, "/request"), strings.HasSuffix(r.URL.Path, "/response"):
			_, _ = io.WriteString(w, `{}`)
		default:
			_, _ = io.WriteString(w, `{"success":true,"result":{}}`)
		}
	}))
	defer apiServer.Close()
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/logs" {
			select {
			case <-r.Context().Done():
			case <-time.After(time.Second):
			}
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer sink.Close()
	providers, err := telemetry.NewProviders(context.Background(), telemetry.ProviderOptions{Endpoint: sink.URL, Protocol: "http", Interval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = providers.Shutdown(ctx)
	}()
	cfg := config.Default()
	cfg.Cloudflare = config.CloudflareConfig{APIBase: apiServer.URL, APIToken: "fixture", Timeout: time.Second, MaxResponseBytes: 4096}
	cfg.Collectors = map[string]config.CollectorConfig{"aigateway.logs": {Enabled: true, InitialLookback: 15 * time.Minute, MaxWindow: 15 * time.Minute}}
	cfg.AIGateway = config.AIGatewayConfig{Gateways: []string{"fixture-gateway"}, CaptureBodies: true, MaxBodyBytes: 163840}
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: &cfg, API: cfapi.New(cfg.Cloudflare), Registry: registry})
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	metrics := &deliveryMetrics{Emitter: providers.Emitter}
	scheduler := collector.NewScheduler(registry, metrics, store)
	scheduler.Flusher = providers
	scheduler.Now = func() time.Time { return from.Add(16 * time.Minute) }
	for i := 0; i < 3; i++ {
		err := scheduler.RunOnce(context.Background(), registry.Entries()[0])
		var aggregate *collector.CommitDeadlineError
		if !errors.Is(err, context.DeadlineExceeded) || errors.As(err, &aggregate) {
			t.Fatalf("single POST timeout classified as aggregate: %v", err)
		}
		mark, _ := store.Get("aigateway.logs")
		if !mark.Equal(from) || metrics.requests != 0 {
			t.Fatalf("single POST timeout advanced cursor/metrics: %s / %v", mark, metrics.requests)
		}
	}
}

func TestRegisterAdaptiveDeliveryBudget(t *testing.T) {
	end := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	to := end.Add(-time.Minute)
	from := to.Add(-15 * time.Minute)
	rows := make([]map[string]any, 0, 53)
	times := map[string]time.Time{}
	rows = append(rows, map[string]any{"id": "outside-lower", "created_at": from.Add(-time.Millisecond), "duration": 1})
	for i := 0; i < 50; i++ {
		at := from.Add(time.Duration(i/2)*18*time.Second + 500*time.Millisecond)
		if i == 26 || i == 27 {
			at = from.Add(225 * time.Second)
		}
		id := fmt.Sprintf("fixture-%d", i)
		times[id] = at
		row := map[string]any{"id": id, "created_at": at, "duration": 1, "path": "chat/completions", "model": "fixture"}
		rows = append(rows, row)
		if i == 0 {
			rows = append(rows, row)
		} // inclusive/pagination duplicate
	}
	rows = append(rows, map[string]any{"id": "outside-upper", "created_at": to, "duration": 1})
	var mu sync.Mutex
	var sourceWindows [][2]time.Time
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/logs") {
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			if page == 1 {
				lower, err1 := time.Parse(time.RFC3339Nano, r.URL.Query().Get("start_date"))
				upper, err2 := time.Parse(time.RFC3339Nano, r.URL.Query().Get("end_date"))
				if err1 != nil || err2 != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				mu.Lock()
				sourceWindows = append(sourceWindows, [2]time.Time{lower, upper})
				mu.Unlock()
			}
			start := (page - 1) * 50
			if start < 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if start > len(rows) {
				start = len(rows)
			}
			stop := min(start+50, len(rows))
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": rows[start:stop]})
		} else if strings.HasSuffix(r.URL.Path, "/request") || strings.HasSuffix(r.URL.Path, "/response") {
			_, _ = io.WriteString(w, `"`+strings.Repeat("x", 163838)+`"`)
		} else {
			_, _ = io.WriteString(w, `{"success":true,"result":{}}`)
		}
	}))
	defer source.Close()
	logs, spans := map[string]int{}, map[string]int{}
	stable := map[string]string{}
	var identityErr error
	var exportedFailures int
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/v1/logs" || r.URL.Path == "/v1/traces" {
			select {
			case <-time.After(deliveryEdgeDelay):
			case <-r.Context().Done():
				return
			}
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/v1/logs":
			var req colLog.ExportLogsServiceRequest
			if proto.Unmarshal(payload, &req) != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			for _, rs := range req.ResourceLogs {
				for _, ss := range rs.ScopeLogs {
					for _, record := range ss.LogRecords {
						id := deliveryAttr(record.Attributes, semconv.AttrAIGatewayLogID)
						side := deliveryAttr(record.Attributes, semconv.AttrAIGatewayContentSide)
						// Ignore self-observability; every AI signal has a stable source identity.
						if id == "" {
							continue
						}
						key := id + "/" + side
						identity := strconv.FormatUint(record.TimeUnixNano, 10) + "/" + deliveryAttr(record.Attributes, semconv.AttrAIGatewayName)
						if prior, ok := stable[key]; ok && prior != identity {
							identityErr = fmt.Errorf("unstable event key %s", key)
						}
						stable[key] = identity
						logs[key]++
						if side != "" && len(record.Body.GetStringValue()) != 163840 {
							identityErr = fmt.Errorf("capture cap changed: %s has %d bytes", key, len(record.Body.GetStringValue()))
						}
					}
				}
			}
		case "/v1/traces":
			var req colTrace.ExportTraceServiceRequest
			if proto.Unmarshal(payload, &req) != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			for _, rs := range req.ResourceSpans {
				for _, ss := range rs.ScopeSpans {
					for _, span := range ss.Spans {
						spans[deliveryAttr(span.Attributes, semconv.AttrAIGatewayLogID)]++
					}
				}
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer sink.Close()
	providers, err := telemetry.NewProviders(context.Background(), telemetry.ProviderOptions{Endpoint: sink.URL, Protocol: "http", Interval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = providers.Shutdown(ctx)
	}()
	providers.SetExportObserver(func(_ context.Context, _ string, err error) {
		if err != nil {
			mu.Lock()
			exportedFailures++
			mu.Unlock()
		}
	})
	cfg := config.Default()
	cfg.Cloudflare = config.CloudflareConfig{APIBase: source.URL, APIToken: "fixture", Timeout: 5 * time.Second, MaxResponseBytes: 1024 * 1024}
	cfg.Collectors = map[string]config.CollectorConfig{"aigateway.logs": {Enabled: true, Interval: time.Minute, InitialLookback: 15 * time.Minute, MaxWindow: 3 * time.Hour}}
	cfg.AIGateway = config.AIGatewayConfig{Gateways: []string{"fixture-gateway"}, CaptureBodies: true, MaxBodyBytes: 163840}
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: &cfg, API: cfapi.New(cfg.Cloudflare), Registry: registry})
	statePath := filepath.Join(t.TempDir(), "state.json")
	store, err := collector.NewFileStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	metrics := &deliveryMetrics{Emitter: providers.Emitter}
	scheduler := collector.NewScheduler(registry, metrics, store)
	scheduler.Flusher = providers
	scheduler.Now = func() time.Time { return end }
	var committed [][2]time.Time
	previous := from
	scheduler.OnCheckpoint = func(_ string, mark time.Time) {
		if mark.Nanosecond() != 0 {
			t.Errorf("fractional checkpoint: %s", mark)
		}
		mu.Lock()
		defer mu.Unlock()
		for id, at := range times {
			if at.Before(previous) || !at.Before(mark) {
				continue
			}
			if logs[id+"/"] == 0 || logs[id+"/request"] == 0 || logs[id+"/response"] == 0 || spans[id] == 0 {
				t.Errorf("checkpoint before required signals for %s", id)
			}
		}
		committed = append(committed, [2]time.Time{previous, mark})
		previous = mark
	}
	// The sink answers each POST after a second, so one commit of the 8 MiB
	// budget (about 24 capped rows) cannot fit a 15-second aggregate budget.
	scheduler.CommitTimeout = 15 * time.Second
	scheduler.CommitBudgetBytes = 8 << 20
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()
	entry := registry.Entries()[0]
	err = scheduler.RunOnce(ctx, entry)
	var aggregate *collector.CommitDeadlineError
	if !errors.As(err, &aggregate) || !errors.Is(err, context.DeadlineExceeded) || aggregate.RetryBudget != 4<<20 {
		t.Fatalf("first commit: want an aggregate deadline that halves the payload budget, got %v", err)
	}
	reopened, err := collector.NewFileStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if mark, _ := reopened.Get("aigateway.logs"); !mark.Equal(from) || metrics.requests != 0 || len(committed) != 0 {
		t.Fatalf("failed commit advanced checkpoint=%s metrics=%v", mark, metrics.requests)
	}
	// Later deadlines are allowed while the budget settles; each one must
	// leave the checkpoint and the metrics of the failed slice untouched.
	deadlines := 1
	for run := 0; run < 20; run++ {
		before, requests := previous, metrics.requests
		err = scheduler.RunOnce(ctx, entry)
		if err != nil {
			if !errors.As(err, &aggregate) {
				t.Fatalf("run %d: %v", run, err)
			}
			deadlines++
			if previous.Equal(before) && metrics.requests != requests {
				t.Fatalf("run %d: a failed slice replayed its metrics", run)
			}
		}
		if previous.Equal(to) {
			break
		}
	}
	reopened, err = collector.NewFileStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	mark, _ := reopened.Get("aigateway.logs")
	if !mark.Equal(to) || metrics.requests != 50 {
		t.Fatalf("final checkpoint=%s requests=%v, want %s / 50 without successful-slice overlap", mark, metrics.requests, to)
	}
	mu.Lock()
	defer mu.Unlock()
	// Every retry keeps the configured window: the payload budget, not the
	// source interval, is what shrinks.
	for _, window := range sourceWindows {
		if window[0].Nanosecond() != 0 || !window[1].Equal(to) {
			t.Fatalf("source window changed: %v", window)
		}
	}
	if identityErr != nil {
		t.Fatal(identityErr)
	}
	if exportedFailures == 0 {
		t.Fatal("missing real SDK export failure evidence")
	}
	for id := range times {
		if logs[id+"/"] < 1 || logs[id+"/request"] < 1 || logs[id+"/response"] < 1 || spans[id] < 1 {
			t.Fatalf("required records not accepted for %s", id)
		}
	}
	if len(spans) != 50 || len(logs) != 150 {
		t.Fatalf("endpoint/duplicate filtering failed: unique spans=%d logs=%d", len(spans), len(logs))
	}
	if len(committed) < 2 {
		t.Fatalf("the burst committed in %d slice(s); the budget never applied", len(committed))
	}
	t.Logf("%d aggregate deadlines preserved cursor/metrics; %d budgeted slices committed; 50 unique spans / 150 unique logs; capture cap unchanged", deadlines, len(committed))
}
