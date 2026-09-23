package telemetry

import (
	"context"
	"sync"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type exportObserver struct {
	mu sync.RWMutex
	fn func(context.Context, string, error)
}

func (o *exportObserver) Set(fn func(context.Context, string, error)) {
	o.mu.Lock()
	o.fn = fn
	o.mu.Unlock()
}

func (o *exportObserver) record(ctx context.Context, signal string, err error) {
	o.mu.RLock()
	fn := o.fn
	o.mu.RUnlock()
	if fn != nil {
		fn(ctx, signal, err)
	}
}

type observedMetricExporter struct {
	sdkmetric.Exporter
	hook *exportObserver
}

func (e observedMetricExporter) Export(ctx context.Context, data *metricdata.ResourceMetrics) error {
	err := e.Exporter.Export(ctx, data)
	e.hook.record(ctx, "metrics", err)
	return err
}

type observedLogExporter struct {
	sdklog.Exporter
	hook *exportObserver
}

func (e observedLogExporter) Export(ctx context.Context, records []sdklog.Record) error {
	err := e.Exporter.Export(ctx, records)
	e.hook.record(ctx, "logs", err)
	return err
}

type observedTraceExporter struct {
	sdktrace.SpanExporter
	hook *exportObserver
}

func (e observedTraceExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	err := e.SpanExporter.ExportSpans(ctx, spans)
	e.hook.record(ctx, "traces", err)
	return err
}
