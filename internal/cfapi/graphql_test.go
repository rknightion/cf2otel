package cfapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

func TestGraphQLRetentionGapCarriesFloor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	if gap.Dataset != "httpRequestsAdaptive" || gap.Floor.Before(now.Add(-61*time.Second)) || gap.Floor.After(now.Add(-59*time.Second)) {
		t.Fatalf("gap=%+v", gap)
	}
}
