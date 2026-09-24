package httpreq

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/identity"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	otellog "go.opentelemetry.io/otel/log"
)

type fakeAPI struct {
	rows          map[string][]map[string]any
	apps          []accessApp
	queries       []cfapi.GraphQLRequest
	loginRows     []map[string]any
	zones         []cfapi.Zone
	zonesErr      error
	queryErrors   map[string]error
	groupSettings *cfapi.DatasetSettings
	settingsErr   error
}

func (f *fakeAPI) Get(_ context.Context, path string, _ url.Values, out any) error {
	if strings.Contains(path, "/access/logs/access_requests") {
		b, _ := json.Marshal(map[string]any{"result": f.loginRows, "result_info": map[string]any{"per_page": 1000}})
		return json.Unmarshal(b, out)
	}
	*(out.(*[]accessApp)) = f.apps
	return nil
}
func (f *fakeAPI) Query(_ context.Context, q cfapi.GraphQLRequest, out any) error {
	f.queries = append(f.queries, q)
	if err := f.queryErrors[q.ScopeID]; err != nil {
		return err
	}
	*(out.(*[]map[string]any)) = f.rows[q.Dataset]
	return nil
}
func (f *fakeAPI) Accounts(context.Context) ([]cfapi.Account, error) { return nil, nil }
func (f *fakeAPI) Zones(context.Context) ([]cfapi.Zone, error) {
	if f.zonesErr != nil {
		return nil, f.zonesErr
	}
	if f.zones != nil {
		return f.zones, nil
	}
	return []cfapi.Zone{{ID: "zone", Name: "example.com"}}, nil
}
func (f *fakeAPI) DatasetSettings(_ context.Context, scope cfapi.Scope, scopeID, dataset string) (cfapi.DatasetSettings, error) {
	if f.settingsErr != nil {
		return cfapi.DatasetSettings{}, f.settingsErr
	}
	if scope != cfapi.ZoneScope || scopeID == "" || dataset != "httpRequestsAdaptiveGroups" {
		return cfapi.DatasetSettings{}, errors.New("unexpected settings request")
	}
	if f.groupSettings != nil {
		return *f.groupSettings, nil
	}
	return defaultHTTPGroupSettings(), nil
}

func TestEventsRetentionUsesLatestZoneFloor(t *testing.T) {
	baseTime := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	f := &fakeAPI{
		zones: []cfapi.Zone{{ID: "one", Name: "one.example.test"}, {ID: "two", Name: "two.example.test"}},
		queryErrors: map[string]error{
			"one": &cfapi.RetentionGapError{Dataset: "httpRequestsAdaptive", Floor: baseTime.Add(time.Minute)},
			"two": &cfapi.RetentionGapError{Dataset: "httpRequestsAdaptive", Floor: baseTime.Add(12 * time.Minute)},
		},
	}
	cfg := config.Default()
	cfg.HTTP.Scope = "all"
	cfg.Identity.Enabled = false
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "checkpoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := collector.NewScheduler(nil, &fakeEmitter{}, store)
	s.Now = func() time.Time { return baseTime.Add(32 * time.Minute) }
	entry := collector.Entry{Collector: events{base{cfg: &cfg, api: f}}, Interval: 5 * time.Minute, InitialLookback: 30 * time.Minute}
	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	mark, ok := store.Get("httpreq.events")
	want := baseTime.Add(20 * time.Minute)
	if !ok || !mark.Equal(want) {
		t.Fatalf("checkpoint=%s, want latest zone floor plus margin %s", mark, want)
	}
	if len(f.queries) != 2 {
		t.Fatalf("queries=%d, want both zones", len(f.queries))
	}
}
func (f *fakeAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) { return nil, nil }

type fakeIdentity struct{ calls int }

func (*fakeIdentity) Observe(identity.Login) {}
func (f *fakeIdentity) Lookup(ip, host string, at time.Time) identity.Match {
	f.calls++
	return identity.Match{UserEmail: "user@example.com", LoginRayID: "invented", Inferred: true}
}

