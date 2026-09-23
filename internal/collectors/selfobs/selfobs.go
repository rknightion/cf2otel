package selfobs

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type Stats struct {
	mu              sync.Mutex
	emitter         telemetry.Emitter
	version, commit string
	lastSuccess     map[string]time.Time
	expected        map[string]struct{}
	checkpoints     map[string]time.Time
}

// Collector exposes Stats as the snapshot collector registered by the root-owned
// register.go. Keep a reference to Stats for scheduler and exporter callbacks.
type Collector struct {
	Stats *Stats
	Now   func() time.Time
}

func NewCollector(s *Stats) *Collector            { return &Collector{Stats: s, Now: time.Now} }
func (*Collector) Name() string                   { return "selfobs" }
func (*Collector) DefaultInterval() time.Duration { return time.Minute }
func (c *Collector) Collect(ctx context.Context, _ telemetry.Emitter) error {
	return c.Stats.Collect(ctx, c.Now())
}

func New(e telemetry.Emitter, version, commit string) *Stats {
	return &Stats{emitter: e, version: version, commit: commit, lastSuccess: map[string]time.Time{}, expected: map[string]struct{}{}, checkpoints: map[string]time.Time{}}
}

// Expect keeps an enabled collector visible even before its first successful poll.
func (s *Stats) Expect(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expected[name] = struct{}{}
}

// Poll records one completed scheduler attempt. The caller supplies completion time.
func (s *Stats) Poll(ctx context.Context, name string, duration time.Duration, pollErr error, at time.Time) error {
	attr := telemetry.Attr{Key: semconv.AttrCollector, Value: name}
	if pollErr == nil {
		s.mu.Lock()
		s.lastSuccess[name] = at
		s.mu.Unlock()
	}
	var errs []error
	if pollErr == nil {
		if err := s.emitter.Counter(ctx, semconv.MetricScrapeSuccess, 1, attr); err != nil {
			errs = append(errs, err)
		}
	}
	if err := s.emitter.Histogram(ctx, semconv.MetricScrapeDuration, duration.Seconds(), attr); err != nil {
		errs = append(errs, err)
	}
	if pollErr != nil {
		if err := s.emitter.Counter(ctx, semconv.MetricScrapeErrors, 1, attr); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Export records the provider's outcome after one attempted export batch.
func (s *Stats) Export(ctx context.Context, signal string, exportErr error) error {
	attr := telemetry.Attr{Key: semconv.AttrExportSignal, Value: signal}
	if exportErr != nil {
		return s.emitter.Counter(ctx, semconv.MetricExportErrors, 1, attr)
	}
	return s.emitter.Counter(ctx, semconv.MetricExportSuccess, 1, attr)
}

func (s *Stats) Checkpoint(name string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpoints[name] = at
}

// Collect emits bounded gauges. It never includes error text or checkpoint keys beyond registered names.
func (s *Stats) Collect(ctx context.Context, now time.Time) error {
	s.mu.Lock()
	last := make(map[string]time.Time, len(s.lastSuccess))
	for k, v := range s.lastSuccess {
		last[k] = v
	}
	for k := range s.expected {
		if _, ok := last[k]; !ok {
			last[k] = time.Time{}
		}
	}
	points := make(map[string]time.Time, len(s.checkpoints))
	for k, v := range s.checkpoints {
		points[k] = v
	}
	s.mu.Unlock()
	var errs []error
	for name, at := range last {
		value := float64(0)
		if !at.IsZero() {
			value = float64(at.Unix())
		}
		if err := s.emitter.Gauge(ctx, semconv.MetricScrapeLastSuccess, value, telemetry.Attr{Key: semconv.AttrCollector, Value: name}); err != nil {
			errs = append(errs, err)
		}
	}
	for name, at := range points {
		age := now.Sub(at).Seconds()
		if age < 0 {
			age = 0
		}
		if err := s.emitter.Gauge(ctx, semconv.MetricCheckpointAge, age, telemetry.Attr{Key: semconv.AttrCollector, Value: name}); err != nil {
			errs = append(errs, err)
		}
	}
	if err := s.emitter.Gauge(ctx, semconv.MetricBuildInfo, 1, telemetry.Attr{Key: semconv.AttrBuildVersion, Value: s.version}, telemetry.Attr{Key: semconv.AttrBuildCommit, Value: s.commit}); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
