package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

const coverageFixtureAccount = "account-fixture"

// A closed five-minute bucket well inside the fixture retention.
var coverageBucket = time.Date(2026, 9, 26, 16, 40, 0, 0, time.UTC)

type coverageLogRow struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

type coverageAPI struct {
	settings cfapi.DatasetSettings
	groups   []map[string]any
	logs     map[string][]time.Time
	// pages, when set for a gateway, is served verbatim by page number.
	pages       map[string][][]coverageLogRow
	queries     []cfapi.GraphQLRequest
	restQueries []string
}

func (f *coverageAPI) Get(_ context.Context, path string, q url.Values, out any) error {
	prefix := "/accounts/" + coverageFixtureAccount + "/ai-gateway/gateways/"
	gateway, ok := strings.CutSuffix(strings.TrimPrefix(path, prefix), "/logs")
	if !strings.HasPrefix(path, prefix) || !ok {
		return fmt.Errorf("unexpected REST path %q", path)
	}
	start, err := time.Parse(time.RFC3339Nano, q.Get("start_date"))
	if err != nil {
		return err
	}
	end, err := time.Parse(time.RFC3339Nano, q.Get("end_date"))
	if err != nil {
		return err
	}
	f.restQueries = append(f.restQueries, gateway+" "+q.Get("page")+" "+start.Format(time.RFC3339)+".."+end.Format(time.RFC3339))
	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if page < 1 || perPage < 1 || perPage > 50 {
		return fmt.Errorf("invalid paging %q", q.Encode())
	}
	if pages, ok := f.pages[gateway]; ok {
		served := []coverageLogRow{}
		if page <= len(pages) {
			served = pages[page-1]
		}
		encoded, err := json.Marshal(served)
		if err != nil {
			return err
		}
		return json.Unmarshal(encoded, out)
	}
	// The live endpoint treats both bounds as inclusive.
	rows := make([]coverageLogRow, 0)
	for i, at := range f.logs[gateway] {
		if !at.Before(start) && !at.After(end) {
			rows = append(rows, coverageLogRow{ID: fmt.Sprintf("%s-log-%03d", gateway, i), CreatedAt: at})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].CreatedAt.Before(rows[j].CreatedAt) })
	first := (page - 1) * perPage
	if first > len(rows) {
		first = len(rows)
	}
	last := min(first+perPage, len(rows))
	encoded, err := json.Marshal(rows[first:last])
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, out)
}
func (f *coverageAPI) Query(_ context.Context, request cfapi.GraphQLRequest, out any) error {
	f.queries = append(f.queries, request)
	encoded, err := json.Marshal(f.groups)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, out)
}
func (f *coverageAPI) DatasetSettings(_ context.Context, scope cfapi.Scope, scopeID, dataset string) (cfapi.DatasetSettings, error) {
	if scope != cfapi.AccountScope || scopeID != coverageFixtureAccount || dataset != "aiGatewayRequestsAdaptiveGroups" {
		return cfapi.DatasetSettings{}, fmt.Errorf("unexpected settings request %s %s %s", scope, scopeID, dataset)
	}
	return f.settings, nil
}
func (*coverageAPI) Accounts(context.Context) ([]cfapi.Account, error) {
	return nil, errors.New("unexpected account discovery")
}
func (*coverageAPI) Zones(context.Context) ([]cfapi.Zone, error) {
	return nil, errors.New("unexpected zone discovery")
}
func (*coverageAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) {
	return nil, errors.New("unexpected gateway discovery")
}

