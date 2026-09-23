package collector

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	otellog "go.opentelemetry.io/otel/log"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type recordingEmitter struct {
	logs, metrics, gapCount int
	gapValue                float64
	failureAttrs            [][]telemetry.Attr
}

func (*recordingEmitter) Gauge(context.Context, string, float64, ...telemetry.Attr) error { return nil }
func (r *recordingEmitter) Counter(_ context.Context, name string, value float64, attrs ...telemetry.Attr) error {
	r.metrics++
	if name == semconv.MetricWindowGap {
		r.gapCount++
		r.gapValue += value
	}
	if name == semconv.MetricWindowCommitFailures {
		r.failureAttrs = append(r.failureAttrs, append([]telemetry.Attr(nil), attrs...))
	}
	return nil
}
func (*recordingEmitter) Histogram(context.Context, string, float64, ...telemetry.Attr) error {
	return nil
}
func (r *recordingEmitter) LogEvent(context.Context, string, string, time.Time, otellog.Severity, ...telemetry.Attr) error {
	r.logs++
	return nil
}
func (*recordingEmitter) Span(context.Context, telemetry.SpanSpec) error { return nil }

type partialWindow struct{}

func (*partialWindow) Name() string                   { return "test.partial" }
func (*partialWindow) DefaultInterval() time.Duration { return time.Minute }
func (*partialWindow) Lag() time.Duration             { return 0 }
func (*partialWindow) CollectWindow(ctx context.Context, from, to time.Time, e telemetry.Emitter) (time.Time, error) {
	_ = e.LogEvent(ctx, "test.event", "body", from, otellog.SeverityInfo)
	_ = e.Counter(ctx, "test.count", 1)
	return from, errors.New("second fetch failed")
}
func TestPartialWindowDoesNotEmit(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "checkpoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	real := &recordingEmitter{}
	s := NewScheduler(nil, real, store)
	s.Now = func() time.Time { return time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC) }
	entry := Entry{Collector: &partialWindow{}, Interval: time.Minute, InitialLookback: time.Minute}
	if err := s.RunOnce(context.Background(), entry); err == nil {
		t.Fatal("expected fetch failure")
	}
	if real.logs != 0 || real.metrics != 0 {
		t.Fatalf("emitted before complete: logs=%d metrics=%d", real.logs, real.metrics)
	}
}
func TestExplicitRangeDiscardsPartialFetch(t *testing.T) {
	emitter := &recordingEmitter{}
	s := NewScheduler(nil, emitter, nil)
	from := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	if err := s.CollectRange(context.Background(), &partialWindow{}, from, from.Add(time.Minute)); err == nil {
		t.Fatal("expected fetch failure")
	}
	if emitter.logs != 0 || emitter.metrics != 0 {
		t.Fatalf("explicit range leaked logs=%d metrics=%d", emitter.logs, emitter.metrics)
	}
}

type emitWindow struct {
	name string
	n    int
	gap  time.Time
}

func (w *emitWindow) Name() string                 { return w.name }
func (*emitWindow) DefaultInterval() time.Duration { return time.Minute }
func (*emitWindow) Lag() time.Duration             { return 0 }
func (w *emitWindow) CollectWindow(ctx context.Context, from, to time.Time, e telemetry.Emitter) (time.Time, error) {
	if !w.gap.IsZero() && from.Before(w.gap) {
		return from, &cfapi.RetentionGapError{Dataset: "fixture", Floor: w.gap}
	}
	for i := 0; i < w.n; i++ {
		if err := e.LogEvent(ctx, "test.event", "x", from, otellog.SeverityInfo); err != nil {
			return from, err
		}
	}
	if err := e.Counter(ctx, "test.count", 1); err != nil {
		return from, err
	}
	return to, nil
}

type stubFlusher struct {
	mu      sync.Mutex
	err     error
	calls   int
	started chan struct{}
	release chan struct{}
}

