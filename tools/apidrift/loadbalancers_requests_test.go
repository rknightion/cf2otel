package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/config"
)

type lbSettingsProbe struct {
	fakeAPI
	client *cfapi.HTTPClient
}

func (a lbSettingsProbe) DatasetSettings(ctx context.Context, scope cfapi.Scope, id, dataset string) (cfapi.DatasetSettings, error) {
	if dataset == "loadBalancingRequestsAdaptiveGroups" {
		return a.client.DatasetSettings(ctx, scope, id, dataset)
	}
	return a.fakeAPI.DatasetSettings(ctx, scope, id, dataset)
}

func TestLoadBalancerRequestsContractThroughHTTP(t *testing.T) {
	c, err := loadContract("../../spec/cloudflare/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var entry *graphContract
	for i := range c.GraphQL {
		if c.GraphQL[i].Dataset == "loadBalancingRequestsAdaptiveGroups" {
			entry = &c.GraphQL[i]
		}
	}
	if entry == nil {
		t.Fatal("documented pool request dataset lacks canary contract")
	}
	if entry.Scope != cfapi.ZoneScope || !entry.AllowDisabled || len(entry.RequiredFields) != 2 || !hasAvailableField(entry.RequiredFields, "count") || !hasAvailableField(entry.RequiredFields, "dimensions.selectedPoolName") {
		t.Fatalf("invalid entitlement-safe pool traffic contract: %+v", entry)
	}
	for _, mode := range []string{"enabled", "absent", "disabled", "field-drift", "limits-drift", "auth"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/graphql" {
					t.Error("unexpected canary request")
				}
				var payload struct {
					Query string `json:"query"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if !strings.Contains(payload.Query, "settings{loadBalancingRequestsAdaptiveGroups{") {
					t.Error("canary must observe actual dataset settings")
				}
				if mode == "auth" {
					http.Error(w, "fixture error", http.StatusForbidden)
					return
				}
				settings := map[string]any{}
				if mode != "absent" {
					fields := []string{"count", "dimensions_selectedPoolName"}
					if mode == "field-drift" {
						fields = []string{"count"}
					}
					duration := entry.MinimumDuration
					if mode == "limits-drift" {
						duration = 1
					}
					settings[entry.Dataset] = map[string]any{"enabled": mode != "disabled", "availableFields": fields, "maxNumberOfFields": 2, "maxDuration": duration, "notOlderThan": entry.MinimumRetention, "maxPageSize": 100}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"zones": []any{map[string]any{"settings": settings}}}}})
			}))
			defer server.Close()
			api := lbSettingsProbe{fakeAPI: fakeAPI{contract: c}, client: cfapi.New(config.CloudflareConfig{APIBase: server.URL})}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			diffs := probe(ctx, api, c)
			if calls != 1 {
				t.Fatalf("expected one settings canary call, got %d", calls)
			}
			switch mode {
			case "enabled", "absent", "disabled":
				if len(diffs) != 0 {
					t.Fatal("optional entitlement unexpectedly fails canary", diffs)
				}
			case "field-drift":
				if !strings.Contains(strings.Join(diffs, "\n"), "missing field dimensions_selectedPoolName") {
					t.Fatal("enabled field drift hidden", diffs)
				}
			case "limits-drift":
				if !strings.Contains(strings.Join(diffs, "\n"), "below minimum") {
					t.Fatal("enabled limit drift hidden", diffs)
				}
			case "auth":
				if !strings.Contains(strings.Join(diffs, "\n"), "settings request failed: http_4xx") {
					t.Fatal("auth wrongly suppressed", diffs)
				}
			}
		})
	}
}
