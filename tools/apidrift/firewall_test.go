package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/rknightion/cf2otel/internal/cfapi"
)

// Exercise the canary's public probe boundary with a discovered ruleset ID,
// rather than only checking that a placeholder exists in the registry.
func TestFirewallRulesetProbeUsesDiscoveredID(t *testing.T) {
	for _, tc := range []struct {
		name, phase string
		omitRules   bool
		wantDetail  bool
	}{
		{"custom", "http_request_firewall_custom", false, true},
		{"managed_drift", "http_request_firewall_managed", true, true},
		{"unrelated", "http_request_transform", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := contract{
				Version: 1,
				GraphQL: []graphContract{{Scope: cfapi.ZoneScope, Dataset: "firewallEventsAdaptiveGroups", RequiredFields: []string{"count"}, MinimumDuration: 60, MinimumRetention: 60}},
				REST: []restContract{
					{Name: "firewall-rulesets", Scope: "zone", Path: "/zones/{zone}/rulesets", RequiredFields: []string{"id", "phase"}, AllowEmpty: true},
					{Name: "firewall-ruleset-detail", Scope: "zone", Path: "/zones/{zone}/rulesets/{id}", RequiredFields: []string{"id", "rules"}, Single: true, AllowEmpty: true},
				},
			}
			if err := validateContract(c); err != nil {
				t.Fatalf("canonical firewall contract rejected: %v", err)
			}
			api := &firewallProbeAPI{fakeAPI: fakeAPI{contract: c}, phase: tc.phase, omitRules: tc.omitRules}
			diffs := probe(context.Background(), api, c)
			if api.detailRead != tc.wantDetail {
				t.Fatalf("detail read=%v want=%v", api.detailRead, tc.wantDetail)
			}
			if tc.omitRules {
				if len(diffs) != 1 || !strings.Contains(diffs[0], "missing field rules") {
					t.Fatalf("rules shape drift not detected: %v", diffs)
				}
			} else if len(diffs) != 0 {
				t.Fatalf("unexpected drift: %v", diffs)
			}
		})
	}
}

type firewallProbeAPI struct {
	fakeAPI
	phase      string
	omitRules  bool
	detailRead bool
}

func (f *firewallProbeAPI) Get(_ context.Context, path string, query url.Values, out any) error {
	if len(query) != 0 {
		return fmt.Errorf("firewall ruleset request received list pagination")
	}
	switch path {
	case "/zones/secret-zone-id/rulesets":
		*out.(*[]map[string]any) = []map[string]any{{"id": "fixture-set", "phase": f.phase}}
	case "/zones/secret-zone-id/rulesets/fixture-set":
		f.detailRead = true
		row := map[string]any{"id": "fixture-set"}
		if !f.omitRules {
			row["rules"] = []any{}
		}
		*out.(*map[string]any) = row
	default:
		return fmt.Errorf("unexpected fixture route")
	}
	return nil
}
