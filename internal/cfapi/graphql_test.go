package cfapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

func TestMain(m *testing.M) {
	// Timing witnesses use a fresh process with the production default budget.
	if os.Getenv("CF2OTEL_TEST_DEFAULT_CHILD") != "1" && os.Getenv("CF2OTEL_TEST_LIMITER_CHILD") != "1" && os.Getenv("CF2OTEL_TEST_LIFECYCLE_CHILD") != "1" && os.Getenv("LOOP_TEST_COLLISION_CHILD") != "1" && os.Getenv("CF2OTEL_TEST_FIFO_CHILD") != "1" && os.Getenv("CF2OTEL_TEST_PACK_CHILD") != "1" {
		if err := ConfigureProcessRateLimitWithClock(config.Default().Cloudflare.RateLimit, &fixtureAdmissionClock{now: time.Now()}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

// Only admission/accumulation time advances; TTLs, localhost HTTP deadlines,
// retries and SDK/export timers remain real. Every credit still follows quota.
type fixtureAdmissionClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fixtureAdmissionClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fixtureAdmissionClock) NewTimer(d time.Duration) AdmissionTimer {
	c.mu.Lock()
	c.now = c.now.Add(max(0, d))
	at := c.now
	c.mu.Unlock()
	ch := make(chan time.Time, 1)
	ch <- at
	return &fixtureAdmissionTimer{ch: ch}
}

type fixtureAdmissionTimer struct{ ch chan time.Time }

func (t *fixtureAdmissionTimer) C() <-chan time.Time { return t.ch }
func (t *fixtureAdmissionTimer) Stop() bool {
	select {
	case <-t.ch:
		return true
	default:
		return false
	}
}

func TestGraphQLRetentionGapCarriesFloor(t *testing.T) {
	requestTime := make(chan time.Time, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestTime <- time.Now()
		_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[{"settings":{"httpRequestsAdaptive":{"enabled":true,"availableFields":["datetime"],"maxNumberOfFields":70,"maxDuration":3600,"notOlderThan":60,"maxPageSize":100}}}]}}}`))
	}))
	defer server.Close()
	client := New(config.CloudflareConfig{APIBase: server.URL, APIToken: "invented", Timeout: time.Second, MaxResponseBytes: 4096})
	now := time.Now()
	var rows []map[string]any
	err := client.Query(context.Background(), GraphQLRequest{Scope: ZoneScope, ScopeID: "invented-zone", Dataset: "httpRequestsAdaptive", WantedFields: []string{"datetime"}, From: now.Add(-2 * time.Minute), To: now}, &rows)
	var gap *RetentionGapError
	if !errors.As(err, &gap) {
		t.Fatalf("expected typed retention gap, got %v", err)
	}
	settingsTime := <-requestTime
	if gap.Dataset != "httpRequestsAdaptive" || gap.Floor.Before(settingsTime.Add(-61*time.Second)) || gap.Floor.After(settingsTime.Add(-59*time.Second)) {
		t.Fatalf("gap=%+v", gap)
	}
}
