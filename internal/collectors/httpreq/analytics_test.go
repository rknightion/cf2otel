package httpreq

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
)

// analyticsAPI models API selection, so unrequested fields cannot make tests pass.
type analyticsAPI struct {
	fakeAPI
	pro    bool
	budget int
}

func (f *analyticsAPI) DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error) {
	fields := []string{"count", "dimensions_clientRequestHTTPHost", "dimensions_edgeResponseStatus", "dimensions_cacheStatus", "avg_originResponseDurationMs", "sum_edgeResponseBytes", "quantiles_originResponseDurationMsP50", "quantiles_originResponseDurationMsP95", "quantiles_originResponseDurationMsP99"}
	if f.pro {
		fields = append(fields, "avg_edgeTimeToFirstByteMs", "quantiles_edgeTimeToFirstByteMsP50", "quantiles_edgeTimeToFirstByteMsP95", "quantiles_edgeTimeToFirstByteMsP99")
	}
	return cfapi.DatasetSettings{Enabled: true, AvailableFields: fields, MaxNumberOfFields: f.budget}, nil
}
func (f *analyticsAPI) Query(_ context.Context, q cfapi.GraphQLRequest, out any) error {
	f.queries = append(f.queries, q)
	row := map[string]any{}
	for _, name := range q.WantedFields {
		switch name {
		case "count":
			row[name] = 7
		case "dimensions.clientRequestHTTPHost", "dimensions.edgeResponseStatus", "dimensions.cacheStatus":
			dims, _ := row["dimensions"].(map[string]any)
			if dims == nil {
				dims = map[string]any{}
				row["dimensions"] = dims
			}
			switch name {
			case "dimensions.clientRequestHTTPHost":
				dims["clientRequestHTTPHost"] = "www.example.com"
			case "dimensions.edgeResponseStatus":
				dims["edgeResponseStatus"] = 200
			default:
				dims["cacheStatus"] = "hit"
			}
		default:
			for _, part := range []string{"sum", "avg", "quantiles"} {
				prefix := part + "."
				if len(name) > len(prefix) && name[:len(prefix)] == prefix {
					values, _ := row[part].(map[string]any)
					if values == nil {
						values = map[string]any{}
						row[part] = values
					}
					value := 2500
					if part == "sum" {
						value = 4096
					}
					values[name[len(prefix):]] = value
				}
			}
		}
	}
	*(out.(*[]map[string]any)) = []map[string]any{row}
	return nil
}
func runAnalytics(t *testing.T, cfg *config.Config, api cfapi.Client) *fakeEmitter {
	t.Helper()
	reg := collector.NewRegistry()
	Register(collector.Deps{Config: cfg, API: api, Registry: reg})
	e := &fakeEmitter{}
	for _, entry := range reg.Entries() {
		if entry.Collector.Name() == "httpreq.metrics" {
			from := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
			mark, err := entry.Collector.(collector.WindowCollector).CollectWindow(context.Background(), from, from.Add(5*time.Minute), e)
			if err != nil || !mark.Equal(from.Add(5*time.Minute)) {
				t.Fatalf("mark=%s err=%v", mark, err)
			}
			return e
		}
	}
	t.Fatal("metrics collector not registered")
	return nil
}
func TestRegisteredAnalyticsEntitlementAndPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		pro          bool
	}{{"Free-eyeball", "eyeball", false}, {"Pro-eyeball", "eyeball", true}, {"Pro-all", "all", true}} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Cloudflare.AccountID = "account-fixture"
			cfg.HTTP.MetricsScope = "all"
			cfg.HTTP.RequestSource = tc.source
			api := &analyticsAPI{pro: tc.pro, budget: 40}
			e := runAnalytics(t, &cfg, api)
			foundBytes := false
			for _, c := range e.counts {
				if c.name == semconv.MetricHTTPResponseBytes {
					foundBytes = true
					if c.value != 4096 || !hasAttr(c.attrs, semconv.AttrHTTPCacheStatus, "hit") || !hasAttr(c.attrs, semconv.AttrStatusClass, "2xx") {
						t.Fatalf("bytes=%+v", c)
					}
				}
			}
			if !foundBytes {
				t.Error("response bytes counter missing")
			}
			stats := map[string]bool{}
			for _, g := range e.gauges {
				if g.name == semconv.MetricHTTPEdgeTTFB || g.name == semconv.MetricHTTPOriginResponseTime {
					if g.value != 2.5 || !hasAttr(g.attrs, semconv.AttrHTTPZone, "example.com") || !hasAttr(g.attrs, semconv.AttrHTTPHost, "www.example.com") || len(g.attrs) != 3 {
						t.Fatalf("latency=%+v", g)
					}
					for _, a := range g.attrs {
						if a.Key == semconv.AttrStatistic {
							stats[g.name+"/"+a.Value] = true
						}
					}
				}
			}
			for _, stat := range []string{"p50", "p95", "p99"} {
				if !stats[semconv.MetricHTTPOriginResponseTime+"/"+stat] {
					t.Errorf("origin %s missing", stat)
				}
			}
			for _, stat := range []string{"avg", "p50", "p95", "p99"} {
				if stats[semconv.MetricHTTPEdgeTTFB+"/"+stat] != tc.pro {
					t.Errorf("TTFB %s entitlement mismatch", stat)
				}
			}
			for _, q := range api.queries {
				hasLatency, hasStatus := false, false
				for _, field := range q.WantedFields {
					hasLatency = hasLatency || strings.HasPrefix(field, "quantiles.") || field == "avg.edgeTimeToFirstByteMs"
					hasStatus = hasStatus || field == "dimensions.edgeResponseStatus" || field == "dimensions.cacheStatus"
				}
				if hasLatency && hasStatus {
					t.Error("host quantiles queried with status/cache grouping")
				}
				if len(q.WantedFields) > 40 {
					t.Fatal("field budget exceeded")
				}
				if tc.source == "eyeball" && q.Filter["requestSource"] != "eyeball" {
					t.Fatal("eyeball filter missing")
				}
				if tc.source == "all" && q.Filter["requestSource"] != nil {
					t.Fatal("all filter unexpectedly restricted")
				}
			}
		})
	}
}
func TestRegisteredAnalyticsFieldBudget(t *testing.T) {
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "account-fixture"
	cfg.HTTP.MetricsScope = "all"
	api := &analyticsAPI{pro: true, budget: 4}
	runAnalytics(t, &cfg, api)
	for _, q := range api.queries {
		if len(q.WantedFields) > 4 {
			t.Fatalf("field budget exceeded: %+v", q)
		}
	}
}
