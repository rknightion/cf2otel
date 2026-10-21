package selfobs

import (
	"time"

	"github.com/rknightion/cf2otel/internal/collector"
)

type pollingState struct {
	entry   collector.Entry
	started time.Time
	elapsed time.Duration
}

func (s *Stats) ObserveEntry(entry collector.Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.polling == nil {
		s.polling = map[string]pollingState{}
	}
	s.polling[entry.Collector.Name()] = pollingState{entry: entry}
}

func (s *Stats) StartPoll(name string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.polling[name]
	if !ok {
		return
	}
	p.started = at
	s.polling[name] = p
}

func effectiveLag(c collector.WindowCollector, now time.Time) time.Duration {
	if clocked, ok := c.(collector.WindowLagAt); ok {
		return clocked.LagAt(now)
	}
	return c.Lag()
}