type emission struct {
	name  string
	value float64
	attrs []telemetry.Attr
	body  string
}
type fakeEmitter struct{ logs, counts, gauges []emission }

func (f *fakeEmitter) Gauge(_ context.Context, n string, v float64, a ...telemetry.Attr) error {
	f.gauges = append(f.gauges, emission{n, v, a, ""})
	return nil
}
func (f *fakeEmitter) Counter(_ context.Context, n string, v float64, a ...telemetry.Attr) error {
	f.counts = append(f.counts, emission{n, v, a, ""})
	return nil
}
func (*fakeEmitter) Histogram(context.Context, string, float64, ...telemetry.Attr) error { return nil }
func (f *fakeEmitter) LogEvent(_ context.Context, n, b string, _ time.Time, _ otellog.Severity, a ...telemetry.Attr) error {
	f.logs = append(f.logs, emission{name: n, body: b, attrs: a})
	return nil
}
func (*fakeEmitter) Span(context.Context, telemetry.SpanSpec) error { return nil }
func hasAttr(a []telemetry.Attr, k, v string) bool {
	for _, x := range a {
		if x.Key == k && x.Value == v {
			return true
		}
	}
	return false
}

func TestEventsScopeAndInference(t *testing.T) {
	f := &fakeAPI{apps: []accessApp{{Domain: "https://protected.example.com/team"}}, rows: map[string][]map[string]any{
		"httpRequestsAdaptive": {
			{"datetime": "2026-09-23T10:00:00Z", "clientRequestHTTPHost": "protected.example.com", "clientRequestPath": "/team/a", "edgeResponseStatus": 200, "clientIP": "192.0.2.4", "rayName": "invented-1"},
			{"datetime": "2026-09-23T10:00:00Z", "clientRequestHTTPHost": "other.example.com", "clientRequestPath": "/", "edgeResponseStatus": 200, "clientIP": "192.0.2.5", "rayName": "invented-2"},
		}}}
	id := &fakeIdentity{}
	e := &fakeEmitter{}
	c := config.Default()
	c.Cloudflare.AccountID = "account"
	collector := events{base{cfg: &c, api: f, identity: id}}
	from := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)
	if _, err := collector.CollectWindow(context.Background(), from, to, e); err != nil {
		t.Fatal(err)
	}
	if len(e.logs) != 1 || id.calls != 1 {
		t.Fatalf("logs=%d lookups=%d", len(e.logs), id.calls)
	}
	if !hasAttr(e.logs[0].attrs, semconv.AttrAccessIdentityInferred, "true") || !hasAttr(e.logs[0].attrs, semconv.AttrAccessUserEmail, "user@example.com") {
		t.Fatalf("missing inferred identity: %+v", e.logs[0].attrs)
	}
	if len(e.counts) != 0 {
		t.Fatalf("raw sampled rows produced metrics: %+v", e.counts)
	}
	if len(f.queries) != 1 || !reflect.DeepEqual(f.queries[0].WantedFields, eventFields) || !reflect.DeepEqual(f.queries[0].JoinFields, []string{"rayName", "datetime"}) {
		t.Fatalf("raw selection did not request bounded joinable fields: %+v", f.queries)
	}
}

func TestEventsDoNotInferWhenDisabled(t *testing.T) {
	f := &fakeAPI{apps: []accessApp{{Domain: "https://protected.example.com"}}, rows: map[string][]map[string]any{
		"httpRequestsAdaptive": {{"datetime": "2026-09-23T10:00:00Z", "clientRequestHTTPHost": "protected.example.com", "clientIP": "192.0.2.4", "rayName": "invented-1"}},
	}}
	id := &fakeIdentity{}
	e := &fakeEmitter{}
	c := config.Default()
	c.Cloudflare.AccountID = "account"
	c.Identity.Enabled = false
	collector := events{base{cfg: &c, api: f, identity: id}}
	from := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	if _, err := collector.CollectWindow(context.Background(), from, from.Add(2*time.Hour), e); err != nil {
		t.Fatal(err)
	}
	if len(e.logs) != 1 || id.calls != 0 || hasAttr(e.logs[0].attrs, semconv.AttrAccessIdentityInferred, "true") {
		t.Fatalf("disabled inference: logs=%d lookups=%d attrs=%+v", len(e.logs), id.calls, e.logs)
	}
}

