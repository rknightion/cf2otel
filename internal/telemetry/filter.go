package telemetry

import (
	"context"
	"time"

	"github.com/rknightion/cf2otel/internal/semconv"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/trace"
)

// denyPolicy is immutable after construction. Config validates exact semantic
// names before provider construction; no exporter-side payload filtering occurs.
type denyPolicy struct{ metrics, attributes map[string]struct{} }

func newDenyPolicy(metrics, attributes []string) *denyPolicy {
	if len(metrics) == 0 && len(attributes) == 0 {
		return nil
	}
	p := &denyPolicy{metrics: make(map[string]struct{}, len(metrics)), attributes: make(map[string]struct{}, len(attributes))}
	for _, n := range metrics {
		p.metrics[n] = struct{}{}
	}
	for _, n := range attributes {
		p.attributes[n] = struct{}{}
	}
	return p
}
func (p *denyPolicy) metric(name string) bool {
	if p == nil {
		return false
	}
	_, ok := p.metrics[name]
	return ok
}
func (p *denyPolicy) attribute(name string) bool {
	if p == nil {
		return false
	}
	_, ok := p.attributes[name]
	return ok
}
func (p *denyPolicy) attrs(input []Attr) []Attr {
	if p == nil || len(p.attributes) == 0 {
		return input
	}
	suppressIdentity := false
	if p.attribute(semconv.AttrAccessIdentityInferred) {
		for _, a := range input {
			if a.Key == semconv.AttrAccessIdentityInferred && a.Value == "true" {
				suppressIdentity = true
				break
			}
		}
	}
	out := make([]Attr, 0, len(input))
	for _, a := range input {
		if !p.dropAttribute(a.Key, suppressIdentity) {
			out = append(out, a)
		}
	}
	return out
}

// Removing a true inference qualifier must also remove the identity it qualifies.
// Decide from the original bag, not the filtered output; direct identity remains
// subject only to explicit denies. The HTTP producer uses email and login ray,
// while the other Access user fields are identity if present in a marked bag.
func (p *denyPolicy) dropAttribute(key string, suppressIdentity bool) bool {
	if p.attribute(key) {
		return true
	}
	if suppressIdentity {
		switch key {
		case semconv.AttrAccessUserEmail, semconv.AttrAccessUserID,
			semconv.AttrAccessUserIPAddress, semconv.AttrAccessIdentityLoginRayID:
			return true
		}
	}
	return false
}

func (p *denyPolicy) linkAttrs(input []attribute.KeyValue) []attribute.KeyValue {
	suppressIdentity := false
	if p.attribute(semconv.AttrAccessIdentityInferred) {
		for _, a := range input {
			if string(a.Key) == semconv.AttrAccessIdentityInferred &&
				(a.Value == attribute.BoolValue(true) || a.Value == attribute.StringValue("true")) {
				suppressIdentity = true
				break
			}
		}
	}
	out := make([]attribute.KeyValue, 0, len(input))
	for _, a := range input {
		if !p.dropAttribute(string(a.Key), suppressIdentity) {
			out = append(out, a)
		}
	}
	return out
}

// FilterEmitter wraps the source boundary, including buffered replay. Optional
// snapshot interfaces are exposed only when supported by the underlying emitter.
// Empty policies return the original emitter without adding copies or allocations.
func FilterEmitter(e Emitter, metrics, attributes []string) Emitter {
	return filterEmitterWithPolicy(e, newDenyPolicy(metrics, attributes))
}
func filterEmitterWithPolicy(e Emitter, p *denyPolicy) Emitter {
	if p == nil {
		return e
	}
	// Construction-only: configure generated semantic attributes before the
	// native SDK emitter can be used, not through mutable runtime reload.
	if native, ok := e.(*otelEmitter); ok {
		native.policy = p
	}
	f := &filteredEmitter{Emitter: e, policy: p}
	single, s := e.(SnapshotEmitter)
	batch, b := e.(SnapshotBatchEmitter)
	switch {
	case s && b:
		return &filteredSnapshots{filteredSingle: &filteredSingle{filteredEmitter: f, snapshot: single}, batch: batch}
	case s:
		return &filteredSingle{filteredEmitter: f, snapshot: single}
	case b:
		return &filteredBatch{filteredEmitter: f, batch: batch}
	default:
		return f
	}
}

