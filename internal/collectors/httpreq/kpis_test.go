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

func TestRegisteredTransferPeriodsAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name, at, raw             string
		duration, retention       int64
		fields                    []string
		wantBytes, wantProjection float64
		wantPosts                 int
		wantErr                   bool
	}{
		{name: "leap-February", at: "2024-02-02T00:10:00Z", raw: `[{"sum":{"edgeResponseBytes":288}}]`, wantBytes: 288, wantProjection: 288 * 29 * 86400.0 / 86700, wantPosts: 1},
		{name: "April-duration-splits", at: "2026-04-02T00:10:00Z", raw: `[{"sum":{"edgeResponseBytes":10}}]`, duration: 43200, wantBytes: 30, wantProjection: 30 * 30 * 86400.0 / 86700, wantPosts: 3},
		{name: "empty-is-zero", at: "2026-04-02T00:10:00Z", raw: `[]`, wantPosts: 1},
		{name: "true-zero", at: "2026-04-02T00:10:00Z", raw: `[{"sum":{"edgeResponseBytes":0}}]`, wantPosts: 1},
		{name: "no-elapsed", at: "2026-05-01T00:04:59Z"},
		{name: "null-dataset", at: "2026-04-02T00:10:00Z", raw: `null`, wantPosts: 1, wantErr: true},
		{name: "null-value", at: "2026-04-02T00:10:00Z", raw: `[{"sum":{"edgeResponseBytes":null}}]`, wantPosts: 1, wantErr: true},
		{name: "missing-value", at: "2026-04-02T00:10:00Z", raw: `[{"sum":{}}]`, wantPosts: 1, wantErr: true},
		{name: "missing-entitlement", at: "2026-04-02T00:10:00Z", fields: []string{"sum_visits"}, wantErr: true},
		{name: "retention-cannot-backfill", at: "2026-04-02T00:10:00Z", retention: 3600, wantErr: true},
		{name: "upstream-error", at: "2026-04-02T00:10:00Z", raw: `error`, wantPosts: 1, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Cloudflare.AccountID = "account-fixture"
			cfg.HTTP.RequestSource = "all"
			fields := tc.fields
			if fields == nil {
				fields = []string{"sum_edgeResponseBytes"}
			}
			now, _ := time.Parse(time.RFC3339, tc.at)
			posts := 0
			coveredTo := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Query string `json:"query"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				q := body.Query
				node := map[string]any{}
				if strings.Contains(q, "settings{") {
					node["settings"] = map[string]any{"httpRequestsAdaptiveGroups": cfapi.DatasetSettings{Enabled: true, AvailableFields: fields, MaxDuration: tc.duration, NotOlderThan: tc.retention, MaxNumberOfFields: 1, MaxPageSize: 10000}}
				} else {
					posts++
					if !strings.Contains(q, `requestSource:"eyeball"`) || !strings.Contains(q, "accounts(") || strings.Contains(q, "clientRequestHTTPHost") {
						t.Errorf("incorrect transfer selection: %s", q)
					}
					bounds := regexp.MustCompile(`datetime_(?:geq|lt):"([^"]+)"`).FindAllStringSubmatch(q, -1)
					if len(bounds) != 2 {
						t.Errorf("missing bounds: %s", q)
					} else {
						from, _ := time.Parse(time.RFC3339, bounds[0][1])
						to, _ := time.Parse(time.RFC3339, bounds[1][1])
						start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
						if !from.Equal(coveredTo) || !to.After(from) {
							t.Errorf("transfer coverage gap/overlap: from=%s previous end=%s to=%s", from, coveredTo, to)
						}
						coveredTo = to
						if from.Before(start) || to.After(now.Add(-5*time.Minute).Truncate(5*time.Minute)) || tc.duration > 0 && to.Sub(from) > time.Duration(tc.duration)*time.Second {
							t.Errorf("invalid complete-period bounds: %s", q)
						}
					}
					if tc.raw == "error" {
						_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": "fixture failure"}}})
						return
					}
					node["kpi"] = json.RawMessage(tc.raw)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"accounts": []any{node}}}})
			}))
			defer srv.Close()
			cfg.Cloudflare.APIBase = srv.URL
			cfg.Cloudflare.APIToken = "fixture"
			reg := collector.NewRegistry()
			Register(collector.Deps{Config: &cfg, API: cfapi.New(cfg.Cloudflare), Registry: reg})
			found := false
			for _, entry := range reg.Entries() {
				if entry.Collector.Name() != "httpreq.transfer" {
					continue
				}
				found = true
				c := entry.Collector.(*transfer)
				c.now = func() time.Time { return now.In(time.FixedZone("fixture", 3600)) }
				e := &fakeEmitter{}
				err := c.Collect(context.Background(), e)
				if (err != nil) != tc.wantErr {
					t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
				}
				if tc.wantErr && len(e.gauges) != 0 {
					t.Fatalf("failed source emitted partial/zero snapshot: %+v", e.gauges)
				}
				if !tc.wantErr {
					if tc.wantPosts > 0 && !coveredTo.Equal(now.Add(-5*time.Minute).Truncate(5*time.Minute)) {
						t.Fatalf("transfer coverage did not reach complete-period end: %s", coveredTo)
					}
					if len(e.gauges) != 2 {
						t.Fatalf("missing snapshot: %+v", e.gauges)
					}
					seen := map[string]bool{}
					for _, point := range e.gauges {
						if seen[point.name] {
							t.Fatalf("duplicate gauge: %s", point.name)
						}
						seen[point.name] = true
						want := tc.wantBytes
						switch point.name {
						case semconv.MetricHTTPAccountTransferProjection:
							want = tc.wantProjection
						case semconv.MetricHTTPAccountTransferMTD:
						default:
							t.Fatalf("unexpected gauge: %s", point.name)
						}
						if diff := point.value - want; diff > 1e-8 || diff < -1e-8 || len(point.attrs) != 0 {
							t.Fatalf("snapshot=%+v want=%g", point, want)
						}
					}
					// Same collector must actively replace the previous month's nonzero
					// snapshot when the current month has no complete elapsed period.
					now = time.Date(now.Year(), now.Month()+1, 1, 0, 4, 59, 0, time.UTC)
					reset := &fakeEmitter{}
					if err := c.Collect(context.Background(), reset); err != nil {
						t.Fatal(err)
					}
					if len(reset.gauges) != 2 || reset.gauges[0].value != 0 || reset.gauges[1].value != 0 {
						t.Fatalf("month rollover retained old snapshot: %+v", reset.gauges)
					}
				}
			}
			if !found {
				t.Fatal("transfer not registered")
			}
			if posts != tc.wantPosts {
				t.Fatalf("data posts=%d want %d", posts, tc.wantPosts)
			}
		})
	}
}

