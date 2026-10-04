package cfapi

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

// AdmissionTimer is a cancelable timer used only for API admission and GraphQL
// accumulation. Network, retry, entitlement and export clocks are independent.
type AdmissionTimer interface {
	C() <-chan time.Time
	Stop() bool
}

// AdmissionClock supplies monotonic time for admission. Production callers use
// ConfigureProcessRateLimit, which retains the real clock. Test binaries may
// inject a clock before work; doing so never changes validated capacity.
type AdmissionClock interface {
	Now() time.Time
	NewTimer(time.Duration) AdmissionTimer
}
type realAdmissionClock struct{}
type realAdmissionTimer struct{ *time.Timer }

func (realAdmissionClock) Now() time.Time { return time.Now() }
func (realAdmissionClock) NewTimer(d time.Duration) AdmissionTimer {
	return realAdmissionTimer{time.NewTimer(d)}
}
func (t realAdmissionTimer) C() <-chan time.Time { return t.Timer.C }

// One coordinator charges every exchange, including redirects and retries.
// Tickets are FIFO within each protocol. The oldest eligible protocol head wins;
// a nested-budget wait cannot block REST, nor can new REST starve ready GraphQL.
var processBudget = newProcessBudget(config.Default().Cloudflare.RateLimit)

type budgetCredit struct{ rate, capacity, tokens float64 }
type graphAdmissionKey struct{}
type graphAdmissionNotice struct {
	once  sync.Once
	ready chan struct{}
}

func (n *graphAdmissionNotice) signal() { n.once.Do(func() { close(n.ready) }) }

type budgetTicket struct {
	ctx    context.Context
	graph  bool
	result chan error
}
type tokenBucket struct {
	mu                             sync.Mutex
	cfg                            config.RateLimitConfig
	aggregate, graph               budgetCredit
	clock                          AdmissionClock
	clockInstalled                 bool
	last                           time.Time
	started, graphStarted, running bool
	tickets                        []*budgetTicket
	wake                           chan struct{}
}

func newProcessBudget(cfg config.RateLimitConfig) *tokenBucket {
	cfg = cfg.WithDefaults()
	return &tokenBucket{cfg: cfg, aggregate: budgetCredit{cfg.RequestsPerSecond, float64(cfg.Burst), float64(cfg.Burst)}, graph: budgetCredit{cfg.GraphQLRequestsPerSecond, float64(cfg.GraphQLBurst), float64(cfg.GraphQLBurst)}, clock: realAdmissionClock{}}
}

// ConfigureProcessRateLimit validates production quotas. Identical active
// configuration is idempotent; it never replaces an installed clock or credit.
func ConfigureProcessRateLimit(cfg config.RateLimitConfig) error {
	return configureProcessBudget(cfg, nil)
}

