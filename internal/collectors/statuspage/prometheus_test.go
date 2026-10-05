package statuspage_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/promexport"
	"github.com/rknightion/cf2otel/internal/telemetry"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/trace/noop"
)

// The dashboard query must use the actual Prometheus OTLP translation, not a
// guessed semantic name. Drive the registered collector and real pull endpoint.
func TestComponentsPrometheusDashboardName(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pull, err := promexport.New()
	if err != nil {
		t.Fatal(err)
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(pull.Reader))
	lp := sdklog.NewLoggerProvider()
	defer func() { _ = mp.Shutdown(ctx); _ = lp.Shutdown(ctx) }()
	e := telemetry.NewEmitter(mp.Meter("fixture"), lp.Logger("fixture"), noop.NewTracerProvider().Tracer("fixture"))
	endpoint, err := pull.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serving, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- endpoint.Run(serving) }()
	defer func() {
		stop()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	summary := `{"components":[{"name":"opaque","status":"major_outage","group":false}]}`
	incidents := `{"incidents":[]}`
	srv := server(t, &summary, &incidents)
	r, components, _ := registered(t, srv.URL, time.Minute, 500)
	s := collector.NewScheduler(r, e, nil)
	run(t, s, components)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+endpoint.Addr().String()+"/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	want := `cloudflare_status_component_status_ratio{cloudflare_status_component_name="opaque",cloudflare_status_component_type="component"} 4`
	if resp.StatusCode != http.StatusOK || !strings.Contains(text, want) {
		t.Fatalf("dashboard gauge name/labels/value absent from real scrape: status=%d body=%s", resp.StatusCode, text)
	}
}
