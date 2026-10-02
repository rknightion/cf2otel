package telemetry_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/collectors/selfobs"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	colmetricpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"
)

func TestMetricHTTPPartialSuccessOutcome(t *testing.T) {
	for _, tc := range []struct {
		name        string
		response    *colmetricpb.ExportMetricsServiceResponse
		wantFailure bool
	}{
		{name: "full_acceptance", response: &colmetricpb.ExportMetricsServiceResponse{}},
		{
			name: "rejected_datapoint",
			response: &colmetricpb.ExportMetricsServiceResponse{PartialSuccess: &colmetricpb.ExportMetricsPartialSuccess{
				RejectedDataPoints: 1, ErrorMessage: "rejection-sentinel",
			}},
			// The silent-acceptance spike hypothesis failed on the pinned SDK:
			// a valid protobuf rejection reaches the caller and observer as an error.
			wantFailure: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := proto.Marshal(tc.response)
			if err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/v1/metrics" || r.Header.Get("Content-Type") != "application/x-protobuf" {
					t.Errorf("unexpected OTLP request: method=%s path=%s content-type=%s", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				data, readErr := io.ReadAll(r.Body)
				if readErr != nil {
					t.Error(readErr)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				var request colmetricpb.ExportMetricsServiceRequest
				if decodeErr := proto.Unmarshal(data, &request); decodeErr != nil {
					t.Error(decodeErr)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				points := 0
				for _, resource := range request.ResourceMetrics {
					for _, scope := range resource.ScopeMetrics {
						for _, metric := range scope.Metrics {
							points += len(metric.GetSum().GetDataPoints())
						}
					}
				}
				if points < 2 {
					t.Errorf("request has %d sum datapoints, want at least 2 so rejection is partial", points)
				}
				requests.Add(1)
				w.Header().Set("Content-Type", "application/x-protobuf")
				w.WriteHeader(http.StatusOK)
				if _, writeErr := w.Write(body); writeErr != nil {
					t.Error(writeErr)
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			providers, err := telemetry.NewProviders(ctx, telemetry.ProviderOptions{Endpoint: server.URL, Protocol: "http", Interval: time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			// Capture the same self-observability callback used by the CLI, without
			// feeding its outcome metrics back into the exporter under observation.
			outcomes := &telemetry.Buffer{}
			stats := selfobs.New(outcomes, "", "")
			var observed []error
			providers.SetExportObserver(func(ctx context.Context, signal string, exportErr error) {
				if signal != "metrics" {
					t.Errorf("unexpected export signal %q", signal)
					return
				}
				observed = append(observed, exportErr)
				if emitErr := stats.Export(ctx, signal, exportErr); emitErr != nil {
					t.Error(emitErr)
				}
			})
			for _, name := range []string{semconv.MetricAPIRequests, semconv.MetricAPIRetries} {
				if err := providers.Emitter.Counter(ctx, name, 1); err != nil {
					t.Error(err)
				}
			}
			// Shutdown forces the periodic reader's final collection through the
			// production metric wrappers and real pinned HTTP exporter.
			shutdownErr := providers.Shutdown(ctx)
			if requests.Load() != 1 || len(observed) != 1 {
				t.Fatalf("requests=%d observer calls=%d, want one each", requests.Load(), len(observed))
			}
			t.Logf("HTTP 200 rejected_data_points=%d: shutdown=%v observer=%v", tc.response.GetPartialSuccess().GetRejectedDataPoints(), shutdownErr, observed[0])
			if (observed[0] != nil) != tc.wantFailure {
				t.Fatalf("observer error=%v, want failure=%t", observed[0], tc.wantFailure)
			}
			if (shutdownErr != nil) != tc.wantFailure {
				t.Fatalf("shutdown error=%v, want failure=%t", shutdownErr, tc.wantFailure)
			}
			wantMetric := semconv.MetricExportSuccess
			if tc.wantFailure {
				wantMetric = semconv.MetricExportErrors
			}
			if len(outcomes.Metrics) != 1 {
				t.Fatalf("outcome metrics=%v, want one", outcomes.Metrics)
			}
			got := outcomes.Metrics[0]
			if got.Name != wantMetric || got.Value != 1 || len(got.Attrs) != 1 || got.Attrs[0] != (telemetry.Attr{Key: semconv.AttrExportSignal, Value: "metrics"}) {
				t.Fatalf("outcome metric=%+v, want %s=1 for metrics", got, wantMetric)
			}
		})
	}
}
