package collector

import (
	"context"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/identity"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type Collector interface {
	Name() string
	DefaultInterval() time.Duration
}
type SnapshotCollector interface {
	Collector
	Collect(context.Context, telemetry.Emitter) error
}
type WindowCollector interface {
	Collector
	CollectWindow(context.Context, time.Time, time.Time, telemetry.Emitter) (time.Time, error)
	Lag() time.Duration
}
type Entry struct {
	Collector                            Collector
	Interval, InitialLookback, MaxWindow time.Duration
}
type Registry struct {
	entries []Entry
	names   map[string]bool
}

func NewRegistry() *Registry { return &Registry{names: map[string]bool{}} }
func (r *Registry) RegisterSnapshot(c SnapshotCollector, interval time.Duration) {
	if r.names[c.Name()] {
		panic("duplicate collector: " + c.Name())
	}
	if interval <= 0 {
		interval = c.DefaultInterval()
	}
	r.names[c.Name()] = true
	r.entries = append(r.entries, Entry{Collector: c, Interval: interval})
}
func (r *Registry) RegisterWindow(c WindowCollector, interval, initialLookback, maxWindow time.Duration) {
	if r.names[c.Name()] {
		panic("duplicate collector: " + c.Name())
	}
	if interval <= 0 {
		interval = c.DefaultInterval()
	}
	r.names[c.Name()] = true
	r.entries = append(r.entries, Entry{Collector: c, Interval: interval, InitialLookback: initialLookback, MaxWindow: maxWindow})
}
func (r *Registry) Entries() []Entry { return append([]Entry(nil), r.entries...) }

type Deps struct {
	Config      *config.Config
	Emitter     telemetry.Emitter
	API         cfapi.Client
	Identity    identity.Index
	Checkpoints CheckpointStore
	Registry    *Registry
}