type filteredEmitter struct {
	Emitter
	policy *denyPolicy
}

func (e *filteredEmitter) Gauge(ctx context.Context, n string, v float64, a ...Attr) error {
	if e.policy.metric(n) {
		return nil
	}
	return e.Emitter.Gauge(ctx, n, v, e.policy.attrs(a)...)
}
func (e *filteredEmitter) Counter(ctx context.Context, n string, v float64, a ...Attr) error {
	if e.policy.metric(n) {
		return nil
	}
	return e.Emitter.Counter(ctx, n, v, e.policy.attrs(a)...)
}
func (e *filteredEmitter) Histogram(ctx context.Context, n string, v float64, a ...Attr) error {
	if e.policy.metric(n) {
		return nil
	}
	return e.Emitter.Histogram(ctx, n, v, e.policy.attrs(a)...)
}
func (e *filteredEmitter) LogEvent(ctx context.Context, n, body string, at time.Time, severity otellog.Severity, a ...Attr) error {
	return e.Emitter.LogEvent(ctx, n, body, at, severity, e.policy.attrs(a)...)
}
func (e *filteredEmitter) Span(ctx context.Context, s SpanSpec) error {
	if len(e.policy.attributes) == 0 {
		return e.Emitter.Span(ctx, s)
	}
	s.Attrs = e.policy.attrs(s.Attrs)
	s.Events = append([]SpanEvent(nil), s.Events...)
	for i := range s.Events {
		s.Events[i].Attrs = e.policy.attrs(s.Events[i].Attrs)
	}
	s.Logs = append([]LogRecord(nil), s.Logs...)
	for i := range s.Logs {
		s.Logs[i].Attrs = e.policy.attrs(s.Logs[i].Attrs)
	}
	s.Links = append([]trace.Link(nil), s.Links...)
	for i := range s.Links {
		s.Links[i].Attributes = e.policy.linkAttrs(s.Links[i].Attributes)
	}
	return e.Emitter.Span(ctx, s)
}

// Collapsed observable-gauge series use the last input point, never SDK
// duplicate-observation summation. Reverse traversal selects winners without
// assigning values through nondeterministic map iteration.
func (p *denyPolicy) points(input []GaugePoint) []GaugePoint {
	out := make([]GaugePoint, 0, len(input))
	seen := make(map[attribute.Distinct]struct{}, len(input))
	for i := len(input) - 1; i >= 0; i-- {
		point := GaugePoint{Value: input[i].Value, Attrs: append([]Attr(nil), p.attrs(input[i].Attrs)...)}
		set := attribute.NewSet(attrs(point.Attrs)...)
		key := set.Equivalent()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, point)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
func (p *denyPolicy) snapshots(input map[string][]GaugePoint) map[string][]GaugePoint {
	out := make(map[string][]GaugePoint, len(input))
	for n, points := range input {
		if !p.metric(n) {
			out[n] = p.points(points)
		}
	}
	return out
}

type filteredSingle struct {
	*filteredEmitter
	snapshot SnapshotEmitter
}

func (e *filteredSingle) GaugeSnapshot(ctx context.Context, n string, ttl time.Duration, points []GaugePoint) error {
	if e.policy.metric(n) {
		return nil
	}
	return e.snapshot.GaugeSnapshot(ctx, n, ttl, e.policy.points(points))
}

type filteredBatch struct {
	*filteredEmitter
	batch SnapshotBatchEmitter
}

func (e *filteredBatch) GaugeSnapshots(ctx context.Context, ttl time.Duration, snapshots map[string][]GaugePoint) error {
	return e.batch.GaugeSnapshots(ctx, ttl, e.policy.snapshots(snapshots))
}

type filteredSnapshots struct {
	*filteredSingle
	batch SnapshotBatchEmitter
}

func (e *filteredSnapshots) GaugeSnapshots(ctx context.Context, ttl time.Duration, snapshots map[string][]GaugePoint) error {
	return e.batch.GaugeSnapshots(ctx, ttl, e.policy.snapshots(snapshots))
}