func TestEventsHydrateIdentityBeforeHTTPWindow(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	loginAt := now.Add(-4 * time.Minute)
	httpAt := now.Add(-3 * time.Minute)
	f := &fakeAPI{apps: []accessApp{{Domain: "https://protected.example.com"}}, loginRows: []map[string]any{{
		"user_email": "user@example.com", "ip_address": "192.0.2.4", "app_domain": "protected.example.com", "allowed": true,
		"ray_id": "login-ray", "created_at": loginAt.Format(time.RFC3339),
	}}, rows: map[string][]map[string]any{
		"httpRequestsAdaptive": {{"datetime": httpAt.Format(time.RFC3339), "clientRequestHTTPHost": "protected.example.com", "clientIP": "192.0.2.4", "rayName": "http-ray"}},
	}}
	c := config.Default()
	c.Cloudflare.AccountID = "account"
	idx, err := identity.NewWithRetention(c.Identity.MatchWindow, 23*time.Hour, c.Identity.MaxCandidates)
	if err != nil {
		t.Fatal(err)
	}
	e := &fakeEmitter{}
	collector := NewEvents(&c, f, idx)
	if _, err := collector.CollectWindow(context.Background(), now.Add(-5*time.Minute), now, e); err != nil {
		t.Fatal(err)
	}
	if len(e.logs) != 1 || !hasAttr(e.logs[0].attrs, semconv.AttrAccessIdentityInferred, "true") {
		t.Fatalf("identity was not hydrated: %+v", e.logs)
	}
}

func TestGroupsProduceCorrectedMetrics(t *testing.T) {
	f := &fakeAPI{rows: map[string][]map[string]any{"httpRequestsAdaptiveGroups": {{"dimensions": map[string]any{"clientRequestHTTPHost": "protected.example.com", "edgeResponseStatus": 503, "cacheStatus": "miss"}, "count": 120, "avg": map[string]any{"originResponseDurationMs": 40}}}}}
	e := &fakeEmitter{}
	c := config.Default()
	c.HTTP.Scope = "hosts"
	c.HTTP.Hosts = []string{"protected.example.com"}
	collector := metrics{base{cfg: &c, api: f}}
	from := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	if _, err := collector.CollectWindow(context.Background(), from, from.Add(time.Hour), e); err != nil {
		t.Fatal(err)
	}
	if len(e.counts) != 1 || e.counts[0].name != semconv.MetricHTTPRequests || e.counts[0].value != 120 {
		t.Fatalf("counts=%+v", e.counts)
	}
	if !hasAttr(e.counts[0].attrs, semconv.AttrStatusClass, "5xx") || !hasAttr(e.counts[0].attrs, semconv.AttrHTTPCacheStatus, "miss") {
		t.Fatalf("dimensions=%+v", e.counts[0].attrs)
	}
	if len(e.gauges) != 1 || e.gauges[0].name != semconv.MetricHTTPOriginDuration || e.gauges[0].value != 0.04 {
		t.Fatalf("duration=%+v", e.gauges)
	}
}

func TestGroupsOmitNoOriginDurationSentinelButKeepRequests(t *testing.T) {
	row := metricGroup("public.example.test", 200, "miss", 7)
	row["avg"].(map[string]any)["originResponseDurationMs"] = -1
	f := &fakeAPI{rows: map[string][]map[string]any{"httpRequestsAdaptiveGroups": {row}}}
	c := config.Default()
	c.HTTP.Scope = "all"
	c.HTTP.MetricsScope = "all"
	e := &fakeEmitter{}
	from := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	mark, err := NewMetrics(&c, f).CollectWindow(context.Background(), from, from.Add(time.Hour), e)
	if err != nil || !mark.Equal(from.Add(time.Hour)) {
		t.Fatalf("sentinel returned mark=%s error=%v", mark, err)
	}
	if len(e.counts) != 1 || e.counts[0].value != 7 || len(e.gauges) != 0 {
		t.Fatalf("sentinel counts=%+v gauges=%+v", e.counts, e.gauges)
	}
}

