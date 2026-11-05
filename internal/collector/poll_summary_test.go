package collector

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/telemetry"
)

func TestRunOnceReportsCanceledPoll(t *testing.T) {
	s := NewScheduler(NewRegistry(), nil, nil)
	var got telemetry.PollSummary
	calls := 0
	s.OnRun = func(ctx context.Context, _ string) (context.Context, func(telemetry.PollSummary)) {
		return ctx, func(v telemetry.PollSummary) { got = v; calls++ }
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := s.RunOnce(ctx, Entry{Collector: &summaryWindow{}})
	if err == nil || calls != 1 || got.Outcome != "canceled" || got.CommittedWindows != 0 {
		t.Fatalf("err=%v calls=%d summary=%+v", err, calls, got)
	}
}

type summaryWindow struct{}

func (*summaryWindow) Name() string                   { return "fixture" }
func (*summaryWindow) DefaultInterval() time.Duration { return time.Minute }
func (*summaryWindow) Lag() time.Duration             { return 0 }
func (*summaryWindow) CollectWindow(context.Context, time.Time, time.Time, telemetry.Emitter) (time.Time, error) {
	panic("canceled poll collected")
}

func TestPollSummaryCountsOnlyCheckpointedDelivery(t *testing.T) {
	for _, fail := range []bool{false, true} {
		store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
		if err != nil {
			t.Fatal(err)
		}
		s := NewScheduler(nil, &recordingEmitter{}, store)
		var summary telemetry.PollSummary
		s.OnRun = func(ctx context.Context, _ string) (context.Context, func(telemetry.PollSummary)) {
			return ctx, func(v telemetry.PollSummary) { summary = v }
		}
		if fail {
			s.Flusher = &stubFlusher{err: errors.New("fixture export error")}
		}
		err = s.RunOnce(context.Background(), Entry{Collector: &emitWindow{name: "fixture", n: 2}, InitialLookback: time.Minute})
		if fail {
			if err == nil || summary.CommittedLogs != 0 || summary.CommittedWindows != 0 || summary.Outcome != "error" {
				t.Fatalf("failed delivery counted: %+v err=%v", summary, err)
			}
		} else {
			if err != nil || summary.CommittedLogs != 2 || summary.CommittedWindows != 1 || summary.Outcome != "success" {
				t.Fatalf("delivered counts: %+v err=%v", summary, err)
			}
		}
	}
}

func TestPollPhasesIncludeEarlyLockWaitAndPanic(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewScheduler(nil, &recordingEmitter{}, store)
	var summary telemetry.PollSummary
	s.OnRun = func(ctx context.Context, _ string) (context.Context, func(telemetry.PollSummary)) {
		return ctx, func(v telemetry.PollSummary) { summary = v }
	}
	commitMu.Lock()
	released := make(chan struct{})
	go func() { time.Sleep(30 * time.Millisecond); commitMu.Unlock(); close(released) }()
	err = s.RunOnce(context.Background(), Entry{Collector: &panicSummaryWindow{}, InitialLookback: time.Minute})
	<-released
	if err == nil || summary.Outcome != "error" || summary.CollectSeconds < 0.01 || summary.CommitLockSeconds < 0.02 {
		t.Fatalf("panic phases lost: %+v err=%v", summary, err)
	}
}

type panicSummaryWindow struct{ summaryWindow }

func (*panicSummaryWindow) CollectWindow(context.Context, time.Time, time.Time, telemetry.Emitter) (time.Time, error) {
	time.Sleep(10 * time.Millisecond)
	panic("fixture")
}
