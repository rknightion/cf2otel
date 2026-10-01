package telemetry

import (
	"context"
	"errors"
	"sort"
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
	instrument metric.Float64ObservableGauge
	points     []GaugePoint
	expires    time.Time
}

// observeSnapshots holds one publication read lock and captures one collection
// instant for every retained instrument, including instruments added later.
func (e *otelEmitter) observeSnapshots(_ context.Context, observer metric.Observer) error {
	e.snapshotMu.RLock()
	defer e.snapshotMu.RUnlock()
	now := time.Now()
	for _, s := range e.snapshots {
		if !now.Before(s.expires) {
			continue
		}
		for _, p := range s.points {
			observer.ObserveFloat64(s.instrument, p.Value, metric.WithAttributes(attrs(p.Attrs)...))
		}
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
	// The map is also read by collection callbacks. Do not hold its lock
	// during SDK registration/unregistration, which can wait for collection.
	e.snapshotMu.RLock()
	instruments := make([]metric.Observable, 0, len(e.snapshots)+len(names))
	for _, s := range e.snapshots {
		instruments = append(instruments, s.instrument)
	}
	e.snapshotMu.RUnlock()
	added := make(map[string]*gaugeSnapshot)
	for _, name := range names {
		if e.snapshots[name] != nil {
			continue
		}
		opts := []metric.Float64ObservableGaugeOption{}
		if spec, ok := semconv.Metric(name); ok {
			opts = append(opts, metric.WithUnit(spec.Unit), metric.WithDescription(spec.Description))
		}
		instrument, err := e.meter.Float64ObservableGauge(name, opts...)
		if err != nil {
			return err
		}
		added[name] = &gaugeSnapshot{instrument: instrument}
		instruments = append(instruments, instrument)
	}
	if len(added) > 0 {
		// Both registrations can briefly observe the SAME old state, but no
		// publication occurs until the old callback is drained and removed.
		// Thus an obsolete callback cannot regenerate a prior generation.
		registration, err := e.meter.RegisterCallback(e.observeSnapshots, instruments...)
		if err != nil {
			return err
		}
		if e.snapshotRegistration != nil {
			if err := e.snapshotRegistration.Unregister(); err != nil {
				return errors.Join(err, registration.Unregister())
			}
		}
		e.snapshotRegistration = registration
	}
	e.snapshotMu.Lock()
	defer e.snapshotMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	for name, s := range added {
		e.snapshots[name] = s
	}
	expires := time.Now().Add(ttl)
	for _, name := range names {
		s := e.snapshots[name]
		s.points = copied[name]
		s.expires = expires
	}
	return nil
}