func TestRegisteredThreatsSchedulerFullHours(t *testing.T) {
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "account-fixture"
	cfg.HTTP.MetricsScope = "all"
	var queried []cfapi.GraphQLRequest
	api := &kpiAPI{settings: cfapi.DatasetSettings{Enabled: true, AvailableFields: []string{"sum_threats", "dimensions_datetime"}, MaxNumberOfFields: 2, MaxDuration: 3600}, batch: func(q cfapi.GraphQLRequest) json.RawMessage {
		queried = append(queried, q)
		if q.Filter["requestSource"] != nil || q.From.Minute() != 0 || q.To.Minute() != 0 || q.To.Sub(q.From) != time.Hour {
			t.Fatalf("partial/restricted rollup: %+v", q)
		}
		raw, _ := json.Marshal([]any{map[string]any{"sum": map[string]any{"threats": 2}, "dimensions": map[string]any{"datetime": q.From.Format(time.RFC3339)}}})
		return raw
	}}
	reg := collector.NewRegistry()
	Register(collector.Deps{Config: &cfg, API: api, Registry: reg})
	store, err := collector.NewFileStore(t.TempDir() + "/checkpoints.json")
	if err != nil {
		t.Fatal(err)
	}
	e := &fakeEmitter{}
	scheduler := collector.NewScheduler(nil, e, store)
	now := time.Date(2026, 9, 30, 10, 9, 59, 0, time.UTC)
	calls := 0
	scheduler.Now = func() time.Time { calls++; return now }
	for _, entry := range reg.Entries() {
		if entry.Collector.Name() != "httpreq.threats" {
			continue
		}
		for _, at := range []time.Time{now, now.Add(time.Second), now.Add(20 * time.Second)} {
			now = at
			if err := scheduler.RunOnce(context.Background(), entry); err != nil {
				t.Fatal(err)
			}
		}
	}
	if calls != 3 || len(queried) != 2 {
		t.Fatalf("clock calls=%d queries=%d want 3 instants and 2 distinct hours", calls, len(queried))
	}
	if !queried[0].From.Equal(time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)) || !queried[1].From.Equal(queried[0].To) {
		t.Fatalf("hour holdback/repeat: %+v", queried)
	}
	mark, ok := store.Get("httpreq.threats")
	if !ok || !mark.Equal(time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("hour checkpoint=%s", mark)
	}
}

