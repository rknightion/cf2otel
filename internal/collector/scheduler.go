package collector

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

var commitMu sync.Mutex

const commitTimeout = 90 * time.Second // SDK retries for up to one minute per export.
const commitChunkBytes = 512 * 1024
const maxSingleSpanBytes = 1024 * 1024 // A capped two-sided AI Gateway span can exceed one normal chunk.
const commitChunkItems = 512           // Below both SDK processors' default 2048 queue capacity.
type CommitFlusher interface {
	BeginCommit() uint64
	FlushCommit(context.Context, uint64) error
}

type rejectionState struct {
	from, to time.Time
	count    int
}

type Scheduler struct {
	Registry     *Registry
	Emitter      telemetry.Emitter
	Checkpoints  CheckpointStore
	Now          func() time.Time
	Logger       *slog.Logger
	OnPoll       func(context.Context, string, time.Duration, error, time.Time)
	OnCheckpoint func(string, time.Time)
	Flusher      CommitFlusher
	failed400    map[string]rejectionState
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
		started := time.Now()
		err := s.RunOnce(ctx, e)
		if s.OnPoll != nil {
			s.OnPoll(ctx, e.Collector.Name(), time.Since(started), err, s.Now())
		}
		if err != nil {
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
	// GraphQL filters serialize RFC3339 seconds. Persisting a subsecond cursor
	// would skip the final fractional second of every successful window.
	to := s.Now().UTC().Add(-c.Lag()).Truncate(time.Second)
	from, ok := s.Checkpoints.Get(c.Name())
	if ok {
		from = from.Truncate(time.Second)
	}
	if !ok {
		if e.InitialLookback == 0 {
			if err := s.Checkpoints.Set(c.Name(), to); err != nil {
				return err
			}
			if s.OnCheckpoint != nil {
				s.OnCheckpoint(c.Name(), to)
			}
			return nil
		}
		from = to.Add(-e.InitialLookback)
		// Persist the initial lower cursor before the first fetch. Otherwise a
		// failed first window moves forward with the clock across retries/restarts.
		if err := s.Checkpoints.Set(c.Name(), from); err != nil {
			return err
		}
	}
	if !from.Before(to) {
		return nil
	}
	if e.MaxWindow > 0 && to.Sub(from) > e.MaxWindow {
		to = from.Add(e.MaxWindow)
	}
	commitMu.Lock()
	if pending, found := s.failed400[c.Name()]; found {
		if pending.from.Equal(from) {
			to = pending.to
		} else {
			delete(s.failed400, c.Name())
		}
	}
	commitMu.Unlock()
	buffer := &telemetry.Buffer{}
	mark, err := c.CollectWindow(ctx, from, to, buffer)
	if err != nil {
		var gap *cfapi.RetentionGapError
		if errors.As(err, &gap) {
			return s.skipGap(ctx, c, e, from, to, err)
		}
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if mark.After(to) || !mark.After(from) {
		return fmt.Errorf("collector %s returned invalid high-water mark", c.Name())
	}
	commitMu.Lock()
	defer commitMu.Unlock()
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), commitTimeout)
	defer cancel()
	if err := s.commit(commitCtx, buffer); err != nil {
		outcome := "retry"
		if s.failed400 == nil {
			s.failed400 = map[string]rejectionState{}
		}
		if payloadRejected(err) {
			pending := s.failed400[c.Name()]
			if !pending.from.Equal(from) || !pending.to.Equal(to) {
				pending = rejectionState{from: from, to: to}
			}
			pending.count++
			s.failed400[c.Name()] = pending
			if pending.count >= 3 {
				outcome = "dropped"
			}
		} else {
			delete(s.failed400, c.Name())
		}
		_ = s.Emitter.Counter(commitCtx, semconv.MetricWindowCommitFailures, 1,
			telemetry.Attr{Key: semconv.AttrCollector, Value: c.Name()},
			telemetry.Attr{Key: semconv.AttrExportSignal, Value: telemetry.FailureSignals(err)},
			telemetry.Attr{Key: semconv.AttrWindowOutcome, Value: outcome})
		if outcome != "dropped" {
			return err
		}
		delete(s.failed400, c.Name())
		s.Logger.Error("window dropped after three payload rejections", "collector", c.Name(), "from", from, "to", to, "error", err)
	} else {
		delete(s.failed400, c.Name())
		for _, m := range buffer.Metrics {
			if err := m.Replay(commitCtx, s.Emitter); err != nil {
				return err
			}
		}
	}
	if err := s.Checkpoints.Set(c.Name(), mark); err != nil {
		return err
	}
	if s.OnCheckpoint != nil {
		s.OnCheckpoint(c.Name(), mark)
	}
	return nil
}
func payloadRejected(err error) bool {
	seenPayload := false
	for _, line := range strings.Split(err.Error(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "background OTLP export failed" {
			continue
		}
		status := strings.SplitN(line, "(body:", 2)[0]
		if strings.Contains(status, ": 400 Bad Request ") || strings.Contains(status, "OTLP partial success:") {
			seenPayload = true
			continue
		}
		return false
	}
	return seenPayload
}

// CollectRange exports an explicit window without changing its durable cursor.
func (s *Scheduler) CollectRange(ctx context.Context, c WindowCollector, from, to time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	buffer := &telemetry.Buffer{}
	mark, err := c.CollectWindow(ctx, from, to, buffer)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if mark.After(to) || !mark.After(from) {
		return fmt.Errorf("collector %s returned invalid high-water mark", c.Name())
	}
	commitMu.Lock()
	defer commitMu.Unlock()
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), commitTimeout)
	defer cancel()
	if err := s.commit(commitCtx, buffer); err != nil {
		return err
	}
	for _, m := range buffer.Metrics {
		if err := m.Replay(commitCtx, s.Emitter); err != nil {
			return err
		}
	}
	return nil
}
func (s *Scheduler) commit(ctx context.Context, b *telemetry.Buffer) error {
	if s.Emitter == nil && len(b.Records) > 0 {
		return errors.New("window emitter is nil")
	}
	var seq uint64
	if s.Flusher != nil {
		seq = s.Flusher.BeginCommit()
	}
	items, bytes := 0, 0
	flush := func() error {
		if s.Flusher == nil {
			return nil
		}
		err := s.Flusher.FlushCommit(ctx, seq)
		seq = s.Flusher.BeginCommit()
		return err
	}
	for _, r := range b.Records {
		size := len(r.Event) + len(r.Body) + 64
		for _, a := range r.Attrs {
			size += len(a.Key) + len(a.Value)
		}
		if r.Span != nil {
			size += len(r.Span.Name) + 64
			for _, a := range r.Span.Attrs {
				size += len(a.Key) + len(a.Value)
			}
			for _, ev := range r.Span.Events {
				size += len(ev.Name) + 64
				for _, a := range ev.Attrs {
					size += len(a.Key) + len(a.Value)
				}
			}
			for _, l := range r.Span.Logs {
				size += len(l.Name) + len(l.Body) + 64
				for _, a := range l.Attrs {
					size += len(a.Key) + len(a.Value)
				}
			}
		}
		if size > commitChunkBytes {
			if r.Span == nil || size > maxSingleSpanBytes {
				return fmt.Errorf("window record exceeds %d byte export limit", maxSingleSpanBytes)
			}
			if items > 0 {
				if err := flush(); err != nil {
					return err
				}
			}
			if err := r.Replay(ctx, s.Emitter); err != nil {
				return err
			}
			if err := flush(); err != nil {
				return err
			}
			items, bytes = 0, 0
			continue
		}
		if items > 0 && (items+1 > commitChunkItems || bytes+size > commitChunkBytes) {
			if err := flush(); err != nil {
				return err
			}
			items, bytes = 0, 0
		}
		if err := r.Replay(ctx, s.Emitter); err != nil {
			return err
		}
		items++
		bytes += size
	}
	return flush()
}
func gapFloor(err error) time.Time {
	var latest time.Time
	var visit func(error)
	visit = func(e error) {
		if e == nil {
			return
		}
		var g *cfapi.RetentionGapError
		if errors.As(e, &g) && g.Floor.After(latest) {
			latest = g.Floor
		}
		if u, ok := e.(interface{ Unwrap() []error }); ok {
			for _, inner := range u.Unwrap() {
				visit(inner)
			}
		} else if u, ok := e.(interface{ Unwrap() error }); ok {
			visit(u.Unwrap())
		}
	}
	visit(err)
	return latest
}
func (s *Scheduler) skipGap(ctx context.Context, c WindowCollector, e Entry, from, to time.Time, cause error) error {
	floor := gapFloor(cause)
	margin := e.Interval + c.Lag() + time.Minute
	mark := floor.Add(margin).Add(time.Second - time.Nanosecond).Truncate(time.Second)
	if mark.After(to) {
		mark = to
	}
	if !mark.After(from) {
		return cause
	}
	commitMu.Lock()
	defer commitMu.Unlock()
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), commitTimeout)
	defer cancel()
	seconds := mark.Sub(from).Seconds()
	var seq uint64
	if s.Flusher != nil {
		seq = s.Flusher.BeginCommit()
	}
	if err := s.Emitter.LogEvent(commitCtx, semconv.EventWindowGap, "retention floor skipped window", s.Now(), 0,
		telemetry.Attr{Key: semconv.AttrCollector, Value: c.Name()},
		telemetry.Attr{Key: semconv.AttrWindowFrom, Value: from.Format(time.RFC3339)},
		telemetry.Attr{Key: semconv.AttrWindowFloor, Value: floor.Format(time.RFC3339)},
		telemetry.Attr{Key: semconv.AttrWindowGapSeconds, Value: strconv.FormatFloat(seconds, 'f', 0, 64)}); err != nil {
		return err
	}
	if s.Flusher != nil {
		if err := s.Flusher.FlushCommit(commitCtx, seq); err != nil {
			return err
		}
	}
	if err := s.Emitter.Counter(commitCtx, semconv.MetricWindowGap, seconds, telemetry.Attr{Key: semconv.AttrCollector, Value: c.Name()}); err != nil {
		return err
	}
	if err := s.Checkpoints.Set(c.Name(), mark); err != nil {
		return err
	}
	delete(s.failed400, c.Name())
	if s.OnCheckpoint != nil {
		s.OnCheckpoint(c.Name(), mark)
	}
	return nil
}
