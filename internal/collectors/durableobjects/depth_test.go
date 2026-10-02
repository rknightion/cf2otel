package durableobjects

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
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

func TestDepthRegisterHTTP(t *testing.T) {
	for _, mode := range []string{"success", "absent", "error", "duplicate", "cap", "cap-default", "cap-default-reversed", "count-null", "count-missing", "count-negative", "count-fractional", "dataset-null", "empty", "zero"} {
		t.Run(mode, func(t *testing.T) {
			from := time.Now().UTC().Truncate(5 * time.Minute).Add(-30 * time.Minute)
			scriptCount := 1
			if strings.HasPrefix(mode, "cap-default") {
				scriptCount = 500
			}
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
					node = map[string]any{"settings": map[string]any{"durableObjectsInvocationsAdaptiveGroups": map[string]any{"enabled": true, "availableFields": fields, "maxNumberOfFields": 3, "maxPageSize": max(100, scriptCount+1), "maxDuration": 300, "notOlderThan": 86400}}}
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
						if mode == "zero" {
							row["sum"].(map[string]any)["errors"] = 0
						}
						if i == 1 && strings.Contains(q, "errors") {
							counts := row["sum"].(map[string]any)
							switch mode {
							case "count-null":
								counts["errors"] = nil
							case "count-missing":
								delete(counts, "errors")
							case "count-negative":
								counts["errors"] = -1
							case "count-fractional":
								counts["errors"] = 1.5
							}
						}
						if strings.Contains(q, "scriptName") {
							for j := 0; j < scriptCount; j++ {
								script := j
								if mode == "cap-default-reversed" {
									script = scriptCount - 1 - j
								}
								name := "example-script"
								if scriptCount > 1 {
									name = fmt.Sprintf("example-script-%03d", script)
								}
								rows = append(rows, map[string]any{"dimensions": map[string]any{"datetimeFiveMinutes": at.Format(time.RFC3339), "scriptName": name}, "sum": row["sum"], "quantiles": row["quantiles"]})
							}
						} else {
							row["sum"].(map[string]any)["requests"] = 7 * scriptCount
							rows = append(rows, row)
						}
						if mode == "duplicate" && strings.Contains(q, "wallTimeP50") {
							rows = append(rows, row)
						}
					}
					key := "durableObjectsInvocationsAdaptiveGroups"
					if strings.Contains(q, "depth:") {
						key = "depth"
					}
					var data any = rows
					if strings.Contains(q, "scriptName") {
						switch mode {
						case "dataset-null":
							data = nil
						case "empty":
							data = []any{}
						}
					}
					node = map[string]any{key: data}
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
			checkpoint, err := window.CollectWindow(context.Background(), from, from.Add(10*time.Minute), out)
			if mode == "error" || mode == "duplicate" || strings.HasPrefix(mode, "count-") || mode == "dataset-null" {
				if err == nil || len(out.Metrics) != 0 || !checkpoint.Equal(from) {
					t.Fatalf("failure must emit nothing: err=%v metrics=%v", err, out.Metrics)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]telemetry.BufferedMetric{}
			for _, m := range out.Metrics {
				got[m.Name] = m
			}
			requests, requestsPresent := got[semconv.MetricDurableObjectsRequests]
			if !requestsPresent || requests.Kind != "counter" || requests.Value != float64(14*scriptCount) || !expectedRemainder(requests.Attrs) {
				t.Fatalf("legacy requests identity/value changed: %v", out.Metrics)
			}
			if !checkpoint.Equal(from.Add(10 * time.Minute)) {
				t.Fatalf("successful window did not advance: %v", checkpoint)
			}
			if strings.HasPrefix(mode, "cap") {
				if len(out.Metrics) != cfg.Platform.MaxMetricSeriesPerWindow {
					t.Fatalf("cap metrics=%v", out.Metrics)
				}
				if scriptCount > 1 {
					if out.Metrics[0].Name != semconv.MetricDurableObjectsRequests {
						t.Fatalf("legacy requests not retained first: %v", out.Metrics)
					}
					for i, point := range out.Metrics[1:] {
						if point.Name != semconv.MetricDurableObjectsErrors || point.Kind != "counter" || point.Value != 4 || len(point.Attrs) != 1 || point.Attrs[0].Key != semconv.AttrWorkersScriptName || point.Attrs[0].Value != fmt.Sprintf("example-script-%03d", i) {
							t.Fatalf("remaining capacity did not retain deterministic script errors: %v", point)
						}
					}
				}
				return
			}
			if mode == "absent" || mode == "empty" {
				if len(out.Metrics) != 1 {
					t.Fatalf("unentitled extension emitted: %v", out.Metrics)
				}
				return
			}
			errorsMetric, errorsPresent := got[semconv.MetricDurableObjectsErrors]
			wantErrors := float64(4)
			if mode == "zero" {
				wantErrors = 0
			}
			if !errorsPresent || errorsMetric.Value != wantErrors || got[semconv.MetricDurableObjectsWallTime].Value != 2.5 {
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