func TestRegisteredThreatsFailureAndEmpty(t *testing.T) {
	from := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, raw string
		budget    int
		duration  int64
		missing   bool
		wantErr   bool
	}{
		{name: "empty", raw: `[]`, budget: 2},
		{name: "true-zero", raw: `[{"sum":{"threats":0},"dimensions":{"datetime":"2026-09-30T09:00:00Z"}}]`, budget: 2},
		{name: "null", raw: `null`, budget: 2, wantErr: true},
		{name: "missing-sum", raw: `[{"dimensions":{"datetime":"2026-09-30T09:00:00Z"}}]`, budget: 2, wantErr: true},
		{name: "partial-hour", raw: `[{"sum":{"threats":2},"dimensions":{"datetime":"2026-09-30T09:15:00Z"}}]`, budget: 2, wantErr: true},
		{name: "duplicate-hour", raw: `[{"sum":{"threats":2},"dimensions":{"datetime":"2026-09-30T09:00:00Z"}},{"sum":{"threats":2},"dimensions":{"datetime":"2026-09-30T09:00:00Z"}}]`, budget: 2, wantErr: true},
		{name: "field-budget", budget: 1, wantErr: true},
		{name: "duration", budget: 2, duration: 1800, wantErr: true},
		{name: "missing-entitlement", budget: 2, missing: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Cloudflare.AccountID = "account-fixture"
			cfg.HTTP.MetricsScope = "all"
			fields := []string{"sum_threats", "dimensions_datetime"}
			if tc.missing {
				fields = fields[1:]
			}
			posts := 0
			api := &kpiAPI{settings: cfapi.DatasetSettings{Enabled: true, AvailableFields: fields, MaxNumberOfFields: tc.budget, MaxDuration: tc.duration}, batch: func(cfapi.GraphQLRequest) json.RawMessage { posts++; return json.RawMessage(tc.raw) }}
			reg := collector.NewRegistry()
			Register(collector.Deps{Config: &cfg, API: api, Registry: reg})
			for _, entry := range reg.Entries() {
				if entry.Collector.Name() != "httpreq.threats" {
					continue
				}
				e := &fakeEmitter{}
				mark, err := entry.Collector.(collector.WindowCollector).CollectWindow(context.Background(), from, from.Add(time.Hour), e)
				if (err != nil) != tc.wantErr {
					t.Fatalf("err=%v", err)
				}
				if tc.wantErr && (!mark.Equal(from) || len(e.counts) != 0) {
					t.Fatalf("failure advanced/emitted: %s %+v", mark, e.counts)
				}
				if !tc.wantErr && (len(e.counts) != 1 || e.counts[0].value != 0 || !mark.Equal(from.Add(time.Hour))) {
					t.Fatalf("legitimate zero lost: %s %+v", mark, e.counts)
				}
				if tc.budget < 2 || tc.duration > 0 || tc.missing {
					if posts != 0 {
						t.Fatalf("invalid entitlement fetched %d times", posts)
					}
				}
			}
		})
	}
}

type kpiAPI struct {
	fakeAPI
	settings cfapi.DatasetSettings
	batch    func(cfapi.GraphQLRequest) json.RawMessage
}

func (f *kpiAPI) DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error) {
	return f.settings, nil
}
func (f *kpiAPI) QueryBatch(_ context.Context, qs []cfapi.GraphQLBatchSelection) (map[string]json.RawMessage, error) {
	return map[string]json.RawMessage{"kpi": f.batch(qs[0].Request)}, nil
}