func coverageSettings() cfapi.DatasetSettings {
	return cfapi.DatasetSettings{
		Enabled:           true,
		AvailableFields:   []string{"count", "dimensions_gateway", "dimensions_datetimeFiveMinutes", "dimensions_model"},
		MaxNumberOfFields: 30,
		MaxDuration:       2764800,
		NotOlderThan:      5356800,
		MaxPageSize:       10000,
	}
}
func coverageConfig(gateways ...string) *config.Config {
	cfg := config.Default()
	cfg.Cloudflare.AccountID = coverageFixtureAccount
	cfg.AIGateway.Gateways = gateways
	return &cfg
}
func groupsRow(gateway string, count float64) map[string]any {
	return map[string]any{"count": count, "dimensions": map[string]any{"gateway": gateway}}
}
func newTestCoverage(api *coverageAPI, now time.Time, gateways ...string) *coverage {
	c := NewCoverage(coverageConfig(gateways...), api)
	c.now = func() time.Time { return now }
	return c
}
func gapByGateway(t *testing.T, metrics []telemetry.BufferedMetric) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	for _, m := range metrics {
		if m.Name != semconv.MetricAIGatewayLogCoverageGap || m.Kind != "gauge" || len(m.Attrs) != 1 || m.Attrs[0].Key != semconv.AttrAIGatewayName {
			t.Fatalf("unexpected coverage metric %+v", m)
		}
		if _, dup := out[m.Attrs[0].Value]; dup {
			t.Fatalf("gateway %q reported twice in one window", m.Attrs[0].Value)
		}
		out[m.Attrs[0].Value] = m.Value
	}
	return out
}

func TestCoverageGapIsGroupsMinusRESTPerGateway(t *testing.T) {
	b := coverageBucket
	api := &coverageAPI{
		settings: coverageSettings(),
		groups: []map[string]any{
			groupsRow("example-gateway", 3),
			groupsRow("example-missing-logs", 5),
			groupsRow("example-unconfigured", 7),
		},
		logs: map[string][]time.Time{
			// The row at the bucket end belongs to the next window.
			"example-gateway":      {b, b.Add(time.Minute), b.Add(5*time.Minute - time.Second), b.Add(5 * time.Minute)},
			"example-missing-logs": {b.Add(time.Minute), b.Add(2 * time.Minute)},
			"example-behind":       {b, b.Add(time.Minute), b.Add(2 * time.Minute), b.Add(3 * time.Minute)},
		},
	}
	c := newTestCoverage(api, b.Add(20*time.Minute), "example-gateway", "example-missing-logs", "example-behind")
	out := &telemetry.Buffer{}
	mark, err := c.CollectWindow(context.Background(), b, b.Add(5*time.Minute), out)
	if err != nil || !mark.Equal(b.Add(5*time.Minute)) {
		t.Fatalf("mark=%s err=%v, want bucket end", mark, err)
	}
	got := gapByGateway(t, out.Metrics)
	want := map[string]float64{"example-gateway": 0, "example-missing-logs": 3, "example-behind": -4}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("coverage gaps = %v, want equal 0, missing-logs 3, GraphQL-behind -4 and no unconfigured gateway", got)
	}
	if len(api.queries) != 1 {
		t.Fatalf("GraphQL queries = %d, want one", len(api.queries))
	}
	q := api.queries[0]
	if q.Scope != cfapi.AccountScope || q.ScopeID != coverageFixtureAccount || q.Dataset != "aiGatewayRequestsAdaptiveGroups" || !q.From.Equal(b) || !q.To.Equal(b.Add(5*time.Minute)) {
		t.Fatalf("GraphQL request = %+v, want account-scoped Groups for the closed bucket", q)
	}
	if fmt.Sprint(q.WantedFields) != "[count dimensions.gateway]" || q.Limit <= 0 || q.Limit > 10000 {
		t.Fatalf("GraphQL selection = %v limit %d, want only count and gateway within maxPageSize", q.WantedFields, q.Limit)
	}
}

