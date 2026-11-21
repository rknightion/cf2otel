package selfobs

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/collector"
)

type startupWindow struct{ pollingWindow }

func (*startupWindow) Name() string { return "fixture.unstarted" }

// A restart must not hide a collector's persisted checkpoint until its first
// poll: selfobs runs once at startup and then only every interval.
func TestFirstCollectAfterStartupReportsPersistedCheckpoints(t *testing.T) {
	now := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	emitter := &fakeEmitter{}
	stats := New(emitter, "fixture", "fixture")
	stored := &pollingWindow{}
	fresh := &startupWindow{}
	registry := collector.NewRegistry()
	registry.ObserveRegistrations(stats.ObserveEntry)
	registry.RegisterWindow(stored, 5*time.Minute, time.Hour, time.Hour)
	registry.RegisterWindow(fresh, 5*time.Minute, time.Hour, time.Hour)
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(stored.Name(), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	scheduler := collector.NewScheduler(registry, emitter, store)
	scheduler.Now = func() time.Time { return now }
	scheduler.OnStart = stats.StartPoll
	scheduler.OnCheckpoint = stats.Checkpoint
	scheduler.OnWindowStart = stats.Checkpoint
	// Cancelled before any collector's staggered first poll can start.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scheduler.Run(ctx)
	if err := stats.Collect(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	ages := map[string]float64{}
	for _, g := range emitter.gauges {
		if g.name == "cf2otel.checkpoint.age" {
			ages[g.attrs[0].Value] = g.value
		}
	}
	if got, ok := ages[stored.Name()]; !ok || got != 3600 {
		t.Errorf("persisted checkpoint age = %v (present %v), want 3600 before the first poll", got, ok)
	}
	if got, ok := ages[fresh.Name()]; ok {
		t.Errorf("collector without a checkpoint reported age %v before its first window", got)
	}
}