func TestMetricsAllScopeBroadensOnlyMetricsAndKeepsEventsScoped(t *testing.T) {
	f := &fakeAPI{
		apps:  []accessApp{{Domain: "protected.example.test"}},
		zones: []cfapi.Zone{{ID: "one", Name: "one.example.test"}, {ID: "two", Name: "two.example.test"}},
		rows: map[string][]map[string]any{
			"httpRequestsAdaptive": {
				{"datetime": "2026-09-23T10:00:00Z", "clientRequestHTTPHost": "protected.example.test", "rayName": "fixture-ray-one"},
				{"datetime": "2026-09-23T10:00:00Z", "clientRequestHTTPHost": "public.example.test", "rayName": "fixture-ray-two"},
			},
			"httpRequestsAdaptiveGroups": {
				metricGroup("protected.example.test", 200, "miss", 2),
				metricGroup("public.example.test", 503, "none", 3),
			},
		},
	}
	c := config.Default()
	c.Cloudflare.AccountID = "account-fixture"
	c.Cloudflare.Zones = []string{"one"}
	c.Identity.Enabled = false
	c.HTTP.MetricsScope = "all"
	e := &fakeEmitter{}
	from := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)

	if _, err := NewEvents(&c, f, nil).CollectWindow(context.Background(), from, to, e); err != nil {
		t.Fatal(err)
	}
	if _, err := NewMetrics(&c, f).CollectWindow(context.Background(), from, to, e); err != nil {
		t.Fatal(err)
	}
	if len(e.logs) != 1 || !hasAttr(e.logs[0].attrs, semconv.AttrHTTPHost, "protected.example.test") {
		t.Fatalf("events escaped the existing HTTP scope: %+v", e.logs)
	}
	if len(e.counts) != 4 {
		t.Fatalf("all-scope metrics emitted %d rows, want both hosts in both zones", len(e.counts))
	}
	if len(f.queries) != 3 || f.queries[0].Dataset != "httpRequestsAdaptive" || f.queries[0].ScopeID != "one" {
		t.Fatalf("event queries changed with metrics scope: %+v", f.queries)
	}
	metricZones := []string{f.queries[1].ScopeID, f.queries[2].ScopeID}
	if !reflect.DeepEqual(metricZones, []string{"one", "two"}) {
		t.Fatalf("all-scope metrics queried zones %v, want every discovered zone", metricZones)
	}
	for _, row := range e.counts {
		if !hasAttr(row.attrs, semconv.AttrHTTPHost, "protected.example.test") && !hasAttr(row.attrs, semconv.AttrHTTPHost, "public.example.test") {
			t.Fatalf("all-scope metrics unexpectedly filtered a host: %+v", row.attrs)
		}
	}
}

func TestMetricsDefaultScopeInheritsExistingHTTPScope(t *testing.T) {
	f := &fakeAPI{
		apps:  []accessApp{{Domain: "protected.example.test"}},
		zones: []cfapi.Zone{{ID: "one", Name: "one.example.test"}, {ID: "two", Name: "two.example.test"}},
		rows: map[string][]map[string]any{"httpRequestsAdaptiveGroups": {
			metricGroup("protected.example.test", 200, "miss", 2),
			metricGroup("public.example.test", 200, "miss", 3),
		}},
	}
	c := config.Default()
	c.Cloudflare.AccountID = "account-fixture"
	c.Cloudflare.Zones = []string{"one"}
	e := &fakeEmitter{}
	from := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	if _, err := NewMetrics(&c, f).CollectWindow(context.Background(), from, from.Add(time.Hour), e); err != nil {
		t.Fatal(err)
	}
	if len(f.queries) != 1 || f.queries[0].ScopeID != "one" {
		t.Fatalf("default metrics zone selection changed: %+v", f.queries)
	}
	if len(e.counts) != 1 || !hasAttr(e.counts[0].attrs, semconv.AttrHTTPHost, "protected.example.test") {
		t.Fatalf("default metrics scope changed: %+v", e.counts)
	}
}

