package telemetry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/trace"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type failingMetricExporter struct {
	sdkmetric.Exporter
	err error
}

type failingLogExporter struct {
	sdklog.Exporter
	err error
}

func (f failingLogExporter) Export(context.Context, []sdklog.Record) error { return f.err }

func TestExporterErrorOmitsResponseBody(t *testing.T) {
	hook := &exportObserver{}
	start := hook.snapshot()
	input := errors.New("failed to send to https://example.com/v1/logs: 400 Bad Request (body: secret-prompt-sentinel)")
	exporter := observedLogExporter{Exporter: failingLogExporter{err: input}, hook: hook}
	err := exporter.Export(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "400 Bad Request") {
		t.Fatalf("missing safe status: %v", err)
	}
	if strings.Contains(err.Error(), "secret-prompt-sentinel") || strings.Contains(hook.failedSince(start).Error(), "secret-prompt-sentinel") {
		t.Fatal("exporter response leaked into diagnostics")
	}
}

func (f failingMetricExporter) Export(context.Context, *metricdata.ResourceMetrics) error {
	return f.err
}

func TestExportObserverReceivesActualResult(t *testing.T) {
	hook := &exportObserver{}
	var gotSignal string
	var gotErr error
	hook.Set(func(_ context.Context, signal string, err error) { gotSignal, gotErr = signal, err })
	want := errors.New("secret backend detail")
	exporter := observedMetricExporter{Exporter: failingMetricExporter{err: want}, hook: hook}
	if err := exporter.Export(context.Background(), &metricdata.ResourceMetrics{}); err == nil || strings.Contains(err.Error(), want.Error()) {
		t.Fatalf("export result: %v", err)
	}
	if gotSignal != "metrics" || gotErr == nil || strings.Contains(gotErr.Error(), want.Error()) {
		t.Fatalf("observer signal=%q error=%v", gotSignal, gotErr)
	}
}

func TestFailureSignalsTracksAllBackgroundErrors(t *testing.T) {
	hook := &exportObserver{}
	start := hook.snapshot()
	hook.record(context.Background(), "logs", errors.New("400 Bad Request"))
	hook.record(context.Background(), "traces", errors.New("401 Unauthorized"))
	if got := FailureSignals(hook.failedSince(start)); got != "logs,traces" {
		t.Fatalf("failed signals = %q, want logs,traces", got)
	}
}

func TestCredentialEndpointRequiresHTTPS(t *testing.T) {
	_, err := NewProviders(context.Background(), ProviderOptions{Endpoint: "http://example.com/otlp", Protocol: "http", InstanceID: "test", Token: "secret"})
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("expected HTTPS error, got %v", err)
	}
}

// A commit must send both independent signals without waiting for either
// acknowledgement, but must not finish until both acknowledgements arrive.
func TestFlushCommitOverlapsSignalsAndWaitsForBoth(t *testing.T) {
	for _, failed := range []string{"", "logs", "traces", "cancel"} {
		t.Run("failure="+failed, func(t *testing.T) {
			arrived := make(chan string, 2)
			releases := map[string]chan struct{}{"logs": make(chan struct{}), "traces": make(chan struct{})}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				signal := strings.TrimPrefix(r.URL.Path, "/v1/")
				release, ok := releases[signal]
				if !ok {
					return
				}
				_, _ = io.Copy(io.Discard, r.Body)
				arrived <- signal
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				if signal == failed {
					w.WriteHeader(http.StatusServiceUnavailable)
				}
			}))
			defer server.Close()
			p, err := NewProviders(context.Background(), ProviderOptions{Endpoint: server.URL, Protocol: "http", Interval: time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = p.Shutdown(ctx)
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			seq := p.BeginCommit()
			at := time.Now()
			if err := p.Emitter.LogEvent(ctx, "fixture.log", "payload", at, otellog.SeverityInfo); err != nil {
				t.Fatal(err)
			}
			if err := p.Emitter.Span(ctx, SpanSpec{Name: "fixture.span", Start: at, End: at.Add(time.Millisecond), Kind: trace.SpanKindInternal}); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- p.FlushCommit(ctx, seq) }()
			seen := map[string]bool{}
			timer := time.NewTimer(time.Second)
			defer timer.Stop()
			for len(seen) < 2 {
				select {
				case signal := <-arrived:
					seen[signal] = true
				case <-timer.C:
					close(releases["logs"])
					close(releases["traces"])
					<-done
					t.Fatalf("both signals must reach receiver before either is acknowledged; got %v", seen)
				}
			}
			if failed == "cancel" {
				cancel()
				select {
				case err := <-done:
					close(releases["logs"])
					close(releases["traces"])
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("missing cancellation: %v", err)
					}
				case <-time.After(time.Second):
					close(releases["logs"])
					close(releases["traces"])
					t.Fatal("flush did not join cancelled signal operations")
				}
				return
			}
			first := "logs"
			if failed != "" {
				first = failed
			}
			second := "traces"
			if first == "traces" {
				second = "logs"
			}
			close(releases[first])
			select {
			case err := <-done:
				close(releases[second])
				t.Fatalf("commit returned before both acknowledgements: %v", err)
			case <-time.After(25 * time.Millisecond):
			}
			close(releases[second])
			err = <-done
			if failed == "" && err != nil {
				t.Fatal(err)
			}
			if failed != "" && (err == nil || !strings.Contains(FailureSignals(err), failed)) {
				t.Fatalf("missing %s failure: %v", failed, err)
			}
		})
	}
}

func BenchmarkFlushCommitSignals(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/v1/logs" || r.URL.Path == "/v1/traces" {
			time.Sleep(25 * time.Millisecond)
		}
	}))
	defer server.Close()
	p, err := NewProviders(context.Background(), ProviderOptions{Endpoint: server.URL, Protocol: "http", Interval: time.Hour})
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = p.Shutdown(ctx)
	}()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		seq := p.BeginCommit()
		at := time.Now()
		if err := p.Emitter.LogEvent(ctx, "fixture.log", strings.Repeat("x", 16384), at, otellog.SeverityInfo); err != nil {
			b.Fatal(err)
		}
		if err := p.Emitter.Span(ctx, SpanSpec{Name: "fixture.span", Start: at, End: at.Add(time.Millisecond), Kind: trace.SpanKindInternal}); err != nil {
			b.Fatal(err)
		}
		if err := p.FlushCommit(ctx, seq); err != nil {
			b.Fatal(err)
		}
		cancel()
	}
	b.StopTimer()
}
