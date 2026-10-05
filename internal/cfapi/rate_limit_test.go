package cfapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

func TestProcessMaxPauseConfigurationIdentity(t *testing.T) {
	if runDefaultBudgetProcess(t) {
		return
	}
	cfg := config.Default().Cloudflare
	cfg.RateLimit.MaxPause = time.Second
	if err := ConfigureProcessRateLimit(cfg.RateLimit); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":[]}`))
	}))
	defer server.Close()
	cfg.APIBase = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var out []any
	if err := New(cfg).Get(ctx, "/first", nil, &out); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureProcessRateLimit(cfg.RateLimit); err != nil {
		t.Fatalf("identical active ceiling rejected: %v", err)
	}
	cfg.RateLimit.MaxPause = 2 * time.Second
	if err := ConfigureProcessRateLimit(cfg.RateLimit); err == nil {
		t.Fatal("active ceiling change accepted")
	}
}

// A subprocess is the process edge: configuration is immutable after traffic,
// so this exercises a genuinely fresh configured process rather than resetting
// the singleton in a test or changing production defaults for fixtures.
// The former 64-bit collision must either be rejected at the loader boundary
// (the bounded contract) or distinguished after real traffic; it cannot silently
// accept a different active configuration.
func TestProcessRateLimitReviewIntegerCollision(t *testing.T) {
	if os.Getenv("LOOP_TEST_COLLISION_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProcessRateLimitReviewIntegerCollision$", "-test.v")
		cmd.Env = append(os.Environ(), "LOOP_TEST_COLLISION_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("integer collision process: %v\n%s", err, output)
		}
		return
	}
	load := func(burst string) (*config.Config, error) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("cloudflare:\n  rate_limit:\n    burst: "+burst+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		return config.Load(path)
	}
	secondBurst := "9223372036854775806"
	first, err := load("9223372036854775807")
	if err != nil {
		if !strings.Contains(err.Error(), "cloudflare.rate_limit.") {
			t.Fatal(err)
		}
		if _, err := load("9223372036854775806"); err == nil || !strings.Contains(err.Error(), "cloudflare.rate_limit.") {
			t.Fatalf("second oversized burst not rejected: %v", err)
		}
		// Under the bounded contract, also exercise exact integer identity
		// at the largest supported capacity through the same public path.
		first, err = load("1000")
		if err != nil {
			t.Fatal(err)
		}
		secondBurst = "999"
	}
	if err := ConfigureProcessRateLimit(first.Cloudflare.RateLimit); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":[]}`))
	}))
	defer server.Close()
	first.Cloudflare.APIBase = server.URL
	var out []any
	if err := New(first.Cloudflare).Get(context.Background(), "/first", nil, &out); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureProcessRateLimit(first.Cloudflare.RateLimit); err != nil {
		t.Fatalf("identical integer burst rejected after HTTP traffic: %v", err)
	}
	second, err := load(secondBurst)
	if err != nil {
		t.Fatal(err)
	}
	if err := ConfigureProcessRateLimit(second.Cloudflare.RateLimit); err == nil {
		t.Fatal("distinct integer bursts collided after HTTP traffic")
	}
}

