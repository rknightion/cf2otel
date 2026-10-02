package queues

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

func TestDepthRegisterHTTP(t *testing.T) {
	for _, mode := range []string{"success", "absent", "error", "duplicate", "cap", "cap-two", "cap-headroom", "cap-headroom-reversed", "count-null", "count-missing", "count-negative", "count-fractional", "dataset-null", "empty", "zero"} {
		t.Run(mode, func(t *testing.T) {
			from := time.Now().UTC().Truncate(5 * time.Minute).Add(-30 * time.Minute)
			fields := []string{"count", "sum_billableOperations", "dimensions_datetimeFiveMinutes", "dimensions_queueId", "avg_lagTime", "avg_retryCount", "dimensions_actionType", "dimensions_consumerType", "dimensions_outcome"}
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
					node = map[string]any{"settings": map[string]any{"queueMessageOperationsAdaptiveGroups": map[string]any{"enabled": true, "availableFields": fields, "maxNumberOfFields": 5, "maxPageSize": 100, "maxDuration": 300, "notOlderThan": 86400}}}
				} else {
					if strings.Contains(q, "lagTime") || strings.Contains(q, "retryCount") {
						if !strings.Contains(q, "actionType:\"ReadMessage\"") || !strings.Contains(q, "queueId") {
							t.Error("missing validated ReadMessage filter/internal queue grouping")
						}
						if strings.Contains(q, "consumerType") || strings.Contains(q, "outcome") {
							t.Error("average split by action dimensions")
						}
					}
					if mode == "error" && strings.Contains(q, "lagTime") {
						_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"message": "fixture failure"}}})
						return
					}
					rows := []map[string]any{}
					for i := 0; i < 2; i++ {
						at := from.Add(time.Duration(i) * 5 * time.Minute)
						if !strings.Contains(q, "datetime_geq:\""+at.Format(time.RFC3339)+"\"") {
							continue
						}
						queues := 1
						if strings.Contains(q, "queueId") {
							queues = 2
						}
						for j := 0; j < queues; j++ {
							lag := 9000
							retries := 9
							if i == 1 {
								lag = 1000 + j*1500
								retries = j * 2
							}
							row := map[string]any{"dimensions": map[string]any{"datetimeFiveMinutes": at.Format(time.RFC3339), "queueId": []string{"invented-q-a", "invented-q-b"}[j], "actionType": "ReadMessage", "consumerType": "worker", "outcome": "success"}, "count": 7, "sum": map[string]any{"billableOperations": 11}, "avg": map[string]any{"lagTime": lag, "retryCount": retries}}
							// Grouping the same fixture by queue must conserve its account totals.
							if strings.Contains(q, "queueId") && !strings.Contains(q, "consumerType") && !strings.Contains(q, "lagTime") {
								row["count"] = 3 + j
								row["sum"] = map[string]any{"billableOperations": 5 + j}
							}
							if strings.Contains(q, "consumerType") {
								row["sum"] = map[string]any{"billableOperations": 8}
								row["dimensions"].(map[string]any)["outcome"] = "none"
								rows = append(rows, map[string]any{"dimensions": map[string]any{"datetimeFiveMinutes": at.Format(time.RFC3339), "actionType": "SendMessage", "consumerType": "", "outcome": "none"}, "sum": map[string]any{"billableOperations": 3}})
							}
							if mode == "zero" && strings.Contains(q, "consumerType") {
								row["sum"].(map[string]any)["billableOperations"] = 0
							}
							if i == 1 && strings.Contains(q, "consumerType") {
								counts := row["sum"].(map[string]any)
								switch mode {
								case "count-null":
									counts["billableOperations"] = nil
								case "count-missing":
									delete(counts, "billableOperations")
								case "count-negative":
									counts["billableOperations"] = -1
								case "count-fractional":
									counts["billableOperations"] = 1.5
								}
							}
							rows = append(rows, row)
							if mode == "duplicate" && strings.Contains(q, "lagTime") {
								rows = append(rows, row)
							}
						}
					}
					if mode == "cap-headroom-reversed" {
						for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
							rows[i], rows[j] = rows[j], rows[i]
						}
					}
					key := "queueMessageOperationsAdaptiveGroups"
					if strings.Contains(q, "depth:") {
						key = "depth"
					}
					var data any = rows
					if strings.Contains(q, "consumerType") || strings.Contains(q, "lagTime") {
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
			cfg := queueTestConfig("queues.message_operations")
			cfg.Cloudflare.APIBase = server.URL
			switch mode {
			case "cap":
				cfg.Platform.MaxMetricSeriesPerWindow = 1
			case "cap-two":
				cfg.Platform.MaxMetricSeriesPerWindow = 2
			case "cap-headroom", "cap-headroom-reversed":
				cfg.Platform.MaxMetricSeriesPerWindow = 3
			}
			window := queueWindow(t, cfg, cfapi.New(cfg.Cloudflare), "queues.message_operations")
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
			got := queueMetrics(out.Metrics)
			billable, billablePresent := got[semconv.MetricQueuesBillableOperations]
			if !billablePresent || billable.Kind != "counter" || billable.Value != 22 || !expectedRemainder(billable.Attrs) {
				t.Fatalf("legacy billable operations identity/value changed: %v", out.Metrics)
			}
			operations, operationsPresent := got[semconv.MetricQueuesMessageOperations]
			if mode == "cap" {
				if operationsPresent {
					t.Fatalf("old one-series cap selection changed: %v", out.Metrics)
				}
			} else if !operationsPresent || operations.Kind != "counter" || operations.Value != 14 || !expectedRemainder(operations.Attrs) {
				t.Fatalf("legacy message operations identity/value changed: %v", out.Metrics)
			}
			if !checkpoint.Equal(from.Add(10 * time.Minute)) {
				t.Fatalf("successful window did not advance: %v", checkpoint)
			}
			if strings.HasPrefix(mode, "cap") {
				if len(out.Metrics) != cfg.Platform.MaxMetricSeriesPerWindow {
					t.Fatalf("cap metrics=%v", out.Metrics)
				}
				if strings.HasPrefix(mode, "cap-headroom") {
					point := out.Metrics[2]
					if point.Name != semconv.MetricQueuesBillableOperationsByAction || point.Kind != "counter" || point.Value != 16 || len(point.Attrs) != 3 || point.Attrs[0].Key != semconv.AttrQueuesActionType || point.Attrs[0].Value != "ReadMessage" || point.Attrs[1].Key != semconv.AttrQueuesConsumerType || point.Attrs[1].Value != "worker" || point.Attrs[2].Key != semconv.AttrQueuesOutcome || point.Attrs[2].Value != "none" {
						t.Fatalf("remaining capacity lost deterministic ReadMessage action count: %v", out.Metrics)
					}
				}
				return
			}
			if mode == "absent" || mode == "empty" {
				if len(out.Metrics) != 2 {
					t.Fatalf("unentitled extension emitted: %v", out.Metrics)
				}
				return
			}
			if got[semconv.MetricQueuesMessageLag].Value != 2.5 || got[semconv.MetricQueuesMessageRetries].Value != 2 {
				t.Fatalf("missing latest max averages/units: %v", out.Metrics)
			}
			actions := map[string]float64{}
			for _, m := range out.Metrics {
				if m.Name != semconv.MetricQueuesBillableOperationsByAction {
					continue
				}
				if len(m.Attrs) != 3 {
					t.Fatalf("missing action dimensions: %v", m)
				}
				actions[m.Attrs[0].Value] = m.Value
				if m.Attrs[0].Value == "SendMessage" && m.Attrs[1].Value != "" {
					t.Fatalf("consumer N/A source value reinterpreted: %v", m)
				}
			}
			readCount, readPresent := actions["ReadMessage"]
			wantReadCount := float64(16)
			if mode == "zero" {
				wantReadCount = 0
			}
			if !readPresent || readCount != wantReadCount || actions["SendMessage"] != 6 {
				t.Fatalf("missing grouped action counts including inapplicable consumer: %v", out.Metrics)
			}
			for _, m := range out.Metrics {
				for _, a := range m.Attrs {
					if a.Key != semconv.AttrQueuesActionType && a.Key != semconv.AttrQueuesConsumerType && a.Key != semconv.AttrQueuesOutcome && (a.Key != semconv.AttrQueuesQueueName || a.Value != "other") {
						t.Fatalf("queue identifier leaked: %v", m)
					}
				}
			}
		})
	}
}
