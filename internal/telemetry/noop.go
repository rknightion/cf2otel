package telemetry

import (
	"context"
	"time"

	otellog "go.opentelemetry.io/otel/log"
)

type noopEmitter struct{}

func NewNoopEmitter() Emitter                                                 { return noopEmitter{} }
func (noopEmitter) Gauge(context.Context, string, float64, ...Attr) error     { return nil }
func (noopEmitter) Counter(context.Context, string, float64, ...Attr) error   { return nil }
func (noopEmitter) Histogram(context.Context, string, float64, ...Attr) error { return nil }
func (noopEmitter) LogEvent(context.Context, string, string, time.Time, otellog.Severity, ...Attr) error {
	return nil
}
func (noopEmitter) Span(context.Context, SpanSpec) error { return nil }
