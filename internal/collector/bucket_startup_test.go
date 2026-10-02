package collector_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/collectors/d1"
	"github.com/rknightion/cf2otel/internal/collectors/durableobjects"
	"github.com/rknightion/cf2otel/internal/collectors/email"
	"github.com/rknightion/cf2otel/internal/collectors/healthchecks"
	"github.com/rknightion/cf2otel/internal/collectors/kv"
	"github.com/rknightion/cf2otel/internal/collectors/logpush"
	"github.com/rknightion/cf2otel/internal/collectors/queues"
	"github.com/rknightion/cf2otel/internal/collectors/r2"
	"github.com/rknightion/cf2otel/internal/collectors/turnstile"
	"github.com/rknightion/cf2otel/internal/collectors/workers"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

// Each fixture advertises only supported fields, including the name resolution
// dimensions required by the registered platform collectors.
func TestBucketCollectorsRestartBeforeNextCompleteBucket(t *testing.T) {
	cases := []struct {
		name, dataset, fields string
		register              func(collector.Deps)
	}{
		{"d1.analytics", "d1AnalyticsAdaptiveGroups", "sum.readQueries sum.writeQueries dimensions.databaseId", d1.Register},
		{"d1.queries", "d1QueriesAdaptiveGroups", "count dimensions.databaseId", d1.Register},
		{"d1.storage", "d1StorageAdaptiveGroups", "max.databaseSizeBytes dimensions.databaseId", d1.Register},
		{"durableobjects.invocations", "durableObjectsInvocationsAdaptiveGroups", "sum.requests dimensions.namespaceId", durableobjects.Register},
		{"durableobjects.periodic", "durableObjectsPeriodicGroups", "sum.subrequests dimensions.namespaceId", durableobjects.Register},
		{"durableobjects.sql_storage", "durableObjectsSqlStorageGroups", "max.storedBytes dimensions.namespaceId", durableobjects.Register},
		{"durableobjects.subrequests", "durableObjectsSubrequestsAdaptiveGroups", "sum.requestBodySizeUncached dimensions.namespaceId", durableobjects.Register},
		{"email.routing", "emailRoutingAdaptiveGroups", "count", email.Register},
		{"email.sending", "emailSendingAdaptiveGroups", "count", email.Register},
		{"healthchecks.events", "healthCheckEventsAdaptiveGroups", "count dimensions.healthStatus dimensions.failureReason dimensions.fqdn dimensions.healthCheckName avg.rttMs avg.timeToFirstByteMs avg.tcpConnMs avg.tlsHandshakeMs", healthchecks.Register},
		{"kv.operations", "kvOperationsAdaptiveGroups", "sum.requests dimensions.namespaceId", kv.Register},
		{"kv.storage", "kvStorageAdaptiveGroups", "max.byteCount max.keyCount dimensions.namespaceId", kv.Register},
		{"logpush.failures", "logpushHealthAdaptiveGroups", "sum.uploads sum.records dimensions.jobId dimensions.destinationType dimensions.status dimensions.final dimensions.success", logpush.Register},
		{"logpush.health", "logpushHealthAdaptiveGroups", "sum.uploads sum.records", logpush.Register},
		{"queues.backlog", "queueBacklogAdaptiveGroups", "avg.messages avg.bytes dimensions.queueId", queues.Register},
		{"queues.consumer", "queueConsumerMetricsAdaptiveGroups", "avg.concurrency dimensions.queueId", queues.Register},
		{"queues.delayed_backlog", "queueDelayedBacklogAdaptiveGroups", "avg.messages dimensions.queueId", queues.Register},
		{"queues.message_operations", "queueMessageOperationsAdaptiveGroups", "count sum.billableOperations dimensions.queueId", queues.Register},
		{"r2.bandwidth", "r2BandwidthUsageAdaptiveGroups", "sum.bytesDownload sum.bytesUpload", r2.Register},
		{"r2.catalog_data", "r2CatalogDataOperationsAdaptiveGroups", "count", r2.Register},
		{"r2.catalog_maintenance", "r2CatalogTableMaintenanceAdaptiveGroups", "count", r2.Register},
		{"r2.operations", "r2OperationsAdaptiveGroups", "sum.requests", r2.Register},
		{"r2.sql", "r2sqlOperationsAdaptiveGroups", "count", r2.Register},
		{"r2.storage", "r2StorageAdaptiveGroups", "max.payloadSize max.objectCount", r2.Register},
		{"turnstile.events", "turnstileAdaptiveGroups", "count", turnstile.Register},
		{"workers.invocations", "workersInvocationsAdaptive", "sum.requests sum.errors sum.subrequests dimensions.scriptName dimensions.status", workers.Register},
		{"workers.overview", "workersOverviewRequestsAdaptiveGroups", "count", workers.Register},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Recent UTC timestamps exercise the real client's retention validation.
			boundary := time.Now().UTC().Add(-10 * time.Minute).Truncate(5 * time.Minute)
			now := boundary.Add(13*time.Minute + 56*time.Second)
			fields := append(strings.Fields(tc.fields), "dimensions.datetimeFiveMinutes")
			dataCalls, restCalls := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					restCalls++
					var rows []any
					switch r.URL.Path {
					case "/zones":
						rows = []any{map[string]any{"id": "zone-opaque", "name": "zone-fixture", "account": map[string]any{"id": "account-opaque"}}}
					case "/accounts/account-opaque/d1/database":
						rows = []any{map[string]any{"uuid": "resource-opaque", "name": "resource-fixture"}}
					case "/accounts/account-opaque/storage/kv/namespaces":
						rows = []any{map[string]any{"id": "resource-opaque", "title": "resource-fixture"}}
					case "/accounts/account-opaque/queues":
						rows = []any{map[string]any{"queue_id": "resource-opaque", "queue_name": "resource-fixture"}}
					case "/accounts/account-opaque/workers/durable_objects/namespaces":
						rows = []any{map[string]any{"id": "resource-opaque", "name": "resource-fixture"}}
					default:
						t.Errorf("unexpected REST path %s", r.URL.Path)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					page := 1
					if raw := r.URL.Query().Get("page"); raw != "" {
						var err error
						page, err = strconv.Atoi(raw)
						if err != nil || page < 1 {
							t.Errorf("invalid REST page %q", raw)
							w.WriteHeader(http.StatusBadRequest)
							return
						}
					}
					// This one-row fixture is exhausted after the first page.
					if page > 1 {
						rows = []any{}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": rows, "result_info": map[string]any{"page": page, "per_page": 1, "count": len(rows), "total_count": 1, "total_pages": 1}})
					return
				}
				if r.Method != http.MethodPost || r.URL.Path != "/graphql" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
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
				scope := "accounts"
				if strings.Contains(q, "zones(") {
					scope = "zones"
				}
				node := map[string]any{}
				if strings.Contains(q, "settings{") {
					available := make([]string, len(fields))
					for i, f := range fields {
						available[i] = strings.ReplaceAll(f, ".", "_")
					}
					node["settings"] = map[string]any{tc.dataset: map[string]any{"enabled": true, "availableFields": available, "maxNumberOfFields": len(fields), "maxPageSize": 100, "maxDuration": 3600, "notOlderThan": 86400}}
				} else {
					dataCalls++
					if !strings.Contains(q, `datetime_geq:"`+boundary.Format(time.RFC3339)+`"`) || !strings.Contains(q, `datetime_lt:"`+boundary.Add(5*time.Minute).Format(time.RFC3339)+`"`) {
						t.Errorf("data request did not cover exactly the retained next bucket: %s", q)
					}
					row := map[string]any{}
					for _, f := range fields {
						group, key, nested := strings.Cut(f, ".")
						var value any = 7
						if group == "dimensions" {
							switch key {
							case "datetimeFiveMinutes":
								value = boundary.Format(time.RFC3339)
							case "healthStatus":
								value = "healthy"
							case "failureReason":
								value = "none"
							case "success", "final":
								value = 0
							case "jobId":
								value = 1
							case "status":
								if tc.name == "logpush.failures" {
									value = 503
								} else {
									value = "failure"
								}
							case "destinationType":
								value = "s3"
							default:
								value = "resource-opaque"
							}
						}
						if nested {
							m, ok := row[group].(map[string]any)
							if !ok {
								m = map[string]any{}
								row[group] = m
							}
							m[key] = value
						} else {
							row[group] = value
						}
					}
					// Ordinary Query uses the dataset as its key; QueryBatch uses aliases.
					matches := regexp.MustCompile(`(?:(\w+):)?`+tc.dataset+`\(limit:`).FindAllStringSubmatch(q, -1)
					if len(matches) == 0 {
						t.Errorf("unexpected data selection: %s", q)
					}
					for _, m := range matches {
						key := m[1]
						if key == "" {
							key = tc.dataset
						}
						node[key] = []any{row}
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{scope: []any{node}}}})
			}))
			defer server.Close()
			cfg := config.Default()
			cfg.Cloudflare.AccountID = "account-opaque"
			cfg.Cloudflare.APIBase = server.URL
			for name, c := range cfg.Collectors {
				c.Enabled = name == tc.name
				cfg.Collectors[name] = c
			}
			c := cfg.Collectors[tc.name]
			c.Enabled = true
			c.Interval = 5 * time.Minute
			c.InitialLookback = 30 * time.Minute
			c.MaxWindow = time.Hour
			cfg.Collectors[tc.name] = c
			registry := collector.NewRegistry()
			tc.register(collector.Deps{Config: &cfg, API: cfapi.New(cfg.Cloudflare), Registry: registry})
			entries := registry.Entries()
			if len(entries) != 1 || entries[0].Collector.Name() != tc.name {
				t.Fatalf("registered entries = %v, want %s only", entries, tc.name)
			}
			path := filepath.Join(t.TempDir(), "state.json")
			initial, err := collector.NewFileStore(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = initial.Set(tc.name, boundary); err != nil {
				t.Fatal(err)
			}
			store, err := collector.NewFileStore(path)
			if err != nil {
				t.Fatal(err)
			}
			out := &bucketStartupEmitter{Emitter: telemetry.NewNoopEmitter()}
			scheduler := collector.NewScheduler(nil, out, store)
			scheduler.Now = func() time.Time { return now }
			if err = scheduler.RunOnce(context.Background(), entries[0]); err != nil {
				t.Fatalf("restart before next complete bucket: %v", err)
			}
			if dataCalls != 0 || restCalls != 0 || len(out.values) != 0 {
				t.Fatalf("premature collection: data=%d REST=%d values=%v", dataCalls, restCalls, out.values)
			}
			if mark, ok := store.Get(tc.name); !ok || !mark.Equal(boundary) {
				t.Fatalf("startup cursor = %s, want retained %s", mark, boundary)
			}
			now = now.Add(5 * time.Minute)
			if err = scheduler.RunOnce(context.Background(), entries[0]); err != nil {
				t.Fatal(err)
			}
			if dataCalls == 0 {
				t.Fatal("closed bucket was not queried")
			}
			found := false
			for _, v := range out.values {
				if v == 7 {
					found = true
				}
			}
			if !found {
				t.Fatalf("fixture value 7 not emitted: %v", out.values)
			}
			reopened, err := collector.NewFileStore(path)
			if err != nil {
				t.Fatal(err)
			}
			if mark, ok := reopened.Get(tc.name); !ok || !mark.Equal(boundary.Add(5*time.Minute)) {
				t.Fatalf("persisted cursor = %s, want next closed bucket", mark)
			}
			// A later early poll is also a no-op, not just the restart poll.
			calls, rest, points := dataCalls, restCalls, len(out.values)
			now = now.Add(time.Minute)
			if err = scheduler.RunOnce(context.Background(), entries[0]); err != nil {
				t.Fatal(err)
			}
			if dataCalls != calls || restCalls != rest || len(out.values) != points {
				t.Fatal("early subsequent poll duplicated collection")
			}
			if mark, ok := store.Get(tc.name); !ok || !mark.Equal(boundary.Add(5*time.Minute)) {
				t.Fatalf("early subsequent poll advanced cursor to %s", mark)
			}
		})
	}
}

type bucketStartupEmitter struct {
	telemetry.Emitter
	values []float64
}

func (e *bucketStartupEmitter) Gauge(_ context.Context, n string, v float64, _ ...telemetry.Attr) error {
	if strings.HasPrefix(n, "cloudflare.") {
		e.values = append(e.values, v)
	}
	return nil
}
func (e *bucketStartupEmitter) Counter(_ context.Context, n string, v float64, _ ...telemetry.Attr) error {
	if strings.HasPrefix(n, "cloudflare.") {
		e.values = append(e.values, v)
	}
	return nil
}
