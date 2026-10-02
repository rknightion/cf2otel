package d1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/config"
)

func TestStickyResourceAdmissionAcrossWindows(t *testing.T) {
	var admission nameAdmission
	for i := range 49 {
		name := fmt.Sprintf("named-%02d", i)
		if got := admission.admit("metric-fixture", name); got != name {
			t.Fatalf("initial admission %q", got)
		}
	}
	// A new window does not evict inactive admitted series or admit newcomers.
	if got := admission.admit("metric-fixture", "newcomer"); got != "other" {
		t.Fatalf("new window newcomer = %q, want other", got)
	}
	if got := admission.admit("metric-fixture", "named-00"); got != "named-00" {
		t.Fatalf("sticky existing series = %q", got)
	}
	if got := admission.admit("another-metric", "newcomer"); got != "newcomer" {
		t.Fatalf("independent metric = %q", got)
	}
}

func TestFailedNameRefreshReplacesExpiredMapForOneHour(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	calls, fail := 0, false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		page := r.URL.Query().Get("page")
		if fail && page == "2" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		row := resourceRow{ID: "opaque-one", Name: "named-one"}
		if page == "2" {
			row = resourceRow{ID: "opaque-two", Name: "named-two"}
		}
		number := 1
		if page == "2" {
			number = 2
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []resourceRow{row}, "result_info": map[string]any{"page": number, "per_page": 1, "count": 1, "total_count": 2}})
	}))
	defer server.Close()
	api := cfapi.New(config.CloudflareConfig{APIBase: server.URL, Timeout: time.Second, MaxResponseBytes: 65536})
	cache := newNameCache()
	cache.now = func() time.Time { return now }
	lookup := func() map[string]string { return cache.lookup(context.Background(), api, "account-fixture") }
	if names := lookup(); len(names) != 2 {
		t.Fatalf("complete initial mapping = %v", names)
	}
	fail = true
	now = now.Add(time.Hour - time.Nanosecond)
	if names := lookup(); len(names) != 2 || calls != 2 {
		t.Fatalf("premature refresh: mapping=%v calls=%d", names, calls)
	}
	now = now.Add(time.Nanosecond)
	if names := lookup(); len(names) != 0 {
		t.Fatalf("failed refresh retained stale or partial names: %v", names)
	}
	if calls != 4 {
		t.Fatalf("refresh calls = %d, want 4", calls)
	}
	fail = false
	now = now.Add(time.Hour - time.Nanosecond)
	if names := lookup(); len(names) != 0 || calls != 4 {
		t.Fatalf("negative TTL not retained: mapping=%v calls=%d", names, calls)
	}
	now = now.Add(time.Nanosecond)
	if names := lookup(); len(names) != 2 || calls != 6 {
		t.Fatalf("failed refresh boundary did not recover: mapping=%v calls=%d", names, calls)
	}
}
