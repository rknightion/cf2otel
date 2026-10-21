package collector

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rknightion/cf2otel/internal/telemetry"
)

type blockingSnapshot struct {
	calls   atomic.Int32
	started chan struct{}
}

func (*blockingSnapshot) Name() string                   { return "test.blocking" }
func (*blockingSnapshot) DefaultInterval() time.Duration { return time.Minute }
func (b *blockingSnapshot) Collect(ctx context.Context, _ telemetry.Emitter) error {
	if b.calls.Add(1) == 1 {
		close(b.started)
	}
	<-ctx.Done()
	return ctx.Err()
}

// A poll still running at shutdown, with a tick already queued behind it,
// must neither count as a failure nor start another poll.
func TestShutdownInterruptsPollWithoutFailureOrRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := &blockingSnapshot{started: make(chan struct{})}
		registry := NewRegistry()
		registry.RegisterSnapshot(c, time.Minute)
		s := NewScheduler(registry, nil, nil)
		var polls atomic.Int32
		s.OnPoll = func(context.Context, string, time.Duration, error, time.Time) { polls.Add(1) }
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { s.Run(ctx); close(done) }()
		<-c.started
		time.Sleep(3 * time.Minute) // The overrunning poll leaves a tick queued.
		cancel()
		<-done
		if n := polls.Load(); n != 0 {
			t.Fatalf("shutdown-interrupted poll reported %d times", n)
		}
		if n := c.calls.Load(); n != 1 {
			t.Fatalf("collector ran %d times, want 1", n)
		}
	})
}
