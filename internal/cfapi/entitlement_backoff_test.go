package cfapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

// Exercise real HTTP, with only settings time replaced at the process edge.
func TestEntitlementBackoffHTTPExpiryAndIsolation(t *testing.T) {
	for _, tc := range []struct {
		name            string
		absent          bool
		configured, ttl time.Duration
	}{
		{"disabled-default", false, 0, time.Hour},
		{"absent-default", true, 0, time.Hour},
		{"disabled-configured", false, 30 * time.Minute, 30 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			absent := tc.absent
			var mu sync.Mutex
			calls := map[string]int{}
			enabled := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Query string `json:"query"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				dataset, zone := "fixtureGroups", "zone-fixture"
				if strings.Contains(body.Query, "otherGroups") {
					dataset = "otherGroups"
				}
				if strings.Contains(body.Query, "zone-other") {
					zone = "zone-other"
				}
				mu.Lock()
				defer mu.Unlock()
				if strings.Contains(body.Query, "settings{") {
					calls[zone+"/"+dataset]++
					settings := map[string]any{}
					if !absent || enabled || dataset == "otherGroups" || zone == "zone-other" {
						settings[dataset] = map[string]any{"enabled": enabled || dataset == "otherGroups" || zone == "zone-other", "availableFields": []string{"count"}}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"zones": []any{map[string]any{"settings": settings}}}}})
				} else {
					_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[{"fixtureGroups":[],"otherGroups":[],"a":[],"b":[]}]}}}`))
				}
			}))
			defer srv.Close()
			c := New(config.CloudflareConfig{APIBase: srv.URL, Timeout: time.Second, EntitlementBackoff: tc.configured})
			now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			c.now = func() time.Time { return now }
			req := GraphQLRequest{Scope: ZoneScope, ScopeID: "zone-fixture", Dataset: "fixtureGroups", WantedFields: []string{"count"}, From: now.Add(-time.Minute), To: now}
			query := func(r GraphQLRequest) error { var rows []any; return c.Query(context.Background(), r, &rows) }
			deny := func(err error) {
				t.Helper()
				var denial *UnentitledError
				if !errors.As(err, &denial) || !denial.Disabled {
					t.Fatalf("expected explicit entitlement denial, got %v", err)
				}
			}
			deny(query(req))
			mu.Lock()
			enabled = true
			mu.Unlock()
			now = now.Add(tc.ttl - time.Minute)
			deny(query(req))
			_, err := c.QueryBatch(context.Background(), []GraphQLBatchSelection{{Alias: "a", Request: req}, {Alias: "b", Request: req}})
			deny(err)
			other := req
			other.ScopeID = "zone-other"
			if err := query(other); err != nil {
				t.Fatalf("independent zone: %v", err)
			}
			other = req
			other.Dataset = "otherGroups"
			if err := query(other); err != nil {
				t.Fatalf("independent dataset: %v", err)
			}
			mu.Lock()
			n := calls["zone-fixture/fixtureGroups"]
			mu.Unlock()
			if n != 1 {
				t.Fatalf("cached denial requested settings %d times, want 1", n)
			}
			now = now.Add(time.Minute)
			result, err := c.QueryBatch(context.Background(), []GraphQLBatchSelection{{Alias: "a", Request: req}, {Alias: "b", Request: req}})
			if err != nil || len(result) != 2 {
				t.Fatalf("backoff expiry must recover both aliases: result=%v error=%v", result, err)
			}
			mu.Lock()
			n = calls["zone-fixture/fixtureGroups"]
			mu.Unlock()
			if n != 2 {
				t.Fatalf("aliases must share one recovered settings decision, got %d requests", n)
			}
		})
	}
}

func TestEntitlementBackoffHTTPSchemaAndFailureRecovery(t *testing.T) {
	for _, broken := range []string{`{"enabled":true}`, `{"enabled":true,"availableFields":null}`, `null`, `{}`, `{"enabled":null}`, `{"enabled":"false"}`, "auth", "transient", "graphql", "missing-settings", "null-settings"} {
		t.Run(broken, func(t *testing.T) {
			var mu sync.Mutex
			recovered := false
			settingsCalls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Query string `json:"query"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				mu.Lock()
				defer mu.Unlock()
				if !strings.Contains(body.Query, "settings{") {
					_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[{"fixtureGroups":[]}]}}}`))
					return
				}
				settingsCalls++
				raw := broken
				if recovered {
					raw = `{"enabled":true,"availableFields":["count"]}`
				}
				if !recovered {
					switch broken {
					case "auth":
						w.WriteHeader(http.StatusForbidden)
						return
					case "transient":
						w.Header().Set("Retry-After", "0")
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					case "graphql":
						_, _ = w.Write([]byte(`{"errors":[{"message":"fixture failure"}]}`))
						return
					case "missing-settings":
						_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[{}]}}}`))
						return
					case "null-settings":
						_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[{"settings":null}]}}}`))
						return
					}
				}
				_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[{"settings":{"fixtureGroups":` + raw + `}}]}}}`))
			}))
			defer srv.Close()
			c := New(config.CloudflareConfig{APIBase: srv.URL, Timeout: time.Second})
			now := time.Now()
			req := GraphQLRequest{Scope: ZoneScope, ScopeID: "zone-fixture", Dataset: "fixtureGroups", WantedFields: []string{"count"}, From: now.Add(-time.Minute), To: now}
			var rows []any
			err := c.Query(context.Background(), req, &rows)
			var denial *UnentitledError
			if err == nil || errors.As(err, &denial) {
				t.Errorf("schema/auth/transient error must not become entitlement denial: %v", err)
			}
			mu.Lock()
			recovered = true
			prior := settingsCalls
			mu.Unlock()
			if err := c.Query(context.Background(), req, &rows); err != nil {
				t.Fatalf("next request must recover without backoff: %v", err)
			}
			mu.Lock()
			n := settingsCalls
			mu.Unlock()
			if n != prior+1 {
				t.Fatalf("schema/failure cached: settings requests %d want %d", n, prior+1)
			}
		})
	}
}

func TestEntitlementBackoffHTTPConcurrentDecision(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(body.Query, "zone-fixture") {
			mu.Lock()
			calls++
			mu.Unlock()
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[{"settings":{"fixtureGroups":{"enabled":false}}}]}}}`))
	}))
	defer srv.Close()
	defer close(release)
	c := New(config.CloudflareConfig{APIBase: srv.URL, Timeout: 2 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := c.DatasetSettings(ctx, ZoneScope, "zone-fixture", "fixtureGroups"); result <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("first settings request did not start")
	}
	// A same-key waiter must cancel locally instead of issuing another HTTP probe.
	waiting, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stop()
	if _, err := c.DatasetSettings(waiting, ZoneScope, "zone-fixture", "fixtureGroups"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter error=%v", err)
	}
	if _, err := c.DatasetSettings(ctx, ZoneScope, "zone-other", "fixtureGroups"); err != nil {
		t.Fatalf("independent settings blocked: %v", err)
	}
	mu.Lock()
	n := calls
	mu.Unlock()
	if n != 1 {
		t.Fatalf("same key issued %d concurrent settings probes, want 1", n)
	}
	// Release the owner without double-closing the channel in deferred cleanup.
	release <- struct{}{}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if _, err := c.DatasetSettings(ctx, ZoneScope, "zone-fixture", "fixtureGroups"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	n = calls
	mu.Unlock()
	if n != 1 {
		t.Fatalf("cached denial requested %d probes", n)
	}
}
