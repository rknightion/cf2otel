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

// Exercise the registered collector through the real API client: raw host
// variants share a metric label but must never have their percentiles combined.
func TestRegisteredLatencyHostVariantsHTTP(t *testing.T) {
	for _, tc := range []struct {
		name    string
		counts  [2]int
		reverse bool
		want    float64
	}{
		{name: "port-largest", counts: [2]int{3, 9}, want: .9},
		{name: "canonical-largest", counts: [2]int{9, 3}, reverse: true, want: .3},
		{name: "tie-canonical", counts: [2]int{6, 6}, reverse: true, want: .3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, from, to, failLatency := latencyHTTPCollector(t, tc.counts, tc.reverse)
			e := &fakeEmitter{}
			mark, err := c.CollectWindow(context.Background(), from, to, e)
			if err != nil || !mark.Equal(to) {
				t.Fatalf("normalized host variants must succeed and advance: mark=%s err=%v", mark, err)
			}
			requests, bytes, country, variants := float64(0), float64(0), float64(0), float64(0)
			for _, p := range e.counts {
				switch p.name {
				case semconv.MetricHTTPRequests:
					requests += p.value
				case semconv.MetricHTTPResponseBytes:
					bytes += p.value
				case semconv.MetricHTTPRequestsByCountry:
					country += p.value
				case semconv.MetricHTTPLatencyHostVariants:
					variants += p.value
					if len(p.attrs) != 1 || !hasAttr(p.attrs, semconv.AttrHTTPZone, "zone-fixture") {
						t.Fatalf("variant attributes=%+v", p.attrs)
					}
				}
			}
			if requests != 12 || bytes != 1200 || country != 12 || variants != 1 {
				t.Fatalf("all raw groups must contribute additive totals; requests=%g bytes=%g country=%g discarded=%g", requests, bytes, country, variants)
			}
			latencies := 0
			for _, p := range e.gauges {
				if p.name == semconv.MetricHTTPOriginResponseTime {
					latencies++
					if p.value != tc.want || !hasAttr(p.attrs, semconv.AttrHTTPHost, "host-fixture") {
						t.Fatalf("latency must come only from largest-count group (canonical wins ties): %+v want=%g", p, tc.want)
					}
				}
			}
			if latencies != 1 {
				t.Fatalf("expected one host percentile, got %d", latencies)
			}

			// A genuine latency failure must be explicit and leave the entire window
			// retryable; request/byte/breakdown totals cannot silently be checkpointed away.
			*failLatency = true
			failed := &fakeEmitter{}
			mark, err = c.CollectWindow(context.Background(), from, to, failed)
			if err == nil || !strings.Contains(err.Error(), "latenc") || !mark.Equal(from) || len(failed.counts) != 0 || len(failed.gauges) != 0 {
				t.Fatalf("latency failure must preserve atomic retry: mark=%s err=%v emitted=%+v", mark, err, failed)
			}
			*failLatency = false
			retry := &fakeEmitter{}
			mark, err = c.CollectWindow(context.Background(), mark, to, retry)
			if err != nil || !mark.Equal(to) {
				t.Fatalf("retry failed: mark=%s err=%v", mark, err)
			}
			recovered := map[string]float64{}
			for _, p := range retry.counts {
				recovered[p.name] += p.value
			}
			if recovered[semconv.MetricHTTPRequests] != requests || recovered[semconv.MetricHTTPResponseBytes] != bytes || recovered[semconv.MetricHTTPRequestsByCountry] != country {
				t.Fatalf("latency failure lost additive totals on retry: %+v", recovered)
			}
		})
	}
}

func latencyHTTPCollector(t *testing.T, counts [2]int, reverse bool) (collector.WindowCollector, time.Time, time.Time, *bool) {
	t.Helper()
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "fixture"
	cfg.HTTP.MetricsScope = "all"
	cfg.HTTP.Breakdowns = []string{"country"}
	from := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute)
	failLatency := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []cfapi.Zone{zoneForAccount("fixture", "zone-fixture", "fixture")}})
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
			node["settings"] = map[string]any{"httpRequestsAdaptiveGroups": cfapi.DatasetSettings{Enabled: true, AvailableFields: []string{"count", "dimensions_clientRequestHTTPHost", "dimensions_edgeResponseStatus", "dimensions_cacheStatus", "sum_edgeResponseBytes", "dimensions_clientCountryName", "quantiles_originResponseDurationMsP95"}, MaxNumberOfFields: 20, MaxDuration: 2592000}}
		} else {
			key := "httpRequestsAdaptiveGroups"
			if alias := regexp.MustCompile(`(\w+):httpRequestsAdaptiveGroups\(`).FindStringSubmatch(q); len(alias) > 1 {
				key = alias[1]
			}
			switch {
			case strings.Contains(q, "quantiles{"):
				if failLatency {
					_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": "fixture latency failure"}}})
					return
				}
				if !regexp.MustCompile(`\bcount\b`).MatchString(q) {
					t.Errorf("latency selection must request group count: %s", q)
				}
				if strings.Contains(q, "edgeResponseStatus") || strings.Contains(q, "cacheStatus") {
					t.Errorf("latency must be host-only: %s", q)
				}
				rows := []any{
					map[string]any{"count": counts[0], "dimensions": map[string]any{"clientRequestHTTPHost": "host-fixture"}, "quantiles": map[string]any{"originResponseDurationMsP95": 300}},
					map[string]any{"count": counts[1], "dimensions": map[string]any{"clientRequestHTTPHost": "host-fixture:8443"}, "quantiles": map[string]any{"originResponseDurationMsP95": 900}},
				}
				if reverse {
					rows[0], rows[1] = rows[1], rows[0]
				}
				node[key] = rows
			case strings.Contains(q, "clientCountryName"):
				node[key] = []any{map[string]any{"count": counts[0], "dimensions": map[string]any{"clientCountryName": "GB"}, "sum": map[string]any{"edgeResponseBytes": 300}}, map[string]any{"count": counts[1], "dimensions": map[string]any{"clientCountryName": "FR"}, "sum": map[string]any{"edgeResponseBytes": 900}}}
			default:
				node[key] = []any{
					map[string]any{"count": counts[0], "dimensions": map[string]any{"clientRequestHTTPHost": "host-fixture", "edgeResponseStatus": 200, "cacheStatus": "hit"}, "sum": map[string]any{"edgeResponseBytes": 300}},
					map[string]any{"count": counts[1], "dimensions": map[string]any{"clientRequestHTTPHost": "host-fixture:8443", "edgeResponseStatus": 200, "cacheStatus": "hit"}, "sum": map[string]any{"edgeResponseBytes": 900}},
				}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"zones": []any{node}}}})
	}))
	t.Cleanup(srv.Close)
	cfg.Cloudflare.APIBase = srv.URL
	cfg.Cloudflare.APIToken = "fixture"
	reg := collector.NewRegistry()
	Register(collector.Deps{Config: &cfg, API: cfapi.New(cfg.Cloudflare), Registry: reg})
	for _, entry := range reg.Entries() {
		if entry.Collector.Name() == "httpreq.metrics" {
			return entry.Collector.(collector.WindowCollector), from, to, &failLatency
		}
	}
	t.Fatal("metrics collector not registered")
	return nil, from, to, &failLatency
}
