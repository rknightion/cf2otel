package telemetry

import (
	"context"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type operationalTraceExporter struct {
	sdktrace.SpanExporter
	hook *exportObserver
}

func (e operationalTraceExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	err := safeExportError(e.SpanExporter.ExportSpans(ctx, spans))
	e.hook.mu.RLock()
	fn := e.hook.fn
	e.hook.mu.RUnlock()
	if fn != nil {
		fn(ctx, "operational_traces", err)
	}
	return err
}
