package tunnels_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/collectors/tunnels"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

func registered(t *testing.T, api cfapi.Client) collector.SnapshotCollector {
	t.Helper()
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "account-example"
	registry := collector.NewRegistry()
	tunnels.Register(collector.Deps{Config: &cfg, API: api, Registry: registry})
	if len(registry.Entries()) != 0 {
		t.Fatal("tunnels must be disabled by default")
	}
	cfg.Collectors["tunnels.status"] = config.CollectorConfig{Enabled: true}
	tunnels.Register(collector.Deps{Config: &cfg, API: api, Registry: registry})
	entries := registry.Entries()
	if len(entries) != 1 {
		t.Fatalf("enabled tunnels.status registered %d collectors; want 1", len(entries))
	}
	if entries[0].Collector.Name() != "tunnels.status" || entries[0].Interval != time.Minute {
		t.Fatalf("unexpected entry: %+v", entries[0])
	}
	c, ok := entries[0].Collector.(collector.SnapshotCollector)
	if !ok {
		t.Fatal("tunnels.status is not a snapshot collector")
	}
	if _, ok := entries[0].Collector.(collector.WindowCollector); ok {
		t.Fatal("snapshot must not use window/checkpoint path")
	}
	return c
}

func attr(attrs []telemetry.Attr, key string) string {
	for _, a := range attrs {
		if a.Key == key {
			return a.Value
		}
	}
	return ""
}

func TestRegisteredSnapshotPaginationAndStatusChange(t *testing.T) {
	status := "healthy"
	failPage := false
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/accounts/account-example/cfd_tunnel" || r.URL.Query().Get("is_deleted") != "false" || r.URL.Query().Get("per_page") != "100" {
			t.Errorf("unexpected REST request: %s %s", r.Method, r.URL)
			http.Error(w, "bad request", 400)
			return
		}
		switch r.URL.Query().Get("page") {
		case "1":
			fmt.Fprintf(w, `{"success":true,"result":[{"id":"tunnel-example","name":"example.com","status":%q,"deleted_at":null,"connections":[{"client_id":"connector-one","client_version":"2026.1","colo_name":"LHR","is_pending_reconnect":false},{"client_id":"connector-one","client_version":"2026.1","colo_name":"LHR","is_pending_reconnect":false},{"client_id":"connector-two","client_version":"2026.2","colo_name":"AMS","is_pending_reconnect":false},{"client_id":"connector-two","client_version":"2026.2","colo_name":"AMS","is_pending_reconnect":true}]}],"result_info":{"page":1,"per_page":1,"total_count":2}}`, status)
		case "2":
			if failPage {
				fmt.Fprint(w, `{"success":true,"result":{}}`)
				return
			}
			fmt.Fprint(w, `{"success":true,"result":[{"id":"tunnel-deleted","name":"deleted.example.com","status":"down","deleted_at":"2026-01-01T00:00:00Z"}],"result_info":{"page":2,"per_page":1,"total_count":2}}`)
		case "3":
			fmt.Fprint(w, `{"success":true,"result":[],"result_info":{"page":3,"per_page":1,"total_count":2}}`)
		default:
			t.Errorf("unexpected page: %s", r.URL.Query().Get("page"))
			fmt.Fprint(w, `{"success":true,"result":[]}`)
		}
	}))
	defer server.Close()
	c := registered(t, cfapi.New(config.CloudflareConfig{APIBase: server.URL}))
	ctx := context.Background()
	first := &telemetry.Buffer{}
	if err := c.Collect(ctx, first); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("list pagination calls=%d; want 3 and no per-tunnel calls", calls)
	}
	if len(first.Records) != 0 {
		t.Fatal("first poll emitted a status event")
	}
	values := map[string]float64{}
	for _, m := range first.Metrics {
		if m.Kind != "gauge" || attr(m.Attrs, semconv.AttrTunnelID) != "tunnel-example" || attr(m.Attrs, semconv.AttrTunnelName) != "example.com" {
			t.Fatalf("unexpected metric: %+v", m)
		}
		switch m.Name {
		case semconv.MetricTunnelStatus:
			if attr(m.Attrs, semconv.AttrTunnelStatus) != "healthy" {
				t.Fatalf("wrong current status: %+v", m)
			}
			values["status"] = m.Value
		case semconv.MetricTunnelConnections:
			values["colo:"+attr(m.Attrs, semconv.AttrTunnelColo)] = m.Value
		case semconv.MetricTunnelConnectors:
			values["version:"+attr(m.Attrs, semconv.AttrTunnelConnectorVersion)] = m.Value
		default:
			t.Fatalf("unexpected signal: %+v", m)
		}
	}
	for key, want := range map[string]float64{"status": 1, "colo:LHR": 2, "colo:AMS": 1, "version:2026.1": 1, "version:2026.2": 1} {
		if got, ok := values[key]; !ok || got != want {
			t.Fatalf("%s=%v (present=%v); want %v", key, got, ok, want)
		}
	}
	if len(first.Metrics) != 5 {
		t.Fatalf("unexpected extra metrics: %+v", first.Metrics)
	}
	status = "down"
	failPage = true
	failed := &telemetry.Buffer{}
	if err := c.Collect(ctx, failed); err == nil {
		t.Fatal("malformed second page must fail snapshot")
	}
	if len(failed.Metrics)+len(failed.Records) != 0 {
		t.Fatal("incomplete snapshot emitted telemetry")
	}
	failPage = false
	rejected := &rejectGauge{Buffer: &telemetry.Buffer{}}
	if err := c.Collect(ctx, rejected); err == nil {
		t.Fatal("emitter failure must fail snapshot")
	}
	second := &telemetry.Buffer{}
	if err := c.Collect(ctx, second); err != nil {
		t.Fatal(err)
	}
	if len(second.Records) != 1 {
		t.Fatalf("status flip emitted %d events; want exactly 1 (failed polls must not advance state)", len(second.Records))
	}
	event := second.Records[0]
	if event.Event != semconv.EventTunnelStatusChange || event.At.IsZero() || attr(event.Attrs, semconv.AttrTunnelPreviousStatus) != "healthy" || attr(event.Attrs, semconv.AttrTunnelStatus) != "down" || attr(event.Attrs, semconv.AttrTunnelID) != "tunnel-example" || attr(event.Attrs, semconv.AttrTunnelName) != "example.com" {
		t.Fatalf("invalid status-change event: %+v", event)
	}
	third := &telemetry.Buffer{}
	if err := c.Collect(ctx, third); err != nil {
		t.Fatal(err)
	}
	if len(third.Records) != 0 {
		t.Fatal("unchanged status emitted another event")
	}
}

type rejectGauge struct{ *telemetry.Buffer }

func (*rejectGauge) Gauge(context.Context, string, float64, ...telemetry.Attr) error {
	return errors.New("fixture emitter failure")
}

func TestRegisteredEmptySnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":true,"result":[],"result_info":{"page":1,"per_page":100,"total_count":0}}`)
	}))
	defer server.Close()
	c := registered(t, cfapi.New(config.CloudflareConfig{APIBase: server.URL}))
	b := &telemetry.Buffer{}
	if err := c.Collect(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if len(b.Metrics)+len(b.Records) != 0 {
		t.Fatal("empty list fabricated telemetry")
	}
}