func TestMetricsEmptyMetricsScopeKeepsLegacyZoneSubset(t *testing.T) {
	f := &fakeAPI{
		zones: []cfapi.Zone{{ID: "one", Name: "one.example.test"}, {ID: "two", Name: "two.example.test"}},
		rows:  map[string][]map[string]any{"httpRequestsAdaptiveGroups": {metricGroup("public.example.test", 200, "miss", 2)}},
	}
	c := config.Default()
	c.Cloudflare.Zones = []string{"one"}
	c.HTTP.Scope = "all"
	e := &fakeEmitter{}
	from := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	if _, err := NewMetrics(&c, f).CollectWindow(context.Background(), from, from.Add(time.Hour), e); err != nil {
		t.Fatal(err)
	}
	if len(f.queries) != 1 || f.queries[0].ScopeID != "one" {
		t.Fatalf("empty metrics_scope changed legacy zone selection: %+v", f.queries)
	}
}

func TestMetricsAllScopeUsesExplicitHTTPZonesAsNarrowingSelector(t *testing.T) {
	f := &fakeAPI{
		zones: []cfapi.Zone{{ID: "one", Name: "one.example.test"}, {ID: "two", Name: "two.example.test"}},
		rows:  map[string][]map[string]any{"httpRequestsAdaptiveGroups": {metricGroup("public.example.test", 200, "miss", 2)}},
	}
	c := config.Default()
	c.Cloudflare.Zones = []string{"one"}
	c.HTTP.Scope = "all"
	c.HTTP.MetricsScope = "all"
	c.HTTP.Zones = []string{"two"}
	e := &fakeEmitter{}
	from := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	if _, err := NewMetrics(&c, f).CollectWindow(context.Background(), from, from.Add(time.Hour), e); err != nil {
		t.Fatal(err)
	}
	if len(f.queries) != 1 || f.queries[0].ScopeID != "two" {
		t.Fatalf("explicit HTTP zone selector was not applied: %+v", f.queries)
	}
	if len(e.counts) != 1 {
		t.Fatalf("explicit HTTP zone selector emitted %d points, want one", len(e.counts))
	}
}

func TestMetricsAllScopeFailsClosedOnMissingZoneDiscovery(t *testing.T) {
	tests := []struct {
		name string
		api  *fakeAPI
	}{
		{name: "empty inventory", api: &fakeAPI{zones: []cfapi.Zone{}}},
		{name: "discovery error", api: &fakeAPI{zonesErr: errors.New("discovery unavailable")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := config.Default()
			c.HTTP.MetricsScope = "all"
			e := &fakeEmitter{}
			from := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
			mark, err := NewMetrics(&c, tt.api).CollectWindow(context.Background(), from, from.Add(time.Hour), e)
			if err == nil || !mark.Equal(from) {
				t.Fatalf("empty zone discovery returned mark=%s error=%v, want error and unchanged mark", mark, err)
			}
			if len(tt.api.queries) != 0 || len(e.counts) != 0 || len(e.gauges) != 0 {
				t.Fatalf("empty zone discovery queried or emitted: queries=%d counts=%d gauges=%d", len(tt.api.queries), len(e.counts), len(e.gauges))
			}
		})
	}
}

