package telemetry

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/rknightion/cf2otel/internal/semconv"
	"go.opentelemetry.io/otel/metric"
)

// SnapshotEmitter replaces all points for a gauge until its snapshot expires.
// It is additive: emitters implementing only Emitter retain their Gauge fallback.
type SnapshotEmitter interface {
	GaugeSnapshot(context.Context, string, time.Duration, []GaugePoint) error
}

// GaugePoint is one observation in a complete gauge snapshot.
type GaugePoint struct {
	Value float64
	Attrs []Attr
}

type gaugeSnapshot struct {
	mu      sync.Mutex
	points  []GaugePoint
	expires time.Time
}

func (s *gaugeSnapshot) observe(_ context.Context, observer metric.Float64Observer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !time.Now().Before(s.expires) {
		return nil
	}
	for _, p := range s.points {
		observer.Observe(p.Value, metric.WithAttributes(attrs(p.Attrs)...))
	}
	return nil
}

// GaugeSnapshot uses an observable gauge so the SDK does not retain attribute
// sets from older snapshots. The caller's slices are copied before publication.
func (e *otelEmitter) GaugeSnapshot(ctx context.Context, name string, ttl time.Duration, points []GaugePoint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ttl <= 0 {
		return errors.New("gauge snapshot TTL must be positive")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.snapshots == nil {
		e.snapshots = make(map[string]*gaugeSnapshot)
	}
	s := e.snapshots[name]
	if s == nil {
		s = &gaugeSnapshot{}
		opts := []metric.Float64ObservableGaugeOption{metric.WithFloat64Callback(s.observe)}
		if spec, ok := semconv.Metric(name); ok {
			opts = append(opts, metric.WithUnit(spec.Unit), metric.WithDescription(spec.Description))
		}
		if _, err := e.meter.Float64ObservableGauge(name, opts...); err != nil {
			return err
		}
		e.snapshots[name] = s
	}
	copyPoints := make([]GaugePoint, len(points))
	for i, p := range points {
		copyPoints[i] = GaugePoint{Value: p.Value, Attrs: append([]Attr(nil), p.Attrs...)}
	}
	s.mu.Lock()
	s.points = copyPoints
	s.expires = time.Now().Add(ttl)
	s.mu.Unlock()
	return nil
}
