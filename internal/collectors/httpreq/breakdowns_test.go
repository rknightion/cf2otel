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
)

// TestRegisteredKPIsHTTPBoundary exercises the real entitlement and GraphQL path.
func TestRegisteredKPIsHTTPBoundary(t *testing.T) {
	for _, name := range []string{"httpreq.threats", "httpreq.transfer", "httpreq.metrics"} {
		t.Run(name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Cloudflare.AccountID = "account-fixture"
			cfg.HTTP.MetricsScope = "all"
			cfg.HTTP.RequestSource = "all"
			cfg.HTTP.Breakdowns = []string{}
			var queries []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []cfapi.Zone{zoneForAccount("zone-fixture", "example.com", "account-fixture")}})
					return
				}
				var body struct {
					Query string `json:"query"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				q := body.Query
				node := map[string]any{}
				dataset := "httpRequestsAdaptiveGroups"
				if strings.Contains(q, "httpRequests1hGroups") {
					dataset = "httpRequests1hGroups"
				}
				if strings.Contains(q, "settings{") {
					node["settings"] = map[string]any{dataset: cfapi.DatasetSettings{Enabled: true, AvailableFields: []string{"count", "dimensions_clientRequestHTTPHost", "dimensions_edgeResponseStatus", "dimensions_cacheStatus", "sum_visits", "sum_edgeResponseBytes", "sum_threats", "dimensions_datetime"}, MaxNumberOfFields: 10, MaxDuration: 86400, MaxPageSize: 10000}}
				} else {
					queries = append(queries, q)
					rows := []any{map[string]any{"count": 7, "dimensions": map[string]any{"clientRequestHTTPHost": "www.example.com", "edgeResponseStatus": 200, "cacheStatus": "hit", "datetime": time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Hour).Format(time.RFC3339)}, "sum": map[string]any{"visits": 3, "edgeResponseBytes": 4096, "threats": 2}}}
					alias := regexp.MustCompile(`(\w+):` + dataset + `\(`).FindStringSubmatch(q)
					key := dataset
					if len(alias) > 1 {
						key = alias[1]
					}
					node[key] = rows
				}
				scope := "zones"
				if strings.Contains(q, "accounts(") {
					scope = "accounts"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{scope: []any{node}}}})
			}))
			defer srv.Close()
			cfg.Cloudflare.APIBase = srv.URL
			cfg.Cloudflare.APIToken = "fixture"
			reg := collector.NewRegistry()
			Register(collector.Deps{Config: &cfg, API: cfapi.New(cfg.Cloudflare), Registry: reg})
			e := &fakeEmitter{}
			found := false
			for _, entry := range reg.Entries() {
				if entry.Collector.Name() != name {
					continue
				}
				found = true
				if c, ok := entry.Collector.(collector.SnapshotCollector); ok {
					if err := c.Collect(context.Background(), e); err != nil {
						t.Fatal(err)
					}
				} else {
					from := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Hour)
					if _, err := entry.Collector.(collector.WindowCollector).CollectWindow(context.Background(), from, from.Add(time.Hour), e); err != nil {
						t.Fatal(err)
					}
				}
			}
			if !found {
				t.Fatalf("%s collector not registered", name)
			}
			metric := semconv.MetricHTTPVisits
			value := float64(3)
			points := e.counts
			if name == "httpreq.threats" {
				metric = semconv.MetricHTTPThreats
				value = 2
			}
			if name == "httpreq.transfer" {
				metric = semconv.MetricHTTPAccountTransferMTD
				points = e.gauges
				value = 4096 * float64(len(queries))
			}
			seen := false
			for _, point := range points {
				if point.name == metric {
					seen = true
					if point.value != value {
						t.Fatalf("%s=%g want %g", metric, point.value, value)
					}
					if name == "httpreq.transfer" && len(point.attrs) != 0 {
						t.Fatalf("account aggregate leaked labels: %+v", point)
					}
					if name != "httpreq.transfer" && (len(point.attrs) != 1 || !hasAttr(point.attrs, semconv.AttrHTTPZone, "example.com")) {
						t.Fatalf("KPI not zone-only: %+v", point)
					}
				}
			}
			if !seen {
				t.Fatalf("%s missing from registered collection", metric)
			}
			for _, q := range queries {
				if name == "httpreq.transfer" && (!strings.Contains(q, `requestSource:"eyeball"`) || !strings.Contains(q, "accounts(")) {
					t.Fatalf("transfer must force account eyeball: %s", q)
				}
				if name != "httpreq.transfer" && strings.Contains(q, "requestSource:") {
					t.Fatalf("all/rollup source restricted: %s", q)
				}
			}
		})
	}
}

func TestRegisteredBreakdownsHTTPBoundary(t *testing.T) {
	for _, tc := range []struct {
		name        string
		toggles     []string
		missingTLS  bool
		budget, cap int
	}{
		{"default", nil, false, 40, 100},
		{"disabled", []string{}, false, 40, 100},
		{"origin-and-TLS-only", []string{"origin_status", "tls_protocol"}, false, 40, 100},
		{"edge-and-HTTP-only", []string{"status", "protocol"}, false, 40, 100},
		{"country-only", []string{"country"}, false, 40, 100},
		{"method-only", []string{"method"}, false, 40, 100},
		{"content-type-only", []string{"content_type"}, false, 40, 100},
		{"all-traffic", nil, false, 40, 100},
		{"missing-TLS", nil, true, 40, 100},
		{"bounded-batches", nil, false, 4, 100},
		{"overflow", nil, false, 40, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Cloudflare.AccountID = "account-fixture"
			cfg.HTTP.MetricsScope = "all"
			cfg.HTTP.MaxMetricSeriesPerWindow = tc.cap
			if tc.name == "all-traffic" {
				cfg.HTTP.RequestSource = "all"
			}
			if tc.toggles != nil {
				cfg.HTTP.Breakdowns = tc.toggles
			}
			fields := []string{"count", "dimensions_clientRequestHTTPHost", "dimensions_cacheStatus", "sum_edgeResponseBytes"}
			values := map[string]any{"edgeResponseStatus": 403, "originResponseStatus": 502, "clientCountryName": "gb", "clientRequestHTTPProtocol": "HTTP/2", "clientSSLProtocol": "TLSv1.3", "clientRequestHTTPMethodName": "GET", "edgeResponseContentTypeName": "html"}
			for dim := range values {
				if dim != "clientSSLProtocol" || !tc.missingTLS {
					fields = append(fields, "dimensions_"+dim)
				}
			}
			aliasRE := regexp.MustCompile(`(\w+):httpRequestsAdaptiveGroups\(`)
			var batchQueries []string
			dataPosts := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/zones" {
					_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []cfapi.Zone{zoneForAccount("zone-fixture", "example.com", "account-fixture")}})
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
					node["settings"] = map[string]any{"httpRequestsAdaptiveGroups": cfapi.DatasetSettings{Enabled: true, AvailableFields: fields, MaxNumberOfFields: tc.budget, MaxPageSize: 10000, MaxDuration: 3600}}
				} else {
					dataPosts++
					aliases := aliasRE.FindAllStringSubmatchIndex(q, -1)
					if len(aliases) == 0 {
						node["httpRequestsAdaptiveGroups"] = []any{map[string]any{"count": 7, "dimensions": map[string]any{"clientRequestHTTPHost": "www.example.com", "edgeResponseStatus": 403, "cacheStatus": "hit"}, "sum": map[string]any{"edgeResponseBytes": 4096}}}
					} else {
						batchQueries = append(batchQueries, q)
						selectedFields := 0
						for i, a := range aliases {
							end := len(q)
							if i+1 < len(aliases) {
								end = aliases[i+1][0]
							}
							section := q[a[0]:end]
							dims := map[string]any{}
							selectedFields++ // count
							for field, value := range values {
								if strings.Contains(section, field) {
									dims[field] = value
									selectedFields++
								}
							}
							row := map[string]any{"count": 7, "dimensions": dims}
							if strings.Contains(section, "edgeResponseBytes") {
								row["sum"] = map[string]any{"edgeResponseBytes": 4096}
								selectedFields++
							}
							node[q[a[2]:a[3]]] = []any{row}
						}
						if selectedFields > tc.budget {
							t.Errorf("combined batch field count %d exceeds %d", selectedFields, tc.budget)
						}
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"zones": []any{node}}}})
			}))
			defer srv.Close()
			cfg.Cloudflare.APIBase = srv.URL
			cfg.Cloudflare.APIToken = "fixture"
			api := cfapi.New(cfg.Cloudflare)
			reg := collector.NewRegistry()
			Register(collector.Deps{Config: &cfg, API: api, Registry: reg})
			e := &fakeEmitter{}
			from := time.Now().UTC().Add(-time.Hour)
			for _, entry := range reg.Entries() {
				if entry.Collector.Name() != "httpreq.metrics" {
					continue
				}
				mark, err := entry.Collector.(collector.WindowCollector).CollectWindow(context.Background(), from, from.Add(time.Minute), e)
				if tc.name == "overflow" {
					if err == nil || !strings.Contains(err.Error(), "series") || !mark.Equal(from) || len(e.counts) != 0 {
						t.Fatalf("overflow mark=%s err=%v points=%d", mark, err, len(e.counts))
					}
					return
				}
				if err != nil || !mark.Equal(from.Add(time.Minute)) {
					t.Fatalf("mark=%s err=%v", mark, err)
				}
			}
			enabled := map[string]bool{}
			for _, name := range cfg.HTTP.Breakdowns {
				enabled[name] = true
			}
			expected := map[string][]string{}
			for _, dim := range []struct{ toggle, metric, attr string }{
				{"status", semconv.MetricHTTPRequestsByStatus, semconv.AttrHTTPStatusCode},
				{"origin_status", semconv.MetricHTTPRequestsByStatus, semconv.AttrHTTPOriginStatusCode},
				{"country", semconv.MetricHTTPRequestsByCountry, semconv.AttrHTTPClientCountry},
				{"protocol", semconv.MetricHTTPRequestsByProtocol, semconv.AttrHTTPProtocol},
				{"tls_protocol", semconv.MetricHTTPRequestsByProtocol, semconv.AttrHTTPTLSProtocol},
				{"method", semconv.MetricHTTPRequestsByMethod, semconv.AttrHTTPMethod},
				{"content_type", semconv.MetricHTTPRequestsByContentType, semconv.AttrHTTPContentType},
			} {
				if enabled[dim.toggle] && (!tc.missingTLS || dim.toggle != "tls_protocol") {
					expected[dim.metric] = append(expected[dim.metric], dim.attr)
				}
			}
			if enabled["country"] && tc.budget >= 3 {
				expected[semconv.MetricHTTPResponseBytesByCountry] = []string{semconv.AttrHTTPClientCountry}
			}
			found := map[string]bool{}
			for _, point := range e.counts {
				attrs, ok := expected[point.name]
				if !ok {
					if point.name != semconv.MetricHTTPRequests && point.name != semconv.MetricHTTPResponseBytes {
						t.Errorf("unexpected instrument %s", point.name)
					}
					continue
				}
				found[point.name] = true
				if len(point.attrs) != len(attrs)+1 || !hasAttr(point.attrs, semconv.AttrHTTPZone, "example.com") {
					t.Errorf("zone-level attrs: %+v", point)
				}
				for _, attr := range attrs {
					expectedValue := ""
					switch attr {
					case semconv.AttrHTTPStatusCode:
						expectedValue = "403"
					case semconv.AttrHTTPOriginStatusCode:
						expectedValue = "502"
					case semconv.AttrHTTPClientCountry:
						expectedValue = "GB"
					case semconv.AttrHTTPProtocol:
						expectedValue = "HTTP/2"
					case semconv.AttrHTTPTLSProtocol:
						expectedValue = "TLSv1.3"
					case semconv.AttrHTTPMethod:
						expectedValue = "GET"
					case semconv.AttrHTTPContentType:
						expectedValue = "html"
					}
					if !hasAttr(point.attrs, attr, expectedValue) {
						t.Errorf("missing enabled attr %s=%s: %+v", attr, expectedValue, point)
					}
				}
				value := float64(7)
				if point.name == semconv.MetricHTTPResponseBytesByCountry {
					value = 4096
				}
				if point.value != value {
					t.Errorf("point value %+v", point)
				}
			}
			for name := range expected {
				if !found[name] {
					t.Errorf("breakdown instrument missing: %s", name)
				}
			}
			if tc.name == "disabled" && (len(batchQueries) != 0 || dataPosts != 1) {
				t.Errorf("disabled breakdown requests: %d data %d batch", dataPosts, len(batchQueries))
			}
			if tc.name == "default" && (len(batchQueries) != 1 || dataPosts != 2) {
				t.Errorf("expected one aliased breakdown POST plus existing totals POST; data=%d batch=%d", dataPosts, len(batchQueries))
			}
			if tc.name == "bounded-batches" && len(batchQueries) <= 1 {
				t.Error("field budget did not split batches")
			}
			for _, q := range batchQueries {
				filtered := strings.Contains(q, `requestSource:"eyeball"`)
				if strings.Contains(q, "clientRequestHTTPHost") || filtered != (cfg.HTTP.RequestSource == "eyeball") {
					t.Errorf("breakdown query host/filter: %s", q)
				}
				if tc.missingTLS && strings.Contains(q, "clientSSLProtocol") {
					t.Error("unavailable TLS dimension selected")
				}
			}
		})
	}
}
