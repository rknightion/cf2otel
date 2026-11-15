package cfapi

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

// All clients share a total ceiling and one cap per request class, including
// retries and REST redirects. Configuration locks REST, GraphQL, total, registry.
var totalBudget = func() *tokenBucket {
	c := config.Default().Cloudflare.RateLimit
	b := newTokenBucket(c.Total.RequestsPerSecond, c.Total.Burst)
	b.maxPause = c.MaxPause
	return b
}()

var restBudget = func() *tokenBucket {
	c := config.Default().Cloudflare.RateLimit
	b := newTokenBucket(c.REST.RequestsPerSecond, c.REST.Burst)
	b.maxPause = c.MaxPause
	return b
}()
var graphqlBudget = func() *tokenBucket {
	c := config.Default().Cloudflare.RateLimit.GraphQL
	return newTokenBucket(c.RequestsPerSecond, c.Burst)
}()

// Under account-based rate limiting Cloudflare meters GraphQL per zone and per
// account, so each resource also gets its own bucket below the process cap.
var scopeBudgets = func() *scopeBuckets {
	c := config.Default().Cloudflare.RateLimit
	return &scopeBuckets{accountBased: c.AccountBased, cfg: c.GraphQLScope, buckets: map[string]*tokenBucket{}}
}()

type scopeBuckets struct {
	mu           sync.Mutex
	accountBased bool
	cfg          config.BucketConfig
	buckets      map[string]*tokenBucket
}

// bucket returns the resource bucket for key, or nil when per-resource
// metering is off or the request names no resource.
func (s *scopeBuckets) bucket(key string) *tokenBucket {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.accountBased || key == "" {
		return nil
	}
	b, ok := s.buckets[key]
	if !ok {
		b = newTokenBucket(s.cfg.RequestsPerSecond, s.cfg.Burst)
		s.buckets[key] = b
	}
	return b
}

func (s *scopeBuckets) enabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accountBased
}

type quotaScopeKey struct{}

// withQuotaScope names the zone or account a GraphQL request is metered against.
func withQuotaScope(ctx context.Context, scope Scope, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, quotaScopeKey{}, string(scope)+"/"+id)
}

func quotaScope(ctx context.Context) string {
	key, _ := ctx.Value(quotaScopeKey{}).(string)
	return key
}

func newTokenBucket(rate float64, burst int) *tokenBucket {
	return &tokenBucket{rate: rate, burst: burst, capacity: float64(burst), tokens: float64(burst)}
}

type tokenBucket struct {
	mu                     sync.Mutex
	rate, capacity, tokens float64
	burst                  int
	last                   time.Time
	pausedUntil            time.Time
	maxPause               time.Duration // REST header ceiling; never applied to GraphQL budget pauses.
	started                bool
}

