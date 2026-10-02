package cfapi

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

// One budget covers every client, account, page, retry and redirect in this
// process. Configure it before any Cloudflare traffic; construction never
// resets tokens or grants another client its own burst.
var processBudget = &tokenBucket{rate: 0.5, capacity: 1, tokens: 1}

type tokenBucket struct {
	mu                     sync.Mutex
	rate, capacity, tokens float64
	last                   time.Time
	started                bool
}

// ConfigureProcessRateLimit applies the application's validated configuration.
// Identical configuration is idempotent, including after traffic starts. Once
// active, a different configuration is rejected without changing queued budgets.
func ConfigureProcessRateLimit(cfg config.RateLimitConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	b := processBudget
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.rate == cfg.RequestsPerSecond && b.capacity == float64(cfg.Burst) {
		return nil
	}
	if b.started {
		return errors.New("cloudflare process rate limit already in use")
	}
	b.rate, b.capacity, b.tokens = cfg.RequestsPerSecond, float64(cfg.Burst), float64(cfg.Burst)
	return nil
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
		if b.tokens >= 1 {
			b.tokens--
			b.mu.Unlock()
			return nil
		}
		seconds := (1 - b.tokens) / b.rate
		delay := time.Duration(math.MaxInt64)
		if seconds < float64(math.MaxInt64)/float64(time.Second) {
			delay = time.Duration(math.Ceil(seconds * float64(time.Second)))
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
