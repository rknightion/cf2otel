package telemetry

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	collectorpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

// Reflection lets this behavioral regression run against the pre-option base:
// there it exercises the old SDK default rather than failing to compile.
func cardinalityOptions(endpoint string, limit int) ProviderOptions {
	o := ProviderOptions{Endpoint: endpoint, Interval: time.Hour}
	field := reflect.ValueOf(&o).Elem().FieldByName("CardinalityLimit")
	if field.IsValid() {
		field.SetInt(int64(limit))
	}
	return o
}

type cardinalityCapture struct {
	mu      sync.Mutex
	metrics []*metricspb.Metric
}

func (c *cardinalityCapture) find(name string) *metricspb.Metric {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.metrics) - 1; i >= 0; i-- {
		if c.metrics[i].Name == name {
			return c.metrics[i]
		}
	}
	return nil
}
func testCardinalityProvider(t *testing.T, limit int) (*Providers, *cardinalityCapture) {
	t.Helper()
	capture := &cardinalityCapture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/metrics" {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			var request collectorpb.ExportMetricsServiceRequest
			if err := proto.Unmarshal(body, &request); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			capture.mu.Lock()
			for _, resource := range request.ResourceMetrics {
				for _, scope := range resource.ScopeMetrics {
					capture.metrics = append(capture.metrics, scope.Metrics...)
				}
			}
			capture.mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	p, err := NewProviders(context.Background(), cardinalityOptions(server.URL, limit))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	return p, capture
}

func flushCardinality(t *testing.T, p *Providers) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.metrics.ForceFlush(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestLoadedCardinalityLimitAvoidsDefaultOverflow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("otlp:\n  metric_cardinality_limit: 3000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p, capture := testCardinalityProvider(t, cfg.OTLP.MetricCardinalityLimit)
	assertCardinalitySeries(t, p, capture, 2500, 2500, false)
}

func TestProviderDefaultAndUnlimitedCardinality(t *testing.T) {
	for _, tc := range []struct {
		name        string
		limit, want int
		overflow    bool
	}{
		{"SDK default", 0, 2000, true}, {"unlimited", -1, 2500, false}, {"configured", 3, 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, c := testCardinalityProvider(t, tc.limit)
			assertCardinalitySeries(t, p, c, 2500, tc.want, tc.overflow)
		})
	}
}

func TestOverflowDatapointIsObservable(t *testing.T) {
	var warnings bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&warnings, nil)))
	defer slog.SetDefault(previousLogger)
	p, capture := testCardinalityProvider(t, 0)
	assertCardinalitySeries(t, p, capture, 2500, 2000, true)
	// Observations made during export enter the next SDK collection.
	flushCardinality(t, p)
	if got := strings.Count(warnings.String(), "metric cardinality limit reached"); got != 1 || !strings.Contains(warnings.String(), semconv.MetricDNSQueries) {
		t.Fatalf("want one warning naming instrument across two exports, got %s", warnings.String())
	}
	m := capture.find(semconv.MetricCardinalityOverflows)
	if m == nil {
		t.Fatal("overflow self-observability counter missing")
	}
	for _, point := range m.GetSum().DataPoints {
		for _, attr := range point.Attributes {
			if attr.Key == semconv.AttrInstrument && attr.Value.GetStringValue() == semconv.MetricDNSQueries && point.GetAsInt() == 1 {
				return
			}
		}
	}
	t.Fatalf("overflow counter did not name the instrument with count 1: %v", m)
}

func assertCardinalitySeries(t *testing.T, p *Providers, capture *cardinalityCapture, count, want int, overflow bool) {
	t.Helper()
	for i := 0; i < count; i++ {
		if err := p.Emitter.Counter(context.Background(), semconv.MetricDNSQueries, 1, Attr{Key: semconv.AttrDNSZone, Value: fmt.Sprintf("zone-%d.example.com", i)}); err != nil {
			t.Fatal(err)
		}
	}
	flushCardinality(t, p)
	m := capture.find(semconv.MetricDNSQueries)
	if m == nil {
		t.Fatal("DNS metric missing")
	}
	points := m.GetSum().DataPoints
	found := false
	var total float64
	for _, point := range points {
		total += point.GetAsDouble()
		for _, attr := range point.Attributes {
			if attr.Key == "otel.metric.overflow" && attr.Value.GetBoolValue() {
				found = true
			}
		}
	}
	if len(points) != want || found != overflow || total != float64(count) {
		t.Fatalf("datapoints=%d overflow=%t total=%v; want %d/%t/%d", len(points), found, total, want, overflow, count)
	}
}
