package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/rknightion/cf2otel/internal/semconv"
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

type metricSnapshot struct {
	unit, description string
	bounds            []float64
	counts            []uint64
	value             float64
}

type metricCapture struct{ metrics map[string]metricSnapshot }

func (*metricCapture) Temporality(k sdkmetric.InstrumentKind) metricdata.Temporality {
	return sdkmetric.DefaultTemporalitySelector(k)
}
func (*metricCapture) Aggregation(k sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.DefaultAggregationSelector(k)
}
func (e *metricCapture) Export(_ context.Context, data *metricdata.ResourceMetrics) error {
	e.metrics = make(map[string]metricSnapshot)
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			snapshot := metricSnapshot{unit: m.Unit, description: m.Description}
			switch points := m.Data.(type) {
			case metricdata.Gauge[float64]:
				if len(points.DataPoints) == 1 {
					snapshot.value = points.DataPoints[0].Value
				}
			case metricdata.Histogram[float64]:
				if len(points.DataPoints) == 1 {
					snapshot.bounds = append([]float64(nil), points.DataPoints[0].Bounds...)
					snapshot.counts = append([]uint64(nil), points.DataPoints[0].BucketCounts...)
				}
			}
			e.metrics[m.Name] = snapshot
		}
	}
	return nil
}
func (*metricCapture) ForceFlush(context.Context) error { return nil }
func (*metricCapture) Shutdown(context.Context) error   { return nil }

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

func TestBufferCopiesLinkAttributes(t *testing.T) {
	links := []trace.Link{{Attributes: []attribute.KeyValue{attribute.String("source", "before")}}}
	start := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	buffer := &Buffer{}
	if err := buffer.Span(context.Background(), SpanSpec{Name: "request", Start: start, End: start.Add(time.Second), Links: links}); err != nil {
		t.Fatal(err)
	}
	links[0].Attributes[0] = attribute.String("source", "after")
	got := buffer.Records[0].Span.Links[0].Attributes[0].Value.AsString()
	if got != "before" {
		t.Fatalf("buffered link attribute=%q, want before", got)
	}
}

func TestEmitterExportsMetricMetadataAndDurationBuckets(t *testing.T) {
	exporter := &metricCapture{}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(time.Hour))))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	emitter := NewEmitter(provider.Meter("test"), nil, nil)
	ctx := context.Background()
	for _, record := range []struct {
		name  string
		value float64
		kind  string
	}{
		{semconv.MetricAPIRequests, 1, "counter"},
		{semconv.MetricCheckpointAge, 9, "gauge"},
		{semconv.MetricRUMLCPP75, 750, "gauge"},
		{semconv.MetricGenAIInputTokens, 12, "counter"},
		{semconv.MetricGenAIDuration, 0.7, "histogram"},
		{semconv.MetricAPIDuration, 0.03, "histogram"},
		{semconv.MetricScrapeDuration, 0.2, "histogram"},
	} {
		var err error
		switch record.kind {
		case "counter":
			err = emitter.Counter(ctx, record.name, record.value)
		case "gauge":
			err = emitter.Gauge(ctx, record.name, record.value)
		case "histogram":
			err = emitter.Histogram(ctx, record.name, record.value)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := provider.ForceFlush(ctx); err != nil {
		t.Fatal(err)
	}
	metrics := exporter.metrics
	for _, want := range []struct{ name, unit string }{
		{semconv.MetricAPIRequests, "{request}"},
		{semconv.MetricCheckpointAge, "s"},
		{semconv.MetricRUMLCPP75, "s"},
		{semconv.MetricGenAIInputTokens, "{token}"},
		{semconv.MetricGenAIDuration, "s"},
		{semconv.MetricAPIDuration, "s"},
		{semconv.MetricScrapeDuration, "s"},
	} {
		got, ok := metrics[want.name]
		if !ok || got.unit != want.unit || got.description == "" {
			t.Errorf("metric %s: found=%t unit=%q description=%q; want unit=%q and description", want.name, ok, got.unit, got.description, want.unit)
		}
	}
	if got := metrics[semconv.MetricRUMLCPP75].value; got != 0.75 {
		t.Errorf("RUM LCP conversion: got %v, want 0.75 seconds", got)
	}
	for _, want := range []struct {
		name       string
		boundary   float64
		bucketHits uint64
	}{
		{semconv.MetricGenAIDuration, 0.5, 0},
		{semconv.MetricAPIDuration, 0.05, 1},
		{semconv.MetricScrapeDuration, 0.25, 1},
	} {
		point, ok := metrics[want.name]
		if !ok || len(point.bounds) == 0 {
			t.Errorf("metric %s: expected histogram data point", want.name)
			continue
		}
		found := false
		for i, boundary := range point.bounds {
			if boundary == want.boundary {
				found = true
				if point.counts[i] != want.bucketHits {
					t.Errorf("metric %s bucket at %v: got %d, want %d", want.name, boundary, point.counts[i], want.bucketHits)
				}
			}
		}
		if !found {
			t.Errorf("metric %s: missing boundary %v in %v", want.name, want.boundary, point.bounds)
		}
	}
}
