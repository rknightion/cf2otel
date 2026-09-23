package telemetry

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type exportObserver struct {
	mu       sync.RWMutex
	fn       func(context.Context, string, error)
	seq      uint64
	failures []observedFailure
}

type observedFailure struct {
	seq    uint64
	signal string
	err    error
}

const maxObservedFailures = 1024

// ExportFailure preserves which OTLP signal failed through joined flush errors.
type ExportFailure struct {
	Signal string
	Err    error
}

func (e *ExportFailure) Error() string { return e.Signal + ": " + e.Err.Error() }
func (e *ExportFailure) Unwrap() error { return e.Err }

// FailureSignals returns the failed signals for commit-failure metric dimensions.
func FailureSignals(err error) string {
	set := map[string]bool{}
	var visit func(error)
	visit = func(e error) {
		if e == nil {
			return
		}
		if failure, ok := e.(*ExportFailure); ok {
			set[failure.Signal] = true
		}
		if joined, ok := e.(interface{ Unwrap() []error }); ok {
			for _, inner := range joined.Unwrap() {
				visit(inner)
			}
		} else if wrapped, ok := e.(interface{ Unwrap() error }); ok {
			visit(wrapped.Unwrap())
		}
	}
	visit(err)
	if len(set) == 0 {
		return "unknown"
	}
	signals := make([]string, 0, len(set))
	for signal := range set {
		signals = append(signals, signal)
	}
	sort.Strings(signals)
	return strings.Join(signals, ",")
}

func (o *exportObserver) Set(fn func(context.Context, string, error)) {
	o.mu.Lock()
	o.fn = fn
	o.mu.Unlock()
}

func (o *exportObserver) record(ctx context.Context, signal string, err error) {
	o.mu.Lock()
	if err != nil && signal != "metrics" {
		o.seq++
		o.failures = append(o.failures, observedFailure{seq: o.seq, signal: signal, err: err})
		if len(o.failures) > maxObservedFailures {
			o.failures = append([]observedFailure(nil), o.failures[len(o.failures)-maxObservedFailures:]...)
		}
	}
	fn := o.fn
	o.mu.Unlock()
	if fn != nil {
		fn(ctx, signal, err)
	}
}
func (o *exportObserver) snapshot() uint64 { o.mu.RLock(); defer o.mu.RUnlock(); return o.seq }
func (o *exportObserver) failedSince(seq uint64) error {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if o.seq != seq {
		failures := []error{errors.New("background OTLP export failed")}
		if len(o.failures) == 0 || seq+1 < o.failures[0].seq {
			failures = append(failures, errors.New("prior export failures omitted"))
		}
		for _, failure := range o.failures {
			if failure.seq > seq {
				failures = append(failures, &ExportFailure{Signal: failure.signal, Err: failure.err})
			}
		}
		return errors.Join(failures...)
	}
	return nil
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
