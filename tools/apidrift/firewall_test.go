package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/config"
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

// The first zone lacks one dimension; only the second advertises the complete
// live-accepted selection. Exercise probe, not a separate selection helper.
func TestFirewallGroupsCanarySelectsEligibleZone(t *testing.T) {
	fields := []string{"count", "dimensions_action", "dimensions_source", "dimensions_ruleId", "dimensions_clientRequestHTTPHost", "dimensions_clientCountryName"}
	for _, tc := range []struct {
		name                string
		duration, retention int64
		maxFields, page     int
		wantQuery           bool
	}{
		{"live_limits", 86400, 259200, 30, 10000, true},
		{"short_window", 60, 120, 6, 1, true},
		{"insufficient_fields", 86400, 259200, 5, 10000, false},
		{"zero_page", 86400, 259200, 30, 0, false},
		{"zero_duration", 0, 259200, 30, 10000, false},
		{"zero_retention", 86400, 0, 30, 10000, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := contract{GraphQL: []graphContract{{Scope: cfapi.ZoneScope, Dataset: "firewallEventsAdaptiveGroups", RequiredFields: []string{"count"}, AllowDisabled: true}}}
			api := &firewallGroupsAPI{fakeAPI: fakeAPI{contract: c}, fields: fields, settings: cfapi.DatasetSettings{Enabled: true, MaxDuration: tc.duration, NotOlderThan: tc.retention, MaxNumberOfFields: tc.maxFields, MaxPageSize: tc.page}}
			before := time.Now().UTC()
			_, output := captureFirewallProbe(t, api, c)
			assertFirewallOutputSanitized(t, output)
			if !strings.Contains(output, "populated value shape unproven") {
				t.Fatalf("missing shape qualification: %q", output)
			}
			if !tc.wantQuery && (!strings.Contains(output, "unprobed") || strings.Contains(output, "selection accepted")) {
				t.Fatalf("ineligible selection claimed acceptance: %q", output)
			}
			if (len(api.requests) != 0) != tc.wantQuery {
				t.Fatalf("dataset query count=%d wantQuery=%v", len(api.requests), tc.wantQuery)
			}
			if !tc.wantQuery {
				return
			}
			if len(api.requests) != 1 {
				t.Fatalf("expected exactly one dataset query: %d", len(api.requests))
			}
			r := api.requests[0]
			want := []string{"count", "dimensions.action", "dimensions.source", "dimensions.ruleId", "dimensions.clientRequestHTTPHost", "dimensions.clientCountryName"}
			if r.Scope != cfapi.ZoneScope || r.ScopeID != "fixture-eligible-zone" || r.Dataset != "firewallEventsAdaptiveGroups" || r.Limit != 1 || !reflect.DeepEqual(r.WantedFields, want) {
				t.Fatalf("unexpected selection: %+v", r)
			}
			window := r.To.Sub(r.From)
			if window <= 0 || window > 5*time.Minute || window > time.Duration(tc.duration)*time.Second || window >= time.Duration(tc.retention)*time.Second || r.To.Before(before.Add(-time.Second)) || r.To.After(time.Now()) {
				t.Fatalf("query outside advertised recent limits: %+v", r)
			}
		})
	}
}

