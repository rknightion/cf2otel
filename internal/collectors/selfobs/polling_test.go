package selfobs

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type pollingWindow struct{ started, release chan struct{} }

func (*pollingWindow) Name() string                   { return "fixture.window" }
func (*pollingWindow) DefaultInterval() time.Duration { return 5 * time.Minute }
func (*pollingWindow) Lag() time.Duration             { return 10 * time.Minute }
func (*pollingWindow) LagAt(time.Time) time.Duration  { return 15 * time.Minute }
func (w *pollingWindow) CollectWindow(ctx context.Context, _, to time.Time, _ telemetry.Emitter) (time.Time, error) {
	close(w.started)
	select {
	case <-w.release:
		return to, nil
	case <-ctx.Done():
		return time.Time{}, ctx.Err()
	}
}

func TestPollingGaugesIncludeActiveRunAndRestoredBacklog(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	emitter := &fakeEmitter{}
	stats := New(emitter, "fixture", "fixture")
	w := &pollingWindow{started: make(chan struct{}), release: make(chan struct{})}
	entry := collector.Entry{Collector: w, Interval: 5 * time.Minute, MaxWindow: time.Hour}
	stats.ObserveEntry(entry)
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(w.Name(), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	scheduler := collector.NewScheduler(nil, emitter, store)
	scheduler.Now = func() time.Time { return now }
	scheduler.OnStart = stats.StartPoll
	scheduler.OnCheckpoint = stats.Checkpoint
	scheduler.OnWindowStart = stats.Checkpoint
	done := make(chan error, 1)
	go func() { done <- scheduler.RunOnce(ctx, entry) }()
	select {
	case <-w.started:
	case <-ctx.Done():
		t.Fatal("poll did not start")
	}
	if err := stats.Collect(ctx, now.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{"cf2otel.scrape.interval": 300, "cf2otel.scrape.elapsed": 360, "cf2otel.checkpoint.backlog": 3060}
	for name, value := range want {
		found := false
		for _, g := range emitter.gauges {
			if g.name == name {
				found = true
				if g.value != value {
					t.Errorf("%s=%v want %v", name, g.value, value)
				}
			}
		}
		if !found {
			t.Errorf("missing %s before first completion", name)
		}
	}
	// Finish the real scheduler attempt before checking the completed duration.
	close(w.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := stats.Poll(ctx, w.Name(), 2*time.Minute, nil, now); err != nil {
		t.Fatal(err)
	}
	emitter.gauges = nil
	if err := stats.Collect(ctx, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, g := range emitter.gauges {
		if g.name == "cf2otel.scrape.elapsed" && g.value != 120 {
			t.Fatalf("completed elapsed=%v", g.value)
		}
	}
}

func TestSlowPreviousPollStaysVisibleUntilFasterCompletion(t *testing.T) {
	e := &fakeEmitter{}
	s := New(e, "fixture", "fixture")
	w := &pollingWindow{}
	s.ObserveEntry(collector.Entry{Collector: w, Interval: 5 * time.Minute})
	now := time.Now()
	if err := s.Poll(context.Background(), w.Name(), 6*time.Minute, nil, now); err != nil {
		t.Fatal(err)
	}
	s.StartPoll(w.Name(), now)
	if err := s.Collect(context.Background(), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range e.gauges {
		if g.name == "cf2otel.scrape.elapsed" {
			found = true
			if g.value != 360 {
				t.Fatalf("slow poll reset on start: %v", g.value)
			}
		}
	}
	if !found {
		t.Fatal("elapsed absent")
	}
}
