package cfapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

func TestGraphQLSaturationIsTypedAndKeepsItsMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&q)
		if strings.Contains(q.Query, "settings") {
			_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[{"settings":{"events":{"enabled":true,"availableFields":["a"],"maxPageSize":1,"maxDuration":60}}}]}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[{"events":[{"a":1}]}]}}}`))
	}))
	defer srv.Close()
	c := New(config.CloudflareConfig{APIBase: srv.URL, APIToken: "test", Timeout: time.Second, MaxResponseBytes: 4096})
	to := time.Now().Truncate(time.Second)
	from := to.Add(-time.Minute)
	var out []map[string]any
	err := c.Query(context.Background(), GraphQLRequest{Scope: ZoneScope, ScopeID: "zone", Dataset: "events", WantedFields: []string{"a"}, From: from, To: to, Limit: 1}, &out)

	var sat *SaturationError
	if !errors.As(fmt.Errorf("wrapped: %w", err), &sat) {
		t.Fatalf("saturation is not a *SaturationError: %T %v", err, err)
	}
	if sat.Dataset != "events" || sat.Limit != 1 || !sat.From.Equal(from) || !sat.To.Equal(to) {
		t.Fatalf("saturation fields = %+v, want dataset events, limit 1, window %s..%s", sat, from, to)
	}
	// Operators and log queries key on this text; the typed error must not change it.
	want := fmt.Sprintf("GraphQL dataset events window %s..%s saturated limit 1", from.Format(time.RFC3339), to.Format(time.RFC3339))
	if err.Error() != want {
		t.Fatalf("message = %q, want %q", err.Error(), want)
	}
}

func TestAsSaturationRecognisesUntypedCanonicalMessages(t *testing.T) {
	from := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute)
	for _, tc := range []struct {
		name string
		err  error
		ok   bool
		want SaturationError
	}{
		{"typed", fmt.Errorf("query: %w", &SaturationError{Dataset: "kvOperationsAdaptiveGroups", From: from, To: to, Limit: 7}), true, SaturationError{Dataset: "kvOperationsAdaptiveGroups", From: from, To: to, Limit: 7}},
		{"untyped with window", fmt.Errorf("GraphQL dataset d1AnalyticsAdaptiveGroups window %s..%s saturated limit 9", from.Format(time.RFC3339), to.Format(time.RFC3339)), true, SaturationError{Dataset: "d1AnalyticsAdaptiveGroups", From: from, To: to, Limit: 9}},
		{"untyped without window", errors.New("GraphQL dataset emailRoutingAdaptiveGroups window saturated limit 3"), true, SaturationError{Dataset: "emailRoutingAdaptiveGroups", Limit: 3}},
		{"other GraphQL error", errors.New("graphql query failed"), false, SaturationError{}},
		{"nil", nil, false, SaturationError{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sat, ok := AsSaturation(tc.err)
			if ok != tc.ok {
				t.Fatalf("AsSaturation ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if sat.Dataset != tc.want.Dataset || sat.Limit != tc.want.Limit || !sat.From.Equal(tc.want.From) || !sat.To.Equal(tc.want.To) {
				t.Fatalf("AsSaturation = %+v, want %+v", *sat, tc.want)
			}
		})
	}
}
