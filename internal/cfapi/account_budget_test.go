package cfapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

// Under the default configuration GraphQL is metered per zone and account:
// every request opts in with the account-based header, distinct zones are not
// held to one shared 1 rps quota, a single zone is, and a budget error that
// names one zone pauses only that zone.
func TestAccountBasedGraphQLBudgetIsPerResource(t *testing.T) {
	if runDefaultBudgetProcess(t) {
		return
	}
	var mu sync.Mutex
	headers := map[string]int{}
	throttledHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		headers[r.Header.Get("X-Rate-Limit-Type")]++
		mu.Unlock()
		if strings.Contains(body.Query, `"zone-throttled"`) {
			mu.Lock()
			throttledHits++
			mu.Unlock()
			_, _ = w.Write([]byte(`{"data":null,"errors":[{"message":"Zone zone-throttled has exceeded its rate limit. Please try again after 5 minutes.","extensions":{"code":"budget"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[{"settings":{"events":{"enabled":true,"availableFields":["count"]}}}]}}}`))
	}))
	defer server.Close()
	cfg := config.Default().Cloudflare
	cfg.APIBase = server.URL
	settings := func(ctx context.Context, zone string) error {
		_, err := New(cfg).DatasetSettings(ctx, ZoneScope, zone, "events")
		return err
	}

	// Eight zones, one request each: well under each zone's quota, so they must
	// not queue behind a single per-token bucket (eight requests at 0.8 rps
	// with burst 2 need about 7.5s).
	start := time.Now()
	for i := range 8 {
		if err := settings(context.Background(), "zone-"+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("distinct zones shared one quota: 8 requests took %s", elapsed)
	}

	// Four requests to one zone are held to that zone's 0.9 rps (burst 2).
	start = time.Now()
	for range 4 {
		if err := settings(context.Background(), "zone-single"); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 2*time.Second {
		t.Errorf("one zone was not held to its own quota: 4 requests took %s", elapsed)
	}

	// A budget error naming one zone pauses that zone, not every GraphQL caller.
	throttled, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := settings(throttled, "zone-throttled"); err == nil {
		t.Fatal("throttled zone succeeded")
	}
	sibling, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	if err := settings(sibling, "zone-sibling"); err != nil {
		t.Errorf("one zone's budget error paused another zone: %v", err)
	}
	again, done := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer done()
	if err := settings(again, "zone-throttled"); err == nil {
		t.Error("throttled zone was not paused")
	}

	mu.Lock()
	defer mu.Unlock()
	// The budget error paused the zone: neither the in-call retry nor the
	// second call reached Cloudflare again.
	if throttledHits != 1 {
		t.Errorf("throttled zone was requested %d times during its pause, want 1", throttledHits)
	}
	if len(headers) != 1 || headers["account-based"] == 0 {
		t.Errorf("GraphQL requests must all opt into account-based rate limiting: %v", headers)
	}
}

// If Cloudflare answers with a per-token budget error despite the header, the
// process GraphQL rate falls back to one the per-token quota sustains.
func TestPerTokenBudgetErrorClampsGraphQLRate(t *testing.T) {
	if runDefaultBudgetProcess(t) {
		return
	}
	var log strings.Builder
	var logMu sync.Mutex
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(writerFunc(func(p []byte) (int, error) {
		logMu.Lock()
		defer logMu.Unlock()
		return log.Write(p)
	}), nil)))
	t.Cleanup(func() { slog.SetDefault(original) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":null,"errors":[{"message":"rate limiter budget depleted","extensions":{"code":"budget"}}]}`))
	}))
	defer server.Close()
	cfg := config.Default().Cloudflare
	cfg.APIBase = server.URL
	for _, zone := range []string{"zone-a", "zone-b"} {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, _ = New(cfg).DatasetSettings(ctx, ZoneScope, zone, "events")
		cancel()
	}
	graphqlBudget.mu.Lock()
	rate := graphqlBudget.rate
	graphqlBudget.mu.Unlock()
	if rate != perTokenSafeRate {
		t.Errorf("GraphQL rate after a per-token budget error = %g, want %g", rate, perTokenSafeRate)
	}
	logMu.Lock()
	defer logMu.Unlock()
	if n := strings.Count(log.String(), "account-based rate limiting"); n != 1 {
		t.Errorf("want one fallback warning, got %d: %s", n, log.String())
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestBudgetErrorsResourceScope(t *testing.T) {
	for _, tc := range []struct {
		name             string
		raw              string
		budget, resource bool
	}{
		{"zone", `{"errors":[{"message":"Zone z has exceeded its rate limit.","extensions":{"code":"budget"}}]}`, true, true},
		{"account", `{"errors":[{"message":"Account a has exceeded its rate limit.","extensions":{"code":"budget"}}]}`, true, true},
		{"per-token", `{"errors":[{"message":"rate limiter budget depleted","extensions":{"code":"budget"}}]}`, true, false},
		{"missing message", `{"errors":[{"extensions":{"code":"budget"}}]}`, true, false},
		{"non-string message", `{"errors":[{"message":7,"extensions":{"code":"budget"}}]}`, true, false},
		{"zone and per-token", `{"errors":[{"message":"Zone z has exceeded its rate limit.","extensions":{"code":"budget"}},{"message":"budget depleted","extensions":{"code":"budget"}}]}`, true, false},
		{"zone message without budget code", `{"errors":[{"message":"Zone z has exceeded its rate limit.","extensions":{"code":"other"}}]}`, false, false},
		{"malformed sibling", `{"errors":[7,{"message":"Zone z has exceeded its rate limit.","extensions":{"code":"budget"}}]}`, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			budget, resource := budgetErrors([]byte(tc.raw))
			if budget != tc.budget || resource != tc.resource {
				t.Fatalf("budget=%v resource=%v; want %v %v", budget, resource, tc.budget, tc.resource)
			}
		})
	}
}
