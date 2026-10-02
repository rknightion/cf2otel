package selfobs_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/collectors/selfobs"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	colMetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"
)

type zeroSnapshot struct{ fail bool }

func (*zeroSnapshot) Name() string                   { return "inventory" }
func (*zeroSnapshot) DefaultInterval() time.Duration { return time.Hour }
func (c *zeroSnapshot) Collect(context.Context, telemetry.Emitter) error {
	if c.fail {
		return context.DeadlineExceeded
	}
	return nil
}

type zeroWindow struct{}

func (zeroWindow) Name() string                   { return "access" }
func (zeroWindow) DefaultInterval() time.Duration { return time.Hour }
func (zeroWindow) Lag() time.Duration             { return 0 }
func (zeroWindow) CollectWindow(_ context.Context, _, to time.Time, _ telemetry.Emitter) (time.Time, error) {
	return to, nil
}

type rejectedCommit struct{}

func (rejectedCommit) BeginCommit() uint64                       { return 1 }
func (rejectedCommit) FlushCommit(context.Context, uint64) error { return context.DeadlineExceeded }

// Observe actual OTLP points: an absent series is not the same as a zero point.
func TestRegisteredZeroCountersRealSDK(t *testing.T) {
	var mu sync.Mutex
	points := map[string]float64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/metrics" {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var req colMetric.ExportMetricsServiceRequest
			if err := proto.Unmarshal(body, &req); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			for _, resource := range req.ResourceMetrics {
				for _, scope := range resource.ScopeMetrics {
					for _, metric := range scope.Metrics {
						for _, point := range metric.GetSum().GetDataPoints() {
							var name, class, outcome string
							for _, attr := range point.Attributes {
								switch attr.Key {
								case semconv.AttrCollector:
									name = attr.Value.GetStringValue()
								case semconv.AttrErrorClass:
									class = attr.Value.GetStringValue()
								case semconv.AttrWindowOutcome:
									outcome = attr.Value.GetStringValue()
								}
							}
							points[metric.Name+"/"+name+"/"+class+"/"+outcome] = point.GetAsDouble()
						}
					}
				}
			}
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	providers, err := telemetry.NewProviders(ctx, telemetry.ProviderOptions{Endpoint: server.URL, Protocol: "http", Interval: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := providers.Shutdown(shutdown); err != nil {
			t.Error(err)
		}
	}()
	stats := selfobs.New(providers.Emitter, "fixture", "fixture")
	registry := collector.NewRegistry()
	snapshot := &zeroSnapshot{}
	registry.RegisterSnapshot(snapshot, 100*time.Millisecond)
	cfg := &config.Config{Collectors: map[string]config.CollectorConfig{"selfobs": {Enabled: false}}}
	selfobs.Register(collector.Deps{Config: cfg, Registry: registry, SelfObs: selfobs.NewCollector(stats)})
	// The window is registered after selfobs; disabled collectors never register.
	registry.RegisterWindow(zeroWindow{}, 100*time.Millisecond, time.Minute, time.Minute)
	for _, entry := range registry.Entries() {
		stats.Expect(entry.Collector.Name())
	}
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	scheduler := collector.NewScheduler(registry, providers.Emitter, store)
	scheduler.Flusher = providers
	scheduler.OnPoll = func(ctx context.Context, name string, d time.Duration, err error, at time.Time) {
		if e := stats.Poll(ctx, name, d, err, at); e != nil {
			t.Error(e)
		}
	}
	runCtx, stopRun := context.WithCancel(ctx)
	completed := make(chan string, 2)
	onPoll := scheduler.OnPoll
	scheduler.OnPoll = func(ctx context.Context, name string, d time.Duration, err error, at time.Time) {
		onPoll(ctx, name, d, err, at)
		select {
		case completed <- name:
		default:
		}
	}
	done := make(chan struct{})
	go func() { scheduler.Run(runCtx); close(done) }()
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case name := <-completed:
			seen[name] = true
		case <-ctx.Done():
			t.Fatal("collectors did not start")
		}
	}
	stopRun()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("scheduler did not stop")
	}
	if err := providers.FlushCommit(ctx, providers.BeginCommit()); err != nil {
		t.Fatal(err)
	}
	assertPoint := func(metric, name, class, outcome string, want float64) {
		t.Helper()
		key := metric + "/" + name + "/" + class + "/" + outcome
		mu.Lock()
		got, ok := points[key]
		mu.Unlock()
		deadline := time.Now().Add(2 * time.Second)
		for (!ok || got != want) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
			mu.Lock()
			got, ok = points[key]
			mu.Unlock()
		}
		if !ok || got != want {
			t.Fatalf("OTLP point %s: present=%v value=%v, want present=true value=%v", key, ok, got, want)
		}
	}
	assertPoint(semconv.MetricScrapeErrors, "inventory", "other", "", 0)
	assertPoint(semconv.MetricScrapeErrors, "access", "other", "", 0)
	assertPoint(semconv.MetricWindowCommitFailures, "access", "", "", 0)
	snapshot.fail = true
	err = scheduler.RunOnce(ctx, registry.Entries()[0])
	if err == nil {
		t.Fatal("expected snapshot failure")
	}
	if err := stats.Poll(ctx, snapshot.Name(), time.Millisecond, err, time.Now()); err != nil {
		t.Fatal(err)
	}
	scheduler.Flusher = rejectedCommit{}
	scheduler.Now = func() time.Time { return time.Now().Add(time.Minute) }
	err = scheduler.RunOnce(ctx, registry.Entries()[1])
	if err == nil {
		t.Fatal("expected window commit failure")
	}
	if err := stats.Poll(ctx, "access", time.Millisecond, err, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := providers.FlushCommit(ctx, providers.BeginCommit()); err != nil {
		t.Fatal(err)
	}
	assertPoint(semconv.MetricScrapeErrors, "inventory", "timeout", "", 1)
	assertPoint(semconv.MetricScrapeErrors, "access", "timeout", "", 1)
	assertPoint(semconv.MetricScrapeErrors, "inventory", "other", "", 0)
	assertPoint(semconv.MetricWindowCommitFailures, "access", "", "retry", 1)
	mu.Lock()
	defer mu.Unlock()
	for key := range points {
		if key == semconv.MetricWindowCommitFailures+"/inventory//" || key == semconv.MetricScrapeErrors+"/selfobs/other/" {
			t.Fatalf("disabled or snapshot-only counter: %s", key)
		}
	}
}
