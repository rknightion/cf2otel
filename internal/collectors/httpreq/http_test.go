package httpreq

import (
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/identity"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	otellog "go.opentelemetry.io/otel/log"
)

type fakeAPI struct {
	rows      map[string][]map[string]any
	apps      []accessApp
	queries   []cfapi.GraphQLRequest
	loginRows []map[string]any
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
	*(out.(*[]map[string]any)) = f.rows[q.Dataset]
	return nil
}
func (f *fakeAPI) Accounts(context.Context) ([]cfapi.Account, error) { return nil, nil }
func (f *fakeAPI) Zones(context.Context) ([]cfapi.Zone, error) {
	return []cfapi.Zone{{ID: "zone", Name: "example.com"}}, nil
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
