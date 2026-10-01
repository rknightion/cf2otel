package collector

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"runtime"
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

// At the roughly 0.4 MB/s observed against the OTLP gateway this leaves the
// aggregate commit budget about half unused.
const commitBudgetBytes = 16 << 20

// A reduced payload budget doubles back only this long after it last changed:
// growing on every success would fail every other commit at the limit.
const commitBudgetRecovery = time.Hour
const maxWindowsPerTick = 4
const commitChunkBytes = 512 * 1024
const maxSingleSpanBytes = 1024 * 1024 // A capped two-sided AI Gateway span can exceed one normal chunk.
const commitChunkItems = 512           // Below both SDK processors' default 2048 queue capacity.
type CommitFlusher interface {
	BeginCommit() uint64
	FlushCommit(context.Context, uint64) error
}

type reducedBudget struct {
	bytes int
	since time.Time
}

type rejectionState struct {
	from, to time.Time
	count    int
}

// CommitDeadlineError distinguishes exhaustion of the aggregate commit budget
// from a single exporter timeout while that budget still has time remaining.
// Unwrap retains the required-signal export errors for errors.Is/errors.As.
type CommitDeadlineError struct {
	Collector           string
	Window, RetryWindow time.Duration
	// Budget and RetryBudget are set instead of the windows for a budgeted collector.
	Budget, RetryBudget int
	Err                 error
}

func (e *CommitDeadlineError) Error() string {
	if e.Budget > 0 {
		if e.RetryBudget >= e.Budget {
			return fmt.Sprintf("collector %s: the smallest %d-byte commit payload cannot fit the aggregate commit budget; checkpoint retained, inspect export latency or timestamp-group volume: %v", e.Collector, e.Budget, e.Err)
		}
		return fmt.Sprintf("collector %s: aggregate commit budget exhausted for a %d-byte payload budget; checkpoint retained, next commit at most %d bytes: %v", e.Collector, e.Budget, e.RetryBudget, e.Err)
	}
	if e.Window <= time.Second {
		return fmt.Sprintf("collector %s: one-second source window cannot fit the aggregate commit budget; checkpoint retained, inspect export latency or timestamp-group volume: %v", e.Collector, e.Err)
	}
	return fmt.Sprintf("collector %s: aggregate commit budget exhausted for %s; checkpoint retained, next source window at most %s: %v", e.Collector, e.Window, e.RetryWindow, e.Err)
}
func (e *CommitDeadlineError) Unwrap() error { return e.Err }

type Scheduler struct {
	Registry     *Registry
	Emitter      telemetry.Emitter
	Checkpoints  CheckpointStore
	Now          func() time.Time
	Logger       *slog.Logger
	OnPoll       func(context.Context, string, time.Duration, error, time.Time)
	OnCheckpoint func(string, time.Time)
	Flusher      CommitFlusher
	// CommitTimeout and CommitBudgetBytes override the aggregate commit budget
	// and the payload a budgeted collector may buffer for one commit. Zero
	// keeps the defaults.
	CommitTimeout     time.Duration
	CommitBudgetBytes int
	failed400         map[string]rejectionState
	adaptiveWindows   map[string]time.Duration // protected by commitMu, independent of rejection/cursor state
	commitBudgets     map[string]reducedBudget // protected by commitMu; reduced payload budgets per budgeted collector
}

