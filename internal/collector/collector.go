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

// WindowLagAt optionally computes a window collector's lag from the same
// clock instant the scheduler uses for its upper bound. Collectors with a
// time-dependent lag can implement it without changing WindowCollector.
type WindowLagAt interface {
	LagAt(time.Time) time.Duration
}

// AdaptiveWindowCollector opts into whole-second source-window subdivision
// after an aggregate commit deadline. It must filter [from,to) and return to
// only after collecting the entire window, so equal-timestamp groups cannot be
// split and a successful commit never persists a fractional high-water mark.
// Reduced limits are process-local and retained until the scheduler restarts;
// successful sparse windows alone are not evidence that a larger one is safe.
type AdaptiveWindowCollector interface {
	AdaptiveCommitWindow() bool
}

// BudgetedWindowCollector is an adaptive collector that also bounds one
// commit's payload. It may return a whole-second mark before to once the
// records it buffered reach budgetBytes, and it always completes at least one
// source second, so a single dense second can exceed the budget. A budget of
// zero or less means no bound. Every row before the returned mark, and none at
// or after it, must be in the buffer.
type BudgetedWindowCollector interface {
	WindowCollector
	CollectWindowBudget(ctx context.Context, from, to time.Time, budgetBytes int, e telemetry.Emitter) (time.Time, error)
}

type Entry struct {
	Collector                            Collector
	Interval, InitialLookback, MaxWindow time.Duration
}
type Registry struct {
	entries    []Entry
	names      map[string]bool
	onRegister func(Entry)
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
	r.add(Entry{Collector: c, Interval: interval})
}
func (r *Registry) RegisterWindow(c WindowCollector, interval, initialLookback, maxWindow time.Duration) {
	if r.names[c.Name()] {
		panic("duplicate collector: " + c.Name())
	}
	if interval <= 0 {
		interval = c.DefaultInterval()
	}
	r.names[c.Name()] = true
	r.add(Entry{Collector: c, Interval: interval, InitialLookback: initialLookback, MaxWindow: maxWindow})
}

// ObserveRegistrations installs a startup observer and replays existing entries.
// Like registration itself, this must be called before scheduling begins.
func (r *Registry) ObserveRegistrations(observe func(Entry)) {
	r.onRegister = observe
	for _, entry := range r.entries {
		observe(entry)
	}
}

func (r *Registry) add(entry Entry) {
	r.entries = append(r.entries, entry)
	if r.onRegister != nil {
		r.onRegister(entry)
	}
}

func (r *Registry) Entries() []Entry { return append([]Entry(nil), r.entries...) }

type Deps struct {
	Config      *config.Config
	Emitter     telemetry.Emitter
	API         cfapi.Client
	Identity    identity.Index
	Apps        AppCatalog
	SelfObs     SnapshotCollector
	Checkpoints CheckpointStore
	Registry    *Registry
}

// AppRecord is the bounded shared view of one Access-protected host.
type AppRecord struct{ ID, Name, Host, ZoneID string }

// AppCatalog is populated by inventory and read by HTTP/Access collectors.
// Implementations must support concurrent polling.
type AppCatalog interface {
	PutApp(AppRecord)
	Hosts() []string
	Lookup(host string) (AppRecord, bool)
}
