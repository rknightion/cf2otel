package cfapi

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

// All clients share one bucket per request class, including retries and REST redirects.
// Configuration and acquisitions lock in REST-then-GraphQL order when both are needed.
var restBudget = func() *tokenBucket {
	b := newTokenBucket(3, 5)
	b.maxPause = config.Default().Cloudflare.RateLimit.MaxPause
	return b
}()
var graphqlBudget = newTokenBucket(0.8, 2)

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
	// Package callers predating max_pause may omit it. Loaded configurations
	// are validated strictly, so explicit YAML/environment zero is rejected.
	if cfg.MaxPause == 0 {
		cfg.MaxPause = config.Default().Cloudflare.RateLimit.MaxPause
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	rest, graphql := cfg.Buckets()
	restBudget.mu.Lock()
	defer restBudget.mu.Unlock()
	graphqlBudget.mu.Lock()
	defer graphqlBudget.mu.Unlock()
	same := func(b *tokenBucket, c config.BucketConfig) bool {
		return b.rate == c.RequestsPerSecond && b.burst == c.Burst
	}
	if same(restBudget, rest) && same(graphqlBudget, graphql) && restBudget.maxPause == cfg.MaxPause {
		return nil
	}
	if restBudget.started || graphqlBudget.started {
		return errors.New("cloudflare process rate limit already in use")
	}
	restBudget.maxPause = cfg.MaxPause
	for _, item := range []struct {
		b *tokenBucket
		c config.BucketConfig
	}{{restBudget, rest}, {graphqlBudget, graphql}} {
		item.b.rate, item.b.burst = item.c.RequestsPerSecond, item.c.Burst
		item.b.capacity, item.b.tokens = float64(item.c.Burst), float64(item.c.Burst)
	}
	return nil
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

func (b *tokenBucket) acquire(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		b.mu.Lock()
		now := time.Now()
		if !b.started {
			b.last, b.started = now, true
		}
		b.tokens = math.Min(b.capacity, b.tokens+now.Sub(b.last).Seconds()*b.rate)
		b.last = now
		delay := b.pausedUntil.Sub(now)
		if delay <= 0 && b.tokens >= 1 {
			b.tokens--
			b.mu.Unlock()
			return nil
		}
		if b.tokens < 1 {
			seconds := (1 - b.tokens) / b.rate
			refill := time.Duration(math.MaxInt64)
			if seconds < float64(math.MaxInt64)/float64(time.Second) {
				refill = time.Duration(math.Ceil(seconds * float64(time.Second)))
			}
			if refill > delay {
				delay = refill
			}
		}
		b.mu.Unlock()
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
