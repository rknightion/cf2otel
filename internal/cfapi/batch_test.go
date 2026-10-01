package cfapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

func TestBatchHTTPAliasesAndLimits(t *testing.T) {
	for _, tc := range []struct {
		name      string
		budget    int
		response  string
		wantPosts int
		wantError bool
	}{
		{"one-post", 6, `{"a":[{"count":2}],"b":[{"count":3}]}`, 1, false},
		{"budget-split", 2, `{"a":[{"count":2}],"b":[{"count":3}]}`, 2, false},
		{"missing-alias", 6, `{"a":[]}`, 1, true},
		{"null-array", 6, `{"a":[],"b":null}`, 1, true},
		{"saturated", 6, `{"a":[{},{}],"b":[]}`, 1, true},
		{"graphql-error", 6, `{"a":[],"b":[]}`, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/graphql" {
					t.Errorf("unexpected HTTP request %s %s", r.Method, r.URL.Path)
				}
				var body struct {
					Query string `json:"query"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				if strings.Contains(body.Query, "settings{") {
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"zones": []any{map[string]any{"settings": map[string]any{"httpRequestsAdaptiveGroups": DatasetSettings{Enabled: true, AvailableFields: []string{"count", "dimensions_clientCountryName"}, MaxNumberOfFields: tc.budget, MaxPageSize: 2}}}}}}})
					return
				}
				posts++
				if tc.name == "one-post" && (!strings.Contains(body.Query, "a:httpRequestsAdaptiveGroups") || !strings.Contains(body.Query, "b:httpRequestsAdaptiveGroups")) {
					t.Errorf("batch aliases missing: %s", body.Query)
				}
				if !strings.Contains(body.Query, `requestSource:"eyeball"`) {
					t.Errorf("filter missing: %s", body.Query)
				}
				payload := `{"data":{"viewer":{"zones":[` + tc.response + `]}}}`
				if tc.name == "graphql-error" {
					payload = `{"errors":[{"message":"fixture failure"}],"data":{"viewer":{"zones":[` + tc.response + `]}}}`
				}
				_, _ = w.Write([]byte(payload))
			}))
			defer srv.Close()
			c := New(config.CloudflareConfig{APIBase: srv.URL, APIToken: "fixture", Timeout: time.Second, MaxResponseBytes: 4096})
			from := time.Now().Add(-time.Hour)
			req := GraphQLRequest{Scope: ZoneScope, ScopeID: "zone-fixture", Dataset: "httpRequestsAdaptiveGroups", WantedFields: []string{"count", "dimensions.clientCountryName"}, From: from, To: from.Add(time.Minute), Limit: 10, Filter: map[string]any{"requestSource": "eyeball"}}
			result, err := c.QueryBatch(context.Background(), []GraphQLBatchSelection{{"a", req}, {"b", req}})
			if (err != nil) != tc.wantError || posts != tc.wantPosts {
				t.Fatalf("err=%v data posts=%d want=%d", err, posts, tc.wantPosts)
			}
			if tc.wantError {
				if result != nil {
					t.Error("partial result returned on failure")
				}
				return
			}
			if string(result["a"]) != `[{"count":2}]` || string(result["b"]) != `[{"count":3}]` {
				t.Errorf("keyed results: %s %s", result["a"], result["b"])
			}
		})
	}
}

func TestBatchFailsClosedBeforeData(t *testing.T) {
	for _, name := range []string{"duplicate-alias", "invalid-alias", "mixed-scope", "mixed-ID", "unavailable", "duration", "retention", "field-limit", "disabled"} {
		t.Run(name, func(t *testing.T) {
			dataPosts := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Query string `json:"query"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				if !strings.Contains(body.Query, "settings{") {
					dataPosts++
					t.Error("invalid batch reached data POST")
					return
				}
				settings := DatasetSettings{Enabled: name != "disabled", AvailableFields: []string{"count", "dimensions_clientCountryName"}, MaxNumberOfFields: 2, MaxDuration: 60, NotOlderThan: 3600}
				if name == "field-limit" {
					settings.MaxNumberOfFields = 1
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"zones": []any{map[string]any{"settings": map[string]any{"httpRequestsAdaptiveGroups": settings}}}}}})
			}))
			defer srv.Close()
			c := New(config.CloudflareConfig{APIBase: srv.URL, APIToken: "fixture", Timeout: time.Second, MaxResponseBytes: 4096})
			from := time.Now().Add(-time.Minute)
			r := GraphQLRequest{Scope: ZoneScope, ScopeID: "zone-fixture", Dataset: "httpRequestsAdaptiveGroups", WantedFields: []string{"count", "dimensions.clientCountryName"}, From: from, To: from.Add(time.Minute), Limit: 100}
			selections := []GraphQLBatchSelection{{"a", r}, {"b", r}}
			switch name {
			case "duplicate-alias":
				selections[1].Alias = "a"
			case "invalid-alias":
				selections[0].Alias = "bad:alias"
			case "mixed-scope":
				selections[1].Request.Scope = AccountScope
			case "mixed-ID":
				selections[1].Request.ScopeID = "other-fixture"
			case "unavailable":
				selections[0].Request.WantedFields = append(selections[0].Request.WantedFields, "dimensions.clientSSLProtocol")
			case "duration":
				selections[0].Request.To = from.Add(2 * time.Minute)
			case "retention":
				selections[0].Request.From = from.Add(-2 * time.Hour)
				selections[0].Request.To = selections[0].Request.From.Add(time.Minute)
			}
			result, err := c.QueryBatch(context.Background(), selections)
			if err == nil || result != nil || dataPosts != 0 {
				t.Fatalf("result=%v err=%v posts=%d", result, err, dataPosts)
			}
			if name == "retention" {
				var gap *RetentionGapError
				if !errors.As(err, &gap) {
					t.Errorf("untyped retention gap: %v", err)
				}
			}
		})
	}
}