func (*stubFlusher) BeginCommit() uint64 { return 0 }
func (f *stubFlusher) FlushCommit(_ context.Context, _ uint64) error {
	f.mu.Lock()
	f.calls++
	started := f.started
	release := f.release
	err := f.err
	f.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if release != nil {
		<-release
	}
	return err
}
func TestCommitFailureAndPayloadDrop(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure error
		drop    bool
	}{
		{"401", errors.New("401 Unauthorized"), false},
		{"400", errors.New("failed to send logs to http://127.0.0.1/v1/logs: 400 Bad Request (body: empty)"), true},
		{"mixed_400_503", errors.Join(errors.New("failed to send logs to http://127.0.0.1/v1/logs: 400 Bad Request (body: empty)"), errors.New("traces export: processor export timeout: retry-able request failure: body: (empty)")), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			e := &recordingEmitter{}
			f := &stubFlusher{err: tc.failure}
			s := NewScheduler(nil, e, store)
			s.Flusher = f
			now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
			s.Now = func() time.Time { return now }
			w := &emitWindow{name: "test." + tc.name, n: 1}
			entry := Entry{Collector: w, Interval: time.Minute, InitialLookback: time.Minute}
			for i := 0; i < 3; i++ {
				now = now.Add(time.Minute)
				runErr := s.RunOnce(context.Background(), entry)
				if i < 2 || !tc.drop {
					if runErr == nil {
						t.Fatal("expected stalled checkpoint")
					}
				} else if runErr != nil {
					t.Fatal(runErr)
				}
			}
			mark, present := store.Get(w.Name())
			want := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
			if tc.drop {
				want = want.Add(time.Minute)
			}
			if !present || !mark.Equal(want) {
				t.Fatalf("checkpoint=%s present=%v, want %s", mark, present, want)
			}
			if e.metrics != 3 {
				t.Fatalf("applied payload metric or missing failure metric: %d", e.metrics)
			}
			for _, attrs := range e.failureAttrs {
				values := map[string]string{}
				for _, attr := range attrs {
					values[attr.Key] = attr.Value
				}
				if values[semconv.AttrCollector] != w.Name() || values[semconv.AttrExportSignal] != "unknown" || values[semconv.AttrWindowOutcome] == "" {
					t.Fatalf("commit failure dimensions: %v", values)
				}
			}
		})
	}
}
func TestConcurrentCommitIsolation(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	e := &recordingEmitter{}
	f := &stubFlusher{err: errors.New("503 Service Unavailable"), started: make(chan struct{}, 1), release: make(chan struct{})}
	s := NewScheduler(nil, e, store)
	s.Flusher = f
	s.Now = func() time.Time { return time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC) }
	a := Entry{Collector: &emitWindow{name: "a", n: 1}, Interval: time.Minute, InitialLookback: time.Minute}
	b := Entry{Collector: &emitWindow{name: "b", n: 1}, Interval: time.Minute, InitialLookback: time.Minute}
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { first <- s.RunOnce(context.Background(), a) }()
	<-f.started
	go func() { second <- s.RunOnce(context.Background(), b) }()
	select {
	case <-second:
		t.Fatal("second commit escaped first flush")
	case <-time.After(20 * time.Millisecond):
	}
	close(f.release)
	if err := <-first; err == nil {
		t.Fatal("first flush unexpectedly succeeded")
	}
	if err := <-second; err == nil {
		t.Fatal("second flush unexpectedly succeeded")
	}
	if mark, ok := store.Get("b"); !ok || !mark.Equal(time.Date(2026, 9, 23, 11, 59, 0, 0, time.UTC)) {
		t.Fatalf("second checkpoint=%s, want initial lower cursor", mark)
	}
}

type cancelAwareFlusher struct{ started, release chan struct{} }

func (*cancelAwareFlusher) BeginCommit() uint64 { return 0 }
func (f *cancelAwareFlusher) FlushCommit(ctx context.Context, _ uint64) error {
	close(f.started)
	<-f.release
	return ctx.Err()
}
func TestCommitCompletesAfterPollCancellation(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := &cancelAwareFlusher{started: make(chan struct{}), release: make(chan struct{})}
	s := NewScheduler(nil, &recordingEmitter{}, store)
	s.Flusher = f
	s.Now = func() time.Time { return time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC) }
	entry := Entry{Collector: &emitWindow{name: "cancel", n: 1}, Interval: time.Minute, InitialLookback: time.Minute}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.RunOnce(ctx, entry) }()
	<-f.started
	cancel()
	select {
	case <-done:
		t.Fatal("commit returned before export finished")
	case <-time.After(20 * time.Millisecond):
	}
	close(f.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("cancel"); !ok {
		t.Fatal("checkpoint absent after completed commit")
	}
}
func TestRetentionGapSkipsPastMovingFloor(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	e := &recordingEmitter{}
	s := NewScheduler(nil, e, store)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	w := &emitWindow{name: "gap", gap: now.Add(-5 * time.Minute), n: 1}
	entry := Entry{Collector: w, Interval: time.Minute, InitialLookback: 10 * time.Minute}
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	mark, ok := store.Get("gap")
	if !ok || !mark.After(w.gap) {
		t.Fatalf("gap mark=%s", mark)
	}
	w.gap = w.gap.Add(time.Minute)
	now = now.Add(time.Minute)
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	if e.logs != 2 {
		t.Fatalf("expected gap and next window logs, got %d", e.logs)
	}
	if e.gapCount != 1 || e.gapValue != mark.Sub(now.Add(-11*time.Minute)).Seconds() {
		t.Fatalf("gap metric count=%d seconds=%v, want one %v-second increment", e.gapCount, e.gapValue, mark.Sub(now.Add(-11*time.Minute)).Seconds())
	}
}

