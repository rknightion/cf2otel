package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	otellog "go.opentelemetry.io/otel/log"

	"github.com/rknightion/cf2otel/internal/semconv"
)

func TestCommitRecoversWhenOTLPConnectionIsSilencedByOutage(t *testing.T) {
	var logsOverHTTP2 atomic.Bool
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/logs" {
			logsOverHTTP2.Store(r.ProtoMajor == 2)
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	original := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	defer func() { http.DefaultTransport = original }()
	proxy := newBlackholeProxy(t, srv.Listener.Addr().String())
	providers, err := NewProviders(context.Background(), ProviderOptions{Endpoint: "https://" + proxy.Addr(), Interval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = providers.Shutdown(ctx)
	}()
	commit := func() error {
		seq := providers.BeginCommit()
		if err := providers.Emitter.LogEvent(context.Background(), semconv.EventWindowGap, "probe", time.Now(), otellog.SeverityInfo); err != nil {
			return err
		}
		return providers.FlushCommit(context.Background(), seq)
	}
	if err := commit(); err != nil {
		t.Fatal(err)
	}
	if !logsOverHTTP2.Load() {
		t.Fatal("fixture did not negotiate HTTP/2")
	}
	proxy.Outage()
	if err := commit(); err == nil {
		t.Fatal("commit succeeded during the outage")
	}
	proxy.Restore()
	deadline := time.Now().Add(45 * time.Second)
	for err := commit(); err != nil; err = commit() {
		if time.Now().After(deadline) {
			t.Fatalf("commit still failing 45s after the network returned: %v", err)
		}
	}
}