func TestCoverageFailsClosedWhenSelectionIsNotAvailable(t *testing.T) {
	for _, missing := range []string{"count", "dimensions_gateway"} {
		settings := coverageSettings()
		settings.AvailableFields = nil
		for _, field := range coverageSettings().AvailableFields {
			if field != missing {
				settings.AvailableFields = append(settings.AvailableFields, field)
			}
		}
		api := &coverageAPI{settings: settings, logs: map[string][]time.Time{"example-gateway": {coverageBucket}}}
		c := newTestCoverage(api, coverageBucket.Add(20*time.Minute), "example-gateway")
		out := &telemetry.Buffer{}
		mark, err := c.CollectWindow(context.Background(), coverageBucket, coverageBucket.Add(5*time.Minute), out)
		if err == nil || !strings.Contains(err.Error(), strings.Replace(missing, "_", ".", 1)) {
			t.Fatalf("without %s: err=%v, want missing-field error", missing, err)
		}
		if !mark.Equal(coverageBucket) || len(api.queries) != 0 || len(api.restQueries) != 0 || len(out.Metrics) != 0 {
			t.Fatalf("without %s: mark=%s queries=%d rest=%d metrics=%d, want no progress", missing, mark, len(api.queries), len(api.restQueries), len(out.Metrics))
		}
	}
}

func TestCoverageRespectsMaxDurationAndNotOlderThan(t *testing.T) {
	b := coverageBucket
	short := coverageSettings()
	short.MaxDuration = 60
	api := &coverageAPI{settings: short}
	out := &telemetry.Buffer{}
	mark, err := newTestCoverage(api, b.Add(20*time.Minute), "example-gateway").CollectWindow(context.Background(), b, b.Add(5*time.Minute), out)
	if err == nil || !mark.Equal(b) || len(api.queries) != 0 || len(api.restQueries) != 0 || len(out.Metrics) != 0 {
		t.Fatalf("maxDuration below one bucket: mark=%s err=%v queries=%d rest=%d, want fail closed", mark, err, len(api.queries), len(api.restQueries))
	}

	old := coverageSettings()
	old.NotOlderThan = 3600
	api = &coverageAPI{settings: old}
	out = &telemetry.Buffer{}
	mark, err = newTestCoverage(api, b.Add(2*time.Hour), "example-gateway").CollectWindow(context.Background(), b, b.Add(5*time.Minute), out)
	var gap *cfapi.RetentionGapError
	if !errors.As(err, &gap) || gap.Floor.After(b.Add(2*time.Hour).Add(-time.Hour)) {
		t.Fatalf("bucket older than notOlderThan: err=%v, want retention gap at the Groups floor", err)
	}
	if !mark.Equal(b) || len(api.queries) != 0 || len(api.restQueries) != 0 || len(out.Metrics) != 0 {
		t.Fatalf("retention gap made progress: mark=%s queries=%d rest=%d metrics=%d", mark, len(api.queries), len(api.restQueries), len(out.Metrics))
	}
}

