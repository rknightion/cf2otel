package telemetry

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type failingMetricExporter struct {
	sdkmetric.Exporter
	err error
}

type failingLogExporter struct {
	sdklog.Exporter
	err error
}

func (f failingLogExporter) Export(context.Context, []sdklog.Record) error { return f.err }

func TestExporterErrorOmitsResponseBody(t *testing.T) {
	hook := &exportObserver{}
	start := hook.snapshot()
	input := errors.New("failed to send to https://example.com/v1/logs: 400 Bad Request (body: secret-prompt-sentinel)")
	exporter := observedLogExporter{Exporter: failingLogExporter{err: input}, hook: hook}
	err := exporter.Export(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "400 Bad Request") {
		t.Fatalf("missing safe status: %v", err)
	}
	if strings.Contains(err.Error(), "secret-prompt-sentinel") || strings.Contains(hook.failedSince(start).Error(), "secret-prompt-sentinel") {
		t.Fatal("exporter response leaked into diagnostics")
	}
}

func (f failingMetricExporter) Export(context.Context, *metricdata.ResourceMetrics) error {
	return f.err
}

func TestExportObserverReceivesActualResult(t *testing.T) {
	hook := &exportObserver{}
	var gotSignal string
	var gotErr error
	hook.Set(func(_ context.Context, signal string, err error) { gotSignal, gotErr = signal, err })
	want := errors.New("secret backend detail")
	exporter := observedMetricExporter{Exporter: failingMetricExporter{err: want}, hook: hook}
	if err := exporter.Export(context.Background(), &metricdata.ResourceMetrics{}); err == nil || strings.Contains(err.Error(), want.Error()) {
		t.Fatalf("export result: %v", err)
	}
	if gotSignal != "metrics" || gotErr == nil || strings.Contains(gotErr.Error(), want.Error()) {
		t.Fatalf("observer signal=%q error=%v", gotSignal, gotErr)
	}
}

func TestFailureSignalsTracksAllBackgroundErrors(t *testing.T) {
	hook := &exportObserver{}
	start := hook.snapshot()
	hook.record(context.Background(), "logs", errors.New("400 Bad Request"))
	hook.record(context.Background(), "traces", errors.New("401 Unauthorized"))
	if got := FailureSignals(hook.failedSince(start)); got != "logs,traces" {
		t.Fatalf("failed signals = %q, want logs,traces", got)
	}
}

func TestCredentialEndpointRequiresHTTPS(t *testing.T) {
	_, err := NewProviders(context.Background(), ProviderOptions{Endpoint: "http://example.com/otlp", Protocol: "http", InstanceID: "test", Token: "secret"})
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("expected HTTPS error, got %v", err)
	}
}
