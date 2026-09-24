package collector

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/telemetry"
)

type catchupWindow struct {
	name      string
	mu        sync.Mutex
	calls     [][2]time.Time
	onCollect func(context.Context, int) error
}

func (w *catchupWindow) Name() string                 { return w.name }
func (*catchupWindow) DefaultInterval() time.Duration { return 5 * time.Minute }
func (*catchupWindow) Lag() time.Duration             { return 0 }
func (w *catchupWindow) CollectWindow(ctx context.Context, from, to time.Time, e telemetry.Emitter) (time.Time, error) {
	w.mu.Lock()
	w.calls = append(w.calls, [2]time.Time{from, to})
	n := len(w.calls)
	w.mu.Unlock()
	if w.onCollect != nil {
		if err := w.onCollect(ctx, n); err != nil {
			return from, err
		}
	}
	if err := e.LogEvent(ctx, "fixture.event", from.Format(time.RFC3339), from, 0); err != nil {
		return from, err
	}
	return to, nil
}

type deadlineFlusher struct {
	mu        sync.Mutex
	calls     int
	failAt    int
	deadlines []time.Duration
}

func (*deadlineFlusher) BeginCommit() uint64 { return 0 }
func (f *deadlineFlusher) FlushCommit(ctx context.Context, _ uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	deadline, ok := ctx.Deadline()
	if !ok {
		return errors.New("missing commit deadline")
	}
	f.deadlines = append(f.deadlines, time.Until(deadline))
	if f.calls == f.failAt {
		return errors.New("export failed")
	}
	return nil
}

func TestCatchupFourWindowsInOneTick(t *testing.T) {
	end := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("catchup", end.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	w := &catchupWindow{name: "catchup"}
	emitter := &recordingEmitter{}
	f := &deadlineFlusher{}
	s := NewScheduler(nil, emitter, store)
	s.Flusher = f
	s.Now = func() time.Time { return end }
	entry := Entry{Collector: w, Interval: 5 * time.Minute, MaxWindow: 15 * time.Minute}
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	if len(w.calls) != 3 {
		t.Fatalf("windows=%d, want three in one tick", len(w.calls))
	}
	for i, call := range w.calls {
		wantFrom := end.Add(-time.Hour + time.Duration(i)*15*time.Minute)
		if !call[0].Equal(wantFrom) || !call[1].Equal(wantFrom.Add(15*time.Minute)) {
			t.Fatalf("window %d=%v", i, call)
		}
	}
	mark, _ := store.Get(w.Name())
	if !mark.Equal(end.Add(-15 * time.Minute)) {
		t.Fatalf("checkpoint=%s, want one window behind", mark)
	}
	if f.calls != 3 {
		t.Fatalf("flushed=%d, want three", f.calls)
	}
	for _, d := range f.deadlines {
		if d <= 0 || d > 90*time.Second {
			t.Fatalf("commit deadline=%s", d)
		}
	}
	if emitter.catchupCount != 2 {
		t.Fatalf("catch-up metric=%d, want two", emitter.catchupCount)
	}
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	if len(w.calls) != 4 {
		t.Fatalf("four source windows took %d ticks; calls=%d", 2, len(w.calls))
	}
	mark, _ = store.Get(w.Name())
	if !mark.Equal(end) {
		t.Fatalf("checkpoint=%s, want %s", mark, end)
	}
}

func TestCatchupStopsAtFailedSecondWindowAndResumes(t *testing.T) {
	end := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("catchup", end.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	w := &catchupWindow{name: "catchup"}
	f := &deadlineFlusher{failAt: 2}
	s := NewScheduler(nil, &recordingEmitter{}, store)
	s.Flusher = f
	s.Now = func() time.Time { return end }
	entry := Entry{Collector: w, Interval: 5 * time.Minute, MaxWindow: 15 * time.Minute}
	if err := s.RunOnce(context.Background(), entry); err == nil {
		t.Fatal("second export failure should stop catch-up")
	}
	mark, _ := store.Get(w.Name())
	if !mark.Equal(end.Add(-45*time.Minute)) || len(w.calls) != 2 {
		t.Fatalf("checkpoint=%s calls=%d", mark, len(w.calls))
	}
	f.failAt = 0
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	if len(w.calls) != 4 || !w.calls[2][0].Equal(w.calls[1][0]) {
		t.Fatalf("replay boundary calls=%v", w.calls)
	}
}

func TestCatchupCancellationKeepsLastCommittedCheckpoint(t *testing.T) {
	end := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("cancel.catchup", end.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &catchupWindow{name: "cancel.catchup"}
	s := NewScheduler(nil, &recordingEmitter{}, store)
	s.Flusher = &deadlineFlusher{}
	s.Now = func() time.Time { return end }
	s.OnCheckpoint = func(_ string, _ time.Time) { cancel() }
	err = s.RunOnce(ctx, Entry{Collector: w, Interval: 5 * time.Minute, MaxWindow: 15 * time.Minute})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want cancellation", err)
	}
	mark, _ := store.Get(w.Name())
	if len(w.calls) != 1 || !mark.Equal(end.Add(-45*time.Minute)) {
		t.Fatalf("calls=%d checkpoint=%s", len(w.calls), mark)
	}
}

func TestCatchupStopsOnProviderThrottle(t *testing.T) {
	end := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("throttled.catchup", end.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	w := &catchupWindow{name: "throttled.catchup", onCollect: func(_ context.Context, n int) error {
		if n == 2 {
			return errors.New("cloudflare 429 Too Many Requests")
		}
		return nil
	}}
	s := NewScheduler(nil, &recordingEmitter{}, store)
	s.Flusher = &deadlineFlusher{}
	s.Now = func() time.Time { return end }
	err = s.RunOnce(context.Background(), Entry{Collector: w, Interval: 5 * time.Minute, MaxWindow: 15 * time.Minute})
	if err == nil || err.Error() != "cloudflare 429 Too Many Requests" {
		t.Fatalf("error=%v", err)
	}
	mark, _ := store.Get(w.Name())
	if len(w.calls) != 2 || !mark.Equal(end.Add(-45*time.Minute)) {
		t.Fatalf("calls=%d checkpoint=%s", len(w.calls), mark)
	}
}

func TestCatchupReleasesCommitForAnotherCollector(t *testing.T) {
	end := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("first.catchup", end.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("second.catchup", end.Add(-15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	secondStarted := make(chan struct{})
	release := make(chan struct{})
	a := &catchupWindow{name: "first.catchup", onCollect: func(ctx context.Context, n int) error {
		if n == 2 {
			close(secondStarted)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}}
	b := &catchupWindow{name: "second.catchup"}
	s := NewScheduler(nil, &recordingEmitter{}, store)
	s.Flusher = &deadlineFlusher{}
	s.Now = func() time.Time { return end }
	other := NewScheduler(nil, &recordingEmitter{}, store)
	other.Flusher = &deadlineFlusher{}
	other.Now = func() time.Time { return end }
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- s.RunOnce(context.Background(), Entry{Collector: a, Interval: 5 * time.Minute, MaxWindow: 15 * time.Minute})
	}()
	select {
	case <-secondStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("second source window did not start")
	}
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- other.RunOnce(context.Background(), Entry{Collector: b, Interval: 5 * time.Minute, MaxWindow: 15 * time.Minute})
	}()
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second collector could not commit between catch-up windows")
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	mark, _ := store.Get(b.Name())
	if !mark.Equal(end) {
		t.Fatalf("second collector checkpoint=%s", mark)
	}
}
