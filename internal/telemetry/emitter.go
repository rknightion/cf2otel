package telemetry

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/rknightion/cf2otel/internal/semconv"
)

var ErrMissingTimestamp = errors.New("telemetry timestamp is required")

type Attr struct {
	Key   string
	Value string
}
type SpanEvent struct {
	Name  string
	At    time.Time
	Attrs []Attr
}
type LogRecord struct {
	Name     string
	Body     string
	At       time.Time
	Severity otellog.Severity
	Attrs    []Attr
}
type SpanSpec struct {
	Name       string
	Start, End time.Time
	Kind       trace.SpanKind
	Links      []trace.Link
	Events     []SpanEvent
	Logs       []LogRecord
	Attrs      []Attr
	Error      error
}
type Emitter interface {
	Gauge(context.Context, string, float64, ...Attr) error
	Counter(context.Context, string, float64, ...Attr) error
	Histogram(context.Context, string, float64, ...Attr) error
	LogEvent(context.Context, string, string, time.Time, otellog.Severity, ...Attr) error
	Span(context.Context, SpanSpec) error
}
type otelEmitter struct {
	meter      metric.Meter
	logger     otellog.Logger
	tracer     trace.Tracer
	mu         sync.Mutex
	gauges     map[string]metric.Float64Gauge
	counters   map[string]metric.Float64Counter
	histograms map[string]metric.Float64Histogram
}

func NewEmitter(m metric.Meter, l otellog.Logger, t trace.Tracer) Emitter {
	return &otelEmitter{meter: m, logger: l, tracer: t, gauges: map[string]metric.Float64Gauge{}, counters: map[string]metric.Float64Counter{}, histograms: map[string]metric.Float64Histogram{}}
}
func attrs(a []Attr) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(a))
	for _, v := range a {
		if v.Key != "" {
			out = append(out, attribute.String(v.Key, v.Value))
		}
	}
	return out
}
func (e *otelEmitter) Gauge(ctx context.Context, n string, v float64, a ...Attr) error {
	e.mu.Lock()
	i := e.gauges[n]
	var err error
	if i == nil {
		i, err = e.meter.Float64Gauge(n)
		if err == nil {
			e.gauges[n] = i
		}
	}
	e.mu.Unlock()
	if err != nil {
		return err
	}
	i.Record(ctx, v, metric.WithAttributes(attrs(a)...))
	return nil
}
func (e *otelEmitter) Counter(ctx context.Context, n string, v float64, a ...Attr) error {
	e.mu.Lock()
	i := e.counters[n]
	var err error
	if i == nil {
		i, err = e.meter.Float64Counter(n)
		if err == nil {
			e.counters[n] = i
		}
	}
	e.mu.Unlock()
	if err != nil {
		return err
	}
	i.Add(ctx, v, metric.WithAttributes(attrs(a)...))
	return nil
}
func (e *otelEmitter) Histogram(ctx context.Context, n string, v float64, a ...Attr) error {
	e.mu.Lock()
	i := e.histograms[n]
	var err error
	if i == nil {
		i, err = e.meter.Float64Histogram(n)
		if err == nil {
			e.histograms[n] = i
		}
	}
	e.mu.Unlock()
	if err != nil {
		return err
	}
	i.Record(ctx, v, metric.WithAttributes(attrs(a)...))
	return nil
}
func (e *otelEmitter) LogEvent(ctx context.Context, event, body string, at time.Time, severity otellog.Severity, a ...Attr) error {
	if at.IsZero() {
		return ErrMissingTimestamp
	}
	var r otellog.Record
	r.SetTimestamp(at)
	r.SetObservedTimestamp(time.Now())
	r.SetSeverity(severity)
	r.SetBody(attribute.StringValue(body))
	r.AddAttributes(attribute.String(semconv.AttrEventName, event))
	for _, v := range a {
		if v.Key != "" {
			r.AddAttributes(attribute.String(v.Key, v.Value))
		}
	}
	e.logger.Emit(ctx, r)
	return nil
}
func (e *otelEmitter) Span(ctx context.Context, s SpanSpec) error {
	if s.Start.IsZero() || s.End.IsZero() {
		return ErrMissingTimestamp
	}
	if s.End.Before(s.Start) {
		return errors.New("span end before start")
	}
	for _, record := range s.Logs {
		if record.At.IsZero() {
			return ErrMissingTimestamp
		}
	}
	opts := []trace.SpanStartOption{trace.WithTimestamp(s.Start), trace.WithSpanKind(s.Kind), trace.WithAttributes(attrs(s.Attrs)...), trace.WithLinks(s.Links...)}
	spanCtx, sp := e.tracer.Start(ctx, s.Name, opts...)
	for _, ev := range s.Events {
		if ev.At.IsZero() {
			ev.At = s.Start
		}
		sp.AddEvent(ev.Name, trace.WithTimestamp(ev.At), trace.WithAttributes(attrs(ev.Attrs)...))
	}
	if s.Error != nil {
		sp.RecordError(s.Error)
		sp.SetStatus(codes.Error, s.Error.Error())
	}
	sp.End(trace.WithTimestamp(s.End))
	for _, record := range s.Logs {
		if err := e.LogEvent(spanCtx, record.Name, record.Body, record.At, record.Severity, record.Attrs...); err != nil {
			return err
		}
	}
	return nil
}