func TestCoverageCommitsOneClosedAlignedBucketPerWindow(t *testing.T) {
	b := coverageBucket
	api := &coverageAPI{
		settings: coverageSettings(),
		groups:   []map[string]any{groupsRow("example-gateway", 1)},
		logs:     map[string][]time.Time{"example-gateway": {b.Add(5 * time.Minute)}},
	}
	c := newTestCoverage(api, b.Add(time.Hour), "example-gateway")
	if c.Lag() < 10*time.Minute {
		t.Fatalf("lag %s, want at least ten minutes past Groups ingestion", c.Lag())
	}

	// An unaligned cursor only advances to the next bucket boundary.
	mark, err := c.CollectWindow(context.Background(), b.Add(-2*time.Minute-17*time.Second), b.Add(3*time.Minute), &telemetry.Buffer{})
	if err != nil || !mark.Equal(b) || len(api.queries) != 0 {
		t.Fatalf("unaligned cursor: mark=%s err=%v queries=%d, want alignment to %s without a query", mark, err, len(api.queries), b)
	}
	// An aligned cursor without a closed bucket cannot progress.
	if mark, err := c.CollectWindow(context.Background(), b, b.Add(4*time.Minute), &telemetry.Buffer{}); err == nil || !mark.Equal(b) || len(api.queries) != 0 {
		t.Fatalf("open bucket: mark=%s err=%v queries=%d, want no progress", mark, err, len(api.queries))
	}

	// Successive one-window calls (as the scheduler's catch-up loop makes,
	// each capped to the registered five-minute MaxWindow) commit one bucket
	// each, and adjacent buckets count the boundary row once.
	first, second := &telemetry.Buffer{}, &telemetry.Buffer{}
	mark, err = c.CollectWindow(context.Background(), b, b.Add(5*time.Minute), first)
	if err != nil || !mark.Equal(b.Add(5*time.Minute)) {
		t.Fatalf("first bucket mark=%s err=%v", mark, err)
	}
	mark, err = c.CollectWindow(context.Background(), mark, b.Add(10*time.Minute), second)
	if err != nil || !mark.Equal(b.Add(10*time.Minute)) {
		t.Fatalf("second bucket mark=%s err=%v", mark, err)
	}
	if g1, g2 := gapByGateway(t, first.Metrics)["example-gateway"], gapByGateway(t, second.Metrics)["example-gateway"]; g1 != 1 || g2 != 0 {
		t.Fatalf("boundary row gaps = %v then %v, want 1 then 0", g1, g2)
	}
	if len(api.queries) != 2 || !api.queries[0].To.Equal(api.queries[1].From) || api.queries[1].To.Sub(api.queries[1].From) != 5*time.Minute {
		t.Fatalf("GraphQL windows = %+v, want two adjacent five-minute buckets", api.queries)
	}
}

// TestCoverageRangeSpanningMultipleWindowsIsRejected covers a -since/-before
// CollectRange call: the scheduler always registers this collector with a
// five-minute MaxWindow (TestRegisterCoverageIsOffByDefault), so it never
// asks CollectWindow for more than one closed window itself. CollectRange
// passes the operator's raw range straight through in one call
// (internal/collector's CollectRange, frozen), so a range spanning two or
// more closed windows must be refused rather than silently committing only
// the first window while reporting success.
func TestCoverageRangeSpanningMultipleWindowsIsRejected(t *testing.T) {
	b := coverageBucket
	for _, tc := range []struct {
		name    string
		to      time.Time
		wantErr bool
	}{
		{"exactly one window", b.Add(5 * time.Minute), false},
		{"one window plus a trailing partial window", b.Add(9 * time.Minute), false},
		{"exactly two windows", b.Add(10 * time.Minute), true},
		{"two windows plus a trailing partial window", b.Add(14 * time.Minute), true},
		{"three windows", b.Add(15 * time.Minute), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &coverageAPI{
				settings: coverageSettings(),
				groups:   []map[string]any{groupsRow("example-gateway", 1)},
				logs:     map[string][]time.Time{"example-gateway": {b}},
			}
			c := newTestCoverage(api, tc.to.Add(20*time.Minute), "example-gateway")
			out := &telemetry.Buffer{}
			mark, err := c.CollectWindow(context.Background(), b, tc.to, out)
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "more than one closed") {
					t.Fatalf("to=%s: err=%v, want a multi-window range refusal", tc.to, err)
				}
				if !mark.Equal(b) || len(api.queries) != 0 || len(api.restQueries) != 0 || len(out.Metrics) != 0 {
					t.Fatalf("to=%s: mark=%s queries=%d rest=%d metrics=%d, want no progress and nothing exported", tc.to, mark, len(api.queries), len(api.restQueries), len(out.Metrics))
				}
				return
			}
			if err != nil || !mark.Equal(b.Add(5*time.Minute)) {
				t.Fatalf("to=%s: mark=%s err=%v, want the first window committed", tc.to, mark, err)
			}
		})
	}
}

