package collector

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/telemetry"
)

// budgetTestWindow records the payload budgets the scheduler hands it and
// stops one second into every window while stopEarly is set.
type budgetTestWindow struct {
	windowFake
	budgets   []int
	stopEarly bool
}

func (*budgetTestWindow) AdaptiveCommitWindow() bool { return true }
func (w *budgetTestWindow) CollectWindowBudget(ctx context.Context, from, to time.Time, budget int, e telemetry.Emitter) (time.Time, error) {
	w.budgets = append(w.budgets, budget)
	if _, err := w.CollectWindow(ctx, from, to, e); err != nil {
		return time.Time{}, err
	}
	if err := e.LogEvent(ctx, "test.event", "", from, 0); err != nil {
		return time.Time{}, err
	}
	if w.stopEarly && to.Sub(from) > time.Second {
		return from.Add(time.Second), nil
	}
	return to, nil
}

// budgetDeadlineFlusher blocks to the aggregate commit deadline while failing is set.
type budgetDeadlineFlusher struct{ failing bool }

func (*budgetDeadlineFlusher) BeginCommit() uint64 { return 0 }
func (f *budgetDeadlineFlusher) FlushCommit(ctx context.Context, _ uint64) error {
	if !f.failing {
		return nil
	}
	<-ctx.Done()
	return &telemetry.ExportFailure{Signal: "traces", Err: ctx.Err()}
}

func TestCommitBudgetHalvesOnDeadlineAndRecovers(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	w := &budgetTestWindow{}
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	from := now.Add(-time.Minute)
	if err := store.Set(w.Name(), from); err != nil {
		t.Fatal(err)
	}
	flusher := &budgetDeadlineFlusher{failing: true}
	s := NewScheduler(nil, &recordingEmitter{}, store)
	s.Flusher = flusher
	s.Now = func() time.Time { return now }
	s.CommitTimeout = 50 * time.Millisecond
	s.CommitBudgetBytes = 4 * commitChunkBytes
	entry := Entry{Collector: w, MaxWindow: 6 * time.Hour} // one window per run, however far the clock moves

	// Two deadlines halve 4 chunks to 1; a third stays at the one-chunk floor.
	for i, want := range []string{"next commit at most 1048576 bytes", "next commit at most 524288 bytes", "smallest 524288-byte commit payload cannot fit"} {
		err := s.RunOnce(context.Background(), entry)
		var deadline *CommitDeadlineError
		if !errors.As(err, &deadline) || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), want) || payloadRejected(err) {
			t.Fatalf("deadline %d: %v", i, err)
		}
		if mark, _ := store.Get(w.Name()); !mark.Equal(from) {
			t.Fatalf("deadline %d moved the checkpoint to %s", i, mark)
		}
	}
	if got := s.sourceWindowLimit(w, entry); got != entry.MaxWindow {
		t.Fatalf("a budgeted collector also shrank its source window to %s", got)
	}

	// Successes inside the recovery interval keep the reduced budget; after it
	// the budget doubles once per interval back to the configured value.
	flusher.failing = false
	for _, wait := range []time.Duration{time.Minute, commitBudgetRecovery, time.Minute, commitBudgetRecovery, time.Minute} {
		now = now.Add(wait)
		if err := s.RunOnce(context.Background(), entry); err != nil {
			t.Fatal(err)
		}
	}
	want := []int{4 * commitChunkBytes, 2 * commitChunkBytes, commitChunkBytes, commitChunkBytes, commitChunkBytes, 2 * commitChunkBytes, 2 * commitChunkBytes, 4 * commitChunkBytes}
	if len(w.budgets) != len(want) {
		t.Fatalf("budgets %v, want %v", w.budgets, want)
	}
	for i := range want {
		if w.budgets[i] != want[i] {
			t.Fatalf("budgets %v, want %v", w.budgets, want)
		}
	}
}

func TestBudgetedCollectorContinuesAfterEarlyMark(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	w := &budgetTestWindow{stopEarly: true}
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	from := now.Add(-time.Minute)
	if err := store.Set(w.Name(), from); err != nil {
		t.Fatal(err)
	}
	s := NewScheduler(nil, &recordingEmitter{}, store)
	s.Now = func() time.Time { return now }
	entry := Entry{Collector: w, MaxWindow: time.Minute}
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	// The remaining window is within MaxWindow, which ends a run for an
	// ordinary collector; an early mark keeps this one going to the per-run cap.
	if mark, _ := store.Get(w.Name()); !mark.Equal(from.Add(maxWindowsPerTick * time.Second)) {
		t.Fatalf("checkpoint %s after one run", mark)
	}
	for i, call := range w.calls {
		if !call[0].Equal(from.Add(time.Duration(i)*time.Second)) || !call[1].Equal(now) {
			t.Fatalf("call %d window %v", i, call)
		}
	}

	// A fractional mark from a budgeted collector is still rejected.
	partial := &adaptiveBudgetPartial{}
	if err := store.Set(partial.Name(), from); err != nil {
		t.Fatal(err)
	}
	if err := s.RunOnce(context.Background(), Entry{Collector: partial, MaxWindow: time.Minute}); err == nil || !strings.Contains(err.Error(), "whole-second") {
		t.Fatalf("fractional mark accepted: %v", err)
	}
}

type adaptiveBudgetPartial struct{ budgetTestWindow }

func (*adaptiveBudgetPartial) Name() string { return "test.partial" }
func (w *adaptiveBudgetPartial) CollectWindowBudget(_ context.Context, from, _ time.Time, _ int, _ telemetry.Emitter) (time.Time, error) {
	return from.Add(1500 * time.Millisecond), nil
}
