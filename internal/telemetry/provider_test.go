package telemetry

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type failingMetricExporter struct {
	sdkmetric.Exporter
	err error
}

func (f failingMetricExporter) Export(context.Context, *metricdata.ResourceMetrics) error {
	return f.err
}

func TestExportObserverReceivesActualResult(t *testing.T) {
	hook := &exportObserver{}
	var gotSignal string
	var gotErr error
	hook.Set(func(_ context.Context, signal string, err error) { gotSignal, gotErr = signal, err })
	want := errors.New("export failed")
	exporter := observedMetricExporter{Exporter: failingMetricExporter{err: want}, hook: hook}
	if err := exporter.Export(context.Background(), &metricdata.ResourceMetrics{}); !errors.Is(err, want) {
		t.Fatalf("export result: %v", err)
	}
	if gotSignal != "metrics" || !errors.Is(gotErr, want) {
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