func coverageValues(sink *telemetry.Buffer) []float64 {
	var values []float64
	for _, m := range sink.Metrics {
		if m.Name == semconv.MetricAIGatewayLogCoverageGap {
			values = append(values, m.Value)
		}
	}
	return values
}

type failOnceFlusher struct{ calls int }

func (*failOnceFlusher) BeginCommit() uint64 { return 0 }
func (f *failOnceFlusher) FlushCommit(context.Context, uint64) error {
	f.calls++
	if f.calls == 1 {
		return errors.New("export unavailable")
	}
	return nil
}

func TestCoverageRetryRecomputesTheSameWindow(t *testing.T) {
	b := coverageBucket
	cfg := coverageConfig("example-gateway")
	cfg.Collectors["aigateway.logs"] = config.CollectorConfig{}
	cfg.Collectors["aigateway.coverage"] = config.CollectorConfig{Enabled: true, Interval: 5 * time.Minute, InitialLookback: 30 * time.Minute, MaxWindow: time.Hour}
	api := &coverageAPI{
		settings: coverageSettings(),
		groups:   []map[string]any{groupsRow("example-gateway", 4)},
		logs:     map[string][]time.Time{"example-gateway": {b, b.Add(time.Minute), b.Add(6 * time.Minute)}},
	}
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: cfg, API: api, Registry: registry})
	entries := registry.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want the coverage collector only", entries)
	}
	cov := entries[0].Collector.(*coverage)
	now := b.Add(15*time.Minute + 30*time.Second)
	cov.now = func() time.Time { return now }
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "checkpoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("aigateway.coverage", b); err != nil {
		t.Fatal(err)
	}
	sink := &telemetry.Buffer{}
	s := collector.NewScheduler(registry, sink, store)
	s.Flusher = &failOnceFlusher{}
	s.Now = func() time.Time { return now }

	if err := s.RunOnce(context.Background(), entries[0]); err == nil {
		t.Fatal("first commit succeeded, want export failure")
	}
	if mark, _ := store.Get("aigateway.coverage"); !mark.Equal(b) || len(coverageValues(sink)) != 0 {
		t.Fatalf("failed commit advanced to %s or emitted %+v", mark, sink.Metrics)
	}
	// Each later tick commits one window: the retried one, then the next.
	for tick := 0; tick < 2; tick++ {
		now = now.Add(5 * time.Minute)
		if err := s.RunOnce(context.Background(), entries[0]); err != nil {
			t.Fatal(err)
		}
	}
	var windows []string
	for _, q := range api.queries {
		windows = append(windows, q.From.Format("15:04")+"-"+q.To.Format("15:04"))
	}
	if strings.Join(windows, ",") != "16:40-16:45,16:40-16:45,16:45-16:50" {
		t.Fatalf("GraphQL windows = %v, want the failed bucket retried before the next", windows)
	}
	if values := coverageValues(sink); fmt.Sprint(values) != "[2 3]" {
		t.Fatalf("committed gaps = %v, want the retried bucket once (4-2) then the next (4-1)", coverageValues(sink))
	}
	if mark, _ := store.Get("aigateway.coverage"); !mark.Equal(b.Add(10 * time.Minute)) {
		t.Fatalf("checkpoint = %s, want %s", mark, b.Add(10*time.Minute))
	}
}

func TestRegisterCoverageIsOffByDefault(t *testing.T) {
	cfg := coverageConfig("example-gateway")
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: cfg, API: &coverageAPI{}, Registry: registry})
	for _, e := range registry.Entries() {
		if e.Collector.Name() == "aigateway.coverage" {
			t.Fatal("aigateway.coverage registered with default configuration")
		}
	}
	cfg.Collectors["aigateway.coverage"] = config.CollectorConfig{Enabled: true, Interval: 15 * time.Minute, InitialLookback: 30 * time.Minute, MaxWindow: time.Hour}
	registry = collector.NewRegistry()
	Register(collector.Deps{Config: cfg, API: &coverageAPI{}, Registry: registry})
	var found bool
	for _, e := range registry.Entries() {
		if e.Collector.Name() == "aigateway.coverage" {
			found = true
			if _, ok := e.Collector.(collector.WindowCollector); !ok || e.Interval != 5*time.Minute || e.MaxWindow != 5*time.Minute || e.InitialLookback != 30*time.Minute {
				t.Fatalf("coverage entry = %+v, want a five-minute cadence and window keeping the configured lookback", e)
			}
		}
	}
	if !found {
		t.Fatal("enabled aigateway.coverage was not registered")
	}
}

