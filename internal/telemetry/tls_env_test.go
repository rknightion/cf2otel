package telemetry

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	otellog "go.opentelemetry.io/otel/log"

	"github.com/rknightion/cf2otel/internal/semconv"
)

func writePEM(t *testing.T, path, kind string, der []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOTLPHTTPExportHonoursCertificateEnvironment(t *testing.T) {
	dir := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fixture-client"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	clientDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := x509.ParseCertificate(clientDER)
	if err != nil {
		t.Fatal(err)
	}
	var authenticated atomic.Bool
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/logs" && r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			authenticated.Store(true)
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	clientCAs := x509.NewCertPool()
	clientCAs.AddCert(clientCert)
	srv.EnableHTTP2 = true
	srv.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: clientCAs}
	srv.StartTLS()
	defer srv.Close()
	writePEM(t, filepath.Join(dir, "ca.pem"), "CERTIFICATE", srv.Certificate().Raw)
	writePEM(t, filepath.Join(dir, "client.pem"), "CERTIFICATE", clientDER)
	writePEM(t, filepath.Join(dir, "client.key"), "EC PRIVATE KEY", keyDER)
	t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", filepath.Join(dir, "ca.pem"))
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_CLIENT_CERTIFICATE", filepath.Join(dir, "client.pem"))
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_CLIENT_KEY", filepath.Join(dir, "client.key"))
	providers, err := NewProviders(context.Background(), ProviderOptions{Endpoint: srv.URL, Interval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = providers.Shutdown(ctx)
	}()
	seq := providers.BeginCommit()
	if err := providers.Emitter.LogEvent(context.Background(), semconv.EventWindowGap, "probe", time.Now(), otellog.SeverityInfo); err != nil {
		t.Fatal(err)
	}
	if err := providers.FlushCommit(context.Background(), seq); err != nil {
		t.Fatalf("export with environment CA and client certificate failed: %v", err)
	}
	if !authenticated.Load() {
		t.Fatal("log export did not present the per-signal client certificate")
	}
}
