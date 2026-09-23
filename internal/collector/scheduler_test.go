package collector

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/telemetry"
)

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
	if _, ok := store.Get(w.Name()); ok {
		t.Fatal("checkpoint advanced on failed collection")
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
