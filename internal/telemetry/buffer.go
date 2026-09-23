package telemetry

import (
	"context"
	"errors"
	"time"

	otellog "go.opentelemetry.io/otel/log"
)

// Buffer holds one complete window before any SDK queue is touched.
type Buffer struct {
	Records []BufferedRecord
	Metrics []BufferedMetric
}
type BufferedRecord struct {
	Event, Body string
	At          time.Time
	Severity    otellog.Severity
	Attrs       []Attr
	Span        *SpanSpec
}
type BufferedMetric struct {
	Kind, Name string
	Value      float64
	Attrs      []Attr
}

func copied(a []Attr) []Attr { return append([]Attr(nil), a...) }
func (b *Buffer) Gauge(_ context.Context, n string, v float64, a ...Attr) error {
	b.Metrics = append(b.Metrics, BufferedMetric{"gauge", n, v, copied(a)})
	return nil
}
func (b *Buffer) Counter(_ context.Context, n string, v float64, a ...Attr) error {
	b.Metrics = append(b.Metrics, BufferedMetric{"counter", n, v, copied(a)})
	return nil
}
func (b *Buffer) Histogram(_ context.Context, n string, v float64, a ...Attr) error {
	b.Metrics = append(b.Metrics, BufferedMetric{"histogram", n, v, copied(a)})
	return nil
}
func (b *Buffer) LogEvent(_ context.Context, event, body string, at time.Time, severity otellog.Severity, a ...Attr) error {
	if at.IsZero() {
		return ErrMissingTimestamp
	}
	b.Records = append(b.Records, BufferedRecord{Event: event, Body: body, At: at, Severity: severity, Attrs: copied(a)})
	return nil
}
func (b *Buffer) Span(_ context.Context, s SpanSpec) error {
	if s.Start.IsZero() || s.End.IsZero() {
		return ErrMissingTimestamp
	}
	if s.End.Before(s.Start) {
		return errors.New("span end before start")
	}
	for _, r := range s.Logs {
		if r.At.IsZero() {
			return ErrMissingTimestamp
		}
	}
	s.Attrs = copied(s.Attrs)
	s.Events = append([]SpanEvent(nil), s.Events...)
	for i := range s.Events {
		s.Events[i].Attrs = copied(s.Events[i].Attrs)
	}
	s.Logs = append([]LogRecord(nil), s.Logs...)
	for i := range s.Logs {
		s.Logs[i].Attrs = copied(s.Logs[i].Attrs)
	}
	s.Links = append(s.Links[:0:0], s.Links...)
	b.Records = append(b.Records, BufferedRecord{Span: &s})
	return nil
}
func (r BufferedRecord) Replay(ctx context.Context, e Emitter) error {
	if r.Span != nil {
		return e.Span(ctx, *r.Span)
	}
	return e.LogEvent(ctx, r.Event, r.Body, r.At, r.Severity, r.Attrs...)
}
func (m BufferedMetric) Replay(ctx context.Context, e Emitter) error {
	switch m.Kind {
	case "gauge":
		return e.Gauge(ctx, m.Name, m.Value, m.Attrs...)
	case "counter":
		return e.Counter(ctx, m.Name, m.Value, m.Attrs...)
	default:
		return e.Histogram(ctx, m.Name, m.Value, m.Attrs...)
	}
}
