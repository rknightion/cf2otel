package collector

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"sync"
	"time"

	"github.com/rknightion/cf2otel/internal/telemetry"
)

type Scheduler struct {
	Registry    *Registry
	Emitter     telemetry.Emitter
	Checkpoints CheckpointStore
	Now         func() time.Time
	Logger      *slog.Logger
}

func NewScheduler(r *Registry, e telemetry.Emitter, store CheckpointStore) *Scheduler {
	return &Scheduler{Registry: r, Emitter: e, Checkpoints: store, Now: time.Now, Logger: slog.Default()}
}
func (s *Scheduler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, entry := range s.Registry.Entries() {
		wg.Add(1)
		go func(e Entry) { defer wg.Done(); s.runEntry(ctx, e) }(entry)
	}
	wg.Wait()
}
func (s *Scheduler) runEntry(ctx context.Context, e Entry) {
	interval := e.Interval
	if interval <= 0 {
		return
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(e.Collector.Name()))
	delay := time.Duration(h.Sum32()%1000) * interval / 1000
	if delay > time.Minute {
		delay = time.Minute
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.RunOnce(ctx, e); err != nil {
			s.Logger.Error("collector run failed", "collector", e.Collector.Name(), "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Scheduler) RunOnce(ctx context.Context, e Entry) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("collector panic: %v", p)
		}
	}()
	if c, ok := e.Collector.(WindowCollector); ok {
		return s.runWindow(ctx, c, e)
	}
	if c, ok := e.Collector.(SnapshotCollector); ok {
		return c.Collect(ctx, s.Emitter)
	}
	return fmt.Errorf("collector %s implements no collection mode", e.Collector.Name())
}
func (s *Scheduler) runWindow(ctx context.Context, c WindowCollector, e Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	to := s.Now().UTC().Add(-c.Lag())
	from, ok := s.Checkpoints.Get(c.Name())
	if !ok {
		if e.InitialLookback == 0 {
			return s.Checkpoints.Set(c.Name(), to)
		}
		from = to.Add(-e.InitialLookback)
	}
	if !from.Before(to) {
		return nil
	}
	if e.MaxWindow > 0 && to.Sub(from) > e.MaxWindow {
		to = from.Add(e.MaxWindow)
	}
	mark, err := c.CollectWindow(ctx, from, to, s.Emitter)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if mark.After(to) || !mark.After(from) {
		return fmt.Errorf("collector %s returned invalid high-water mark", c.Name())
	}
	return s.Checkpoints.Set(c.Name(), mark)
}
