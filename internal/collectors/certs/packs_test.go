package certs

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
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
	otellog "go.opentelemetry.io/otel/log"
)

type point struct {
	name  string
	value float64
	attrs []telemetry.Attr
}
type recordingEmitter struct{ points []point }

func (e *recordingEmitter) Gauge(_ context.Context, n string, v float64, a ...telemetry.Attr) error {
	e.points = append(e.points, point{n, v, a})
	return nil
}
func (*recordingEmitter) Counter(context.Context, string, float64, ...telemetry.Attr) error {
	return nil
}
func (*recordingEmitter) Histogram(context.Context, string, float64, ...telemetry.Attr) error {
	return nil
}
func (*recordingEmitter) LogEvent(context.Context, string, string, time.Time, otellog.Severity, ...telemetry.Attr) error {
	return nil
}
func (*recordingEmitter) Span(context.Context, telemetry.SpanSpec) error { return nil }

func snapshot(t *testing.T, handler http.HandlerFunc, observer cfapi.Observer) collector.SnapshotCollector {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := config.Default()
	cc := cfg.Collectors["certs.packs"]
	cc.Enabled = true
	cfg.Collectors["certs.packs"] = cc
	cfg.Cloudflare.APIBase = server.URL
	reg := collector.NewRegistry()
	Register(collector.Deps{Config: &cfg, API: cfapi.NewObserved(cfg.Cloudflare, observer), Registry: reg})
	entries := reg.Entries()
	if len(entries) != 1 {
		t.Fatalf("registered snapshots = %d, want 1", len(entries))
	}
	c, ok := entries[0].Collector.(collector.SnapshotCollector)
	if !ok {
		t.Fatal("not a snapshot collector")
	}
	if c.Name() != "certs.packs" || entries[0].Interval != time.Hour || c.DefaultInterval() != time.Hour || entries[0].InitialLookback != 0 {
		t.Fatalf("snapshot contract: %+v", entries[0])
	}
	return c
}
func zones(w http.ResponseWriter) {
	_, _ = fmt.Fprint(w, `{"success":true,"result":[{"id":"zone-demo","name":"example.com"}],"result_info":{"page":1,"per_page":100,"total_count":1,"total_pages":1}}`)
}

func TestRegisterSnapshotExpiryAndPagination(t *testing.T) {
	expiry := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	calls := 0
	c := snapshot(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/zones" {
			zones(w)
			return
		}
		if r.Method != "GET" || r.URL.Path != "/zones/zone-demo/ssl/certificate_packs" || r.URL.Query().Get("status") != "all" || r.URL.Query().Get("per_page") != "5" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.Error(w, "bad request", 400)
			return
		}
		calls++
		if r.URL.Query().Get("page") == "1" {
			_, _ = fmt.Fprintf(w, `{"result":[{"id":"pack-demo","type":"universal","certificate_authority":"google","status":"active","certificates":[{"expires_on":%q},{"expires_on":%q}]},{"id":"pack-empty","certificates":[]},{"id":"pack-null","certificates":null},{"id":"pack-malformed","certificates":[{"expires_on":"bad"}]},{"id":"pack-missing","certificates":[{"expires_on":null}]}]}`, expiry.Add(time.Hour).Format(time.RFC3339), expiry.Format(time.RFC3339))
			return
		}
		_, _ = fmt.Fprintf(w, `{"result":[{"id":"pack-expired","type":"advanced","certificate_authority":"ssl_com","status":"backup_issued","certificates":[{"expires_on":%q}]}]}`, expiry.Add(-48*time.Hour).Format(time.RFC3339))
	}, nil)
	e := &recordingEmitter{}
	before := time.Now()
	if err := c.Collect(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	after := time.Now()
	if calls != 2 || len(e.points) != 2 {
		t.Fatalf("pages=%d points=%+v", calls, e.points)
	}
	for i, p := range e.points {
		want := expiry
		if i == 1 {
			want = expiry.Add(-48 * time.Hour)
		}
		if p.name != semconv.MetricCertificateExpiry || p.value < want.Sub(after).Seconds() || p.value > want.Sub(before).Seconds() {
			t.Fatalf("expiry point=%+v", p)
		}
		attrs := map[string]string{}
		for _, a := range p.attrs {
			attrs[a.Key] = a.Value
		}
		id, typ, authority, status := "pack-demo", "universal", "google", "active"
		if i == 1 {
			id, typ, authority, status = "pack-expired", "advanced", "ssl_com", "backup_issued"
		}
		if len(attrs) != 5 || attrs[semconv.AttrCertificateZone] != "example.com" || attrs[semconv.AttrCertificatePackID] != id || attrs[semconv.AttrCertificateType] != typ || attrs[semconv.AttrCertificateAuthority] != authority || attrs[semconv.AttrCertificateStatus] != status {
			t.Fatalf("attrs=%v", attrs)
		}
	}
}

func TestPermissionPartialSuccessAndWarningThrottle(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	apiErrors := 0
	c := snapshot(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/zones" {
			_, _ = fmt.Fprint(w, `{"result":[{"id":"zone-denied","name":"denied.example.com"},{"id":"zone-ok","name":"example.com"}],"result_info":{"total_pages":1,"page":1,"per_page":100,"total_count":2}}`)
			return
		}
		if strings.Contains(r.URL.Path, "zone-denied") {
			w.WriteHeader(403)
			_, _ = fmt.Fprint(w, `{"errors":[{"code":9109}]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"result":[]}`)
	}, func(_, _ string, status int, _ time.Duration, _ bool) {
		if status == 403 {
			apiErrors++
		}
	})
	for range 2 {
		if err := c.Collect(context.Background(), &recordingEmitter{}); err != nil {
			t.Fatal(err)
		}
	}
	if apiErrors != 2 || strings.Count(logs.String(), "level=WARN") != 1 {
		t.Fatalf("API errors=%d warnings=%s", apiErrors, logs.String())
	}
}

func TestEmptyAndFailedSnapshots(t *testing.T) {
	for _, kind := range []string{"empty", "denied", "malformed", "null", "later-page-failure", "discovery-failure"} {
		t.Run(kind, func(t *testing.T) {
			c := snapshot(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/zones" {
					if kind == "discovery-failure" {
						http.Error(w, "error", 500)
						return
					}
					zones(w)
					return
				}
				switch kind {
				case "empty":
					_, _ = fmt.Fprint(w, `{"result":[]}`)
				case "null":
					_, _ = fmt.Fprint(w, `{"result":null}`)
				case "malformed":
					_, _ = fmt.Fprint(w, `{"result":{"unexpected":true}}`)
				case "later-page-failure":
					if r.URL.Query().Get("page") == "1" {
						_, _ = fmt.Fprint(w, `{"result":[{},{},{},{},{}]}`)
						return
					}
					http.Error(w, "error", 500)
				default:
					w.WriteHeader(403)
					_, _ = fmt.Fprint(w, `{"errors":[{"code":9109}]}`)
				}
			}, nil)
			e := &recordingEmitter{}
			err := c.Collect(context.Background(), e)
			if (err == nil) != (kind == "empty" || kind == "null") {
				t.Fatalf("%s error=%v", kind, err)
			}
			if len(e.points) != 0 {
				t.Fatalf("unknown expiry emitted: %+v", e.points)
			}
		})
	}
}

func TestDisabledByDefault(t *testing.T) {
	cfg := config.Default()
	reg := collector.NewRegistry()
	Register(collector.Deps{Config: &cfg, Registry: reg})
	if len(reg.Entries()) != 0 {
		t.Fatal("default collector enabled")
	}
}