type joinedGapWindow struct{ earlier, later time.Time }

func (*joinedGapWindow) Name() string                   { return "joined.gap" }
func (*joinedGapWindow) DefaultInterval() time.Duration { return time.Minute }
func (*joinedGapWindow) Lag() time.Duration             { return time.Minute }
func (w *joinedGapWindow) CollectWindow(context.Context, time.Time, time.Time, telemetry.Emitter) (time.Time, error) {
	return time.Time{}, errors.Join(&cfapi.RetentionGapError{Dataset: "first", Floor: w.earlier}, &cfapi.RetentionGapError{Dataset: "second", Floor: w.later})
}
func TestRetentionGapUsesLatestReportedFloor(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	w := &joinedGapWindow{earlier: now.Add(-6 * time.Minute), later: now.Add(-5 * time.Minute)}
	s := NewScheduler(nil, &recordingEmitter{}, store)
	s.Now = func() time.Time { return now }
	entry := Entry{Collector: w, Interval: time.Minute, InitialLookback: 10 * time.Minute}
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	mark, ok := store.Get(w.Name())
	want := w.later.Add(3 * time.Minute)
	if !ok || !mark.Equal(want) {
		t.Fatalf("checkpoint=%s, want latest floor plus margin %s", mark, want)
	}
}

type windowFake struct {
	calls [][2]time.Time
	fail  bool
}

func (*windowFake) Name() string                   { return "test.window" }
func (*windowFake) DefaultInterval() time.Duration { return time.Minute }
func (*windowFake) Lag() time.Duration             { return 0 }
func (w *windowFake) CollectWindow(_ context.Context, from, to time.Time, _ telemetry.Emitter) (time.Time, error) {
	w.calls = append(w.calls, [2]time.Time{from, to})
	if w.fail {
		return time.Time{}, errors.New("transient")
	}
	return to, nil
}
func TestWindowRestartBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoints.json")
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	entry := Entry{Collector: &windowFake{}, Interval: time.Minute, InitialLookback: 10 * time.Minute, MaxWindow: 5 * time.Minute}
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s := NewScheduler(nil, nil, store)
	s.Now = func() time.Time { return now }
	w := entry.Collector.(*windowFake)
	w.fail = true
	if err := s.RunOnce(context.Background(), entry); err == nil {
		t.Fatal("expected failure")
	}
	if mark, ok := store.Get(w.Name()); !ok || !mark.Equal(now.Add(-10*time.Minute)) {
		t.Fatalf("checkpoint=%s, want initial lower cursor", mark)
	}
	w.fail = false
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Checkpoints = reopened
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	if len(w.calls) != 3 {
		t.Fatalf("calls=%d", len(w.calls))
	}
	if !w.calls[1][1].Equal(w.calls[2][0]) {
		t.Fatalf("gap or overlap: first end=%v restart start=%v", w.calls[1][1], w.calls[2][0])
	}
}

func TestZeroLookbackStartsFromDurableNow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoints.json")
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	w := &windowFake{}
	entry := Entry{Collector: w, Interval: time.Minute, InitialLookback: 0, MaxWindow: time.Minute}
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s := NewScheduler(nil, nil, store)
	s.Now = func() time.Time { return now }
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	if len(w.calls) != 0 {
		t.Fatal("zero-lookback collector should not consume history")
	}
	reopened, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Checkpoints = reopened
	s.Now = func() time.Time { return now.Add(time.Minute) }
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	if len(w.calls) != 1 || !w.calls[0][0].Equal(now) || !w.calls[0][1].Equal(now.Add(time.Minute)) {
		t.Fatalf("restart window=%v", w.calls)
	}
}

func TestWindowCheckpointUsesGraphQLSecondPrecision(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 750000000, time.UTC)
	store, err := NewFileStore(filepath.Join(t.TempDir(), "checkpoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	w := &windowFake{}
	s := NewScheduler(nil, nil, store)
	s.Now = func() time.Time { return now }
	entry := Entry{Collector: w, Interval: time.Minute, InitialLookback: time.Minute, MaxWindow: time.Minute}
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	if len(w.calls) != 1 || w.calls[0][0].Nanosecond() != 0 || w.calls[0][1].Nanosecond() != 0 {
		t.Fatalf("GraphQL cannot represent subsecond checkpoint: %v", w.calls)
	}
}
