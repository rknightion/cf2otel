package selfobs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/identity"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	otellog "go.opentelemetry.io/otel/log"
)

type sample struct {
	name  string
	value float64
	attrs []telemetry.Attr
}
type fakeEmitter struct {
	mu                           sync.Mutex
	gauges, counters, histograms []sample
}

func (e *fakeEmitter) Gauge(_ context.Context, n string, v float64, a ...telemetry.Attr) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.gauges = append(e.gauges, sample{n, v, a})
	return nil
}
func (e *fakeEmitter) Counter(_ context.Context, n string, v float64, a ...telemetry.Attr) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.counters = append(e.counters, sample{n, v, a})
	return nil
}
func (e *fakeEmitter) Histogram(_ context.Context, n string, v float64, a ...telemetry.Attr) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.histograms = append(e.histograms, sample{n, v, a})
	return nil
}
func (e *fakeEmitter) LogEvent(context.Context, string, string, time.Time, otellog.Severity, ...telemetry.Attr) error {
	return nil
}
func (e *fakeEmitter) Span(context.Context, telemetry.SpanSpec) error { return nil }

func TestPollAndCollect(t *testing.T) {
	e := &fakeEmitter{}
	s := New(e, "0.1.0", "abc")
	s.Expect("httpreq.events")
	s.SetIdentityStats(func() identity.Stats { return identity.Stats{Matched: 2, Unmatched: 3, Ambiguous: 1} })
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	if err := s.Poll(context.Background(), "access.logins", 250*time.Millisecond, nil, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Poll(context.Background(), "httpreq.events", time.Second, context.DeadlineExceeded, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Export(context.Background(), "logs", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Export(context.Background(), "traces", context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	s.Checkpoint("access.logins", now.Add(-time.Minute))
	if err := s.Collect(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if len(e.counters) != 4 || len(e.histograms) != 2 {
		t.Fatalf("counters=%d histograms=%d", len(e.counters), len(e.histograms))
	}
	if len(e.gauges) != 7 {
		t.Fatalf("gauges=%d", len(e.gauges))
	}
	identityValues := map[string]float64{}
	for _, g := range e.gauges {
		identityValues[g.name] = g.value
	}
	if identityValues[semconv.MetricIdentityMatched] != 2 || identityValues[semconv.MetricIdentityUnmatched] != 3 || identityValues[semconv.MetricIdentityAmbiguous] != 1 {
		t.Fatalf("identity outcome gauges: %+v", identityValues)
	}
	missing := false
	for _, g := range e.gauges {
		if g.name == "cf2otel.scrape.last_success_timestamp" && g.value == 0 && len(g.attrs) == 1 && g.attrs[0].Value == "httpreq.events" {
			missing = true
		}
	}
	if !missing {
		t.Fatal("missing collector did not produce sentinel last-success gauge")
	}
}
