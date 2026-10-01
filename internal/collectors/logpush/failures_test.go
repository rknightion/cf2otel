package logpush

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
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

// Exercises registration and the real REST discovery/GraphQL settings/query path.
func TestLogpushFailuresRegisteredHTTP(t *testing.T) {
	for _, scenario := range []string{"both scopes", "zone query failure", "invalid success", "missing final", "cap", "field budget", "retention", "saturation", "invalid final", "missing success", "fractional success", "string final", "negative success", "entitlement", "irreducible", "empty", "destination bound"} {
		t.Run(scenario, func(t *testing.T) {
			from := time.Now().UTC().Truncate(5 * time.Minute).Add(-20 * time.Minute)
			queries := 0
			settings := cfapi.DatasetSettings{Enabled: true, AvailableFields: []string{"sum_uploads", "dimensions_datetimeFiveMinutes", "dimensions_jobId", "dimensions_destinationType", "dimensions_status", "dimensions_final", "dimensions_success"}, MaxNumberOfFields: 7, MaxPageSize: 4, MaxDuration: 300, NotOlderThan: 3600}
			if scenario == "field budget" {
				settings.MaxNumberOfFields = 6
			}
			if scenario == "entitlement" {
				settings.AvailableFields = settings.AvailableFields[:6]
			}
			if scenario == "irreducible" {
				settings.MaxPageSize = 3
			}
			if scenario == "retention" {
				settings.NotOlderThan = 60
			}
			if scenario == "saturation" {
				settings.MaxDuration = 3600
				settings.MaxPageSize = 4
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					if r.URL.Path != "/zones" {
						t.Errorf("unexpected REST path %s", r.URL.Path)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []any{map[string]any{"id": "synthetic-zone", "name": "example.com", "account": map[string]any{"id": "synthetic-account"}}, map[string]any{"id": "unowned-zone", "name": "other.example.com", "account": map[string]any{"id": "other-account"}}}, "result_info": map[string]any{"page": 1, "per_page": 50, "total_count": 2}})
					return
				}
				var request struct {
					Query string `json:"query"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				q := request.Query
				scope := "accounts"
				if strings.Contains(q, "zones(") {
					scope = "zones"
				}
				if strings.Contains(q, "unowned-zone") {
					t.Error("queried unowned zone")
				}
				var node any
				if strings.Contains(q, "settings{") {
					node = map[string]any{"settings": map[string]any{logpushTestDataset: settings}}
				} else {
					queries++
					if strings.Contains(q, "error") || strings.Contains(q, "success:") || strings.Contains(q, "status:") || strings.Contains(q, "final:") {
						t.Errorf("unvalidated selection/filter: %s", q)
					}
					if scenario == "zone query failure" && scope == "zones" {
						_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": "synthetic failure"}}})
						return
					}
					_, tail, ok := strings.Cut(q, `datetime_geq:"`)
					if !ok {
						t.Error("missing window filter")
						return
					}
					stamp, _, _ := strings.Cut(tail, `"`)
					bucket, err := time.Parse(time.RFC3339, stamp)
					if err != nil {
						t.Error(err)
						return
					}
					_, tail, _ = strings.Cut(q, `datetime_lt:"`)
					endStamp, _, _ := strings.Cut(tail, `"`)
					end, _ := time.Parse(time.RFC3339, endStamp)
					var rows []any
					if scenario == "saturation" && end.Sub(bucket) > 5*time.Minute {
						rows = []any{failureHTTPRow(bucket, 0, 1, 503, 2), failureHTTPRow(bucket, 0, 1, 503, 2), failureHTTPRow(bucket, 0, 1, 503, 2), failureHTTPRow(bucket, 0, 1, 503, 2)}
					} else {
						success := any(0)
						final := any(1)
						if scenario == "invalid success" && scope == "zones" {
							success = 256
						}
						if scenario == "missing final" && scope == "zones" {
							final = nil
						}
						if scenario == "invalid final" && scope == "zones" {
							final = 2
						}
						if scope == "zones" {
							switch scenario {
							case "missing success":
								success = nil
							case "fractional success":
								success = 0.5
							case "string final":
								final = "1"
							case "negative success":
								success = -1
							}
						}
						rows = []any{failureHTTPRow(bucket, success, final, 503, 7), failureHTTPRow(bucket, 1, 1, 201, 50), failureHTTPRow(bucket, 0, 0, 200, 2)}
					}
					if scenario == "empty" {
						rows = []any{}
					}
					if scenario == "destination bound" {
						for _, raw := range rows {
							raw.(map[string]any)["dimensions"].(map[string]any)["destinationType"] = strings.Repeat("x", 200)
						}
					}
					node = map[string]any{logpushTestDataset: rows}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{scope: []any{node}}}})
			}))
			defer server.Close()
			cfg := logpushTestConfig()
			cfg.Cloudflare.APIBase = server.URL
			cfg.Collectors["logpush.health"] = config.CollectorConfig{Enabled: false}
			cfg.Collectors["logpush.failures"] = config.CollectorConfig{Enabled: true, Interval: 5 * time.Minute}
			if scenario == "cap" {
				cfg.Platform.MaxMetricSeriesPerWindow = 1
			}
			registry := collector.NewRegistry()
			Register(collector.Deps{Config: cfg, API: cfapi.New(cfg.Cloudflare), Registry: registry})
			var window collector.WindowCollector
			for _, entry := range registry.Entries() {
				if entry.Collector.Name() == "logpush.failures" {
					window, _ = entry.Collector.(collector.WindowCollector)
				}
			}
			if window == nil {
				t.Fatal("unknown collector: enabled logpush.failures was not registered")
			}
			out := &telemetry.Buffer{}
			mark, err := window.CollectWindow(context.Background(), from, from.Add(10*time.Minute), out)
			if scenario == "both scopes" || scenario == "cap" || scenario == "saturation" || scenario == "empty" || scenario == "destination bound" {
				if err != nil || !mark.Equal(from.Add(10*time.Minute)) {
					t.Fatalf("mark=%s error=%v", mark, err)
				}
				want := 4
				if scenario == "empty" {
					want = 0
				}
				if scenario == "cap" {
					want = 1
				}
				if len(out.Metrics) != want {
					t.Fatalf("metrics=%+v want %d", out.Metrics, want)
				}
				scopes := map[string]bool{}
				for _, m := range out.Metrics {
					if m.Name != semconv.MetricLogpushFailedUploads || m.Kind != "counter" {
						t.Fatalf("wrong metric %+v", m)
					}
					attrs := map[string]string{}
					for _, a := range m.Attrs {
						attrs[a.Key] = a.Value
						if strings.Contains(a.Value, "synthetic-") || a.Value == "unowned-zone" {
							t.Fatalf("source ID leaked %+v", m)
						}
					}
					if scenario == "destination bound" && len(attrs[semconv.AttrLogpushDestinationType]) != 128 {
						t.Fatalf("destination not bounded: %+v", m)
					}
					scope := attrs[semconv.AttrLogpushScope]
					scopes[scope] = true
					if scope == "zone" && attrs[semconv.AttrLogpushZone] != "example.com" {
						t.Fatalf("zone name missing %+v", m)
					}
					if attrs[semconv.AttrLogpushJobID] != "18446744073709551615" {
						t.Fatalf("uint64 job lost precision %+v", m)
					}
					if attrs[semconv.AttrLogpushStatusCode] == "201" {
						t.Fatal("success=1 counted as failure")
					}
					wantValue := float64(14)
					if attrs[semconv.AttrLogpushFinalAttempt] == "false" {
						wantValue = 4
					}
					if m.Value != wantValue {
						t.Fatalf("source upload sum=%v want %v", m.Value, wantValue)
					}
				}
				if scenario != "cap" && scenario != "empty" && (!scopes["account"] || !scopes["zone"]) {
					t.Fatalf("scopes=%v", scopes)
				}
				if queries == 0 {
					t.Fatal("no data queried")
				}
			} else if err == nil || !mark.Equal(from) || len(out.Metrics) > 0 {
				t.Fatalf("failure emitted/advanced: mark=%s err=%v metrics=%+v", mark, err, out.Metrics)
			}
		})
	}
}

func failureHTTPRow(bucket time.Time, success, final, status, uploads any) map[string]any {
	return map[string]any{"dimensions": map[string]any{"datetimeFiveMinutes": bucket.Format(time.RFC3339), "jobId": json.Number("18446744073709551615"), "destinationType": "S3", "success": success, "final": final, "status": status}, "sum": map[string]any{"uploads": uploads}}
}