func TestMetricsRequireEntitledAndPresentGroupFields(t *testing.T) {
	t.Run("required field is not entitled", func(t *testing.T) {
		settings := defaultHTTPGroupSettings()
		settings.AvailableFields = []string{"count", "dimensions_edgeResponseStatus", "dimensions_cacheStatus"}
		f := &fakeAPI{
			groupSettings: &settings,
			rows:          map[string][]map[string]any{"httpRequestsAdaptiveGroups": {metricGroup("public.example.test", 200, "miss", 1)}},
		}
		c := config.Default()
		c.HTTP.Scope = "all"
		c.HTTP.MetricsScope = "all"
		e := &fakeEmitter{}
		from := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
		mark, err := NewMetrics(&c, f).CollectWindow(context.Background(), from, from.Add(time.Hour), e)
		if err == nil || !mark.Equal(from) || len(f.queries) != 0 || len(e.counts) != 0 {
			t.Fatalf("unentitled required field returned mark=%s error=%v queries=%d counts=%d", mark, err, len(f.queries), len(e.counts))
		}
	})
	t.Run("required row dimension is missing", func(t *testing.T) {
		row := metricGroup("public.example.test", 200, "miss", 1)
		delete(row["dimensions"].(map[string]any), "cacheStatus")
		f := &fakeAPI{rows: map[string][]map[string]any{"httpRequestsAdaptiveGroups": {row}}}
		c := config.Default()
		c.HTTP.Scope = "all"
		c.HTTP.MetricsScope = "all"
		e := &fakeEmitter{}
		from := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
		mark, err := NewMetrics(&c, f).CollectWindow(context.Background(), from, from.Add(time.Hour), e)
		if err == nil || !mark.Equal(from) || len(e.counts) != 0 {
			t.Fatalf("missing dimension returned mark=%s error=%v counts=%d", mark, err, len(e.counts))
		}
	})
}

func TestMetricsSelectionFitsTheAdvertisedFieldLimit(t *testing.T) {
	settings := defaultHTTPGroupSettings()
	settings.MaxNumberOfFields = len(requiredHTTPGroupFields)
	f := &fakeAPI{
		groupSettings: &settings,
		rows:          map[string][]map[string]any{"httpRequestsAdaptiveGroups": {metricGroup("one.example.test", 200, "miss", 1)}},
	}
	c := config.Default()
	c.HTTP.Scope = "all"
	c.HTTP.MetricsScope = "all"
	e := &fakeEmitter{}
	from := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	if _, err := NewMetrics(&c, f).CollectWindow(context.Background(), from, from.Add(time.Hour), e); err != nil {
		t.Fatal(err)
	}
	if len(f.queries) != 1 || !reflect.DeepEqual(f.queries[0].WantedFields, requiredHTTPGroupFields) {
		t.Fatalf("HTTP groups selection ignored the zone field limit: %+v", f.queries)
	}
	if len(e.counts) != 1 || len(e.gauges) != 0 {
		t.Fatalf("optional duration field exceeded the advertised selection: counts=%d gauges=%d", len(e.counts), len(e.gauges))
	}
}

func TestMetricsEnforceHostAndSeriesCapsBeforeEmission(t *testing.T) {
	t.Run("host cap", func(t *testing.T) {
		f := &fakeAPI{rows: map[string][]map[string]any{"httpRequestsAdaptiveGroups": {
			metricGroup("one.example.test", 200, "miss", 1),
			metricGroup("two.example.test", 200, "miss", 1),
		}}}
		c := config.Default()
		c.HTTP.Scope = "all"
		c.HTTP.MetricsScope = "all"
		c.HTTP.MaxMetricHostsPerZone = 1
		e := &fakeEmitter{}
		from := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
		mark, err := NewMetrics(&c, f).CollectWindow(context.Background(), from, from.Add(time.Hour), e)
		if err == nil || !mark.Equal(from) || len(e.counts) != 0 || len(e.gauges) != 0 {
			t.Fatalf("host cap returned mark=%s error=%v counts=%d gauges=%d", mark, err, len(e.counts), len(e.gauges))
		}
	})
	t.Run("series cap", func(t *testing.T) {
		f := &fakeAPI{rows: map[string][]map[string]any{"httpRequestsAdaptiveGroups": {
			metricGroup("one.example.test", 200, "miss", 1),
			metricGroup("one.example.test", 503, "miss", 1),
		}}}
		c := config.Default()
		c.HTTP.Scope = "all"
		c.HTTP.MetricsScope = "all"
		c.HTTP.MaxMetricSeriesPerWindow = 3
		e := &fakeEmitter{}
		from := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
		mark, err := NewMetrics(&c, f).CollectWindow(context.Background(), from, from.Add(time.Hour), e)
		if err == nil || !mark.Equal(from) || len(e.counts) != 0 || len(e.gauges) != 0 {
			t.Fatalf("series cap returned mark=%s error=%v counts=%d gauges=%d", mark, err, len(e.counts), len(e.gauges))
		}
	})
}

