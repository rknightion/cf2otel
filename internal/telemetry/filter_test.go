package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/semconv"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/trace"
	logscollector "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	metricscollector "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	tracescollector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	tracespb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// Reflection makes the regression exercise the actual base SDK path before
// options exist, rather than failing to compile against that base.
func denyOptions(o *ProviderOptions, metrics, attributes []string) {
	for name, values := range map[string][]string{"MetricDenylist": metrics, "AttributeDenylist": attributes} {
		field := reflect.ValueOf(o).Elem().FieldByName(name)
		if field.IsValid() {
			field.Set(reflect.ValueOf(values))
		}
	}
}

func TestDenylistOTLPBufferedSignals(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var mu sync.Mutex
	var metrics []*metricspb.Metric
	var logs []*logspb.LogRecord
	var spans []*tracespb.Span
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/v1/metrics":
			var req metricscollector.ExportMetricsServiceRequest
			if err := proto.Unmarshal(body, &req); err != nil {
				t.Error(err)
				return
			}
			metrics = nil
			for _, rm := range req.ResourceMetrics {
				resourceKept := false
				for _, a := range rm.Resource.Attributes {
					if a.Key == semconv.AttrServiceName && a.Value.GetStringValue() == semconv.ServiceName {
						resourceKept = true
					}
				}
				if !resourceKept {
					t.Error("resource service.name removed by signal deny policy")
				}
				for _, sm := range rm.ScopeMetrics {
					metrics = append(metrics, sm.Metrics...)
				}
			}
		case "/v1/logs":
			var req logscollector.ExportLogsServiceRequest
			if err := proto.Unmarshal(body, &req); err != nil {
				t.Error(err)
				return
			}
			for _, rm := range req.ResourceLogs {
				for _, sl := range rm.ScopeLogs {
					logs = append(logs, sl.LogRecords...)
				}
			}
		case "/v1/traces":
			var req tracescollector.ExportTraceServiceRequest
			if err := proto.Unmarshal(body, &req); err != nil {
				t.Error(err)
				return
			}
			for _, rm := range req.ResourceSpans {
				for _, ss := range rm.ScopeSpans {
					spans = append(spans, ss.Spans...)
				}
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	o := ProviderOptions{Endpoint: server.URL, Interval: time.Hour}
	denyOptions(&o, []string{semconv.MetricAPIRequests}, []string{semconv.AttrAccessUserID, semconv.AttrEventName, semconv.AttrServiceName})
	p, err := NewProviders(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := p.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	a := []Attr{{Key: semconv.AttrAccessUserID, Value: "opaque-identity"}, {Key: semconv.AttrStatusClass, Value: "2xx"}}
	b := &Buffer{}
	for _, f := range []func(context.Context, string, float64, ...Attr) error{b.Gauge, b.Counter, b.Histogram} {
		if err := f(ctx, semconv.MetricAPIRequests, 9, a...); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Counter(ctx, semconv.MetricDNSQueries, 2, a...); err != nil {
		t.Fatal(err)
	}
	if err := b.Counter(ctx, semconv.MetricDNSQueries, 3, Attr{Key: semconv.AttrAccessUserID, Value: "other-identity"}, a[1]); err != nil {
		t.Fatal(err)
	}
	if err := b.Gauge(ctx, semconv.MetricCheckpointAge, 7, a...); err != nil {
		t.Fatal(err)
	}
	if err := b.Gauge(ctx, semconv.MetricCheckpointAge, 11, Attr{Key: semconv.AttrAccessUserID, Value: "other-identity"}, a[1]); err != nil {
		t.Fatal(err)
	}
	if err := b.Histogram(ctx, semconv.MetricAPIDuration, .2, a...); err != nil {
		t.Fatal(err)
	}
	if err := b.Histogram(ctx, semconv.MetricAPIDuration, .3, Attr{Key: semconv.AttrAccessUserID, Value: "other-identity"}, a[1]); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := b.LogEvent(ctx, semconv.EventAccessLogin, "kept-body", now, otellog.SeverityInfo, a...); err != nil {
		t.Fatal(err)
	}
	linkAttrs := []attribute.KeyValue{attribute.Int(semconv.AttrAccessUserID, 42), attribute.Bool(semconv.AttrAccessAllowed, true)}
	spec := SpanSpec{Name: semconv.SpanAPIRequest, Start: now.Add(-time.Second), End: now, Attrs: a, Events: []SpanEvent{{Name: "kept-event", Attrs: a}}, Logs: []LogRecord{{Name: semconv.EventAccessLogin, Body: "nested-body", At: now, Attrs: a}}, Links: []trace.Link{{SpanContext: trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}}), Attributes: linkAttrs}}}
	if err := b.Span(ctx, spec); err != nil {
		t.Fatal(err)
	}
	if err := b.ReplayInto(ctx, p.Emitter); err != nil {
		t.Fatal(err)
	}
	if err := p.metrics.ForceFlush(ctx); err != nil {
		t.Fatal(err)
	}
	if err := p.FlushCommit(ctx, p.BeginCommit()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	check := func(bag []*commonpb.KeyValue) {
		t.Helper()
		allowed := false
		for _, a := range bag {
			if a.Key == semconv.AttrStatusClass && a.Value.GetStringValue() == "2xx" || a.Key == semconv.AttrAccessAllowed && a.Value.GetBoolValue() {
				allowed = true
			}
			if a.Key == semconv.AttrAccessUserID || a.Key == semconv.AttrEventName {
				t.Errorf("denied attribute exported: %s", a.Key)
			}
		}
		if !allowed {
			t.Error("unlisted attribute lost or changed")
		}
	}
	found := map[string]*metricspb.Metric{}
	for _, m := range metrics {
		found[m.Name] = m
		if m.Name == semconv.MetricAPIRequests {
			t.Error("denied metric exported")
		}
		for _, d := range m.GetSum().GetDataPoints() {
			check(d.Attributes)
		}
		for _, d := range m.GetGauge().GetDataPoints() {
			check(d.Attributes)
		}
		for _, d := range m.GetHistogram().GetDataPoints() {
			check(d.Attributes)
		}
	}
	if m := found[semconv.MetricDNSQueries]; m == nil || len(m.GetSum().DataPoints) != 1 || m.GetSum().DataPoints[0].GetAsDouble() != 5 {
		t.Errorf("filtered counter did not aggregate: %v", m)
	}
	if m := found[semconv.MetricCheckpointAge]; m == nil || len(m.GetGauge().DataPoints) != 1 || m.GetGauge().DataPoints[0].GetAsDouble() != 11 {
		t.Errorf("gauge collision not last observation: %v", m)
	}
	if m := found[semconv.MetricAPIDuration]; m == nil || len(m.GetHistogram().DataPoints) != 1 || m.GetHistogram().DataPoints[0].Count != 2 || m.GetHistogram().DataPoints[0].GetSum() != .5 {
		t.Errorf("filtered histogram did not aggregate: %v", m)
	}
	if len(logs) != 2 || len(spans) != 1 {
		t.Fatalf("signals lost: logs=%d spans=%d", len(logs), len(spans))
	}
	for _, l := range logs {
		check(l.Attributes)
		if l.Body.GetStringValue() != "kept-body" && l.Body.GetStringValue() != "nested-body" {
			t.Error("body changed")
		}
	}
	sp := spans[0]
	check(sp.Attributes)
	for _, ev := range sp.Events {
		check(ev.Attributes)
	}
	for _, link := range sp.Links {
		check(link.Attributes)
		if len(link.Attributes) != 1 || !link.Attributes[0].Value.GetBoolValue() {
			t.Error("link attribute type/value lost")
		}
	}
	if a[0].Key != semconv.AttrAccessUserID || spec.Links[0].Attributes[0].Value.AsInt64() != 42 {
		t.Error("caller input mutated")
	}
}
