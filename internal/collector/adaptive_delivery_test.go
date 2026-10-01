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

type adaptiveTestWindow struct {
	windowFake
	partial bool
}

func (*adaptiveTestWindow) AdaptiveCommitWindow() bool { return true }
func (w *adaptiveTestWindow) CollectWindow(ctx context.Context, from, to time.Time, e telemetry.Emitter) (time.Time, error) {
	mark, err := w.windowFake.CollectWindow(ctx, from, to, e)
	if w.partial {
		mark = from.Add(time.Millisecond)
	}
	return mark, err
}

func TestAdaptiveDeadlineFloorAndRestart(t *testing.T) {
	from := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	w := &adaptiveTestWindow{}
	entry := Entry{Collector: w, MaxWindow: 15 * time.Minute}
	s := NewScheduler(nil, nil, nil)
	cause := errors.Join(&telemetry.ExportFailure{Signal: "logs", Err: context.DeadlineExceeded}, &telemetry.ExportFailure{Signal: "traces", Err: context.DeadlineExceeded})
	window := 15 * time.Minute
	for window > time.Second {
		commitMu.Lock()
		err := s.reduceSourceWindow(w, from, from.Add(window), cause)
		commitMu.Unlock()
		var deadline *CommitDeadlineError
		if !errors.As(err, &deadline) || !errors.Is(err, context.DeadlineExceeded) || telemetry.FailureSignals(err) != "logs,traces" {
			t.Fatalf("deadline lost required-signal identity: %v", err)
		}
		want := max(time.Second, (window / 2).Truncate(time.Second))
		if got := s.sourceWindowLimit(w, entry); got != want || got%time.Second != 0 {
			t.Fatalf("limit=%s want whole-second half %s", got, want)
		}
		window = want
	}
	commitMu.Lock()
	err := s.reduceSourceWindow(w, from, from.Add(time.Second), cause)
	commitMu.Unlock()
	if !strings.Contains(err.Error(), "one-second source window cannot fit") || !strings.Contains(err.Error(), "checkpoint retained") || payloadRejected(err) {
		t.Fatalf("irreducible group must fail closed, not become a payload drop: %v", err)
	}
	// Neither a healthy unopted collector nor a new scheduler learns this limit.
	if got := s.sourceWindowLimit(&w.windowFake, entry); got != entry.MaxWindow {
		t.Fatalf("unopted collector inherited adaptive limit %s", got)
	}
	if got := NewScheduler(nil, nil, nil).sourceWindowLimit(w, entry); got != entry.MaxWindow {
		t.Fatalf("process restart did not restore configured bound: %s", got)
	}
}

func TestAdaptiveIndividualDeadlineAndExplicitRangeDoNotLearn(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	w := &adaptiveTestWindow{}
	entry := Entry{Collector: w, InitialLookback: 15 * time.Minute, MaxWindow: 15 * time.Minute}
	s := NewScheduler(nil, &recordingEmitter{}, store)
	s.Flusher = &stubFlusher{err: &telemetry.ExportFailure{Signal: "logs", Err: context.DeadlineExceeded}}
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if err := s.RunOnce(context.Background(), entry); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("individual timeout returned %v", err)
		}
		if mark, _ := store.Get(w.Name()); !mark.Equal(now.Add(-15 * time.Minute)) {
			t.Fatalf("individual timeout changed cursor: %s", mark)
		}
		if len(s.adaptiveWindows) != 0 {
			t.Fatal("individual timeout changed source bound before aggregate context expired")
		}
	}
	from := now.Add(-time.Hour)
	if err := s.CollectRange(context.Background(), w, from, now); !errors.Is(err, context.DeadlineExceeded) || len(s.adaptiveWindows) != 0 {
		t.Fatalf("explicit range altered adaptive state: %v", err)
	}
	if mark, _ := store.Get(w.Name()); !mark.Equal(now.Add(-15 * time.Minute)) {
		t.Fatalf("explicit range changed durable cursor: %s", mark)
	}
}

func TestAdaptiveRejectsPartialAndSubsecondBounds(t *testing.T) {
	for _, tc := range []struct {
		name    string
		partial bool
		window  time.Duration
	}{
		{"partial_mark", true, time.Minute},
		{"subsecond_bound", false, 500 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			w := &adaptiveTestWindow{partial: tc.partial}
			emitter := &recordingEmitter{}
			s := NewScheduler(nil, emitter, store)
			now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
			s.Now = func() time.Time { return now }
			entry := Entry{Collector: w, InitialLookback: time.Minute, MaxWindow: tc.window}
			if err := s.RunOnce(context.Background(), entry); err == nil {
				t.Fatal("unsafe adaptive window unexpectedly succeeded")
			}
			if mark, _ := store.Get(w.Name()); !mark.Equal(now.Add(-time.Minute)) || emitter.metrics != 0 {
				t.Fatalf("unsafe window advanced cursor/metrics: %s / %d", mark, emitter.metrics)
			}
		})
	}
}