func TestProcessRateLimitRepeatedConfiguration(t *testing.T) {
	if os.Getenv("CF2OTEL_TEST_LIFECYCLE_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProcessRateLimitRepeatedConfiguration$", "-test.v")
		cmd.Env = append(os.Environ(), "CF2OTEL_TEST_LIFECYCLE_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("lifecycle process: %v\n%s", err, output)
		}
		return
	}
	cfg := config.Default().Cloudflare
	if err := ConfigureProcessRateLimit(cfg.RateLimit); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"result":[]}`))
	}))
	defer server.Close()
	cfg.APIBase = server.URL
	var out []any
	for range cfg.RateLimit.REST.Burst {
		if err := New(cfg).Get(context.Background(), "/first", nil, &out); err != nil {
			t.Fatal(err)
		}
	}
	if err := ConfigureProcessRateLimit(cfg.RateLimit); err != nil {
		t.Fatalf("identical active configuration rejected: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := NewObserved(cfg, nil).Get(ctx, "/second", nil, &out); !errors.Is(err, context.DeadlineExceeded) || calls.Load() != int32(cfg.RateLimit.REST.Burst) {
		t.Fatalf("reconfiguration reset active bucket: err=%v calls=%d", err, calls.Load())
	}
	if err := ConfigureProcessRateLimit(config.RateLimitConfig{RequestsPerSecond: 20, Burst: 2}); err == nil {
		t.Fatal("different active configuration accepted")
	}
}

func TestConfiguredLimiterPublicPaths(t *testing.T) {
	if os.Getenv("CF2OTEL_TEST_LIMITER_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestConfiguredLimiterPublicPaths$", "-test.v")
		cmd.Env = append(os.Environ(), "CF2OTEL_TEST_LIMITER_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("configured process: %v\n%s", err, output)
		}
		return
	}
	if err := ConfigureProcessRateLimit(config.RateLimitConfig{RequestsPerSecond: 20, Burst: 2}); err != nil {
		t.Fatal(err)
	}
	var calls, retries atomic.Int32
	var received []time.Time
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		mu.Lock()
		received = append(received, time.Now())
		mu.Unlock()
		switch r.URL.Path {
		case "/pages":
			if r.URL.Query().Get("page") == "1" {
				_, _ = w.Write([]byte(`{"result":[{"id":"opaque"}]}`))
			} else {
				_, _ = w.Write([]byte(`{"result":[]}`))
			}
		case "/retry":
			if retries.Add(1) == 1 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(429)
				return
			}
			_, _ = w.Write([]byte(`{"result":[]}`))
		case "/redirect":
			http.Redirect(w, r, "/redirect-final", http.StatusTemporaryRedirect)
		case "/redirect-final":
			_, _ = w.Write([]byte(`{"result":[]}`))
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/slow":
			time.Sleep(60 * time.Millisecond)
			_, _ = w.Write([]byte(`{"result":[]}`))
		default:
			_, _ = w.Write([]byte(`{"result":[]}`))
		}
	}))
	defer server.Close()
	cfg := config.Default().Cloudflare
	cfg.APIBase = server.URL
	cfg.Timeout = time.Second
	a, b := New(cfg), NewObserved(cfg, nil)
	var out []any
	started := time.Now()
	if err := a.Pages(context.Background(), "/pages", nil, 1, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("pagination rows=%d", len(out))
	}
	if err := b.Get(context.Background(), "/retry", nil, &out); err != nil {
		t.Fatal(err)
	}
	if err := a.Get(context.Background(), "/redirect", nil, &out); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 6 || time.Since(started) < 180*time.Millisecond {
		t.Fatalf("shared configured pacing: calls=%d elapsed=%s", calls.Load(), time.Since(started))
	}
	mu.Lock()
	for i := 2; i < len(received); i++ {
		if received[i].Sub(received[i-1]) < 35*time.Millisecond {
			t.Errorf("hop %d was not paced: %s", i, received[i].Sub(received[i-1]))
		}
	}
	mu.Unlock()
	if err := ConfigureProcessRateLimit(cfg.RateLimit); err == nil {
		t.Fatal("active budget was reconfigured")
	}
	cfg.Timeout = 10 * time.Millisecond
	c := New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Get(ctx, "/slow", nil, &out); !os.IsTimeout(err) || ctx.Err() != nil {
		t.Fatalf("network timeout not retained independently of caller context: %v, caller=%v", err, ctx.Err())
	}
	before := calls.Load()
	queued, stop := context.WithTimeout(context.Background(), time.Millisecond)
	defer stop()
	if err := c.Get(queued, "/unused", nil, &out); !errors.Is(err, context.DeadlineExceeded) || calls.Load() != before {
		t.Fatalf("queue cancellation made a network attempt: %v calls=%d before=%d", err, calls.Load(), before)
	}
	before = calls.Load()
	if err := a.Get(context.Background(), "/loop", nil, &out); err == nil || calls.Load()-before != 10 {
		t.Fatalf("redirect chain not bounded: err=%v attempts=%d", err, calls.Load()-before)
	}
	var forwarded atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded.Store(r.Header.Get("Authorization") != "")
		_, _ = w.Write([]byte(`{"result":[]}`))
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/final", http.StatusFound)
	}))
	defer redirect.Close()
	cfg.APIBase, cfg.APIToken, cfg.Timeout = redirect.URL, "opaque", time.Second
	if err := New(cfg).Get(context.Background(), "/start", nil, &out); err != nil || forwarded.Load() {
		t.Fatalf("cross-origin redirect: err=%v credentialsForwarded=%v", err, forwarded.Load())
	}
}

func runDefaultBudgetProcess(t *testing.T) bool {
	t.Helper()
	if os.Getenv("CF2OTEL_TEST_DEFAULT_CHILD") == "1" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$", "-test.v")
	cmd.Env = append(os.Environ(), "CF2OTEL_TEST_DEFAULT_CHILD=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("default process: %v\n%s", err, output)
	}
	return true
}

func TestReviewSharedBudgetPublicBoundary(t *testing.T) {
	if runDefaultBudgetProcess(t) {
		return
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"success":true,"result":[]}`))
	}))
	defer server.Close()
	cfg := config.Default().Cloudflare
	cfg.APIBase = server.URL
	a, b := New(cfg), NewObserved(cfg, nil)
	var out []any
	for range cfg.RateLimit.REST.Burst {
		if err := a.Get(context.Background(), "/zones", nil, &out); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := b.Get(ctx, "/accounts", nil, &out)
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != int32(cfg.RateLimit.REST.Burst) {
		t.Fatalf("shared limiter missing: err=%v upstreamAttempts=%d; want deadline after the REST burst is exhausted", err, calls.Load())
	}
}

