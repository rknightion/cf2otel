package cfapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

// Saturate each bucket independently so another bucket cannot mask an unsafe cap.
func TestDefaultBucketsFiveMinuteBudgets(t *testing.T) {
	cfg := config.Default().Cloudflare.RateLimit
	for _, tc := range []struct {
		name   string
		cfg    config.BucketConfig
		budget int
	}{
		{"zone", cfg.GraphQLScope, 300},
		{"account", cfg.GraphQLScope, 300},
		{"graphql", cfg.GraphQL, 1200},
		{"total", cfg.Total, 1200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				assertFiveMinuteAdmissions(t, newTokenBucket(tc.cfg.RequestsPerSecond, tc.cfg.Burst), tc.budget)
			})
		})
	}
}

func assertFiveMinuteAdmissions(t *testing.T, bucket *tokenBucket, budget int) {
	t.Helper()
	start := time.Now()
	var times []time.Time
	left, peak := 0, 0
	for time.Since(start) <= 600*time.Second {
		if err := acquireBudgets(context.Background(), bucket); err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		times = append(times, now)
		// Inclusive endpoints are conservative, including simultaneous admissions.
		for now.Sub(times[left]) > 300*time.Second {
			left++
		}
		count := len(times) - left
		peak = max(peak, count)
		if count > budget {
			t.Fatalf("admitted %d in a 300s span, budget %d", count, budget)
		}
	}
	if len(times) <= budget {
		t.Fatalf("demand did not saturate the bucket: %d admissions", len(times))
	}
	t.Logf("peak admissions in any 300s span: %d (budget %d), observed %s", peak, budget, time.Since(start))
}

// Exercise the real GraphQL error path with only the network replaced.
func TestPerTokenFallbackFiveMinuteBudget(t *testing.T) {
	if runDefaultBudgetProcess(t) {
		return
	}
	synctest.Test(t, func(t *testing.T) {
		c := New(config.Default().Cloudflare)
		c.client.Transport = budgetErrorTransport{}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, err := c.DatasetSettings(ctx, ZoneScope, "zone-test", "events"); err == nil {
			t.Fatal("budget error succeeded")
		}
		// Observe after the upstream pause, when a full bucket can burst again.
		time.Sleep(300 * time.Second)
		assertFiveMinuteAdmissions(t, graphqlBudget, 300)
	})
}

type budgetErrorTransport struct{}

func (budgetErrorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errors":[{"message":"rate limiter budget depleted","extensions":{"code":"budget"}}]}`))}, nil
}

func TestFallbackNeverRaisesLimitsOrTokens(t *testing.T) {
	for _, tc := range []struct {
		name   string
		rate   float64
		burst  int
		tokens float64
	}{
		{"large burst with low rate", 0.8, 60, 60},
		{"small burst with high rate", 2.5, 5, 3},
		{"already lower", 0.5, 5, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newTokenBucket(tc.rate, tc.burst)
			b.tokens = tc.tokens
			b.clampRate(perTokenSafeRate)
			if b.rate > tc.rate || b.capacity > float64(tc.burst) || b.tokens > tc.tokens {
				t.Fatalf("fallback raised limits or tokens: %+v", b)
			}
			if b.capacity+300*b.rate > 290 || b.tokens > b.capacity {
				t.Fatalf("fallback exceeds safe bound: %+v", b)
			}
			if b.clampRate(perTokenSafeRate) {
				t.Fatal("repeated fallback changed limits")
			}
		})
	}
}
