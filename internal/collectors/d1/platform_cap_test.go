package d1_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/collectors/d1"
	"github.com/rknightion/cf2otel/internal/collectors/durableobjects"
	"github.com/rknightion/cf2otel/internal/collectors/kv"
	"github.com/rknightion/cf2otel/internal/collectors/queues"
	"github.com/rknightion/cf2otel/internal/collectors/r2"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

func TestPlatformStickyCapThroughHTTP(t *testing.T) {
	cases := []struct {
		name, dataset, dimension, field, idKey, nameKey, metric, attr string
		register                                                      func(collector.Deps)
	}{
		{"d1.queries", "d1QueriesAdaptiveGroups", "databaseId", "count", "uuid", "name", "cloudflare.d1.queries", "cloudflare.d1.database_name", d1.Register},
		{"kv.operations", "kvOperationsAdaptiveGroups", "namespaceId", "sum.requests", "id", "title", "cloudflare.kv.requests", "cloudflare.kv.namespace_name", kv.Register},
		{"queues.message_operations", "queueMessageOperationsAdaptiveGroups", "queueId", "count", "queue_id", "queue_name", "cloudflare.queues.message.operations", "cloudflare.queues.queue_name", queues.Register},
		{"durableobjects.invocations", "durableObjectsInvocationsAdaptiveGroups", "namespaceId", "sum.requests", "id", "name", "cloudflare.durableobjects.requests", "cloudflare.durableobjects.namespace_name", durableobjects.Register},
		{"r2.operations", "r2OperationsAdaptiveGroups", "actionType", "sum.requests", "", "", "cloudflare.r2.requests", "cloudflare.r2.action_type", r2.Register},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from := time.Now().UTC().Truncate(5 * time.Minute).Add(-20 * time.Minute)
			var phase atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					page, _ := strconv.Atoi(r.URL.Query().Get("page"))
					rows := []any{}
					first, last := 0, 50
					if page == 2 {
						first, last = 50, 60
					}
					for i := first; i < last; i++ {
						rows = append(rows, map[string]any{tc.idKey: fmt.Sprintf("opaque-%02d", i), tc.nameKey: fmt.Sprintf("named-%02d", i)})
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": rows, "result_info": map[string]any{"page": page, "per_page": 50, "count": len(rows), "total_count": 60}})
					return
				}
				var request struct {
					Query string `json:"query"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				if strings.Contains(request.Query, "settings{") {
					fields := []string{strings.ReplaceAll(tc.field, ".", "_"), "dimensions_datetimeFiveMinutes", "dimensions_" + tc.dimension}
					if tc.name == "queues.message_operations" {
						fields = append(fields, "sum_billableOperations")
					}
					if tc.name == "r2.operations" {
						fields = append(fields, "dimensions_bucketName")
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"accounts": []any{map[string]any{"settings": map[string]any{tc.dataset: map[string]any{"enabled": true, "availableFields": fields, "maxNumberOfFields": 20, "maxPageSize": 100, "maxDuration": 3600, "notOlderThan": 86400}}}}}}})
					return
				}
				indexes := []int{}
				if phase.Load() == 0 {
					for i := range 55 {
						indexes = append(indexes, i)
					}
				} else {
					indexes = []int{0, 55}
				}
				rows := []any{}
				for _, i := range indexes {
					dims := map[string]any{"datetimeFiveMinutes": from.Add(time.Duration(phase.Load()) * 5 * time.Minute).Format(time.RFC3339), tc.dimension: fmt.Sprintf("opaque-%02d", i)}
					if tc.name == "r2.operations" {
						dims[tc.dimension] = fmt.Sprintf("named-%02d", i)
						dims["bucketName"] = "named-bucket"
					}
					row := map[string]any{"dimensions": dims}
					if tc.field == "count" {
						row["count"] = 1
					} else {
						row["sum"] = map[string]any{"requests": 1}
					}
					if tc.name == "queues.message_operations" {
						row["sum"] = map[string]any{"billableOperations": 1}
					}
					rows = append(rows, row)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"accounts": []any{map[string]any{tc.dataset: rows}}}}})
			}))
			defer server.Close()
			cfg := config.Default()
			cfg.Cloudflare.AccountID = "account-fixture"
			registry := collector.NewRegistry()
			tc.register(collector.Deps{Config: &cfg, API: cfapi.New(config.CloudflareConfig{APIBase: server.URL, Timeout: time.Second, MaxResponseBytes: 65536}), Registry: registry})
			var window collector.WindowCollector
			for _, entry := range registry.Entries() {
				if entry.Collector.Name() == tc.name {
					window = entry.Collector.(collector.WindowCollector)
				}
			}
			if window == nil {
				t.Fatal("collector not registered")
			}
			for currentPhase := int32(0); currentPhase < 2; currentPhase++ {
				phase.Store(currentPhase)
				out := &telemetry.Buffer{}
				start := from.Add(time.Duration(currentPhase) * 5 * time.Minute)
				mark, err := window.CollectWindow(context.Background(), start, start.Add(5*time.Minute), out)
				if err != nil || !mark.Equal(start.Add(5*time.Minute)) {
					t.Fatalf("window checkpoint=%v err=%v", mark, err)
				}
				values := map[string]float64{}
				for _, point := range out.Metrics {
					if point.Name == tc.metric {
						for _, attr := range point.Attrs {
							if attr.Key == tc.attr {
								values[attr.Value] += point.Value
							}
							if strings.HasPrefix(attr.Value, "opaque-") || strings.Contains(attr.Key, "_id") {
								t.Fatal("ID leaked into metric")
							}
						}
					}
				}
				total := 0.0
				for _, value := range values {
					total += value
				}
				if currentPhase == 0 {
					if len(values) != 50 || values["other"] != 6 || total != 55 {
						t.Fatalf("first-window capped counts: sets=%d remainder=%v total=%v", len(values), values["other"], total)
					}
				} else if len(values) != 2 || values["named-00"] != 1 || values["other"] != 1 || total != 2 {
					t.Fatalf("second-window sticky admission = %v", values)
				}
			}
		})
	}
}
