package cfapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

func TestReadOnlyGuard(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{"success":true,"result":[]}`))
	}))
	defer srv.Close()
	c := New(config.CloudflareConfig{APIBase: srv.URL, APIToken: "test", Timeout: time.Second, MaxResponseBytes: 1024})
	for _, tc := range []struct{ method, path string }{{"PUT", "/zones"}, {"POST", "/zones"}, {"DELETE", "/graphql"}, {"POST", "/graphql/other"}} {
		if _, err := c.do(context.Background(), tc.method, tc.path, nil, nil); err == nil {
			t.Errorf("accepted %s %s", tc.method, tc.path)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("guard made %d requests", calls.Load())
	}
}
func TestRESTErrorDoesNotEchoResponseText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":[{"code":10000,"message":"token-private and user-private"}]}`))
	}))
	defer srv.Close()
	c := New(config.CloudflareConfig{APIBase: srv.URL, APIToken: "test", Timeout: time.Second, MaxResponseBytes: 1024})
	var out any
	err := c.Get(context.Background(), "/zones", nil, &out)
	if err == nil || strings.Contains(err.Error(), "token-private") || strings.Contains(err.Error(), "user-private") || !strings.Contains(err.Error(), "10000") {
		t.Fatalf("REST error redaction: %v", err)
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != 403 || httpErr.Code != 10000 {
		t.Fatalf("REST error type: %v", err)
	}
}
func TestRawBodyEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"messages":[{"role":"user","content":"fixture"}]}`))
	}))
	defer srv.Close()
	c := New(config.CloudflareConfig{APIBase: srv.URL, APIToken: "test", Timeout: time.Second, MaxResponseBytes: 1024})
	var out map[string]json.RawMessage
	if err := c.GetRaw(context.Background(), "/accounts/example/ai-gateway/gateways/example/logs/example/request", nil, &out); err != nil {
		t.Fatal(err)
	}
	if len(out["messages"]) == 0 {
		t.Fatal("raw messages missing")
	}
}
func TestGraphQLErrorDoesNotEchoResponseText(t *testing.T) {
	var response graphResponse
	response.Errors = append(response.Errors, struct {
		Message string `json:"message"`
	}{Message: "query failed for token-private"})
	err := gqlErrors(response)
	if err == nil || strings.Contains(err.Error(), "token-private") {
		t.Fatalf("GraphQL error redaction: %v", err)
	}
}
func TestPageIgnoresTotalCount(t *testing.T) {
	firstPage, err := os.ReadFile("testdata/zones-page-1.json")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			_, _ = w.Write(firstPage)
		} else {
			w.Write([]byte(`{"success":true,"result":[],"result_info":{"page":2,"per_page":1,"total_count":0}}`))
		}
	}))
	defer srv.Close()
	c := New(config.CloudflareConfig{APIBase: srv.URL, APIToken: "test", Timeout: time.Second, MaxResponseBytes: 1024})
	var out []struct {
		ID string `json:"id"`
	}
	if err := c.Pages(context.Background(), "/zones", url.Values{}, 1, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || calls.Load() != 2 {
		t.Fatalf("rows=%d calls=%d", len(out), calls.Load())
	}
}
func TestGetPagePreservesServerPageSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"one"}],"result_info":{"per_page":1,"total_count":0}}`))
	}))
	defer srv.Close()
	c := New(config.CloudflareConfig{APIBase: srv.URL, APIToken: "test", Timeout: time.Second, MaxResponseBytes: 1024})
	var page Page
	if err := c.GetPage(context.Background(), "/zones", nil, &page); err != nil {
		t.Fatal(err)
	}
	if page.ResultInfo.PerPage != 1 || string(page.Result) != `[{"id":"one"}]` {
		t.Fatalf("page size=%d result=%s", page.ResultInfo.PerPage, page.Result)
	}
}
func TestGraphQLNegotiatesAndSplits(t *testing.T) {
	var dataCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Query string `json:"query"`
		}
		json.NewDecoder(r.Body).Decode(&q)
		if strings.Contains(q.Query, "settings") {
			w.Write([]byte(`{"data":{"viewer":{"zones":[{"settings":{"events":{"enabled":true,"availableFields":["datetime","a","b"],"maxNumberOfFields":2,"maxDuration":60,"notOlderThan":3600,"maxPageSize":10}}}]}}}`))
			return
		}
		dataCalls.Add(1)
		w.Write([]byte(`{"data":{"viewer":{"zones":[{"events":[{"a":1}]}]}}}`))
	}))
	defer srv.Close()
	c := New(config.CloudflareConfig{APIBase: srv.URL, APIToken: "test", Timeout: time.Second, MaxResponseBytes: 4096})
	var out []map[string]any
	now := time.Now().UTC()
	err := c.Query(context.Background(), GraphQLRequest{Scope: ZoneScope, ScopeID: "zone", Dataset: "events", WantedFields: []string{"a", "b", "forbidden"}, From: now.Add(-2 * time.Minute), To: now}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if dataCalls.Load() != 2 {
		t.Fatalf("calls=%d", dataCalls.Load())
	}
}

