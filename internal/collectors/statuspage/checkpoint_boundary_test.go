package statuspage_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/collectors/statuspage"
	"github.com/rknightion/cf2otel/internal/config"
)

// Exercise only public registration, scheduler, durable FileStore and SDK logs.
// Restart means new instances, not an in-memory dedup ledger. Older delayed
// updates intentionally remain un-emitted under the best-effort contract.
func TestIncidentsFileStoreRestartCheckpoint(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)
	update := func(id, body string, stamp time.Time) string {
		return fmt.Sprintf(`{"id":%q,"status":"monitoring","body":%q,"updated_at":%q}`, id, body, stamp.Format(time.RFC3339Nano))
	}
	page := func(updates ...string) string {
		return `{"incidents":[{"id":"incident-opaque","name":"opaque-incident","status":"monitoring","impact":"minor","incident_updates":[` + strings.Join(updates, ",") + `]}]}`
	}
	first := update("update-first", "opaque-first", at)
	summary := `{"components":[]}`
	incidents := page(first)
	srv := server(t, &summary, &incidents)
	e, _, logs := emitter(t)
	checkpointPath := filepath.Join(t.TempDir(), "checkpoints")
	poll := func(now time.Time) {
		t.Helper()
		r, _, entry := registered(t, srv.URL, time.Minute, 500)
		store, err := collector.NewFileStore(checkpointPath)
		if err != nil {
			t.Fatal(err)
		}
		s := collector.NewScheduler(r, e, store)
		s.Now = func() time.Time { return now }
		run(t, s, entry)
		if cp, ok := store.Get(entry.Collector.Name()); !ok || !cp.Equal(now) {
			t.Fatalf("checkpoint=%s, present=%v; want successful source bound %s without lag", cp, ok, now)
		}
	}
	poll(at)
	if len(logs.records) != 1 || logs.records[0].Body().AsString() != "opaque-first" {
		t.Fatal("first update at the source bound must be emitted once")
	}
	poll(at.Add(10 * time.Second))
	if len(logs.records) != 1 {
		t.Fatal("restart replayed an already committed update")
	}
	newer := update("update-first", "opaque-revision", at.Add(20*time.Second))
	incidents = page(first, newer, newer) // Same ID is a new revision; response duplicates emit once.
	poll(at.Add(30 * time.Second))
	if len(logs.records) != 2 || logs.records[1].Body().AsString() != "opaque-revision" {
		t.Fatalf("newer update after restart must emit once: records=%d", len(logs.records))
	}
	older := update("update-delayed", "opaque-delayed", at.Add(-2*time.Hour))
	equal := update("update-equal", "opaque-equal", at.Add(30*time.Second))
	future := update("update-future", "opaque-future", at.Add(time.Hour))
	incidents = page(first, newer, older, equal, future)
	poll(at.Add(time.Minute))
	if len(logs.records) != 2 {
		t.Fatal("old, equal-checkpoint or future update was emitted")
	}
	incidents = `{"incidents":[]}`
	poll(at.Add(2 * time.Minute))
	incidents = page(update("update-empty-delay", "opaque-empty-delay", at.Add(90*time.Second)))
	poll(at.Add(3 * time.Minute))
	if len(logs.records) != 2 {
		t.Fatal("empty poll must advance checkpoint; subsequent older publication is intentionally missed")
	}
}

