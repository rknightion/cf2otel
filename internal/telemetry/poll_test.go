package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/semconv"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	logspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestPollReceiverCorrelationAndFailureIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var mu sync.Mutex
	var logs []*logspb.ExportLogsServiceRequest
	var spans []*tracepb.ExportTraceServiceRequest
	var metrics []*metricspb.ExportMetricsServiceRequest
	rejectOps := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/v1/traces" {
			req := new(tracepb.ExportTraceServiceRequest)
			if err := proto.Unmarshal(data, req); err != nil {
				t.Error(err)
			}
			if rejectOps {
				for _, rs := range req.ResourceSpans {
					for _, ss := range rs.ScopeSpans {
						if ss.Scope.Name == "cf2otel.poll" {
							w.WriteHeader(400)
							return
						}
					}
				}
			}
			spans = append(spans, req)
		}
		if r.URL.Path == "/v1/metrics" {
			req := new(metricspb.ExportMetricsServiceRequest)
			if err := proto.Unmarshal(data, req); err != nil {
				t.Error(err)
			}
			metrics = append(metrics, req)
		}
		if r.URL.Path == "/v1/logs" {
			req := new(logspb.ExportLogsServiceRequest)
			if err := proto.Unmarshal(data, req); err != nil {
				t.Error(err)
			}
			logs = append(logs, req)
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer server.Close()
	p, err := NewProviders(ctx, ProviderOptions{Endpoint: server.URL, Protocol: "http", Interval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Shutdown(ctx)
	opctx, finish := p.ObservePoll(context.Background(), "fixture")
	if trace.SpanContextFromContext(opctx).IsValid() {
		t.Fatal("operational parent leaked to source context")
	}
	now := time.Now()
	if err := p.Emitter.Span(opctx, SpanSpec{Name: "source.fixture", Start: now, End: now.Add(time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	APIRequestObserver(p.Emitter)(cfapi.WithCollector(opctx, "fixture"), cfapi.RequestObservation{Started: now, Duration: time.Millisecond, Method: "rest", Status: 404, ErrorClass: "other"})
	finish(PollSummary{Collector: "fixture", Outcome: "success", Requests: cfapi.RequestTotals{Requests: 1, Failures: 1}})
	if err := p.operations.ForceFlush(ctx); err != nil {
		t.Fatal(err)
	}
	if err := p.process.ForceFlush(ctx); err != nil {
		t.Fatal(err)
	}
	if err := p.metrics.ForceFlush(ctx); err != nil {
		t.Fatal(err)
	}
	if err := p.traces.ForceFlush(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	phaseSeen, itemsSeen, sourceSeen := false, false, false
	for _, batch := range metrics {
		for _, rs := range batch.ResourceMetrics {
			for _, ss := range rs.ScopeMetrics {
				for _, m := range ss.Metrics {
					if m.Name == semconv.MetricPollPhaseDuration {
						phaseSeen = true
						if len(m.GetHistogram().DataPoints) != 3 {
							t.Error("missing phase metrics")
						}
					}
					if m.Name == semconv.MetricPollCommitted {
						itemsSeen = true
						if !m.GetSum().IsMonotonic {
							t.Error("committed counter not monotonic")
						}
					}
				}
			}
		}
	}
	var pollID, childParent, logID string
	for _, batch := range spans {
		for _, rs := range batch.ResourceSpans {
			for _, ss := range rs.ScopeSpans {
				for _, sp := range ss.Spans {
					switch sp.Name {
					case "source.fixture":
						sourceSeen = true
						if len(sp.ParentSpanId) != 0 {
							t.Error("source span inherited poll parent")
						}
					case semconv.SpanPoll:
						pollID = string(sp.SpanId)
					case semconv.SpanAPIRequest:
						childParent = string(sp.ParentSpanId)
						if sp.Status.GetCode() != 2 {
							t.Error("HTTP error span lacks error status")
						}
					}
				}
			}
		}
	}
	for _, batch := range logs {
		for _, rs := range batch.ResourceLogs {
			for _, ss := range rs.ScopeLogs {
				for _, l := range ss.LogRecords {
					var summary PollSummary
					if json.Unmarshal([]byte(l.Body.GetStringValue()), &summary) == nil && summary.Collector == "fixture" {
						logID = string(l.SpanId)
						if summary.Requests.Requests != 1 {
							t.Error("wrong summary")
						}
					}
				}
			}
		}
	}
	rejectOps = true
	mu.Unlock()
	if !phaseSeen || !itemsSeen || !sourceSeen {
		t.Fatalf("missing metrics/source witness: %v %v %v", phaseSeen, itemsSeen, sourceSeen)
	}
	if pollID == "" || childParent != pollID || logID != pollID {
		t.Fatalf("poll/API/log correlation absent poll=%x child=%x log=%x", pollID, childParent, logID)
	}
	seq := p.BeginCommit()
	_, failFinish := p.ObservePoll(context.Background(), "fixture")
	failFinish(PollSummary{Collector: "fixture", Outcome: "error"})
	if err := p.operations.ForceFlush(ctx); err == nil {
		t.Fatal("expected rejected operational export")
	}
	if err := p.Emitter.Span(context.Background(), SpanSpec{Name: "source.after-operational-failure", Start: time.Now(), End: time.Now().Add(time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	if err := p.FlushCommit(ctx, seq); err != nil {
		t.Fatalf("operational failure contaminated source commit: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	delivered := false
	for _, batch := range spans {
		for _, rs := range batch.ResourceSpans {
			for _, ss := range rs.ScopeSpans {
				for _, sp := range ss.Spans {
					if sp.Name == "source.after-operational-failure" {
						delivered = true
					}
				}
			}
		}
	}
	if !delivered {
		t.Fatal("healthy source span not delivered after operational failure")
	}
}

type blockingOperationalExporter struct{ sdktrace.SpanExporter }

func (blockingOperationalExporter) ExportSpans(ctx context.Context, _ []sdktrace.ReadOnlySpan) error {
	<-ctx.Done()
	return ctx.Err()
}
func (blockingOperationalExporter) Shutdown(context.Context) error { return nil }
func TestShutdownPrioritizesSourceOverBlockedDiagnostics(t *testing.T) {
	source := &spanExporter{}
	p := &Providers{
		logs: sdklog.NewLoggerProvider(), process: sdklog.NewLoggerProvider(), metrics: sdkmetric.NewMeterProvider(),
		traces:     sdktrace.NewTracerProvider(sdktrace.WithBatcher(source)),
		operations: sdktrace.NewTracerProvider(sdktrace.WithBatcher(blockingOperationalExporter{})),
	}
	_, sp := p.traces.Tracer("source").Start(context.Background(), "source")
	sp.End()
	_, sp = p.operations.Tracer("ops").Start(context.Background(), "ops")
	sp.End()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = p.Shutdown(ctx)
	if len(source.spans) != 1 {
		t.Fatalf("source lost while diagnostics consumed shutdown budget: %d", len(source.spans))
	}
}