func TestMetricsFailClosedWhenAZoneQueryIsIncomplete(t *testing.T) {
	f := &fakeAPI{
		zones:       []cfapi.Zone{{ID: "one", Name: "one.example.test"}, {ID: "two", Name: "two.example.test"}},
		rows:        map[string][]map[string]any{"httpRequestsAdaptiveGroups": {metricGroup("one.example.test", 200, "miss", 1)}},
		queryErrors: map[string]error{"two": errors.New("fixture query failed")},
	}
	c := config.Default()
	c.HTTP.Scope = "all"
	c.HTTP.MetricsScope = "all"
	e := &fakeEmitter{}
	from := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	mark, err := NewMetrics(&c, f).CollectWindow(context.Background(), from, from.Add(time.Hour), e)
	if err == nil || !mark.Equal(from) {
		t.Fatalf("incomplete zone query returned mark=%s error=%v", mark, err)
	}
	if len(e.counts) != 0 || len(e.gauges) != 0 {
		t.Fatalf("incomplete zone query emitted partial metrics: counts=%d gauges=%d", len(e.counts), len(e.gauges))
	}
}

func TestMetricsIncompleteZoneWindowDoesNotAdvanceCheckpoint(t *testing.T) {
	f := &fakeAPI{
		zones:       []cfapi.Zone{{ID: "one", Name: "one.example.test"}, {ID: "two", Name: "two.example.test"}},
		rows:        map[string][]map[string]any{"httpRequestsAdaptiveGroups": {metricGroup("one.example.test", 200, "miss", 1)}},
		queryErrors: map[string]error{"two": errors.New("fixture query failed")},
	}
	c := config.Default()
	c.HTTP.Scope = "all"
	c.HTTP.MetricsScope = "all"
	e := &fakeEmitter{}
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "checkpoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	scheduler := collector.NewScheduler(nil, e, store)
	scheduler.Now = func() time.Time { return time.Date(2026, 9, 23, 10, 32, 0, 0, time.UTC) }
	entry := collector.Entry{Collector: NewMetrics(&c, f), Interval: 5 * time.Minute, InitialLookback: 30 * time.Minute}
	if err := scheduler.RunOnce(context.Background(), entry); err == nil {
		t.Fatal("incomplete zone query unexpectedly committed")
	}
	mark, ok := store.Get("httpreq.metrics")
	want := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	if !ok || !mark.Equal(want) {
		t.Fatalf("incomplete zone query checkpoint=%s present=%v, want the unchanged initial cursor %s", mark, ok, want)
	}
	if len(e.counts) != 0 || len(e.gauges) != 0 {
		t.Fatalf("incomplete window reached the emitter: counts=%d gauges=%d", len(e.counts), len(e.gauges))
	}
}

func TestMetricsFailClosedOnSaturatedGroupsQuery(t *testing.T) {
	rows := make([]map[string]any, 10000)
	for i := range rows {
		rows[i] = metricGroup("one.example.test", 200, "miss", 1)
	}
	f := &fakeAPI{rows: map[string][]map[string]any{"httpRequestsAdaptiveGroups": rows}}
	c := config.Default()
	c.HTTP.Scope = "all"
	c.HTTP.MetricsScope = "all"
	c.HTTP.MaxMetricSeriesPerWindow = 20000
	e := &fakeEmitter{}
	from := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	mark, err := NewMetrics(&c, f).CollectWindow(context.Background(), from, from.Add(time.Hour), e)
	if err == nil || !mark.Equal(from) || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("saturated query returned mark=%s error=%v", mark, err)
	}
	if len(e.counts) != 0 || len(e.gauges) != 0 {
		t.Fatalf("saturated query emitted metrics: counts=%d gauges=%d", len(e.counts), len(e.gauges))
	}
}