func TestGraphQLRetentionGapDoesNotQuery(t *testing.T) {
	var dataCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&q)
		if strings.Contains(q.Query, "settings") {
			_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[{"settings":{"events":{"enabled":true,"availableFields":["datetime","a"],"maxNumberOfFields":2,"maxDuration":60,"notOlderThan":3600}}}]}}}`))
			return
		}
		dataCalls.Add(1)
		_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[{"events":[]}]}}}`))
	}))
	defer srv.Close()
	c := New(config.CloudflareConfig{APIBase: srv.URL, APIToken: "test", Timeout: time.Second, MaxResponseBytes: 4096})
	var out []map[string]any
	now := time.Now().UTC()
	err := c.Query(context.Background(), GraphQLRequest{Scope: ZoneScope, ScopeID: "zone", Dataset: "events", WantedFields: []string{"a"}, From: now.Add(-2 * time.Hour), To: now.Add(-time.Hour)}, &out)
	if err == nil || !strings.Contains(err.Error(), "retention gap") || dataCalls.Load() != 0 {
		t.Fatalf("retention gap: err=%v data calls=%d", err, dataCalls.Load())
	}
}

func TestEntitlementRenegotiates(t *testing.T) {
	var settingsCalls, dataCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Query string `json:"query"`
		}
		json.NewDecoder(r.Body).Decode(&q)
		if strings.Contains(q.Query, "settings") {
			n := settingsCalls.Add(1)
			fields := `["a","b"]`
			if n > 1 {
				fields = `["a"]`
			}
			w.Write([]byte(`{"data":{"viewer":{"zones":[{"settings":{"events":{"enabled":true,"availableFields":` + fields + `,"maxNumberOfFields":2,"maxDuration":60}}}]}}}`))
			return
		}
		dataCalls.Add(1)
		if strings.Contains(q.Query, " b") {
			w.Write([]byte(`{"errors":[{"message":"Does not have access to the field 'B'"}]}`))
			return
		}
		w.Write([]byte(`{"data":{"viewer":{"zones":[{"events":[{"a":1}]}]}}}`))
	}))
	defer srv.Close()
	c := New(config.CloudflareConfig{APIBase: srv.URL, APIToken: "test", Timeout: time.Second, MaxResponseBytes: 4096})
	var out []map[string]any
	now := time.Now().UTC()
	err := c.Query(context.Background(), GraphQLRequest{Scope: ZoneScope, ScopeID: "zone", Dataset: "events", WantedFields: []string{"a", "b"}, From: now.Add(-time.Minute), To: now}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if settingsCalls.Load() != 2 || dataCalls.Load() != 2 || len(out) != 1 {
		t.Fatalf("settings=%d data=%d rows=%d", settingsCalls.Load(), dataCalls.Load(), len(out))
	}
}
func TestFieldLimitRequiresJoinKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"viewer":{"zones":[{"settings":{"events":{"enabled":true,"availableFields":["a","b"],"maxNumberOfFields":1,"maxDuration":60}}}]}}}`))
	}))
	defer srv.Close()
	c := New(config.CloudflareConfig{APIBase: srv.URL, APIToken: "test", Timeout: time.Second, MaxResponseBytes: 4096})
	now := time.Now()
	var out []map[string]any
	err := c.Query(context.Background(), GraphQLRequest{Scope: ZoneScope, ScopeID: "zone", Dataset: "events", WantedFields: []string{"a", "b"}, From: now.Add(-time.Minute), To: now}, &out)
	var limit *FieldLimitError
	if !errors.As(err, &limit) {
		t.Fatalf("expected field limit, got %v", err)
	}
}

func TestGraphQLFieldChunksJoinByKey(t *testing.T) {
	var dataCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Query string `json:"query"`
		}
		json.NewDecoder(r.Body).Decode(&q)
		if strings.Contains(q.Query, "settings") {
			w.Write([]byte(`{"data":{"viewer":{"zones":[{"settings":{"events":{"enabled":true,"availableFields":["id","a","b"],"maxNumberOfFields":2,"maxDuration":60}}}]}}}`))
			return
		}
		dataCalls.Add(1)
		if strings.Contains(q.Query, "a id") {
			w.Write([]byte(`{"data":{"viewer":{"zones":[{"events":[{"id":"1","a":1},{"id":"2","a":2}]}]}}}`))
		} else {
			w.Write([]byte(`{"data":{"viewer":{"zones":[{"events":[{"id":"2","b":20},{"id":"1","b":10}]}]}}}`))
		}
	}))
	defer srv.Close()
	c := New(config.CloudflareConfig{APIBase: srv.URL, APIToken: "test", Timeout: time.Second, MaxResponseBytes: 4096})
	now := time.Now()
	var out []map[string]any
	err := c.Query(context.Background(), GraphQLRequest{Scope: ZoneScope, ScopeID: "zone", Dataset: "events", WantedFields: []string{"id", "a", "b"}, JoinFields: []string{"id"}, From: now.Add(-time.Minute), To: now}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if dataCalls.Load() != 2 || len(out) != 2 || out[0]["b"] != float64(10) {
		t.Fatalf("calls=%d rows=%v", dataCalls.Load(), out)
	}
}

func TestAvailableFieldsFlattenedNames(t *testing.T) {
	got := intersect([]string{"dimensions.allowed", "sum.logins", "max.value", "quantiles.largestContentfulPaintP75", "datetime"}, []string{"dimensions_allowed", "sum_logins", "max_value", "quantiles_largestContentfulPaintP75"})
	if len(got) != 4 || got[0] != "dimensions.allowed" || got[1] != "sum.logins" || got[2] != "max.value" || got[3] != "quantiles.largestContentfulPaintP75" {
		t.Fatalf("got %v", got)
	}
	defaults := intersect(nil, []string{"max_value"})
	if len(defaults) != 1 || defaults[0] != "max.value" {
		t.Fatalf("default fields %v", defaults)
	}
}

func TestRetryAfterAndResponseCap(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"success":true,"result":[{"id":"x"}]}`))
	}))
	defer srv.Close()
	c := New(config.CloudflareConfig{APIBase: srv.URL, APIToken: "test", Timeout: time.Second, MaxResponseBytes: 1024})
	var out []Zone
	if err := c.Get(context.Background(), "/zones", nil, &out); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || len(out) != 1 {
		t.Fatalf("calls=%d rows=%d", calls.Load(), len(out))
	}
	capped := New(config.CloudflareConfig{APIBase: srv.URL, APIToken: "test", Timeout: time.Second, MaxResponseBytes: 8})
	if err := capped.Get(context.Background(), "/zones", nil, &out); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected cap error, got %v", err)
	}
}
func TestCursorPagination(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Write([]byte(`{"result":[{"id":"a"}],"result_info":{"cursor":"next","total_count":0}}`))
		} else {
			if r.URL.Query().Get("cursor") != "next" {
				t.Error("missing cursor")
			}
			w.Write([]byte(`{"result":[{"id":"b"}],"result_info":{}}`))
		}
	}))
	defer srv.Close()
	c := New(config.CloudflareConfig{APIBase: srv.URL, APIToken: "test", Timeout: time.Second, MaxResponseBytes: 1024})
	var out []Zone
	if err := c.Cursors(context.Background(), "/zones", nil, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || calls.Load() != 2 {
		t.Fatalf("rows=%d calls=%d", len(out), calls.Load())
	}
}

