package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rknightion/cf2otel/internal/config"
)

func TestPrometheusModesAndOccupiedBind(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Content-Type", "application/x-protobuf") }))
	defer upstream.Close()
	path := writeRunConfig(t, t.TempDir(), upstream.URL, upstream.URL)
	t.Setenv("CF2OTEL_CLOUDFLARE__API_TOKEN", "fixture-token")
	t.Setenv("CF2OTEL_OTLP__GRAFANA_CLOUD__TOKEN", "fixture-token")
	t.Setenv("CF2OTEL_PROMETHEUS__ENABLED", "true")
	t.Setenv("CF2OTEL_PROMETHEUS__LISTEN", occupied.Addr().String())
	for name := range config.Default().Collectors {
		t.Setenv("CF2OTEL_COLLECTORS__"+strings.ToUpper(strings.ReplaceAll(name, ".", "_"))+"__ENABLED", "false")
	}
	for _, args := range [][]string{{"-dry-run", "-once"}, {"-print-effective-config"}, {"-validate"}} {
		_, err := captureStdout(t, func() error { return run(append([]string{"-config", path}, args...)) })
		if err != nil {
			t.Fatalf("mode %v opened pull listener: %v", args, err)
		}
	}
	// The real OTLP exporter must have HTTPS when credentials are present.
	// Use a local TLS upstream trusted by the test transport, never a live tenant.
	tlsUpstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Content-Type", "application/x-protobuf") }))
	defer tlsUpstream.Close()
	originalTransport := http.DefaultTransport
	http.DefaultTransport = tlsUpstream.Client().Transport
	defer func() { http.DefaultTransport = originalTransport }()
	path = writeRunConfig(t, t.TempDir(), upstream.URL, tlsUpstream.URL)
	if err := run([]string{"-config", path, "-once"}); err != nil {
		t.Fatalf("once opened pull listener: %v", err)
	}
	if err := run([]string{"-config", path}); err == nil || !strings.Contains(err.Error(), "prometheus listener") {
		t.Fatalf("occupied bind did not fail startup: %v", err)
	}
}
