package cfapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

func TestMain(m *testing.M) {
	// Timing witnesses use a fresh process with the production default budget.
	if os.Getenv("CF2OTEL_TEST_DEFAULT_CHILD") != "1" && os.Getenv("CF2OTEL_TEST_LIMITER_CHILD") != "1" && os.Getenv("CF2OTEL_TEST_LIFECYCLE_CHILD") != "1" {
		if err := ConfigureProcessRateLimit(config.RateLimitConfig{RequestsPerSecond: 10000, Burst: 1}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

func TestGraphQLMixedMalformedEnvelopes(t *testing.T) {
	for _, tc := range []struct {
		name, body, class string
	}{
		{"data_and_error", `{"data":{"viewer":{"zones":[{"settings":{}}]}},"errors":[{"message":"private"}]}`, "other"},
		{"missing_extensions", `{"errors":[{"message":"private"}]}`, "other"},
		{"absent_code", `{"errors":[{"extensions":{}}]}`, "other"},
		{"numeric_code", `{"errors":[{"extensions":{"code":123}}]}`, "other"},
		{"null_code", `{"errors":[{"extensions":{"code":null}}]}`, "other"},
		{"array_code", `{"errors":[{"extensions":{"code":["budget"]}}]}`, "other"},
		{"object_code", `{"errors":[{"extensions":{"code":{"value":"budget"}}}]}`, "other"},
		{"message_only_budget", `{"errors":[{"message":"budget"}]}`, "other"},
		{"scalar_extensions", `{"errors":[{"extensions":false}]}`, "other"},
		{"null_error", `{"errors":[null]}`, "other"},
		{"scalar_error", `{"errors":[false]}`, "schema"},
		{"errors_object", `{"errors":{"extensions":{"code":"budget"}}}`, "schema"},
		{"malformed_message", `{"errors":[{"message":123}]}`, "schema"},
		{"malformed_data", `{"data":{"viewer":{"zones":false}},"errors":[{"extensions":{"code":123}}]}`, "schema"},
		{"entitlement_with_data", `{"data":{"viewer":{"zones":[]}},"errors":[{"message":"not entitled to field 'fixture'"}]}`, "unentitled"},
		{"budget_with_data", `{"data":{"viewer":{"zones":[]}},"errors":[{"extensions":{"code":"budget"}}]}`, "budget"},
		{"budget_with_malformed_sibling", `{"data":false,"errors":[false,{"extensions":false},{"extensions":{"code":"budget"}}]}`, "budget"},
		{"budget_before_malformed_sibling", `{"errors":[{"extensions":{"code":"budget"}},{"extensions":123}]}`, "budget"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if runDefaultBudgetProcess(t) {
				return
			}
			var calls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			cfg := config.Default().Cloudflare
			if err := json.Unmarshal([]byte(`{"max_pause":1000000000}`), &cfg.RateLimit); err != nil {
				t.Fatal(err)
			}
			if err := ConfigureProcessRateLimit(cfg.RateLimit); err != nil {
				t.Fatal(err)
			}
			cfg.APIBase = server.URL
			api := NewObserved(cfg, func(string, string, int, time.Duration, bool) { calls++ })
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			_, err := api.DatasetSettings(ctx, ZoneScope, "fixture", "events")
			var budget *GraphQLBudgetError
			var denied *UnentitledError
			var shape *json.UnmarshalTypeError
			class := "other"
			switch {
			case errors.As(err, &budget):
				class = "budget"
			case errors.As(err, &denied):
				class = "unentitled"
			case errors.As(err, &shape):
				class = "schema"
			}
			if err == nil || class != tc.class || calls != 1 {
				t.Fatalf("class=%s want=%s err=%v calls=%d", class, tc.class, err, calls)
			}
			graphqlBudget.mu.Lock()
			pause := time.Until(graphqlBudget.pausedUntil)
			graphqlBudget.mu.Unlock()
			if tc.class == "budget" {
				if !errors.Is(err, context.DeadlineExceeded) || pause < 299*time.Second || pause > 300*time.Second {
					t.Fatalf("budget pause not preserved: err=%v remaining=%s", err, pause)
				}
			} else if pause > 0 {
				t.Fatalf("non-string/absent budget code paused GraphQL: %s", pause)
			}
		})
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
