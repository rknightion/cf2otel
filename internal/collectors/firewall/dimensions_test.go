package firewall

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

// Zone-discovery gauges are self-observation, not firewall event series, and
// therefore do not consume the firewall domain's per-window series budget.
func firewallEventPoints(out *telemetry.Buffer) []telemetry.BufferedMetric {
	var points []telemetry.BufferedMetric
	for _, point := range out.Metrics {
		if point.Name == semconv.MetricFirewallEvents {
			points = append(points, point)
		}
	}
	return points
}

// A finer grouping can saturate the endpoint's page limit. Split source windows,
// but apply the series cap only once after all leaves and zones succeed.
func TestRegisteredFirewallSplitsSaturatedMetrics(t *testing.T) {
	for _, irreducible := range []bool{false, true} {
		t.Run(fmt.Sprintf("irreducible_%v", irreducible), func(t *testing.T) {
			cfg := config.Default()
			cfg.Collectors["firewall.events"] = config.CollectorConfig{}
			cfg.Firewall.RuleDimensions = true
			cfg.Firewall.MaxMetricSeriesPerWindow = 1
			api := &firewallFakeAPI{
				zones: []cfapi.Zone{{ID: "fixture-zone-a", Name: "fixture-a"}, {ID: "fixture-zone-b", Name: "fixture-b"}},
				settings: map[string]cfapi.DatasetSettings{
					"fixture-zone-a/" + groupsDataset: settings(true, "count", "dimensions_ruleId"),
					"fixture-zone-b/" + groupsDataset: settings(true, "count", "dimensions_ruleId"),
				},
				query: func(r cfapi.GraphQLRequest) (any, error) {
					if r.To.Sub(r.From) > time.Minute || (irreducible && r.ScopeID == "fixture-zone-b") {
						return nil, &cfapi.SaturationError{Dataset: r.Dataset, From: r.From, To: r.To, Limit: 3}
					}
					return []map[string]any{
						{"count": 10, "dimensions": map[string]any{"ruleId": fmt.Sprintf("fixture-rule-a-%d", r.From.Unix())}},
						{"count": 20, "dimensions": map[string]any{"ruleId": fmt.Sprintf("fixture-rule-b-%d", r.From.Unix())}},
					}, nil
				},
			}
			registry := collector.NewRegistry()
			Register(collector.Deps{Config: &cfg, API: api, Registry: registry})
			c := registry.Entries()[0].Collector.(collector.WindowCollector)
			from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
			to := from.Add(4 * time.Minute)
			out := &telemetry.Buffer{}
			mark, err := c.CollectWindow(context.Background(), from, to, out)
			if irreducible {
				if err == nil || !mark.Equal(from) || len(out.Metrics) != 0 {
					t.Fatalf("incomplete window: mark=%s err=%v points=%d", mark, err, len(out.Metrics))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			points := firewallEventPoints(out)
			if !mark.Equal(to) || len(points) != 1 || points[0].Value != 240 {
				t.Fatalf("split window: mark=%s points=%v; want one capped point retaining 240 events", mark, points)
			}
		})
	}
}

// Exercise registration and the real read-only HTTP/GraphQL client, not a query mock.
func TestRegisteredHTTPFirewallCapConservesEvents(t *testing.T) {
	for _, tc := range []struct {
		name          string
		cap           int
		enabled       bool
		wantPoints    int
		lookupFailure bool
		unavailable   bool
		maxFields     int
	}{
		{"bounded", 3, true, 3, false, false, 20}, {"cap_one", 1, true, 1, false, false, 20}, {"default", 3, false, 2, false, false, 20},
		{"lookup_failure", 3, true, 3, true, false, 20}, {"unadvertised", 3, true, 2, false, true, 20},
		{"no_enrichment_budget", 3, true, 2, false, false, 3}, {"rule_only_budget", 3, true, 3, false, false, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			canEnrich := tc.enabled && !tc.unavailable && tc.maxFields > 3
			var lookups int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/zones" {
					fmt.Fprint(w, `{"success":true,"result":[{"id":"fixture-zone","name":"fixture"}],"result_info":{"page":1,"total_pages":1}}`)
					return
				}
				if r.URL.Path == "/zones/fixture-zone/rulesets" {
					lookups++
					if tc.lookupFailure {
						w.WriteHeader(http.StatusForbidden)
						fmt.Fprint(w, `{"success":false}`)
						return
					}
					fmt.Fprint(w, `{"success":true,"result":[{"id":"fixture-set","phase":"http_request_firewall_custom"}]}`)
					return
				}
				if r.URL.Path == "/zones/fixture-zone/rulesets/fixture-set" {
					fmt.Fprint(w, `{"success":true,"result":{"rules":[{"id":"rule-a","description":"fixture description"}]}}`)
					return
				}
				if r.URL.Path != "/graphql" {
					t.Errorf("unexpected route %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				var req struct {
					Query string `json:"query"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					return
				}
				if strings.Contains(req.Query, "settings{") {
					if tc.unavailable {
						fmt.Fprint(w, `{"data":{"viewer":{"zones":[{"settings":{"firewallEventsAdaptiveGroups":{"enabled":true,"availableFields":["count","dimensions_action","dimensions_source"],"maxNumberOfFields":20,"maxDuration":3600,"notOlderThan":86400,"maxPageSize":10000}}}]}}}`)
						return
					}
					fmt.Fprintf(w, `{"data":{"viewer":{"zones":[{"settings":{"firewallEventsAdaptiveGroups":{"enabled":true,"availableFields":["count","dimensions_action","dimensions_source","dimensions_ruleId","dimensions_clientRequestHTTPHost","dimensions_clientCountryName"],"maxNumberOfFields":%d,"maxDuration":3600,"notOlderThan":86400,"maxPageSize":10000}}}]}}}`, tc.maxFields)
					return
				}
				enriched := strings.Contains(req.Query, "ruleId")
				if tc.unavailable && (enriched || strings.Contains(req.Query, "clientRequestHTTPHost") || strings.Contains(req.Query, "clientCountryName")) {
					t.Error("queried unadvertised dimensions")
				}
				rows := []map[string]any{{"count": 2, "dimensions": map[string]any{"action": "allow", "source": "fixture"}}}
				if enriched {
					for i := 0; i < 5; i++ {
						rows = append(rows, map[string]any{"count": i + 1, "dimensions": map[string]any{"action": "block", "source": "fixture", "ruleId": fmt.Sprintf("rule-%c", 'a'+i), "clientRequestHTTPHost": fmt.Sprintf("host-%d", i), "clientCountryName": "ZZ"}})
					}
				} else {
					rows = append(rows, map[string]any{"count": 15, "dimensions": map[string]any{"action": "block", "source": "fixture"}})
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"zones": []any{map[string]any{groupsDataset: rows}}}}})
			}))
			defer server.Close()
			cfg := config.Default()
			cfg.Cloudflare.APIBase = server.URL
			cfg.Firewall.RuleDimensions = tc.enabled
			cfg.Firewall.MaxMetricSeriesPerWindow = tc.cap
			cfg.Collectors["firewall.events"] = config.CollectorConfig{}
			registry := collector.NewRegistry()
			Register(collector.Deps{Config: &cfg, API: cfapi.New(cfg.Cloudflare), Registry: registry})
			c := registry.Entries()[0].Collector.(collector.WindowCollector)
			from := time.Now().UTC().Truncate(time.Second).Add(-10 * time.Minute)
			to := from.Add(time.Minute)
			out := &telemetry.Buffer{}
			mark, err := c.CollectWindow(context.Background(), from, to, out)
			if err != nil {
				t.Fatal(err)
			}
			if !mark.Equal(to) {
				t.Fatal("window did not advance")
			}
			points := firewallEventPoints(out)
			if len(points) != tc.wantPoints {
				t.Fatalf("points=%d want=%d under total cap=%d", len(points), tc.wantPoints, tc.cap)
			}
			var total, remainder float64
			var preserved, enriched bool
			for _, m := range points {
				total += m.Value
				a := attrMap(m.Attrs)
				if a[semconv.AttrFirewallRuleID] == "other" {
					remainder += m.Value
					for _, key := range []string{semconv.AttrFirewallRuleID, semconv.AttrFirewallRuleDescription, semconv.AttrFirewallHost, semconv.AttrFirewallCountry} {
						if a[key] != "other" {
							t.Fatalf("remainder %s=%q", key, a[key])
						}
					}
				} else if a[semconv.AttrFirewallAction] == "allow" {
					preserved = m.Value == 2 && len(a) == 3
				} else if a[semconv.AttrFirewallRuleID] == "rule-a" {
					wantDescription := "fixture description"
					if tc.lookupFailure {
						wantDescription = ""
					}
					wantHost, wantCountry := "host-0", "ZZ"
					if tc.maxFields == 4 {
						wantHost, wantCountry = "", ""
					}
					enriched = a[semconv.AttrFirewallRuleDescription] == wantDescription && a[semconv.AttrFirewallHost] == wantHost && a[semconv.AttrFirewallCountry] == wantCountry
				}
			}
			if total != 17 {
				t.Fatalf("events=%g want=17", total)
			}
			if canEnrich {
				want := float64(14)
				if tc.cap == 1 {
					want = 17
				}
				if remainder != want {
					t.Fatalf("discarded=%g want=%g", remainder, want)
				}
				if tc.cap > 1 && (!preserved || !enriched) {
					t.Fatalf("legacy preserved=%v rule enriched=%v", preserved, enriched)
				}
			} else if !preserved {
				t.Fatal("default low-cardinality point changed")
			}
			if _, err := c.CollectWindow(context.Background(), from, to, &telemetry.Buffer{}); err != nil {
				t.Fatal(err)
			}
			wantLookups := 0
			if canEnrich {
				wantLookups = 1
			}
			if lookups != wantLookups {
				t.Fatalf("ruleset reads=%d want=%d across two windows", lookups, wantLookups)
			}
			if wantLookups > 0 {
				m := c.(*metrics)
				future := time.Now().Add(time.Hour + time.Minute)
				m.now = func() time.Time { return future }
				if _, err := c.CollectWindow(context.Background(), from, to, &telemetry.Buffer{}); err != nil {
					t.Fatal(err)
				}
				if lookups != 2 {
					t.Fatalf("expired cache reads=%d want=2", lookups)
				}
			}
		})
	}
}
