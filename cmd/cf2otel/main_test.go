package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDryRunKeepsCheckpointBytesAndDoesNotExport(t *testing.T) {
	started := time.Now().UTC().Truncate(time.Second)
	rowTime := started.Add(-2 * time.Minute)
	var cloudflareRequests atomic.Int32
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cloudflareRequests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/accounts/test-account/access/logs/access_requests" {
			t.Errorf("unexpected Cloudflare request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"success":true,"result":[{"user_email":"%s","user_id":"test-user","ip_address":"192.0.2.10","country":"GB","app_uid":"test-app","app_name":"Test App","app_domain":"https://app.example.com/","app_type":"self_hosted","action":"login","connection":"warp","allowed":true,"ray_id":"test-ray-id","created_at":%q}],"result_info":{"per_page":1000}}`, "sample"+"@"+"example.com", rowTime.Format(time.RFC3339Nano))
	}))
	defer apiServer.Close()

	var otlpRequests atomic.Int32
	otlpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otlpRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer otlpServer.Close()

	stateDir := t.TempDir()
	configPath := writeRunConfig(t, stateDir, apiServer.URL, otlpServer.URL)
	checkpointPath := filepath.Join(stateDir, "checkpoints.json")
	seed, err := json.Marshal(map[string]time.Time{"access.logins": started.Add(-5 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checkpointPath, seed, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CF2OTEL_CLOUDFLARE__API_TOKEN", "test-token")
	t.Setenv("CF2OTEL_OTLP__GRAFANA_CLOUD__TOKEN", "test-token")
	output, err := captureStdout(t, func() error {
		return run([]string{"-config", configPath, "-dry-run", "-once", "-datasets", "access.logins"})
	})
	if err != nil {
		t.Fatalf("run dry-run: %v", err)
	}
	got, err := os.ReadFile(checkpointPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, seed) {
		t.Fatalf("dry-run changed checkpoint bytes\n before: %s\n after:  %s", seed, got)
	}
	if cloudflareRequests.Load() == 0 {
		t.Fatal("dry-run did not execute the seeded collector window")
	}
	if got := otlpRequests.Load(); got != 0 {
		t.Fatalf("dry-run sent %d OTLP requests, want none", got)
	}
	line := output
	var records, metrics int
	if _, err := fmt.Sscanf(line, "dry-run access.logins: records=%d metrics=%d", &records, &metrics); err != nil {
		t.Fatalf("dry-run summary missing per-collector record and metric counts: %q", line)
	}
	if records == 0 || metrics == 0 {
		t.Fatalf("dry-run summary did not count emitted records and metrics: %q", line)
	}
}

func TestUnknownDatasetListsValidNames(t *testing.T) {
	stateDir := t.TempDir()
	configPath := writeRunConfig(t, stateDir, "http://127.0.0.1:1", "http://127.0.0.1:1")
	t.Setenv("CF2OTEL_CLOUDFLARE__API_TOKEN", "test-token")
	t.Setenv("CF2OTEL_OTLP__GRAFANA_CLOUD__TOKEN", "test-token")
	err := run([]string{"-config", configPath, "-dry-run", "-once", "-datasets", "unknown.collector"})
	if err == nil {
		t.Fatal("accepted unknown dataset")
	}
	if !strings.Contains(err.Error(), "unknown.collector") || !strings.Contains(err.Error(), "access.logins") {
		t.Fatalf("unknown dataset error does not list valid names: %v", err)
	}
}

func writeRunConfig(t *testing.T, stateDir, apiBase, otlpEndpoint string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := fmt.Sprintf("cloudflare:\n  account_id: test-account\n  api_base: %q\notlp:\n  endpoint: %q\n  grafana_cloud:\n    instance_id: test-instance\nstate:\n  dir: %q\nidentity:\n  enabled: false\n", apiBase, otlpEndpoint, stateDir)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func captureStdout(t *testing.T, call func() error) (string, error) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	callErr := call()
	_ = writer.Close()
	os.Stdout = previous
	output, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(output), callErr
}
