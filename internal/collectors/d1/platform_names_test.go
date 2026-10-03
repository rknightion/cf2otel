package d1_test

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
	"github.com/rknightion/cf2otel/internal/collectors/d1"
	"github.com/rknightion/cf2otel/internal/collectors/durableobjects"
	"github.com/rknightion/cf2otel/internal/collectors/kv"
	"github.com/rknightion/cf2otel/internal/collectors/queues"
	"github.com/rknightion/cf2otel/internal/collectors/r2"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

// The process edge is HTTP; registration, settings, query construction, paging,
// aggregation and emission are the real collaborators.
func TestPlatformNamesThroughHTTP(t *testing.T) {
	cases := []struct {
		name, dataset, dimension, field, path, idKey, nameKey, attr string
		register                                                    func(collector.Deps)
	}{
		{"d1.queries", "d1QueriesAdaptiveGroups", "databaseId", "count", "/d1/database", "uuid", "name", "cloudflare.d1.database_name", d1.Register},
		{"kv.operations", "kvOperationsAdaptiveGroups", "namespaceId", "sum.requests", "/storage/kv/namespaces", "id", "title", "cloudflare.kv.namespace_name", kv.Register},
		{"queues.message_operations", "queueMessageOperationsAdaptiveGroups", "queueId", "count", "/queues", "queue_id", "queue_name", "cloudflare.queues.queue_name", queues.Register},
		{"durableobjects.invocations", "durableObjectsInvocationsAdaptiveGroups", "namespaceId", "sum.requests", "/workers/durable_objects/namespaces", "id", "name", "cloudflare.durableobjects.namespace_name", durableobjects.Register},
		{"r2.operations", "r2OperationsAdaptiveGroups", "actionType", "sum.requests", "", "", "", "cloudflare.r2.action_type", r2.Register},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from := time.Now().UTC().Truncate(5 * time.Minute).Add(-15 * time.Minute)
			restCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					restCalls++
					if r.URL.Path != "/accounts/account-fixture"+tc.path {
						t.Errorf("unexpected list path %s", r.URL.Path)
					}
					page := r.URL.Query().Get("page")
					id, name := "resource-one", "named-one"
					if page == "2" {
						id, name = "resource-two", "named-two"
					}
					size := "50"
					if page == "2" {
						size = "1"
					}
					if r.URL.Query().Get("per_page") != size {
						t.Errorf("requested page size %s", r.URL.Query().Get("per_page"))
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []any{map[string]any{tc.idKey: id, tc.nameKey: name}}, "result_info": map[string]any{"page": pageNumber(page), "per_page": 1, "count": 1, "total_count": 2}})
					return
				}
				var body struct {
					Query string `json:"query"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				if strings.Contains(body.Query, "settings{") {
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
				rows := []any{}
				for i, id := range []string{"resource-one", "resource-two"} {
					dimensions := map[string]any{"datetimeFiveMinutes": from.Format(time.RFC3339), tc.dimension: id}
					if tc.name == "r2.operations" {
						dimensions[tc.dimension] = []string{"ReadObject", "WriteObject"}[i]
						dimensions["bucketName"] = "named-bucket"
					}
					row := map[string]any{"dimensions": dimensions}
					if tc.field == "count" {
						row["count"] = i + 2
					} else {
						row["sum"] = map[string]any{"requests": i + 2}
					}
					if tc.name == "queues.message_operations" {
						row["sum"] = map[string]any{"billableOperations": i + 2}
					}
					rows = append(rows, row)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"accounts": []any{map[string]any{tc.dataset: rows}}}}})
			}))
			defer server.Close()
			cfg := config.Default()
			cfg.Cloudflare.AccountID = "account-fixture"
			api := cfapi.New(config.CloudflareConfig{APIBase: server.URL, Timeout: time.Second, MaxResponseBytes: 65536})
			registry := collector.NewRegistry()
			tc.register(collector.Deps{Config: &cfg, API: api, Registry: registry})
			var window collector.WindowCollector
			for _, entry := range registry.Entries() {
				if entry.Collector.Name() == tc.name {
					window = entry.Collector.(collector.WindowCollector)
				}
			}
			if window == nil {
				t.Fatal("collector not registered")
			}
			for iteration := 0; iteration < 2; iteration++ {
				out := &telemetry.Buffer{}
				mark, err := window.CollectWindow(context.Background(), from, from.Add(5*time.Minute), out)
				if err != nil {
					t.Fatal(err)
				}
				if !mark.Equal(from.Add(5 * time.Minute)) {
					t.Fatal("checkpoint did not advance")
				}
				names := map[string]float64{}
				for _, point := range out.Metrics {
					if tc.name == "queues.message_operations" && point.Name != "cloudflare.queues.message.operations" {
						continue
					}
					for _, attr := range point.Attrs {
						if attr.Key == tc.attr {
							names[fmt.Sprint(attr.Value)] += point.Value
						}
						if strings.Contains(attr.Key, "_id") || strings.HasPrefix(fmt.Sprint(attr.Value), "resource-") {
							t.Fatal("source ID leaked into labels")
						}
					}
				}
				expected := map[string]float64{"named-one": 2, "named-two": 3}
				if tc.name == "r2.operations" {
					expected = map[string]float64{"ReadObject": 2, "WriteObject": 3}
				}
				if len(names) != 2 {
					t.Fatalf("resolved names/actions = %v, want two distinct labelled series", names)
				}
				for name, value := range expected {
					if names[name] != value {
						t.Errorf("%s=%v, want %v", name, names[name], value)
					}
				}
			}
			if tc.path != "" && restCalls != 2 {
				t.Fatalf("list calls=%d, want two server-capped pages cached across windows", restCalls)
			}
		})
	}
}

func pageNumber(s string) int {
	if s == "2" {
		return 2
	}
	return 1
}
