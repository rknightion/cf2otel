package health

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rknightion/cf2otel/internal/config"
)

func TestHandlerRedactsAndProbes(t *testing.T) {
	c := config.Default()
	c.Cloudflare.APIToken = "cloudflare-test-secret"
	c.OTLP.GrafanaCloud.Token = "grafana-test-secret"
	c.OTLP.Headers["Authorization"] = "header-test-secret"
	s := httptest.NewServer(Handler(&c))
	defer s.Close()
	if err := Probe(context.Background(), s.URL); err != nil {
		t.Fatal(err)
	}
	r, err := http.Get(s.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"cloudflare-test-secret", "grafana-test-secret", "header-test-secret"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
	if strings.Contains(string(b), "config") {
		t.Fatal("health response exposed configuration")
	}
}

func TestProbeRejectsWrongPathAndFailure(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "bad", http.StatusServiceUnavailable) }))
	defer s.Close()
	if err := Probe(context.Background(), s.URL); err == nil {
		t.Fatal("expected failure")
	}
	if err := Probe(context.Background(), "https://example.com/other"); err == nil {
		t.Fatal("expected invalid target")
	}
}

func TestListenRejectsNonLoopback(t *testing.T) {
	c := config.Default()
	c.Health.Listen = "0.0.0.0:0"
	if _, err := Listen(&c); err == nil {
		t.Fatal("accepted non-loopback listener")
	}
}