func TestFirewallGroupsCanaryThroughHTTP(t *testing.T) {
	for _, result := range []string{"empty", "saturated", "rejected", "entitlement_refresh"} {
		t.Run(result, func(t *testing.T) {
			calls, eligibleSettingsCalls := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Query string `json:"query"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				if strings.Contains(body.Query, "settings") {
					fields := []string{"count", "dimensions_action", "dimensions_source", "dimensions_ruleId", "dimensions_clientRequestHTTPHost", "dimensions_clientCountryName"}
					if strings.Contains(body.Query, "fixture-ineligible-zone") {
						fields = fields[:5]
					} else {
						eligibleSettingsCalls++
						if result == "entitlement_refresh" && eligibleSettingsCalls > 1 {
							// Query's old renegotiation would lose ruleId and retry.
							fields = append(fields[:3], fields[4:]...)
						}
					}
					encoded, _ := json.Marshal(fields)
					fmt.Fprintf(w, `{"data":{"viewer":{"zones":[{"settings":{"firewallEventsAdaptiveGroups":{"enabled":true,"availableFields":%s,"maxNumberOfFields":30,"maxDuration":86400,"notOlderThan":259200,"maxPageSize":10000}}}]}}}`, encoded)
					return
				}
				calls++
				for _, fragment := range []string{`zoneTag:"fixture-eligible-zone"`, "firewall_canary:firewallEventsAdaptiveGroups(limit:1", "count", "dimensions{action clientCountryName clientRequestHTTPHost ruleId source}", "datetime_geq", "datetime_lt"} {
					if !strings.Contains(body.Query, fragment) {
						t.Errorf("selection missing %q: %s", fragment, body.Query)
					}
				}
				alias := "firewall_canary"
				switch result {
				case "empty":
					fmt.Fprintf(w, `{"data":{"viewer":{"zones":[{%q:[]}]}}}`, alias)
				case "saturated":
					fmt.Fprintf(w, `{"data":{"viewer":{"zones":[{%q:[{"count":1,"dimensions":{"action":"fixture-private-value"}}]}]}}}`, alias)
				case "entitlement_refresh":
					if calls == 1 {
						fmt.Fprint(w, `{"errors":[{"message":"not entitled to field 'ruleId'"}]}`)
					} else {
						fmt.Fprintf(w, `{"data":{"viewer":{"zones":[{%q:[]}]}}}`, alias)
					}
				case "rejected":
					fmt.Fprint(w, `{"errors":[{"message":"fixture-private-error"}]}`)
				}
			}))
			defer srv.Close()
			c := contract{GraphQL: []graphContract{{Scope: cfapi.ZoneScope, Dataset: "firewallEventsAdaptiveGroups", RequiredFields: []string{"count"}, AllowDisabled: true}}}
			api := &firewallHTTPAPI{firewallGroupsAPI: firewallGroupsAPI{fakeAPI: fakeAPI{contract: c}}, client: cfapi.New(config.CloudflareConfig{APIBase: srv.URL, APIToken: "fixture", Timeout: time.Second, MaxResponseBytes: 4096})}
			diffs, output := captureFirewallProbe(t, api, c)
			assertFirewallOutputSanitized(t, output+strings.Join(diffs, "\n"))
			if result == "rejected" || result == "entitlement_refresh" {
				if len(diffs) != 1 || !strings.Contains(diffs[0], "selection probe failed") || strings.Contains(output, "selection accepted") {
					t.Errorf("rejection falsely accepted: diffs=%v output=%q", diffs, output)
				}
			} else if !strings.Contains(output, "selection accepted") || !strings.Contains(output, "populated value shape unproven") {
				t.Errorf("missing honest acceptance qualification: %q", output)
			}
			if result == "empty" && !strings.Contains(output, "zero rows") {
				t.Errorf("empty response evidence omitted zero rows: %q", output)
			}
			if result == "saturated" && !strings.Contains(output, "limit reached") {
				t.Errorf("saturated response evidence omitted limit reached: %q", output)
			}
			if result == "entitlement_refresh" && eligibleSettingsCalls != 1 {
				t.Errorf("entitlement rejection caused settings refresh: %d calls", eligibleSettingsCalls)
			}
			if calls != 1 {
				t.Fatalf("dataset query calls=%d want 1", calls)
			}
			if result == "rejected" || result == "entitlement_refresh" {
				if len(diffs) != 1 || !strings.Contains(diffs[0], "selection probe failed") || strings.Contains(diffs[0], "fixture-private-error") {
					t.Fatalf("unsafe or missing failure: %v", diffs)
				}
			} else if len(diffs) != 0 {
				t.Fatalf("accepted selection marked drift: %v", diffs)
			}
		})
	}
}

func captureFirewallProbe(t *testing.T, api probeAPI, c contract) ([]string, string) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "firewall-output-*")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	original := os.Stdout
	os.Stdout = file
	defer func() { os.Stdout = original }()
	diffs := probe(context.Background(), api, c)
	output, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	return diffs, string(output)
}

func assertFirewallOutputSanitized(t *testing.T, output string) {
	t.Helper()
	for _, private := range []string{"fixture-ineligible-zone", "fixture-eligible-zone", "secret-account-id", "fixture-private-error", "fixture-private-value", "not entitled to field"} {
		if strings.Contains(output, private) {
			t.Errorf("private response or identifier in output: %q", output)
		}
	}
}

type firewallHTTPAPI struct {
	firewallGroupsAPI
	client *cfapi.HTTPClient
}

func (f firewallHTTPAPI) DatasetSettings(ctx context.Context, scope cfapi.Scope, id, dataset string) (cfapi.DatasetSettings, error) {
	return f.client.DatasetSettings(ctx, scope, id, dataset)
}

func (f firewallHTTPAPI) QueryBatch(ctx context.Context, selections []cfapi.GraphQLBatchSelection) (map[string]json.RawMessage, error) {
	return f.client.QueryBatch(ctx, selections)
}

type firewallGroupsAPI struct {
	fakeAPI
	fields   []string
	settings cfapi.DatasetSettings
	requests []cfapi.GraphQLRequest
}

func (f *firewallGroupsAPI) Zones(context.Context) ([]cfapi.Zone, error) {
	return []cfapi.Zone{{ID: "fixture-ineligible-zone"}, {ID: "fixture-eligible-zone"}}, nil
}

func (f *firewallGroupsAPI) DatasetSettings(_ context.Context, _ cfapi.Scope, id, _ string) (cfapi.DatasetSettings, error) {
	s := f.settings
	s.AvailableFields = append([]string(nil), f.fields...)
	if id == "fixture-ineligible-zone" {
		s.AvailableFields = s.AvailableFields[:len(s.AvailableFields)-1]
	}
	return s, nil
}

func (f *firewallGroupsAPI) QueryBatch(_ context.Context, selections []cfapi.GraphQLBatchSelection) (map[string]json.RawMessage, error) {
	if len(selections) != 1 || selections[0].Alias != "firewall_canary" {
		return nil, fmt.Errorf("expected one strict firewall selection")
	}
	f.requests = append(f.requests, selections[0].Request)
	return map[string]json.RawMessage{"firewall_canary": json.RawMessage(`[]`)}, nil
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