func TestReviewRedirectQueueOutsideNetworkTimeout(t *testing.T) {
	if runDefaultBudgetProcess(t) {
		return
	}
	// Exhaust the real REST burst before redirecting; quota waits remain outside HTTP timeout.
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/zones", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"result":[]}`))
	}))
	defer server.Close()
	cfg := config.Default().Cloudflare
	cfg.APIBase = server.URL
	cfg.Timeout = 40 * time.Millisecond
	api := New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var out []any
	for range cfg.RateLimit.REST.Burst {
		if err := api.Get(ctx, "/zones", nil, &out); err != nil {
			t.Fatal(err)
		}
	}
	started := time.Now()
	err := api.Get(ctx, "/redirect", nil, &out)
	if err != nil || calls.Load() != int32(cfg.RateLimit.REST.Burst+2) || time.Since(started) < 600*time.Millisecond {
		t.Fatalf("quota waiting consumed network timeout: err=%v attempts=%d elapsed=%s callerContext=%v; want successful paced redirect within caller deadline", err, calls.Load(), time.Since(started), ctx.Err())
	}
}

// GraphQL is the only permitted POST path. Redirecting it must never convert
// it to GET or replay its body to another API path or origin.
func TestGraphQLRedirectFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		crossOrigin bool
	}{
		{"moved", http.StatusMovedPermanently, false},
		{"found", http.StatusFound, true},
		{"see_other", http.StatusSeeOther, false},
		{"temporary", http.StatusTemporaryRedirect, false},
		{"permanent_cross_origin", http.StatusPermanentRedirect, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var first, redirected atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				redirected.Add(1)
				_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[]}}}`))
			}))
			defer target.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/graphql" {
					redirected.Add(1)
					_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[]}}}`))
					return
				}
				first.Add(1)
				if r.Method != http.MethodPost {
					t.Errorf("initial GraphQL method=%s; want POST", r.Method)
				}
				location := "/arbitrary-api-path"
				if tc.crossOrigin {
					location = target.URL + location
				}
				w.Header().Set("Location", location)
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			cfg := config.Default().Cloudflare
			cfg.APIBase = server.URL
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := New(cfg).DatasetSettings(ctx, ZoneScope, "opaque", "events")
			var httpErr *HTTPError
			if !errors.As(err, &httpErr) || httpErr.Status != tc.status || first.Load() != 1 || redirected.Load() != 0 {
				t.Fatalf("GraphQL redirect did not fail closed: err=%v first=%d redirected=%d; want HTTP %d and no second upstream request", err, first.Load(), redirected.Load(), tc.status)
			}
		})
	}
}
