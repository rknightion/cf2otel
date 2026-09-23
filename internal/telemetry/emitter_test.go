package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type recordExporter struct{ records []sdklog.Record }

func (e *recordExporter) Export(_ context.Context, records []sdklog.Record) error {
	for _, record := range records {
		e.records = append(e.records, record.Clone())
	}
	return nil
}
func (*recordExporter) Shutdown(context.Context) error   { return nil }
func (*recordExporter) ForceFlush(context.Context) error { return nil }

type spanExporter struct{ spans []sdktrace.ReadOnlySpan }

func (e *spanExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.spans = append(e.spans, spans...)
	return nil
}
func (*spanExporter) Shutdown(context.Context) error { return nil }

func TestSpanLogRecordSharesSpanContext(t *testing.T) {
	logs := &recordExporter{}
	spans := &spanExporter{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(logs)))
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(spans)))
	t.Cleanup(func() { _ = lp.Shutdown(context.Background()); _ = tp.Shutdown(context.Background()) })
	emitter := NewEmitter(nil, lp.Logger("test"), tp.Tracer("test"))
	start := time.Now().Add(-time.Second)
	end := time.Now()
	err := emitter.Span(context.Background(), SpanSpec{Name: "request", Start: start, End: end, Logs: []LogRecord{{Name: "content", Body: "sample", At: end, Severity: otellog.SeverityInfo}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(spans.spans) != 1 || len(logs.records) != 1 {
		t.Fatalf("got %d spans and %d logs", len(spans.spans), len(logs.records))
	}
	spanContext := spans.spans[0].SpanContext()
	if logs.records[0].TraceID() != spanContext.TraceID() || logs.records[0].SpanID() != spanContext.SpanID() {
		t.Fatal("content log lost its parent span context")
	}
}

func TestSpanRejectsInvalidLogBeforeExport(t *testing.T) {
	logs := &recordExporter{}
	spans := &spanExporter{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(logs)))
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(spans)))
	t.Cleanup(func() { _ = lp.Shutdown(context.Background()); _ = tp.Shutdown(context.Background()) })
	emitter := NewEmitter(nil, lp.Logger("test"), tp.Tracer("test"))
	end := time.Now()
	err := emitter.Span(context.Background(), SpanSpec{Name: "request", Start: end.Add(-time.Second), End: end, Logs: []LogRecord{{Name: "content"}}})
	if !errors.Is(err, ErrMissingTimestamp) || len(spans.spans) != 0 || len(logs.records) != 0 {
		t.Fatalf("invalid log emitted %d spans and %d logs: %v", len(spans.spans), len(logs.records), err)
	}
}