func TestCoverageTickWithoutAClosedWindowIsANoOp(t *testing.T) {
	b := coverageBucket
	cfg := coverageConfig("example-gateway")
	cfg.Collectors["aigateway.logs"] = config.CollectorConfig{}
	cfg.Collectors["aigateway.coverage"] = config.CollectorConfig{Enabled: true, Interval: 5 * time.Minute, InitialLookback: 30 * time.Minute, MaxWindow: time.Hour}
	api := &coverageAPI{settings: coverageSettings(), groups: []map[string]any{groupsRow("example-gateway", 1)}}
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: cfg, API: api, Registry: registry})
	entry := registry.Entries()[0]
	cov := entry.Collector.(*coverage)
	// Scheduler jitter: the tick lands one second before the next window closes.
	now := b.Add(19*time.Minute + 59*time.Second)
	cov.now = func() time.Time { return now }
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "checkpoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("aigateway.coverage", b.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	s := collector.NewScheduler(registry, &telemetry.Buffer{}, store)
	s.Now = func() time.Time { return now }
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatalf("tick before the window closed: %v, want a no-op", err)
	}
	if lag := cov.Lag(); lag < 10*time.Minute || !now.Add(-lag).Equal(now.Add(-lag).Truncate(coverageWindow)) {
		t.Fatalf("lag %s, want at least ten minutes ending on a window boundary", lag)
	}
	if mark, _ := store.Get("aigateway.coverage"); !mark.Equal(b.Add(5*time.Minute)) || len(api.queries) != 0 {
		t.Fatalf("no-op tick moved to %s or queried %d times", mark, len(api.queries))
	}
	now = b.Add(20 * time.Minute)
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	if mark, _ := store.Get("aigateway.coverage"); !mark.Equal(b.Add(10 * time.Minute)) {
		t.Fatalf("checkpoint = %s after the window closed, want %s", mark, b.Add(10*time.Minute))
	}
}

