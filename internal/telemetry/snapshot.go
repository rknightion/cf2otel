package telemetry

import (
	"context"
	"errors"
	"sort"
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

// SnapshotBatchEmitter publishes related gauge snapshots in one transaction.
// Any error leaves every previously published point and expiry unchanged.
type SnapshotBatchEmitter interface {
	GaugeSnapshots(context.Context, time.Duration, map[string][]GaugePoint) error
}

// GaugePoint is one observation in a complete gauge snapshot.
type GaugePoint struct {
	Value float64
	Attrs []Attr
}

type gaugeSnapshot struct {
	mu      *sync.RWMutex
	points  []GaugePoint
	expires time.Time
}

func (s *gaugeSnapshot) observe(_ context.Context, observer metric.Float64Observer) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !time.Now().Before(s.expires) {
		return nil
	}
	for _, p := range s.points {
		observer.Observe(p.Value, metric.WithAttributes(attrs(p.Attrs)...))
	}
	return nil
}

// GaugeSnapshot retains standalone semantics through the batch publication path.
func (e *otelEmitter) GaugeSnapshot(ctx context.Context, name string, ttl time.Duration, points []GaugePoint) error {
	return e.GaugeSnapshots(ctx, ttl, map[string][]GaugePoint{name: points})
}

// GaugeSnapshots validates, copies and registers all instruments before updating
// retained state. Instrument registration may survive a failed transaction, but
// its empty callback cannot expose unpublished points or refresh any expiry.
func (e *otelEmitter) GaugeSnapshots(ctx context.Context, ttl time.Duration, snapshots map[string][]GaugePoint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ttl <= 0 {
		return errors.New("gauge snapshot TTL must be positive")
	}
	copied := make(map[string][]GaugePoint, len(snapshots))
	names := make([]string, 0, len(snapshots))
	for name, points := range snapshots {
		if name == "" {
			return errors.New("gauge snapshot name must not be empty")
		}
		names = append(names, name)
		copyPoints := make([]GaugePoint, len(points))
		for i, p := range points {
			copyPoints[i] = GaugePoint{Value: p.Value, Attrs: append([]Attr(nil), p.Attrs...)}
		}
		copied[name] = copyPoints
	}
	sort.Strings(names)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.snapshots == nil {
		e.snapshots = make(map[string]*gaugeSnapshot)
	}
	for _, name := range names {
		if e.snapshots[name] != nil {
			continue
		}
		s := &gaugeSnapshot{mu: &e.snapshotMu}
		opts := []metric.Float64ObservableGaugeOption{metric.WithFloat64Callback(s.observe)}
		if spec, ok := semconv.Metric(name); ok {
			opts = append(opts, metric.WithUnit(spec.Unit), metric.WithDescription(spec.Description))
		}
		if _, err := e.meter.Float64ObservableGauge(name, opts...); err != nil {
			return err
		}
		e.snapshots[name] = s
	}
	e.snapshotMu.Lock()
	defer e.snapshotMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	expires := time.Now().Add(ttl)
	for _, name := range names {
		s := e.snapshots[name]
		s.points = copied[name]
		s.expires = expires
	}
	return nil
}
