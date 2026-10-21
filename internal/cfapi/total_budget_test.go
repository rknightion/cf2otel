package cfapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

// Sparse GraphQL traffic must not strand most of the general API allowance
// while REST body capture falls behind its source arrival rate.
func TestDefaultRESTCanUseSpareCapacity(t *testing.T) {
	if runDefaultBudgetProcess(t) {
		return
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"result":[]}`))
	}))
	defer server.Close()
	cfg := config.Default().Cloudflare
	cfg.APIBase = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for i := range 12 {
		var out []any
		if err := New(cfg).Get(ctx, "/fixture", nil, &out); err != nil {
			t.Fatalf("REST stranded spare capacity: completed %d of 12 requests in three seconds: %v", i, err)
		}
	}
}

// Class limits may overlap, but their traffic must still fit one per-user
// allowance, even when issued concurrently through distinct client instances.
func TestDefaultTotalBudgetCoversRESTAndGraphQL(t *testing.T) {
	if runDefaultBudgetProcess(t) {
		return
	}
	var mu sync.Mutex
	var starts []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[{"settings":{"events":{"enabled":true,"availableFields":["count"]}}}]}}}`))
		} else {
			_, _ = w.Write([]byte(`{"success":true,"result":[]}`))
		}
	}))
	defer server.Close()
	cfg := config.Default().Cloudflare
	cfg.APIBase = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	errs := make(chan error, 16)
	for i := range 8 {
		go func() {
			var out []any
			errs <- New(cfg).Get(ctx, "/fixture", nil, &out)
		}()
		go func() {
			_, err := New(cfg).DatasetSettings(ctx, ZoneScope, "zone-"+strconv.Itoa(i), "events")
			errs <- err
		}()
	}
	for range 16 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(starts) != 16 {
		t.Fatalf("received %d requests, want all 16", len(starts))
	}
	// Five initial tokens plus eleven refills at 3.75 rps require 2.93s.
	if elapsed := starts[len(starts)-1].Sub(starts[0]); elapsed < 2800*time.Millisecond {
		t.Fatalf("combined traffic bypassed the total ceiling: 16 requests in %s", elapsed)
	}
}
