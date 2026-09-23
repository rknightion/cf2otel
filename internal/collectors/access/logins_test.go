package access

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	otellog "go.opentelemetry.io/otel/log"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/identity"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type loginAPI struct {
	rows    []loginRow
	queries []url.Values
}

func (a *loginAPI) Get(_ context.Context, path string, q url.Values, out any) error {
	if path != "/accounts/test-account/access/logs/access_requests" {
		return fmt.Errorf("path: %s", path)
	}
	a.queries = append(a.queries, q)
	page := 0
	if _, err := fmt.Sscan(q.Get("page"), &page); err != nil {
		return err
	}
	if page < 1 {
		return fmt.Errorf("page %d", page)
	}
	// Two rows per page, with a duplicate at the page boundary. The reported
	// total_count is zero even while the result contains rows.
	all := append([]loginRow(nil), a.rows...)
	if len(all) > 2 {
		all = append(all[:2], append(all[1:2], all[2:]...)...)
	}
	start := (page - 1) * 2
	if start > len(all) {
		start = len(all)
	}
	end := start + 2
	if end > len(all) {
		end = len(all)
	}
	b, err := json.Marshal(map[string]any{"result": all[start:end], "result_info": map[string]any{"page": page, "per_page": 2, "count": end - start, "total_count": 0}})
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
func (*loginAPI) Query(context.Context, cfapi.GraphQLRequest, any) error { panic("unexpected Query") }
func (*loginAPI) Accounts(context.Context) ([]cfapi.Account, error)      { panic("unexpected Accounts") }
func (*loginAPI) Zones(context.Context) ([]cfapi.Zone, error)            { panic("unexpected Zones") }
func (*loginAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) {
	panic("unexpected Gateways")
}

type loginLog struct {
	at    time.Time
	attrs []telemetry.Attr
}
type loginEmitter struct {
	logs []loginLog
}

func (*loginEmitter) Gauge(context.Context, string, float64, ...telemetry.Attr) error {
	panic("unexpected Gauge")
}
func (*loginEmitter) Counter(context.Context, string, float64, ...telemetry.Attr) error {
	panic("REST logins must not emit sampled metrics")
}
func (*loginEmitter) Histogram(context.Context, string, float64, ...telemetry.Attr) error {
	panic("unexpected Histogram")
}
func (e *loginEmitter) LogEvent(_ context.Context, event, _ string, at time.Time, _ otellog.Severity, a ...telemetry.Attr) error {
	if event != semconv.EventAccessLogin {
		return fmt.Errorf("event %s", event)
	}
	e.logs = append(e.logs, loginLog{at, append([]telemetry.Attr(nil), a...)})
	return nil
}
func (*loginEmitter) Span(context.Context, telemetry.SpanSpec) error { panic("unexpected Span") }

type loginIndex struct{ logins []identity.Login }

func (i *loginIndex) Observe(l identity.Login)                      { i.logins = append(i.logins, l) }
func (*loginIndex) Lookup(string, string, time.Time) identity.Match { panic("unexpected Lookup") }

func attrsMap(attrs []telemetry.Attr) map[string]string {
	m := map[string]string{}
	for _, a := range attrs {
		m[a.Key] = a.Value
	}
	return m
}

func TestLoginsPaginationBoundaryAndRestart(t *testing.T) {
	t0 := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	api := &loginAPI{rows: []loginRow{
		{RayID: "ray-a", CreatedAt: t0.Add(time.Second), AppDomain: "app.example.com/api/a", AppName: "Example", AppUID: "app-1", AppType: "self_hosted", UserEmail: "a@example.com", UserID: "user-a", IPAddress: "192.0.2.1", Country: "GB", Action: "login", Connection: "google", Allowed: true},
		{RayID: "ray-b", CreatedAt: t0.Add(2 * time.Second), AppDomain: "https://app.example.com/assets/b.js", AppName: "Example", AppUID: "app-1", AppType: "self_hosted", UserEmail: "b@example.com", UserID: "user-b", IPAddress: "192.0.2.2", Country: "GB", Action: "login", Connection: "google", Allowed: false},
		{RayID: "ray-c", CreatedAt: t0.Add(3 * time.Second), AppDomain: "app.example.com", AppName: "Example", AppUID: "app-1", AppType: "self_hosted", UserEmail: "c@example.com", UserID: "user-c", IPAddress: "192.0.2.3", Country: "GB", Action: "login", Connection: "google", Allowed: true},
	}}
	e := &loginEmitter{}
	i := &loginIndex{}
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "test-account"
	c := newLogins(collector.Deps{Config: &cfg, API: api, Identity: i})
	for _, w := range [][2]time.Time{{t0, t0.Add(2 * time.Second)}, {t0.Add(2 * time.Second), t0.Add(4 * time.Second)}} {
		mark, err := c.CollectWindow(context.Background(), w[0], w[1], e)
		if err != nil {
			t.Fatal(err)
		}
		if !mark.Equal(w[1]) {
			t.Fatalf("mark %v want %v", mark, w[1])
		}
	}
	if len(e.logs) != 3 || len(i.logins) != 2 {
		t.Fatalf("logs=%d identities=%d", len(e.logs), len(i.logins))
	}
	got := []string{}
	for _, l := range e.logs {
		got = append(got, attrsMap(l.attrs)[semconv.AttrAccessRayID])
	}
	if !reflect.DeepEqual(got, []string{"ray-a", "ray-b", "ray-c"}) {
		t.Fatalf("rays: %v", got)
	}
	first := attrsMap(e.logs[0].attrs)
	if first[semconv.AttrAccessHost] != "app.example.com" || first[semconv.AttrAccessPath] != "/api/a" {
		t.Fatalf("host/path: %v", first)
	}
	if i.logins[0].Host != "app.example.com" || i.logins[0].ClientIP != "192.0.2.1" {
		t.Fatalf("index: %+v", i.logins[0])
	}
	if len(api.queries) < 4 {
		t.Fatalf("pagination stopped on total_count=0: %d queries", len(api.queries))
	}
}

func TestLoginsCheckpointSurvivesRestart(t *testing.T) {
	t0 := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	api := &loginAPI{rows: []loginRow{
		{RayID: "ray-a", CreatedAt: t0.Add(time.Second), AppDomain: "app.example.com", UserEmail: "a@example.com", IPAddress: "192.0.2.1", Allowed: true},
		{RayID: "ray-b", CreatedAt: t0.Add(2 * time.Second), AppDomain: "app.example.com", UserEmail: "b@example.com", IPAddress: "192.0.2.2", Allowed: true},
	}}
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "test-account"
	e := &loginEmitter{}
	path := filepath.Join(t.TempDir(), "checkpoints.json")
	for run, now := range []time.Time{t0.Add(62 * time.Second), t0.Add(64 * time.Second)} {
		store, err := collector.NewFileStore(path)
		if err != nil {
			t.Fatal(err)
		}
		c := newLogins(collector.Deps{Config: &cfg, API: api})
		s := collector.NewScheduler(collector.NewRegistry(), e, store)
		s.Now = func() time.Time { return now }
		entry := collector.Entry{Collector: c, InitialLookback: 2 * time.Second}
		if err := s.RunOnce(context.Background(), entry); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
	}
	if len(e.logs) != 2 {
		t.Fatalf("restart yielded %d logs, want 2", len(e.logs))
	}
	if got := attrsMap(e.logs[0].attrs)[semconv.AttrAccessRayID]; got != "ray-a" {
		t.Fatalf("first ray %s", got)
	}
	if got := attrsMap(e.logs[1].attrs)[semconv.AttrAccessRayID]; got != "ray-b" {
		t.Fatalf("second ray %s", got)
	}
}