func TestEmptyWantedUsesAvailableFields(t *testing.T) {
	got := intersect(nil, []string{"dimensions_allowed", "sum_logins", "quantiles_largestContentfulPaintP75", "count"})
	if len(got) != 4 || got[0] != "dimensions.allowed" || got[1] != "sum.logins" || got[2] != "quantiles.largestContentfulPaintP75" {
		t.Fatalf("got %v", got)
	}
}
func TestGraphQLSaturatedWindowFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Query string `json:"query"`
		}
		json.NewDecoder(r.Body).Decode(&q)
		if strings.Contains(q.Query, "settings") {
			w.Write([]byte(`{"data":{"viewer":{"zones":[{"settings":{"events":{"enabled":true,"availableFields":["a"],"maxPageSize":1,"maxDuration":60}}}]}}}`))
		} else {
			w.Write([]byte(`{"data":{"viewer":{"zones":[{"events":[{"a":1}]}]}}}`))
		}
	}))
	defer srv.Close()
	c := New(config.CloudflareConfig{APIBase: srv.URL, APIToken: "test", Timeout: time.Second, MaxResponseBytes: 4096})
	now := time.Now()
	var out []map[string]any
	err := c.Query(context.Background(), GraphQLRequest{Scope: ZoneScope, ScopeID: "zone", Dataset: "events", WantedFields: []string{"a"}, From: now.Add(-time.Minute), To: now, Limit: 1}, &out)
	if err == nil || !strings.Contains(err.Error(), "saturated") {
		t.Fatalf("expected saturation, got %v", err)
	}
}
