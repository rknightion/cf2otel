package telemetry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// "é" is 0xc3 0xa9; dropping the last byte leaves a split rune, which the
// OTLP protobuf marshal rejects before any request is sent.
const splitRune = "body ends mid-rune \xc3"

func TestEmitterExportsInvalidUTF8Strings(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	ctx := context.Background()
	hook := &exportObserver{}
	start := hook.snapshot()

	lx, err := otlploghttp.New(ctx, otlploghttp.WithEndpointURL(srv.URL+"/v1/logs"), otlploghttp.WithRetry(otlploghttp.RetryConfig{Enabled: false}))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(srv.URL+"/v1/traces"), otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}))
	if err != nil {
		t.Fatal(err)
	}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(observedLogExporter{Exporter: lx, hook: hook})))
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(observedTraceExporter{SpanExporter: tx, hook: hook}))
	defer func() { _ = lp.Shutdown(ctx); _ = tp.Shutdown(ctx) }()

	e := NewEmitter(nil, lp.Logger("test"), tp.Tracer("test"))
	at := time.Now()
	if err := e.LogEvent(ctx, "test.event", splitRune, at, otellog.SeverityInfo, Attr{Key: "body", Value: splitRune}); err != nil {
		t.Fatal(err)
	}
	if err := e.Span(ctx, SpanSpec{Name: "span " + splitRune, Start: at, End: at.Add(time.Millisecond), Attrs: []Attr{{Key: "body", Value: splitRune}},
		Events: []SpanEvent{{Name: "event", At: at, Attrs: []Attr{{Key: "body", Value: splitRune}}}}}); err != nil {
		t.Fatal(err)
	}
	if err := hook.failedSince(start); err != nil {
		t.Fatalf("export failed for a split rune: %v", err)
	}
	if requests.Load() < 2 {
		t.Fatalf("server received %d OTLP requests, want logs and traces", requests.Load())
	}
}

func TestSafeExportErrorNamesInvalidUTF8(t *testing.T) {
	err := safeExportError(errors.New("proto: string field contains invalid UTF-8"))
	if err == nil || !strings.Contains(err.Error(), "invalid UTF-8") {
		t.Fatalf("marshal failure is not classified: %v", err)
	}
}