// ConfigureProcessRateLimitWithClock installs a single immutable clock identity
// before any admission or graph work, only in binaries built by go test.
// Production binaries reject the call without changing configuration or clock.
// Clock implementations must be non-nil pointers, so repeated calls can check
// identity without comparing clock state. There is deliberately no reset,
// refill, grant-credit or runtime flag API.
func ConfigureProcessRateLimitWithClock(cfg config.RateLimitConfig, clock AdmissionClock) error {
	if !testing.Testing() {
		return errors.New("cloudflare admission clock is only available in test binaries")
	}
	value := reflect.ValueOf(clock)
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() {
		return errors.New("admission clock must be a non-nil pointer")
	}
	return configureProcessBudget(cfg, clock)
}
func configureProcessBudget(cfg config.RateLimitConfig, clock AdmissionClock) error {
	cfg = cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return err
	}
	b := processBudget
	b.mu.Lock()
	defer b.mu.Unlock()
	if clock != nil {
		if b.clockInstalled {
			if b.clock != clock {
				return errors.New("cloudflare admission clock already installed")
			}
		} else if b.started || b.graphStarted {
			return errors.New("cloudflare admission clock already in use")
		}
	}
	if b.cfg != cfg {
		if b.started || b.graphStarted {
			return errors.New("cloudflare process rate limit already in use")
		}
		b.cfg = cfg
		b.aggregate = budgetCredit{cfg.RequestsPerSecond, float64(cfg.Burst), float64(cfg.Burst)}
		b.graph = budgetCredit{cfg.GraphQLRequestsPerSecond, float64(cfg.GraphQLBurst), float64(cfg.GraphQLBurst)}
	}
	if clock != nil {
		b.clock = clock
		b.clockInstalled = true
	}
	return nil
}
func graphWorkClock() AdmissionClock {
	b := processBudget
	b.mu.Lock()
	defer b.mu.Unlock()
	b.graphStarted = true
	return b.clock
}
func (b *tokenBucket) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}
func (b *tokenBucket) acquire(ctx context.Context, graph bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ticket := &budgetTicket{ctx: ctx, graph: graph, result: make(chan error, 1)}
	b.mu.Lock()
	if !b.started {
		b.started = true
		b.last = b.clock.Now()
		b.wake = make(chan struct{}, 1)
	}
	b.tickets = append(b.tickets, ticket)
	if notice, ok := ctx.Value(graphAdmissionKey{}).(*graphAdmissionNotice); ok {
		notice.signal()
	}
	if !b.running {
		b.running = true
		go b.run()
	}
	b.mu.Unlock()
	stop := context.AfterFunc(ctx, b.signal)
	defer stop()
	b.signal()
	// The coordinator resolves cancellation versus grant under its mutex. Once
	// granted, credit stays spent even if cancellation races the HTTP exchange.
	return <-ticket.result
}
func (c *budgetCredit) refill(seconds float64) {
	c.tokens = math.Min(c.capacity, c.tokens+seconds*c.rate)
}
func (c budgetCredit) delay() time.Duration {
	if c.tokens >= 1 {
		return 0
	}
	seconds := (1 - c.tokens) / c.rate
	if seconds >= float64(math.MaxInt64)/float64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return max(time.Nanosecond, time.Duration(math.Ceil(seconds*float64(time.Second))))
}
func (b *tokenBucket) run() {
	for {
		b.mu.Lock()
		now := b.clock.Now()
		elapsed := max(0, now.Sub(b.last).Seconds())
		b.aggregate.refill(elapsed)
		b.graph.refill(elapsed)
		b.last = now
		kept := b.tickets[:0]
		for _, ticket := range b.tickets {
			if err := ticket.ctx.Err(); err != nil {
				ticket.result <- err
			} else {
				kept = append(kept, ticket)
			}
		}
		clear(b.tickets[len(kept):])
		b.tickets = kept
		if len(kept) == 0 {
			b.running = false
			b.mu.Unlock()
			return
		}
		heads := [2]int{-1, -1}
		for i, ticket := range kept {
			protocol := 0
			if ticket.graph {
				protocol = 1
			}
			if heads[protocol] < 0 {
				heads[protocol] = i
			}
		}
		chosen := -1
		delay := time.Duration(math.MaxInt64)
		for protocol, index := range heads {
			if index < 0 {
				continue
			}
			wait := b.aggregate.delay()
			if protocol == 1 {
				wait = max(wait, b.graph.delay())
			}
			if wait == 0 && (chosen < 0 || index < chosen) {
				chosen = index
			}
			delay = min(delay, wait)
		}
		if chosen >= 0 {
			ticket := kept[chosen]
			b.aggregate.tokens--
			if ticket.graph {
				b.graph.tokens--
			}
			b.tickets = append(kept[:chosen], kept[chosen+1:]...)
			kept[len(kept)-1] = nil
			ticket.result <- nil
			b.mu.Unlock()
			continue
		}
		b.mu.Unlock()
		timer := b.clock.NewTimer(delay)
		select {
		case <-b.wake:
			timer.Stop()
		case <-timer.C():
		}
	}
}
