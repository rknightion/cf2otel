package d1

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
			fields := []string{"sum_readQueries", "sum_writeQueries", "dimensions_datetimeFiveMinutes", "sum_rowsRead", "sum_rowsWritten", "quantiles_queryBatchTimeMsP50", "quantiles_queryBatchTimeMsP99", "quantiles_queryBatchResponseBytesP50", "quantiles_queryBatchResponseBytesP75"}
			if mode == "absent" {
				fields = fields[:3]
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Query string `json:"query"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				q := req.Query
				var node map[string]any
				if strings.Contains(q, "settings{") {
					node = map[string]any{"settings": map[string]any{"d1AnalyticsAdaptiveGroups": map[string]any{"enabled": true, "availableFields": fields, "maxNumberOfFields": 3, "maxPageSize": 100, "maxDuration": 300, "notOlderThan": 86400}}}
				} else {
					if strings.Contains(q, "databaseId") {
						t.Error("database identifier selected")
					}
					if mode == "error" && strings.Contains(q, "queryBatchTimeMsP50") {
						_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"message": "fixture failure"}}})
						return
					}
					rows := []map[string]any{}
					for i := 0; i < 2; i++ {
						at := from.Add(time.Duration(i) * 5 * time.Minute)
						if !strings.Contains(q, "datetime_geq:\""+at.Format(time.RFC3339)+"\"") {
							continue
						}
						value := 9000
						if i == 1 {
							value = 2500
						}
						row := map[string]any{"dimensions": map[string]any{"datetimeFiveMinutes": at.Format(time.RFC3339)}, "sum": map[string]any{"readQueries": 7, "writeQueries": 2, "rowsRead": 11, "rowsWritten": 0}, "quantiles": map[string]any{"queryBatchTimeMsP50": value, "queryBatchTimeMsP99": map[bool]int{true: -1, false: 10000}[i == 1], "queryBatchResponseBytesP50": 0}}
						rows = append(rows, row)
						if mode == "duplicate" && strings.Contains(q, "queryBatchTimeMsP50") {
							rows = append(rows, row)
						}
					}
					node = map[string]any{"d1AnalyticsAdaptiveGroups": rows}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"accounts": []any{node}}}})
			}))
			defer server.Close()
			cfg := testConfig()
			cfg.Cloudflare.APIBase = server.URL
			if mode == "cap" {
				cfg.Platform.MaxMetricSeriesPerWindow = 1
			}
			reg := collector.NewRegistry()
			Register(collector.Deps{Config: cfg, API: cfapi.New(cfg.Cloudflare), Registry: reg})
			var window collector.WindowCollector
			for _, entry := range reg.Entries() {
				if entry.Collector.Name() == "d1.analytics" {
					window = entry.Collector.(collector.WindowCollector)
				}
			}
			if window == nil {
				t.Fatal("not registered")
			}
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
			got := metricRows(out.Metrics)
			if got[semconv.MetricD1ReadQueries].Value != 14 || got[semconv.MetricD1WriteQueries].Value != 4 {
				t.Fatalf("old queries changed: %v", out.Metrics)
			}
			if mode == "absent" {
				if len(out.Metrics) != 2 {
					t.Fatalf("unentitled extension emitted: %v", out.Metrics)
				}
				return
			}
			if got[semconv.MetricD1RowsRead].Value != 22 || got[semconv.MetricD1QueryBatchTime].Value != 2.5 {
				t.Fatalf("missing additive rows/latest seconds: %v", out.Metrics)
			}
			for _, name := range []string{semconv.MetricD1RowsWritten, semconv.MetricD1QueryBatchResponseSize} {
				m, ok := got[name]
				if !ok || m.Value != 0 {
					t.Fatalf("true zero omitted: %v", out.Metrics)
				}
			}
			if len(out.Metrics) != 6 {
				t.Fatalf("negative N/A emitted: %v", out.Metrics)
			}
			for _, m := range out.Metrics {
				for _, a := range m.Attrs {
					if a.Key != semconv.AttrStatistic {
						t.Fatalf("resource attribute leaked: %v", m)
					}
				}
			}
		})
	}
}
