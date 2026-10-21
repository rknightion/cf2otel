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

// Buckets are passed in resource, class, total order. Consume all tokens at
// the actual admission instant: reserving a class token before waiting on the
// total ceiling could bunch delayed requests and violate the class quota.
func acquireBudgets(ctx context.Context, buckets ...*tokenBucket) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, b := range buckets {
			if b != nil {
				b.mu.Lock()
			}
		}
		now := time.Now()
		var delay time.Duration
		for _, b := range buckets {
			if b == nil {
				continue
			}
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
				if b != nil {
					b.tokens--
				}
			}
		}
		for i := len(buckets) - 1; i >= 0; i-- {
			if buckets[i] != nil {
				buckets[i].mu.Unlock()
			}
		}
		if delay <= 0 {
			return nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
