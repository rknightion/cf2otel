package cfapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

func TestPerClassGraphQLSaturationDoesNotDelayREST(t *testing.T) {
	if runDefaultBudgetProcess(t) {
		return
	}
	if err := ConfigureProcessRateLimit(config.RateLimitConfig{RequestsPerSecond: 0.01, Burst: 1}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[]}}}`))
		} else {
			_, _ = w.Write([]byte(`{"result":[]}`))
		}
	}))
	defer server.Close()
	cfg := config.Default().Cloudflare
	cfg.APIBase = server.URL
	a, b := New(cfg), New(cfg)
	if _, err := a.graph(context.Background(), "{viewer{zones{}}}"); err != nil {
		t.Fatal(err)
	}
	queued, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := a.graph(queued, "{viewer{zones{}}}"); done <- err }()
	ctx, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stop()
	var out []any
	if err := b.Get(ctx, "/rest", nil, &out); err != nil {
		t.Errorf("saturated GraphQL delayed REST: %v", err)
	}
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GraphQL not saturated: %v", err)
	}
}

func TestRatelimitHeaderDefersNextREST(t *testing.T) {
	if runDefaultBudgetProcess(t) {
		return
	}
	if err := ConfigureProcessRateLimit(config.RateLimitConfig{RequestsPerSecond: 10000, Burst: 5}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Ratelimit", "default;r=0;t=1")
		}
		_, _ = w.Write([]byte(`{"result":[]}`))
	}))
	defer server.Close()
	cfg := config.Default().Cloudflare
	cfg.APIBase = server.URL
	var out []any
	if err := New(cfg).Get(context.Background(), "/first", nil, &out); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	if err := New(cfg).Get(ctx, "/sibling", nil, &out); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("Ratelimit did not defer next REST token >=1s: %s", elapsed)
	}
}

func TestGraphQLBudgetPausesOnlyGraphQL(t *testing.T) {
	if runDefaultBudgetProcess(t) {
		return
	}
	if err := ConfigureProcessRateLimit(config.RateLimitConfig{RequestsPerSecond: 10000, Burst: 5}); err != nil {
		t.Fatal(err)
	}
	var graphCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			graphCalls.Add(1)
			_, _ = w.Write([]byte(`{"errors":[{"message":"private upstream content","extensions":{"code":"budget"}}]}`))
		} else {
			_, _ = w.Write([]byte(`{"result":[]}`))
		}
	}))
	defer server.Close()
	cfg := config.Default().Cloudflare
	cfg.APIBase = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := New(cfg).DatasetSettings(ctx, ZoneScope, "fixture", "events")
	var budgetError *GraphQLBudgetError
	if !errors.As(err, &budgetError) {
		t.Errorf("budget error did not preserve rate-limit type: %v", err)
	}
	graphqlBudget.mu.Lock()
	remainingPause := time.Until(graphqlBudget.pausedUntil)
	graphqlBudget.mu.Unlock()
	if remainingPause < 299*time.Second || remainingPause > 300*time.Second {
		t.Errorf("production pause is not exactly 300s: remaining=%s", remainingPause)
	}
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) < 80*time.Millisecond {
		t.Errorf("budget did not wait for context cancellation: %v elapsed=%s", err, time.Since(start))
	}
	sibling, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer stop()
	_, err = New(cfg).DatasetSettings(sibling, ZoneScope, "fixture", "events")
	if !errors.Is(err, context.DeadlineExceeded) || graphCalls.Load() != 1 {
		t.Errorf("budget did not pause GraphQL siblings: %v calls=%d", err, graphCalls.Load())
	}
	rest, done := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer done()
	var out []any
	if err := New(cfg).Get(rest, "/rest", nil, &out); err != nil {
		t.Fatalf("budget paused REST: %v", err)
	}
}

func TestRateLimitHeaderParsing(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   time.Duration
	}{
		{"default;r=0;t=1", time.Second}, {"default;r=5;t=1", time.Second}, {"default;r=6;t=1", 0},
		{"default;t=2;r=1", 2 * time.Second}, {"default;r=0;t=1, other;r=2;t=2", 2 * time.Second},
		{"default;r=0;t=bad", 0}, {"default;r=-1;t=1", 0}, {"default;r=0;t=-1", 0},
		{"default;r=0;t=9223372036854775807", 0}, {"default;r=0", 0}, {"default;t=1", 0},
		{"default;r=0;r=1;t=1", 0}, {"default;r=0;t=1;t=2", 0}, {";r=0;t=1", 0}, {"garbage", 0},
	} {
		if got := ratelimitDelay([]string{tc.header}, 5); got != tc.want {
			t.Errorf("%q: got %s want %s", tc.header, got, tc.want)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		header string
		want   time.Duration
		valid  bool
	}{
		{"1", time.Second, true}, {"0", 0, true}, {now.Add(time.Second).Format(http.TimeFormat), time.Second, true},
		{now.Add(-time.Second).Format(http.TimeFormat), 0, true}, {"-1", 0, false}, {"1.5", 0, false},
		{strconv.FormatUint(^uint64(0), 10), 0, false}, {"garbage", 0, false},
	} {
		delay, ok := retryAfterDelay(tc.header, now)
		if delay != tc.want || ok != tc.valid {
			t.Errorf("Retry-After %q: got %s,%t", tc.header, delay, ok)
		}
	}
}

func TestRESTRetryAfterPausesSiblingOnly(t *testing.T) {
	if runDefaultBudgetProcess(t) {
		return
	}
	if err := ConfigureProcessRateLimit(config.RateLimitConfig{RequestsPerSecond: 10000, Burst: 5}); err != nil {
		t.Fatal(err)
	}
	received := make(chan struct{}, 1)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/limited" {
			calls.Add(1)
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			received <- struct{}{}
			return
		}
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[]}}}`))
		} else {
			_, _ = w.Write([]byte(`{"result":[]}`))
		}
	}))
	defer server.Close()
	cfg := config.Default().Cloudflare
	cfg.APIBase = server.URL
	first, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stop()
	done := make(chan error, 1)
	go func() { var out []any; done <- New(cfg).Get(first, "/limited", nil, &out) }()
	<-received
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("limited request did not cancel during pause: %v", err)
	}
	rest, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var out []any
	if err := New(cfg).Get(rest, "/sibling", nil, &out); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Retry-After did not pause sibling: %v", err)
	}
	graph, cancelGraph := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelGraph()
	if _, err := New(cfg).graph(graph, "{viewer{zones{}}}"); err != nil {
		t.Fatalf("REST pause delayed GraphQL: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("cancelled REST retried: %d", calls.Load())
	}
}

func TestGraphQLMalformedDataBudgetStillPauses(t *testing.T) {
	if runDefaultBudgetProcess(t) {
		return
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":false}},"errors":[{"extensions":{"code":"budget"}}]}`))
	}))
	defer server.Close()
	cfg := config.Default().Cloudflare
	cfg.APIBase = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := New(cfg).DatasetSettings(ctx, ZoneScope, "fixture", "events")
	var budget *GraphQLBudgetError
	if !errors.As(err, &budget) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("authoritative budget was hidden by malformed data: %v", err)
	}
}