func (s *Scheduler) commitTimeout() time.Duration {
	if s.CommitTimeout > 0 {
		return s.CommitTimeout
	}
	return commitTimeout
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
	now := s.Now().UTC()
	var lag time.Duration
	if clocked, ok := c.(WindowLagAt); ok {
		lag = clocked.LagAt(now)
	} else {
		lag = c.Lag()
	}
	to := now.Add(-lag).Truncate(time.Second)
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
		if adaptiveWindow(c) {
			from = from.Truncate(time.Second)
		}
		// Persist the initial lower cursor before the first fetch. Otherwise a
		// failed first window moves forward with the clock across retries/restarts.
		if err := s.Checkpoints.Set(c.Name(), from); err != nil {
			return err
		}
	}
	// A budgeted collector that stopped before its window end has more of the
	// same window to deliver, so it continues inside this run.
	truncated := false
	for committed := 0; committed < maxWindowsPerTick && from.Before(to); committed++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Leave the leading window for the normal cadence. Unbounded entries
		// already cover their whole backlog in one commit.
		limit := s.sourceWindowLimit(c, e)
		if adaptiveWindow(c) && limit > 0 && limit < time.Second {
			return fmt.Errorf("collector %s: adaptive source-window bound must be at least one second", c.Name())
		}
		if committed > 0 && !truncated && (limit <= 0 || to.Sub(from) <= limit) {
			break
		}
		windowTo := to
		if limit > 0 && windowTo.Sub(from) > limit {
			windowTo = from.Add(limit)
		}
		mark, err := s.commitWindow(ctx, c, e, from, windowTo)
		if err != nil {
			return err
		}
		if !mark.After(from) {
			// A retention gap may be unresolvable within this window.
			return nil
		}
		_, isBudgeted := budgetedWindow(c)
		truncated = isBudgeted && mark.Before(windowTo)
		from = mark
		if committed > 0 && s.Emitter != nil {
			_ = s.Emitter.Counter(context.WithoutCancel(ctx), semconv.MetricWindowCatchupWindows, 1,
				telemetry.Attr{Key: semconv.AttrCollector, Value: c.Name()})
		}
		// The process-wide export lock belongs to a single window. Give
		// other collectors a chance to commit before taking it again.
		if committed+1 < maxWindowsPerTick && (truncated || limit > 0 && to.Sub(from) > limit) {
			runtime.Gosched()
		}
	}
	return nil
}

// EffectiveCommitWindow is the maximum source interval exported by one commit.
// Zero means the entry has no configured source-window bound.
func EffectiveCommitWindow(e Entry) time.Duration {
	if e.MaxWindow > 0 {
		return e.MaxWindow
	}
	return 0
}

func adaptiveWindow(c WindowCollector) bool {
	adaptive, ok := c.(AdaptiveWindowCollector)
	return ok && adaptive.AdaptiveCommitWindow()
}

func (s *Scheduler) sourceWindowLimit(c WindowCollector, e Entry) time.Duration {
	limit := EffectiveCommitWindow(e)
	if !adaptiveWindow(c) {
		return limit
	}
	commitMu.Lock()
	reduced := s.adaptiveWindows[c.Name()]
	commitMu.Unlock()
	if reduced > 0 && (limit <= 0 || reduced < limit) {
		limit = reduced
	}
	if limit > 0 && limit < time.Second {
		return limit // runWindow rejects it rather than silently removing the bound.
	}
	return limit.Truncate(time.Second)
}

// Called only under commitMu, after a typed required-signal deadline and the
// independent aggregate context have both expired. Do not learn a smaller
// source interval from authentication errors, 400s, or individual POST stalls.
func (s *Scheduler) reduceSourceWindow(c WindowCollector, from, to time.Time, err error) error {
	window := to.Sub(from)
	retry := max(time.Second, (window / 2).Truncate(time.Second))
	if s.adaptiveWindows == nil {
		s.adaptiveWindows = map[string]time.Duration{}
	}
	if current := s.adaptiveWindows[c.Name()]; current == 0 || retry < current {
		s.adaptiveWindows[c.Name()] = retry
	}
	return &CommitDeadlineError{Collector: c.Name(), Window: window, RetryWindow: retry, Err: err}
}

func budgetedWindow(c WindowCollector) (BudgetedWindowCollector, bool) {
	budgeted, ok := c.(BudgetedWindowCollector)
	return budgeted, ok && adaptiveWindow(c)
}

func (s *Scheduler) configuredCommitBudget() int {
	if s.CommitBudgetBytes > 0 {
		return s.CommitBudgetBytes
	}
	return commitBudgetBytes
}

func (s *Scheduler) commitBudget(c WindowCollector) int {
	commitMu.Lock()
	defer commitMu.Unlock()
	if reduced := s.commitBudgets[c.Name()]; reduced.bytes > 0 {
		return reduced.bytes
	}
	return s.configuredCommitBudget()
}