// Exercise the registered collector with the scheduler clock read just before
// the collector clock read, as happens with time.Now in the running process.
func TestCoverageRegisteredSchedulerUsesOneClockForWindowEnd(t *testing.T) {
	b := coverageBucket
	for _, tc := range []struct {
		name string
		now  time.Time
	}{
		{"fractional tick", b.Add(15*time.Minute + 30*time.Second)},
		{"exact boundary", b.Add(15 * time.Minute)},
		{"crossing boundary", b.Add(15*time.Minute - time.Nanosecond)},
	} {
		for _, cold := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cold=%t", tc.name, cold), func(t *testing.T) {
				cfg := coverageConfig("example-gateway")
				cfg.Collectors["aigateway.logs"] = config.CollectorConfig{}
				cfg.Collectors["aigateway.coverage"] = config.CollectorConfig{Enabled: true, InitialLookback: 30 * time.Minute}
				api := &coverageAPI{settings: coverageSettings(), groups: []map[string]any{groupsRow("example-gateway", 3)}}
				registry := collector.NewRegistry()
				Register(collector.Deps{Config: cfg, API: api, Registry: registry})
				entry := registry.Entries()[0]
				cov := entry.Collector.(*coverage)
				cov.now = func() time.Time { return tc.now.Add(time.Nanosecond) }
				store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "checkpoints.json"))
				if err != nil {
					t.Fatal(err)
				}
				if !cold {
					if err := store.Set(cov.Name(), b.Add(-5*time.Minute)); err != nil {
						t.Fatal(err)
					}
				}
				sink := &telemetry.Buffer{}
				s := collector.NewScheduler(registry, sink, store)
				s.Now = func() time.Time { return tc.now }
				// Three runs drain a cold 30-minute lookback with the scheduler's
				// four-window bound and leading-window deferral, including
				// the previously failing tail.
				for run := 0; run < 3; run++ {
					if err := s.RunOnce(context.Background(), entry); err != nil {
						t.Fatalf("registered startup run %d: %v", run, err)
					}
				}
				wantEnd := tc.now.Add(-10 * time.Minute).Truncate(5 * time.Minute)
				if mark, _ := store.Get(cov.Name()); !mark.Equal(wantEnd) {
					t.Fatalf("checkpoint = %s, want held aligned end %s", mark, wantEnd)
				}
				if len(api.queries) == 0 || len(coverageValues(sink)) != len(api.queries) {
					t.Fatalf("queries=%d coverage=%v, want each real window exported", len(api.queries), coverageValues(sink))
				}
				for _, q := range api.queries {
					if !q.From.Equal(q.From.Truncate(5*time.Minute)) || q.To.Sub(q.From) != 5*time.Minute || q.To.After(tc.now.Add(-10*time.Minute)) {
						t.Fatalf("unsafe query window %s..%s for scheduler clock %s", q.From, q.To, tc.now)
					}
				}
				for _, value := range coverageValues(sink) {
					if value != 3 {
						t.Fatalf("coverage gap=%v, want Groups 3 minus REST 0", value)
					}
				}
			})
		}
	}
}

func TestCoverageRefusesAWindowInsideTheHoldback(t *testing.T) {
	b := coverageBucket
	api := &coverageAPI{
		settings: coverageSettings(),
		groups:   []map[string]any{groupsRow("example-gateway", 1)},
		logs:     map[string][]time.Time{"example-gateway": {b}},
	}
	// An explicit range bypasses the scheduler lag; the window ends 9m59s ago.
	c := newTestCoverage(api, b.Add(14*time.Minute+59*time.Second), "example-gateway")
	out := &telemetry.Buffer{}
	mark, err := c.CollectWindow(context.Background(), b, b.Add(5*time.Minute), out)
	if err == nil || !mark.Equal(b) || len(api.queries) != 0 || len(api.restQueries) != 0 || len(out.Metrics) != 0 {
		t.Fatalf("window inside the holdback: mark=%s err=%v queries=%d rest=%d metrics=%d, want refusal", mark, err, len(api.queries), len(api.restQueries), len(out.Metrics))
	}
}

func TestCoverageRESTCountDedupesIDsRepeatedAcrossPages(t *testing.T) {
	b := coverageBucket
	first := make([]coverageLogRow, 50)
	for i := range first {
		first[i] = coverageLogRow{ID: fmt.Sprintf("example-log-%02d", i), CreatedAt: b.Add(time.Duration(i) * time.Second)}
	}
	// A row inserted mid-read shifts the last row of page one onto page two.
	second := []coverageLogRow{first[49], {ID: "example-log-50", CreatedAt: b.Add(time.Minute)}}
	api := &coverageAPI{
		settings: coverageSettings(),
		groups:   []map[string]any{groupsRow("example-gateway", 51)},
		pages:    map[string][][]coverageLogRow{"example-gateway": {first, second}},
	}
	out := &telemetry.Buffer{}
	if _, err := newTestCoverage(api, b.Add(20*time.Minute), "example-gateway").CollectWindow(context.Background(), b, b.Add(5*time.Minute), out); err != nil {
		t.Fatal(err)
	}
	if got := gapByGateway(t, out.Metrics)["example-gateway"]; got != 0 {
		t.Fatalf("gap = %v with 51 distinct REST IDs and one repeat, want 0", got)
	}
}