// Pin the smallest supported incident bounds. Source updates may still have
// fractional timestamps, while every successfully persisted bound stays whole.
func TestIncidentsWholeSecondFileStoreRestart(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 1, 0, time.UTC)
	summary := `{"components":[]}`
	firstAt := at.Add(-750 * time.Millisecond)
	incidents := incidentJSON(firstAt, "opaque-fractional-update")
	srv := server(t, &summary, &incidents)
	checkpointPath := filepath.Join(t.TempDir(), "checkpoints")
	e, _, logs := emitter(t)
	poll := func(now time.Time) {
		t.Helper()
		cfg := config.Default()
		cfg.Cloudflare.APIToken = "opaque-token"
		cfg.Cloudflare.AccountID = "opaque-account"
		cfg.OTLP.Endpoint = "https://opaque.invalid/otlp"
		cfg.OTLP.GrafanaCloud.InstanceID = "opaque-instance"
		cfg.OTLP.GrafanaCloud.Token = "opaque-token"
		cfg.Statuspage.BaseURL = srv.URL
		cfg.Collectors["statuspage.incidents"] = config.CollectorConfig{Enabled: true, Interval: time.Second, InitialLookback: time.Second, MaxWindow: time.Second}
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
		r := collector.NewRegistry()
		statuspage.Register(collector.Deps{Config: &cfg, Registry: r})
		store, err := collector.NewFileStore(checkpointPath)
		if err != nil {
			t.Fatal(err)
		}
		entries := r.Entries()
		if len(entries) != 1 || entries[0].Collector.Name() != "statuspage.incidents" {
			t.Fatalf("independent incident registration: %v", entries)
		}
		s := collector.NewScheduler(r, e, store)
		s.Now = func() time.Time { return now }
		run(t, s, entries[0])
		cp, ok := store.Get("statuspage.incidents")
		if !ok || !cp.Equal(now.Truncate(time.Second)) || cp.Nanosecond() != 0 {
			t.Fatalf("whole-second checkpoint=%s present=%v now=%s", cp, ok, now)
		}
	}
	poll(at.Add(900 * time.Millisecond))
	if len(logs.records) != 1 || !logs.records[0].Timestamp().Equal(firstAt) {
		t.Fatalf("fractional source update must emit once: records=%d", len(logs.records))
	}
	poll(at.Add(time.Second)) // New registry, scheduler and FileStore loaded from disk.
	if len(logs.records) != 1 {
		t.Fatalf("restart replayed committed fractional update: records=%d", len(logs.records))
	}
	newerAt := at.Add(1250 * time.Millisecond)
	incidents = incidentJSON(newerAt, "opaque-newer-revision")
	poll(at.Add(2 * time.Second))
	if len(logs.records) != 2 || !logs.records[1].Timestamp().Equal(newerAt) {
		t.Fatalf("newer fractional revision must emit once: records=%d", len(logs.records))
	}
	poll(at.Add(3 * time.Second))
	if len(logs.records) != 2 {
		t.Fatalf("second restart replayed newer revision: records=%d", len(logs.records))
	}
}

func TestStatuspageNeedsNoCloudflareCredentials(t *testing.T) {
	for _, credentials := range []bool{false, true} {
		t.Run(fmt.Sprint(credentials), func(t *testing.T) {
			requests := make(chan string, 2)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method != http.MethodGet || req.URL.RawQuery != "" {
					t.Error("status request must be a fixed unauthenticated GET")
				}
				for key, values := range req.Header {
					if strings.Contains(strings.ToLower(key), "auth") || strings.Contains(strings.ToLower(key), "cookie") || strings.Contains(strings.Join(values, ""), "opaque-secret") {
						t.Error("source request contains a credential header")
					}
				}
				requests <- req.URL.Path
				switch req.URL.Path {
				case "/api/v2/summary.json":
					fmt.Fprint(w, `{"components":[{"name":"opaque","status":"operational","group":false}]}`)
				case "/api/v2/incidents.json":
					fmt.Fprint(w, `{"incidents":[]}`)
				default:
					t.Error("unexpected public API path")
				}
			}))
			defer srv.Close()
			cfg := config.Default()
			cfg.Statuspage.BaseURL = srv.URL
			if credentials {
				cfg.Cloudflare.APIToken = "opaque-secret-cf"
				cfg.OTLP.Headers = map[string]string{"Authorization": "opaque-secret-otlp", "X-Auth-Key": "opaque-secret-key"}
			}
			for _, name := range []string{"statuspage.components", "statuspage.incidents"} {
				entry := cfg.Collectors[name]
				entry.Enabled = true
				cfg.Collectors[name] = entry
			}
			r := collector.NewRegistry()
			// No cfapi client is supplied, and no application validation (which
			// still requires global tenant/OTLP settings) is bypassed in production.
			statuspage.Register(collector.Deps{Config: &cfg, Registry: r})
			e, reader, _ := emitter(t)
			store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "checkpoints"))
			if err != nil {
				t.Fatal(err)
			}
			s := collector.NewScheduler(r, e, store)
			for _, entry := range r.Entries() {
				if err := s.RunOnce(context.Background(), entry); err != nil {
					t.Fatal(err)
				}
			}
			calls := map[string]int{}
			for len(requests) > 0 {
				calls[<-requests]++
			}
			if calls["/api/v2/summary.json"] != 1 || calls["/api/v2/incidents.json"] != 1 || len(points(t, reader)) != 1 {
				t.Fatalf("credential-free collectors did not exercise both public endpoints: %v", calls)
			}
		})
	}
}