// Called only under commitMu, with the same evidence as reduceSourceWindow.
// The collector fetched no more than budget bytes, so halving the payload, not
// the source interval, is what makes the next commit smaller.
func (s *Scheduler) reduceCommitBudget(c WindowCollector, budget int, err error) error {
	// Below one export chunk a smaller budget no longer makes a smaller commit.
	retry := max(min(s.configuredCommitBudget(), commitChunkBytes), budget/2)
	if retry > budget {
		retry = budget
	}
	if s.commitBudgets == nil {
		s.commitBudgets = map[string]reducedBudget{}
	}
	s.commitBudgets[c.Name()] = reducedBudget{bytes: retry, since: s.Now()}
	return &CommitDeadlineError{Collector: c.Name(), Budget: budget, RetryBudget: retry, Err: err}
}

// Called only under commitMu after a successful commit. A reduced budget
// doubles back towards the configured one after each quiet recovery interval
// instead of lasting until restart.
func (s *Scheduler) restoreCommitBudget(c WindowCollector) {
	reduced, found := s.commitBudgets[c.Name()]
	if !found || s.Now().Sub(reduced.since) < commitBudgetRecovery {
		return
	}
	if restored := reduced.bytes * 2; restored < s.configuredCommitBudget() {
		s.commitBudgets[c.Name()] = reducedBudget{bytes: restored, since: s.Now()}
		return
	}
	delete(s.commitBudgets, c.Name())
}

func (s *Scheduler) commitWindow(ctx context.Context, c WindowCollector, e Entry, from, to time.Time) (time.Time, error) {
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
	budgeted, isBudgeted := budgetedWindow(c)
	budget := 0
	var mark time.Time
	var err error
	if isBudgeted {
		budget = s.commitBudget(c)
		mark, err = budgeted.CollectWindowBudget(ctx, from, to, budget, buffer)
	} else {
		mark, err = c.CollectWindow(ctx, from, to, buffer)
	}
	if err != nil {
		var gap *cfapi.RetentionGapError
		if errors.As(err, &gap) {
			if err := s.skipGap(ctx, c, e, from, to, err); err != nil {
				return from, err
			}
			mark, _ := s.Checkpoints.Get(c.Name())
			return mark, nil
		}
		return from, err
	}
	if err := ctx.Err(); err != nil {
		return from, err
	}
	if mark.After(to) || !mark.After(from) {
		return from, fmt.Errorf("collector %s returned invalid high-water mark", c.Name())
	}
	if adaptiveWindow(c) && (from.Nanosecond() != 0 || to.Nanosecond() != 0 || mark.Nanosecond() != 0 || !mark.Equal(to) && !isBudgeted) {
		return from, fmt.Errorf("collector %s: adaptive commit requires a complete whole-second source window", c.Name())
	}
	commitMu.Lock()
	defer commitMu.Unlock()
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.commitTimeout())
	defer cancel()
	if err := s.commit(commitCtx, buffer); err != nil {
		if adaptiveWindow(c) && errors.Is(err, context.DeadlineExceeded) && errors.Is(commitCtx.Err(), context.DeadlineExceeded) {
			if isBudgeted {
				err = s.reduceCommitBudget(c, budget, err)
			} else {
				err = s.reduceSourceWindow(c, from, to, err)
			}
		}
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
			return from, err
		}
		delete(s.failed400, c.Name())
		s.Logger.Error("window dropped after three payload rejections", "collector", c.Name(), "from", from, "to", to, "error", err)
	} else {
		delete(s.failed400, c.Name())
		if isBudgeted {
			s.restoreCommitBudget(c)
		}
		for _, m := range buffer.Metrics {
			if err := m.Replay(commitCtx, s.Emitter); err != nil {
				return from, err
			}
		}
	}
	if err := s.Checkpoints.Set(c.Name(), mark); err != nil {
		return from, err
	}
	if s.OnCheckpoint != nil {
		s.OnCheckpoint(c.Name(), mark)
	}
	return mark, nil
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
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.commitTimeout())
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
		size := r.ApproxBytes()
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
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.commitTimeout())
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
