package durableobjects

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

func TestDepthRegisterHTTP(t *testing.T) {
	for _, mode := range []string{"success", "absent", "error", "duplicate", "cap"} {
		t.Run(mode, func(t *testing.T) {
			from := time.Now().UTC().Truncate(5 * time.Minute).Add(-30 * time.Minute)
			fields := []string{"sum_requests", "sum_errors", "dimensions_datetimeFiveMinutes", "dimensions_scriptName", "quantiles_wallTimeP50", "quantiles_wallTimeP99", "quantiles_responseBodySizeP50", "quantiles_responseBodySizeP75"}
			if mode == "absent" {
				fields = fields[:1]
				fields = append(fields, "dimensions_datetimeFiveMinutes")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Query string `json:"query"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				q := req.Query
				var node map[string]any
				if strings.Contains(q, "settings{") {
					node = map[string]any{"settings": map[string]any{"durableObjectsInvocationsAdaptiveGroups": map[string]any{"enabled": true, "availableFields": fields, "maxNumberOfFields": 3, "maxPageSize": 100, "maxDuration": 300, "notOlderThan": 86400}}}
				} else {
					if strings.Contains(q, "status") || strings.Contains(q, "namespace") {
						t.Error("ambiguous/resource grouping selected")
					}
					if mode == "error" && strings.Contains(q, "wallTimeP50") {
						_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"message": "fixture failure"}}})
						return
					}
					rows := []map[string]any{}
					for i := 0; i < 2; i++ {
						at := from.Add(time.Duration(i) * 5 * time.Minute)
						if !strings.Contains(q, "datetime_geq:\""+at.Format(time.RFC3339)+"\"") {
							continue
						}
						value := 9000000
						if i == 1 {
							value = 2500000
						}
						row := map[string]any{"dimensions": map[string]any{"datetimeFiveMinutes": at.Format(time.RFC3339), "scriptName": "example-script"}, "sum": map[string]any{"requests": 7, "errors": 2}, "quantiles": map[string]any{"wallTimeP50": value, "wallTimeP99": map[bool]int{true: -1, false: 10000000}[i == 1], "responseBodySizeP50": 0}}
						rows = append(rows, row)
						if mode == "duplicate" && strings.Contains(q, "wallTimeP50") {
							rows = append(rows, row)
						}
					}
					node = map[string]any{"durableObjectsInvocationsAdaptiveGroups": rows}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"accounts": []any{node}}}})
			}))
			defer server.Close()
			cfg := platformConfig()
			cfg.Cloudflare.APIBase = server.URL
			if mode == "cap" {
				cfg.Platform.MaxMetricSeriesPerWindow = 1
			}
			reg := collector.NewRegistry()
			Register(collector.Deps{Config: cfg, API: cfapi.New(cfg.Cloudflare), Registry: reg})
			window := collectorEntry(t, reg, "durableobjects.invocations").Collector.(collector.WindowCollector)
			out := &telemetry.Buffer{}
			_, err := window.CollectWindow(context.Background(), from, from.Add(10*time.Minute), out)
			if mode == "error" || mode == "duplicate" {
				if err == nil || len(out.Metrics) != 0 {
					t.Fatalf("failure must emit nothing: err=%v metrics=%v", err, out.Metrics)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "cap" {
				if len(out.Metrics) != 1 {
					t.Fatalf("cap metrics=%v", out.Metrics)
				}
				return
			}
			got := map[string]telemetry.BufferedMetric{}
			for _, m := range out.Metrics {
				got[m.Name] = m
			}
			if got[semconv.MetricDurableObjectsRequests].Value != 14 {
				t.Fatalf("old requests changed: %v", out.Metrics)
			}
			if mode == "absent" {
				if len(out.Metrics) != 1 {
					t.Fatalf("unentitled extension emitted: %v", out.Metrics)
				}
				return
			}
			if got[semconv.MetricDurableObjectsErrors].Value != 4 || got[semconv.MetricDurableObjectsWallTime].Value != 2.5 {
				t.Fatalf("missing errors/latest seconds: %v", out.Metrics)
			}
			size, ok := got[semconv.MetricDurableObjectsResponseSize]
			if !ok || size.Value != 0 {
				t.Fatalf("true zero size omitted: %v", out.Metrics)
			}
			if len(out.Metrics) != 4 {
				t.Fatalf("negative N/A emitted: %v", out.Metrics)
			}
			for _, m := range out.Metrics {
				if m.Name == semconv.MetricDurableObjectsRequests {
					continue
				}
				if len(m.Attrs) == 0 || m.Attrs[0].Key != semconv.AttrWorkersScriptName || m.Attrs[0].Value != "example-script" {
					t.Fatalf("missing script attribute: %v", m)
				}
			}
		})
	}
}
