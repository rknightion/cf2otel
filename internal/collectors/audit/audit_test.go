package audit

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	otellog "go.opentelemetry.io/otel/log"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type fakeAPI struct {
	cfapi.Client
	pages          []auditPage
	queries        []url.Values
	endless        bool
	exclusiveSince bool
}

func (f *fakeAPI) GetPage(_ context.Context, _ string, q url.Values, out any) error {
	copyQ := url.Values{}
	for key, values := range q {
		copyQ[key] = append([]string(nil), values...)
	}
	f.queries = append(f.queries, copyQ)
	index := len(f.queries) - 1
	var page auditPage
	if f.endless {
		page.ResultInfo.Cursor = strconv.Itoa(index + 1)
	} else if index < len(f.pages) {
		page = f.pages[index]
	}
	if f.exclusiveSince {
		since, err := time.Parse(time.RFC3339Nano, q.Get("since"))
		if err != nil {
			return err
		}
		var filtered []auditEvent
		for _, event := range page.Result {
			if event.Action.Time.After(since) {
				filtered = append(filtered, event)
			}
		}
		page.Result = filtered
	}
	b, err := json.Marshal(page)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

type emittedLog struct {
	severity otellog.Severity
	attrs    []telemetry.Attr
}
type emittedMetric struct{ attrs []telemetry.Attr }
type fakeEmitter struct {
	telemetry.Emitter
	logs    []emittedLog
	metrics []emittedMetric
}

func (f *fakeEmitter) LogEvent(_ context.Context, name, _ string, _ time.Time, severity otellog.Severity, attrs ...telemetry.Attr) error {
	if name != semconv.EventAuditEvent {
		panic(name)
	}
	f.logs = append(f.logs, emittedLog{severity, append([]telemetry.Attr(nil), attrs...)})
	return nil
}
func (f *fakeEmitter) Counter(_ context.Context, name string, value float64, attrs ...telemetry.Attr) error {
	if name != semconv.MetricAuditEvents || value != 1 {
		panic(name)
	}
	f.metrics = append(f.metrics, emittedMetric{append([]telemetry.Attr(nil), attrs...)})
	return nil
}
func event(id string, when time.Time, result string) auditEvent {
	var e auditEvent
	e.ID = id
	e.Action.Time = when
	e.Action.Type = "update"
	e.Action.Result = result
	e.Actor.Email = "person@example.com"
	e.Actor.IPAddress = "192.0.2.1"
	e.Raw.RayID = "example-ray"
	e.Resource.Product = "workers"
	return e
}
func attributes(attrs []telemetry.Attr) map[string]string {
	out := map[string]string{}
	for _, a := range attrs {
		out[a.Key] = a.Value
	}
	return out
}

func TestAuditCursorBoundaryAndAttributes(t *testing.T) {
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	a := event("a", from, "success")
	b := event("b", from.Add(time.Second), "failure")
	c := event("c", to, "success")
	f := &fakeAPI{pages: []auditPage{{Result: []auditEvent{a}}, {Result: []auditEvent{a, b}}, {Result: []auditEvent{c}}}}
	f.pages[0].ResultInfo.Cursor = "first"
	f.pages[1].ResultInfo.Cursor = "second"
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "example-account"
	out := &fakeEmitter{}
	mark, err := newLogs(collector.Deps{Config: &cfg, API: f}).CollectWindow(context.Background(), from, to, out)
	if err != nil || !mark.Equal(to) {
		t.Fatalf("mark=%v err=%v", mark, err)
	}
	if len(f.queries) != 3 || f.queries[1].Get("cursor") != "first" || f.queries[2].Get("cursor") != "second" {
		t.Fatalf("pagination: %v", f.queries)
	}
	if len(out.logs) != 2 || len(out.metrics) != 2 {
		t.Fatalf("logs=%d metrics=%d", len(out.logs), len(out.metrics))
	}
	if out.logs[0].severity != otellog.SeverityInfo || out.logs[1].severity != otellog.SeverityWarn {
		t.Fatal("severity mapping")
	}
	logAttrs := attributes(out.logs[0].attrs)
	if logAttrs[semconv.AttrAuditActorEmail] != "person@example.com" || logAttrs[semconv.AttrAuditActorIP] != "192.0.2.1" || logAttrs[semconv.AttrAuditRayID] != "example-ray" {
		t.Fatal("audit log missing actor/raw attributes")
	}
	metricAttrs := attributes(out.metrics[0].attrs)
	if len(metricAttrs) != 3 || metricAttrs[semconv.AttrAuditResourceProduct] != "workers" {
		t.Fatalf("metric attrs: %v", metricAttrs)
	}
	for key := range metricAttrs {
		if strings.Contains(key, "email") || strings.Contains(key, "ip") || strings.Contains(key, "ray") {
			t.Fatalf("sensitive metric attr %s", key)
		}
	}
}

func TestAuditPageCapFailsClosed(t *testing.T) {
	f := &fakeAPI{endless: true}
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "example-account"
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	_, err := newLogs(collector.Deps{Config: &cfg, API: f}).CollectWindow(context.Background(), from, from.Add(time.Minute), &fakeEmitter{})
	if err == nil || !strings.Contains(err.Error(), "page limit") || len(f.queries) != pageLimit {
		t.Fatalf("queries=%d err=%v", len(f.queries), err)
	}
}

func TestAuditRestartBoundary(t *testing.T) {
	start := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	boundary := start.Add(30 * time.Minute)
	f := &fakeAPI{exclusiveSince: true, pages: []auditPage{{Result: []auditEvent{event("old", start.Add(time.Minute), "success"), event("boundary", boundary, "success")}}}}
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "example-account"
	out := &fakeEmitter{}
	path := filepath.Join(t.TempDir(), "checkpoints.json")
	for _, now := range []time.Time{boundary.Add(2 * time.Minute), boundary.Add(3 * time.Minute)} {
		f.queries = nil
		store, err := collector.NewFileStore(path)
		if err != nil {
			t.Fatal(err)
		}
		s := collector.NewScheduler(collector.NewRegistry(), out, store)
		s.Now = func() time.Time { return now }
		entry := collector.Entry{Collector: newLogs(collector.Deps{Config: &cfg, API: f}), InitialLookback: 30 * time.Minute}
		if err := s.RunOnce(context.Background(), entry); err != nil {
			t.Fatal(err)
		}
		querySince, err := time.Parse(time.RFC3339Nano, f.queries[0].Get("since"))
		if err != nil || !querySince.Before(now.Add(-2*time.Minute)) {
			t.Fatalf("query since=%s, want overlap before checkpoint", querySince)
		}
	}
	if len(out.logs) != 2 || len(out.metrics) != 2 {
		t.Fatalf("restart logs=%d metrics=%d", len(out.logs), len(out.metrics))
	}
}