// ConfigureProcessRateLimit is atomic across classes and idempotent after traffic.
// A different configuration cannot reset either active class's budget.
func ConfigureProcessRateLimit(cfg config.RateLimitConfig) error {
	// Package callers predating max_pause or graphql_scope may omit them. Loaded
	// configurations are validated strictly, so explicit YAML/environment zero is rejected.
	if cfg.MaxPause == 0 {
		cfg.MaxPause = config.Default().Cloudflare.RateLimit.MaxPause
	}
	if cfg.GraphQLScope == (config.BucketConfig{}) {
		cfg.GraphQLScope = config.Default().Cloudflare.RateLimit.GraphQLScope
	}
	// Older package callers did not have a combined ceiling. Preserve their
	// explicit class capacities; loaded configurations always supply total.
	if cfg.Total == (config.BucketConfig{}) {
		rest, graphql := cfg.Buckets()
		cfg.Total = config.BucketConfig{RequestsPerSecond: rest.RequestsPerSecond + graphql.RequestsPerSecond, Burst: min(1000, rest.Burst+graphql.Burst)}
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	rest, graphql := cfg.Buckets()
	restBudget.mu.Lock()
	defer restBudget.mu.Unlock()
	graphqlBudget.mu.Lock()
	defer graphqlBudget.mu.Unlock()
	totalBudget.mu.Lock()
	defer totalBudget.mu.Unlock()
	scopeBudgets.mu.Lock()
	defer scopeBudgets.mu.Unlock()
	same := func(b *tokenBucket, c config.BucketConfig) bool {
		return b.rate == c.RequestsPerSecond && b.burst == c.Burst
	}
	if same(restBudget, rest) && same(graphqlBudget, graphql) && same(totalBudget, cfg.Total) && restBudget.maxPause == cfg.MaxPause &&
		scopeBudgets.accountBased == cfg.AccountBased && scopeBudgets.cfg == cfg.GraphQLScope {
		return nil
	}
	if restBudget.started || graphqlBudget.started || totalBudget.started || len(scopeBudgets.buckets) > 0 {
		return errors.New("cloudflare process rate limit already in use")
	}
	restBudget.maxPause = cfg.MaxPause
	totalBudget.maxPause = cfg.MaxPause
	scopeBudgets.accountBased, scopeBudgets.cfg = cfg.AccountBased, cfg.GraphQLScope
	for _, item := range []struct {
		b *tokenBucket
		c config.BucketConfig
	}{{restBudget, rest}, {graphqlBudget, graphql}, {totalBudget, cfg.Total}} {
		item.b.rate, item.b.burst = item.c.RequestsPerSecond, item.c.Burst
		item.b.capacity, item.b.tokens = float64(item.c.Burst), float64(item.c.Burst)
	}
	return nil
}

// perTokenSafeRate keeps GraphQL under Cloudflare's default per-token quota of
// 300 queries per five minutes.
const perTokenSafeRate = 0.9

// clampRate lowers the bucket's rate, never raises it, and reports whether it changed.
func (b *tokenBucket) clampRate(rate float64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.rate <= rate {
		return false
	}
	b.rate = rate
	return true
}

// Extend, never shorten, a shared pause. No caller holds a reservation, so all
// queued siblings recheck the pause before consuming a token.
func (b *tokenBucket) pause(delay time.Duration) {
	if delay <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	until := time.Now().Add(delay)
	if until.After(b.pausedUntil) {
		b.pausedUntil = until
	}
}

// admissions orders every waiter on the shared budgets. Waiters that each poll
// for a refilled token race at the same instant, so one can lose for many
// token intervals; granting in arrival order bounds a wait by the waiters
// queued ahead of it.
var admissions = &admissionQueue{}

type admissionQueue struct {
	mu      sync.Mutex
	waiters []*admissionWaiter
}

type admissionWaiter struct {
	ctx     context.Context
	buckets []*tokenBucket
	granted chan struct{}
	retry   time.Duration // until this waiter's own budgets could admit it
}

// Buckets are passed in resource, class, total order. Consume all tokens at
// the actual admission instant: reserving a class token before waiting on the
// total ceiling could bunch delayed requests and violate the class quota.
func acquireBudgets(ctx context.Context, buckets ...*tokenBucket) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w := &admissionWaiter{ctx: ctx, granted: make(chan struct{})}
	for _, b := range buckets {
		if b != nil {
			w.buckets = append(w.buckets, b)
		}
	}
	admissions.mu.Lock()
	admissions.waiters = append(admissions.waiters, w)
	admissions.dispatchLocked()
	admissions.mu.Unlock()
	for {
		admissions.mu.Lock()
		retry := w.retry
		admissions.mu.Unlock()
		timer := time.NewTimer(retry)
		select {
		case <-w.granted:
			timer.Stop()
			return nil
		case <-ctx.Done():
			timer.Stop()
			if admissions.leave(w) {
				return ctx.Err()
			}
			return nil // admitted before the cancellation was seen
		case <-timer.C:
			admissions.mu.Lock()
			admissions.dispatchLocked()
			admissions.mu.Unlock()
		}
	}
}

// dispatchLocked admits waiters oldest first. A waiter its own budgets hold
// back (a class pause, an empty resource bucket) is skipped rather than
// blocking younger waiters; it keeps its place for the next dispatch. A
// cancelled waiter is dropped without consuming tokens.
func (q *admissionQueue) dispatchLocked() {
	now := time.Now()
	kept := q.waiters[:0]
	for _, w := range q.waiters {
		if w.ctx.Err() != nil {
			continue
		}
		if delay := admit(now, w.buckets); delay > 0 {
			w.retry = delay
			kept = append(kept, w)
			continue
		}
		close(w.granted)
	}
	clear(q.waiters[len(kept):])
	q.waiters = kept
}

// leave removes a waiter and reports whether it was still waiting. A
// cancelled waiter that dispatch dropped was never admitted.
func (q *admissionQueue) leave(w *admissionWaiter) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	select {
	case <-w.granted:
		return false
	default:
	}
	for i, queued := range q.waiters {
		if queued == w {
			q.waiters = append(q.waiters[:i], q.waiters[i+1:]...)
			break
		}
	}
	return true
}

// admit consumes one token from every bucket if all can spare one now, and
// otherwise reports how long until they could.
func admit(now time.Time, buckets []*tokenBucket) time.Duration {
	for _, b := range buckets {
		b.mu.Lock()
	}
	var delay time.Duration
	for _, b := range buckets {
		if !b.started {
			b.last, b.started = now, true
		}
		b.tokens = math.Min(b.capacity, b.tokens+now.Sub(b.last).Seconds()*b.rate)
		b.last = now
		delay = max(delay, b.pausedUntil.Sub(now))
		if b.tokens < 1 {
			seconds := (1 - b.tokens) / b.rate
			refill := time.Duration(math.MaxInt64)
			if seconds < float64(math.MaxInt64)/float64(time.Second) {
				refill = time.Duration(math.Ceil(seconds * float64(time.Second)))
			}
			delay = max(delay, refill)
		}
	}
	if delay <= 0 {
		for _, b := range buckets {
			b.tokens--
		}
	}
	for i := len(buckets) - 1; i >= 0; i-- {
		buckets[i].mu.Unlock()
	}
	return delay
}
