package telemetry

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/rknightion/cf2otel/internal/semconv"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// cardinalityExporter observes SDK overflow datapoints, not the number of
// measurements folded into them. A cumulative overflow series is observed on
// every collection that includes it, even when no new measurements were made.
// Its counter is collected by the SDK on the next export cycle.
type cardinalityExporter struct {
	sdkmetric.Exporter
	mu             sync.Mutex
	counter        metric.Int64Counter
	omitInstrument bool
	lastWarning    map[string]time.Time
}

func (e *cardinalityExporter) setCounter(counter metric.Int64Counter) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.counter = counter
}

func (e *cardinalityExporter) Export(ctx context.Context, data *metricdata.ResourceMetrics) error {
	for _, scope := range data.ScopeMetrics {
		for _, instrument := range scope.Metrics {
			count := overflowDatapoints(instrument.Data)
			if count == 0 {
				continue
			}
			now := time.Now()
			e.mu.Lock()
			if e.counter != nil {
				if e.omitInstrument {
					e.counter.Add(ctx, count)
				} else {
					e.counter.Add(ctx, count, metric.WithAttributes(attribute.String(semconv.AttrInstrument, instrument.Name)))
				}
			}
			last := e.lastWarning[instrument.Name]
			warn := last.IsZero() || now.Sub(last) >= time.Hour
			if warn {
				e.lastWarning[instrument.Name] = now
			}
			e.mu.Unlock()
			if warn {
				slog.WarnContext(ctx, "metric cardinality limit reached", semconv.AttrInstrument, instrument.Name)
			}
		}
	}
	return e.Exporter.Export(ctx, data)
}

func isOverflow(attrs attribute.Set) bool {
	value, ok := attrs.Value(attribute.Key("otel.metric.overflow"))
	return ok && value.Type() == attribute.BOOL && value.AsBool()
}

func overflowDatapoints(data metricdata.Aggregation) int64 {
	var count int64
	switch value := data.(type) {
	case metricdata.Sum[int64]:
		for _, point := range value.DataPoints {
			if isOverflow(point.Attributes) {
				count++
			}
		}
	case metricdata.Sum[float64]:
		for _, point := range value.DataPoints {
			if isOverflow(point.Attributes) {
				count++
			}
		}
	case metricdata.Gauge[int64]:
		for _, point := range value.DataPoints {
			if isOverflow(point.Attributes) {
				count++
			}
		}
	case metricdata.Gauge[float64]:
		for _, point := range value.DataPoints {
			if isOverflow(point.Attributes) {
				count++
			}
		}
	case metricdata.Histogram[int64]:
		for _, point := range value.DataPoints {
			if isOverflow(point.Attributes) {
				count++
			}
		}
	case metricdata.Histogram[float64]:
		for _, point := range value.DataPoints {
			if isOverflow(point.Attributes) {
				count++
			}
		}
	case metricdata.ExponentialHistogram[int64]:
		for _, point := range value.DataPoints {
			if isOverflow(point.Attributes) {
				count++
			}
		}
	case metricdata.ExponentialHistogram[float64]:
		for _, point := range value.DataPoints {
			if isOverflow(point.Attributes) {
				count++
			}
		}
	}
	return count
}
