package httpreq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// Observe the collector's public batch request before the real client applies
// its own page clamp; otherwise that second guard masks a collector regression.
type highPageLimitClient struct {
	*cfapi.HTTPClient
	t *testing.T
}

func (c highPageLimitClient) QueryBatch(ctx context.Context, selections []cfapi.GraphQLBatchSelection) (map[string]json.RawMessage, error) {
	for _, selection := range selections {
		if strings.HasPrefix(selection.Alias, "high_") && selection.Request.Limit != 5 {
			c.t.Errorf("collector batch page limit=%d want advertised maxPageSize=5", selection.Request.Limit)
		}
	}
	return c.HTTPClient.QueryBatch(ctx, selections)
}

// Catch loss of group counts, leaked route templates, and partial SDK commits.
func TestRegisteredHighCardinalityHTTP(t *testing.T) {
	for _, mode := range []string{"enabled", "mixed-zones", "late-zone-failure", "default-off", "host-filter", "asn-unavailable", "field-budget", "duration-split", "smaller-page", "smaller-page-saturated", "invalid-string", "invalid-asn-description", "invalid-count", "invalid-status", "invalid-path", "malformed-path", "null", "missing-alias", "upstream-error", "saturated", "healthy-path-saturation", "global-cap", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			cfg := config.Default()
			cfg.Cloudflare.AccountID = "account-fixture"
			cfg.HTTP.MetricsScope = "all"
			cfg.HTTP.Breakdowns = []string{"colo", "asn", "error_path"}
			cfg.HTTP.HighCardinalityLimit = 1
			cfg.HTTP.ErrorPathRoutes = map[string]string{"item": "/items/:number/:uuid/:hex"}
			uuid := strings.Repeat("a", 8) + "-" + strings.Repeat("b", 4) + "-" + strings.Repeat("c", 4) + "-" + strings.Repeat("d", 4) + "-" + strings.Repeat("e", 12)
			pathSuffix := "/" + uuid + "/" + strings.Repeat("a", 8)
			if mode == "default-off" {
				cfg.HTTP.Breakdowns = config.Default().HTTP.Breakdowns
			}
			if mode == "host-filter" {
				cfg.HTTP.HighCardinalityHosts = []string{"host-alpha"}
			}
			if mode == "global-cap" {
				cfg.HTTP.MaxMetricSeriesPerWindow = 3
			}
			highCalls := 0
			aliases := regexp.MustCompile(`(\w+):httpRequestsAdaptiveGroups\(limit:[0-9]+,filter:(\{[^)]*\})\)`)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					zones := []cfapi.Zone{zoneForAccount("zone-fixture", "zone-alpha", "account-fixture")}
					if mode == "mixed-zones" || mode == "late-zone-failure" {
						zones = append(zones, zoneForAccount("zone-free", "zone-beta", "account-fixture"))
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": zones})
					return
				}
				var body struct {
					Query string `json:"query"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				q := body.Query
				node := map[string]any{}
				if strings.Contains(q, "settings{") {
					fields := []string{"count", "dimensions_clientRequestHTTPHost", "dimensions_edgeResponseStatus", "dimensions_cacheStatus", "sum_edgeResponseBytes", "sum_visits", "dimensions_coloCode", "dimensions_clientRequestPath"}
					if mode != "asn-unavailable" && (mode != "mixed-zones" || !strings.Contains(q, "zone-free")) {
						fields = append(fields, "dimensions_clientAsn", "dimensions_clientASNDescription")
					}
					budget, duration, pageSize := 40, int64(2592000), 10000
					if mode == "field-budget" {
						budget = 2
					}
					if mode == "duration-split" || strings.HasPrefix(mode, "smaller-page") {
						duration = 150
					}
					if strings.HasPrefix(mode, "smaller-page") {
						pageSize = 5
					}
					node["settings"] = map[string]any{"httpRequestsAdaptiveGroups": cfapi.DatasetSettings{Enabled: true, AvailableFields: fields, MaxNumberOfFields: budget, MaxDuration: duration, MaxPageSize: pageSize}}
				} else {
					matches := aliases.FindAllStringSubmatch(q, -1)
					if len(matches) == 0 {
						matches = [][]string{{"", "httpRequestsAdaptiveGroups", q}}
					}
					for _, match := range matches {
						alias := match[1]
						filter := match[2]
						hasErrorRange := strings.Contains(filter, "edgeResponseStatus_geq:400") && strings.Contains(filter, "edgeResponseStatus_leq:599")
						if !strings.HasPrefix(alias, "high_") {
							if strings.Contains(filter, "edgeResponseStatus_geq") || strings.Contains(filter, "edgeResponseStatus_leq") {
								t.Error("legacy selection acquired error-only status policy")
							}
							node[alias] = []any{map[string]any{"count": 7, "dimensions": map[string]any{"clientRequestHTTPHost": "host-alpha", "edgeResponseStatus": 200, "cacheStatus": "hit"}, "sum": map[string]any{"edgeResponseBytes": 70, "visits": 3}}}
							continue
						}
						highCalls++
						if !strings.Contains(filter, `requestSource:"eyeball"`) || strings.Contains(filter, "_in:") {
							t.Error("high-cardinality selection changed source policy")
						}
						if alias == "high_error_route" {
							if !hasErrorRange && mode != "healthy-path-saturation" {
								t.Error("error route selection lacks verified integer 400..599 predicate")
							}
						} else if strings.Contains(filter, "edgeResponseStatus_geq") || strings.Contains(filter, "edgeResponseStatus_leq") {
							t.Error("colo or ASN selection acquired error-only status policy")
						}
						if mode == "upstream-error" {
							_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": "fixture failure"}}})
							return
						}
						if mode == "missing-alias" {
							continue
						}
						if mode == "null" {
							node[alias] = nil
							continue
						}
						rows := []map[string]any{}
						for i, count := range []int{2, 3, 7} {
							dims := map[string]any{"clientRequestHTTPHost": "HOST-alpha", "coloCode": []string{"colo-alpha", "colo-alpha", "colo-beta"}[i], "clientAsn": []string{"asn-alpha", "asn-alpha", "asn-beta"}[i], "clientASNDescription": []string{"org-alpha", "org-alpha", "org-beta"}[i], "clientRequestPath": []string{"/items/123" + pathSuffix + "?opaque", "/items/456" + pathSuffix + "#opaque", "/unmapped/789"}[i], "edgeResponseStatus": 404}
							if i == 2 {
								dims["clientRequestHTTPHost"] = "host-beta"
								dims["edgeResponseStatus"] = 503
							}
							row := map[string]any{"count": count, "dimensions": dims}
							if mode == "invalid-string" {
								dims["coloCode"] = nil
							}
							if mode == "invalid-asn-description" {
								dims["clientASNDescription"] = nil
							}
							if mode == "invalid-path" {
								dims["clientRequestPath"] = 123
							}
							if mode == "malformed-path" {
								dims["clientRequestPath"] = "/items/%zz"
							}
							if mode == "invalid-count" || (mode == "late-zone-failure" && strings.Contains(q, "zone-free")) {
								row["count"] = -1
							}
							if mode == "invalid-status" {
								dims["edgeResponseStatus"] = 404.5
							}
							rows = append(rows, row)
						}
						if alias == "high_error_route" {
							rows = append(rows, map[string]any{"count": 11, "dimensions": map[string]any{"clientRequestPath": "/items/123" + pathSuffix, "edgeResponseStatus": 200, "clientRequestHTTPHost": "host-alpha"}})
						}
						// Model the source grouping boundary: healthy path groups fill
						// the query limit unless the upstream error range excludes them.
						// Retain the out-of-range row above to exercise local defense too.
						if mode == "healthy-path-saturation" && alias == "high_error_route" && !hasErrorRange {
							rows = nil
							for i := 0; i < 10000; i++ {
								rows = append(rows, map[string]any{"count": 1, "dimensions": map[string]any{"clientRequestPath": "/healthy/" + time.Unix(int64(i), 0).Format("150405"), "edgeResponseStatus": 200}})
							}
						}
						if mode == "saturated" {
							for len(rows) < 10000 {
								rows = append(rows, rows[0])
							}
						}
						// Saturate only after one successful duration chunk to prove
						// that the full window, not just this page, remains atomic.
						if mode == "smaller-page-saturated" && highCalls > len(highSpecs) {
							for len(rows) < 5 {
								rows = append(rows, rows[0])
							}
						}
						node[alias] = rows
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"zones": []any{node}}}})
			}))
			defer srv.Close()
			cfg.Cloudflare.APIBase = srv.URL
			cfg.Cloudflare.APIToken = "fixture"
			reg := collector.NewRegistry()
			var api cfapi.Client = cfapi.New(cfg.Cloudflare)
			if strings.HasPrefix(mode, "smaller-page") {
				api = highPageLimitClient{HTTPClient: cfapi.New(cfg.Cloudflare), t: t}
			}
			Register(collector.Deps{Config: &cfg, API: api, Registry: reg})
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			defer func() { _ = provider.Shutdown(context.Background()) }()
			emitter := telemetry.NewEmitter(provider.Meter("fixture"), nil, nil)
			store, err := collector.NewFileStore(t.TempDir() + "/checkpoints.json")
			if err != nil {
				t.Fatal(err)
			}
			from := time.Now().UTC().Truncate(time.Minute).Add(-7 * time.Minute)
			if err := store.Set("httpreq.metrics", from); err != nil {
				t.Fatal(err)
			}
			scheduler := collector.NewScheduler(reg, emitter, store)
			scheduler.Now = func() time.Time { return from.Add(7 * time.Minute) }
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			found := false
			for _, entry := range reg.Entries() {
				if entry.Collector.Name() == "httpreq.metrics" {
					found = true
					err = scheduler.RunOnce(ctx, entry)
				}
			}
			if !found {
				t.Fatal("metrics collector not registered")
			}
			wantErr := strings.HasPrefix(mode, "invalid-") || mode == "null" || mode == "missing-alias" || mode == "upstream-error" || mode == "saturated" || mode == "smaller-page-saturated" || mode == "global-cap" || mode == "field-budget" || mode == "canceled" || mode == "late-zone-failure"
			if (err != nil) != wantErr {
				t.Fatalf("window error=%v wantErr=%t", err, wantErr)
			}
			var data metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &data); err != nil {
				t.Fatal(err)
			}
			mark, _ := store.Get("httpreq.metrics")
			multiplier := float64(1)
			if mode == "duration-split" || strings.HasPrefix(mode, "smaller-page") {
				multiplier = 2
			}
			totals, sizes := map[string]float64{}, map[string]int{}
			allowed := map[string]bool{semconv.AttrHTTPZone: true, semconv.AttrHTTPColo: true, semconv.AttrHTTPClientASN: true, semconv.AttrHTTPClientASNDescription: true, semconv.AttrHTTPRouteName: true, semconv.AttrHTTPStatusClass: true, semconv.AttrHTTPBreakdownRemainder: true}
			for _, scope := range data.ScopeMetrics {
				for _, m := range scope.Metrics {
					sum, ok := m.Data.(metricdata.Sum[float64])
					if !ok {
						continue
					}
					high := m.Name == semconv.MetricHTTPRequestsByColo || m.Name == semconv.MetricHTTPRequestsByASN || m.Name == semconv.MetricHTTPErrorsByRoute
					for _, p := range sum.DataPoints {
						totals[m.Name] += p.Value
						sizes[m.Name]++
						if high {
							if m.Unit != "{request}" || !sum.IsMonotonic {
								t.Fatal("wrong counter contract")
							}
							for _, a := range p.Attributes.ToSlice() {
								if !allowed[string(a.Key)] || a.Value.Type() != attribute.STRING || strings.ContainsAny(a.Value.AsString(), "/?#") {
									t.Fatal("unsafe new metric attribute")
								}
							}
							remainder, ok := p.Attributes.Value(attribute.Key(semconv.AttrHTTPBreakdownRemainder))
							if !ok || (remainder.AsString() != "false" && remainder.AsString() != "true") {
								t.Fatal("remainder flag missing or invalid")
							}
							if mode != "malformed-path" {
								wantPoint := 5 * multiplier
								if remainder.AsString() == "true" {
									wantPoint = 7 * multiplier
								}
								if p.Value != wantPoint {
									t.Fatalf("lexical selection or merged group count wrong: %g want %g", p.Value, wantPoint)
								}
							}
							if m.Name == semconv.MetricHTTPErrorsByRoute {
								name, _ := p.Attributes.Value(attribute.Key(semconv.AttrHTTPRouteName))
								if name.AsString() != "item" && name.AsString() != "other" {
									t.Fatal("route label is not a configured safe name")
								}
							}
						}
					}
				}
			}
			if wantErr {
				if mode == "field-budget" && highCalls != 1 {
					t.Fatal("unavailable ASN/route feature exceeded field budget")
				}
				if !mark.Equal(from) || totals[semconv.MetricHTTPRequests] != 0 || sizes[semconv.MetricHTTPRequestsByColo] != 0 || sizes[semconv.MetricHTTPRequestsByASN] != 0 || sizes[semconv.MetricHTTPErrorsByRoute] != 0 {
					t.Fatal("failed window advanced or exported metric points")
				}
				return
			}
			zoneMultiplier := multiplier
			if mode == "mixed-zones" {
				zoneMultiplier = 2
			}
			if !mark.Equal(from.Add(5*time.Minute)) || totals[semconv.MetricHTTPRequests] != 7*zoneMultiplier {
				t.Fatalf("base totals/checkpoint changed: %v %v", totals, mark)
			}
			if mode != "field-budget" && (totals[semconv.MetricHTTPResponseBytes] != 70*zoneMultiplier || totals[semconv.MetricHTTPVisits] != 3*zoneMultiplier) {
				t.Fatal("base bytes or visits lost")
			}
			want := 12 * zoneMultiplier
			if mode == "host-filter" {
				want = 5
			}
			for _, name := range []string{semconv.MetricHTTPRequestsByColo, semconv.MetricHTTPRequestsByASN, semconv.MetricHTTPErrorsByRoute} {
				expected := want
				if mode == "mixed-zones" && name == semconv.MetricHTTPRequestsByASN {
					expected = 12
				}
				if mode == "default-off" || (mode == "field-budget" && name != semconv.MetricHTTPRequestsByColo) || (mode == "asn-unavailable" && name == semconv.MetricHTTPRequestsByASN) {
					expected = 0
				}
				if totals[name] != expected {
					t.Fatalf("group count conservation: %s=%g want=%g", name, totals[name], expected)
				}
				maxPoints := 2
				if mode == "mixed-zones" {
					maxPoints = 4
				}
				if sizes[name] > maxPoints {
					t.Fatal("per-family cap/remainder exceeded")
				}
			}
			if mode == "default-off" && highCalls != 0 {
				t.Fatal("default high-cardinality features queried source")
			}
		})
	}
}

// Catch incorrect segment matching, double decoding, or invalid input acceptance.
func TestNormalizedErrorPath(t *testing.T) {
	uuid := strings.Repeat("a", 8) + "-" + strings.Repeat("b", 4) + "-" + strings.Repeat("c", 4) + "-" + strings.Repeat("d", 4) + "-" + strings.Repeat("e", 12)
	for _, tc := range []struct {
		raw, want string
		valid     bool
	}{
		{"/items/123?opaque#opaque", "/items/:number", true},
		{"/objects/" + uuid, "/objects/:uuid", true},
		{"/digests/" + strings.Repeat("a", 8), "/digests/:hex", true},
		{"/items/%31%32%33", "/items/:number", true},
		{"/items/%2531", "/items/%31", true},
		{"/items/word123", "/items/word123", true},
		{"items/123", "", false}, {"/items/%zz", "", false}, {"/items/%ff", "", false}, {"/" + strings.Repeat("x", 4096), "", false},
	} {
		got, ok := normalizedErrorPath(tc.raw)
		if ok != tc.valid || got != tc.want {
			t.Fatalf("normalizer branch mismatch: valid=%t want=%t", ok, tc.valid)
		}
	}
}
