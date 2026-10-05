package cfapi

import (
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func retryAfterDelay(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseUint(value, 10, 64); err == nil {
		if seconds > uint64(math.MaxInt64/int64(time.Second)) {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	if when, err := http.ParseTime(value); err == nil {
		delay := when.Sub(now)
		if delay < 0 {
			delay = 0
		}
		return delay, true
	}
	return 0, false
}

// Each policy is independent. Require exactly one nonnegative integral r and t;
// ignore malformed policies rather than turning an untrusted header into a pause.
func ratelimitDelay(values []string, burst int) time.Duration {
	var delay time.Duration
	for _, value := range values {
		for _, policy := range strings.Split(value, ",") {
			parts := strings.Split(policy, ";")
			if strings.TrimSpace(parts[0]) == "" {
				continue
			}
			remaining, seconds := int64(-1), int64(-1)
			valid := true
			for _, param := range parts[1:] {
				key, text, ok := strings.Cut(strings.TrimSpace(param), "=")
				if !ok {
					valid = false
					break
				}
				if key != "r" && key != "t" {
					continue
				}
				n, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
				if err != nil || n < 0 {
					valid = false
					break
				}
				if key == "r" {
					if remaining >= 0 {
						valid = false
					}
					remaining = n
				} else {
					if seconds >= 0 {
						valid = false
					}
					seconds = n
				}
			}
			if !valid || remaining < 0 || remaining > int64(burst) || seconds < 0 || seconds > math.MaxInt64/int64(time.Second) {
				continue
			}
			if d := time.Duration(seconds) * time.Second; d > delay {
				delay = d
			}
		}
	}
	return delay
}

// The retry loop uses the same ceiling without logging again: observation is
// the single warning site, even when both headers request an excessive pause.
func (b *tokenBucket) restPause(delay time.Duration) time.Duration {
	b.mu.Lock()
	ceiling := b.maxPause
	b.mu.Unlock()
	return min(delay, ceiling)
}

func (b *tokenBucket) observeRESTHeaders(header http.Header) {
	b.mu.Lock()
	burst, ceiling := b.burst, b.maxPause
	b.mu.Unlock()
	delay := ratelimitDelay(header.Values("Ratelimit"), burst)
	if retry, ok := retryAfterDelay(header.Get("Retry-After"), time.Now()); ok {
		delay = max(delay, retry)
	}
	if delay > ceiling {
		slog.Warn("cloudflare REST rate-limit pause capped", "requested_pause", delay, "max_pause", ceiling)
		delay = ceiling
	}
	b.pause(delay)
}