func TestMetricsHostLabelsAreSanitizedAndNeverContainIP(t *testing.T) {
	t.Run("strips userinfo and path", func(t *testing.T) {
		f := &fakeAPI{rows: map[string][]map[string]any{"httpRequestsAdaptiveGroups": {
			metricGroup("https://alice@example.test/private?q=fixture", 200, "miss", 1),
		}}}
		c := config.Default()
		c.HTTP.Scope = "all"
		c.HTTP.MetricsScope = "all"
		e := &fakeEmitter{}
		from := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
		if _, err := NewMetrics(&c, f).CollectWindow(context.Background(), from, from.Add(time.Hour), e); err != nil {
			t.Fatal(err)
		}
		if len(e.counts) != 1 || !hasAttr(e.counts[0].attrs, semconv.AttrHTTPHost, "example.test") {
			t.Fatalf("metric host was not canonicalized: %+v", e.counts)
		}
		for _, attr := range e.counts[0].attrs {
			key := strings.ToLower(attr.Key)
			if strings.Contains(key, "ip") || strings.Contains(key, "email") || strings.Contains(key, "path") || strings.Contains(key, "ray") || strings.Contains(key, "user_agent") {
				t.Fatalf("PII attribute key reached metric: %q", attr.Key)
			}
			if strings.Contains(attr.Value, "alice") || strings.Contains(attr.Value, "/private") || strings.Contains(attr.Value, "fixture") || strings.Contains(attr.Value, "@") {
				t.Fatalf("PII-like host content reached metric: %+v", attr)
			}
		}
	})
	t.Run("rejects an IP host", func(t *testing.T) {
		f := &fakeAPI{rows: map[string][]map[string]any{"httpRequestsAdaptiveGroups": {
			metricGroup("203.0.113.7", 200, "miss", 1),
		}}}
		c := config.Default()
		c.HTTP.Scope = "all"
		c.HTTP.MetricsScope = "all"
		e := &fakeEmitter{}
		from := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
		mark, err := NewMetrics(&c, f).CollectWindow(context.Background(), from, from.Add(time.Hour), e)
		if err == nil || !mark.Equal(from) || len(e.counts) != 0 {
			t.Fatalf("IP host returned mark=%s error=%v counts=%d", mark, err, len(e.counts))
		}
	})
}

func metricGroup(host string, status int, cache string, count int) map[string]any {
	return map[string]any{
		"dimensions": map[string]any{
			"clientRequestHTTPHost": host,
			"edgeResponseStatus":    status,
			"cacheStatus":           cache,
		},
		"count": count,
		"avg":   map[string]any{"originResponseDurationMs": 40},
	}
}

func defaultHTTPGroupSettings() cfapi.DatasetSettings {
	return cfapi.DatasetSettings{
		Enabled: true,
		AvailableFields: []string{
			"count", "dimensions_clientRequestHTTPHost", "dimensions_edgeResponseStatus",
			"dimensions_cacheStatus", "avg_originResponseDurationMs",
		},
		MaxNumberOfFields: 70,
		MaxDuration:       3600,
		NotOlderThan:      31 * 24 * 3600,
		MaxPageSize:       10000,
	}
}

func TestHostScope(t *testing.T) {
	if !selected("api.example.com", map[string]bool{"*.example.com": true}) {
		t.Fatal("wildcard Access host not selected")
	}
	if selected("example.com", map[string]bool{"*.example.com": true}) {
		t.Fatal("wildcard selected the apex")
	}
	if selected("other.example.net", map[string]bool{"protected.example.com": true}) {
		t.Fatal("unprotected host selected")
	}
	if !selected("other.example.net", nil) {
		t.Fatal("all-host scope rejected host")
	}
}
