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

// Inferred identity must never become direct-looking when its qualifier is denied.
// Exercise the SDK/export boundary and independently filtered nested attribute bags.
func TestDenylistOTLPInferenceQualifier(t *testing.T) {
	for _, tc := range []struct {
		name                                 string
		denied                               []string
		inferredKept, markerKept, directKept bool
	}{
		{"marker denied", []string{semconv.AttrAccessIdentityInferred}, false, false, true},
		{"identity denied", []string{semconv.AttrAccessUserEmail}, false, true, false},
		{"empty policy", nil, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var mu sync.Mutex
			var bags, directBags [][]*commonpb.KeyValue
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
					for _, rm := range req.ResourceMetrics {
						for _, sm := range rm.ScopeMetrics {
							for _, m := range sm.Metrics {
								for _, d := range m.GetSum().GetDataPoints() {
									bags = append(bags, d.Attributes)
								}
								for _, d := range m.GetGauge().GetDataPoints() {
									bags = append(bags, d.Attributes)
								}
								for _, d := range m.GetHistogram().GetDataPoints() {
									bags = append(bags, d.Attributes)
								}
							}
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
							for _, l := range sl.LogRecords {
								if l.Body.GetStringValue() == "direct" {
									directBags = append(directBags, l.Attributes)
								} else {
									bags = append(bags, l.Attributes)
								}
							}
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
							for _, s := range ss.Spans {
								bags = append(bags, s.Attributes)
								for _, ev := range s.Events {
									bags = append(bags, ev.Attributes)
								}
								for _, link := range s.Links {
									bags = append(bags, link.Attributes)
								}
							}
						}
					}
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			p, err := NewProviders(ctx, ProviderOptions{Endpoint: server.URL, Interval: time.Hour, AttributeDenylist: tc.denied})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := p.Shutdown(ctx); err != nil {
					t.Error(err)
				}
			}()
			inferred := []Attr{
				{Key: semconv.AttrAccessUserEmail, Value: "opaque-inferred"},
				{Key: semconv.AttrAccessUserID, Value: "opaque-user"},
				{Key: semconv.AttrAccessUserIPAddress, Value: "opaque-address"},
				{Key: semconv.AttrAccessIdentityInferred, Value: "true"},
				{Key: semconv.AttrAccessIdentityLoginRayID, Value: "opaque-login-ray"},
				{Key: semconv.AttrStatusClass, Value: "2xx"},
			}
			b := &Buffer{}
			for _, emit := range []func(context.Context, string, float64, ...Attr) error{b.Gauge, b.Counter, b.Histogram} {
				if err := emit(ctx, semconv.MetricDNSQueries, 1, inferred...); err != nil {
					t.Fatal(err)
				}
			}
			now := time.Now()
			if err := b.LogEvent(ctx, semconv.EventHTTPRequest, "inferred", now, otellog.SeverityInfo, inferred...); err != nil {
				t.Fatal(err)
			}
			// Both absent and explicit false markers describe non-inferred identity.
			for _, marker := range []string{"", "false"} {
				direct := []Attr{{Key: semconv.AttrAccessUserEmail, Value: "opaque-direct"}, {Key: semconv.AttrStatusClass, Value: "2xx"}}
				if marker != "" {
					direct = append(direct, Attr{Key: semconv.AttrAccessIdentityInferred, Value: marker})
				}
				if err := b.LogEvent(ctx, semconv.EventAccessLogin, "direct", now, otellog.SeverityInfo, direct...); err != nil {
					t.Fatal(err)
				}
			}
			links := []trace.Link{{SpanContext: trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}}), Attributes: []attribute.KeyValue{
				attribute.String(semconv.AttrAccessUserEmail, "opaque-inferred"),
				attribute.String(semconv.AttrAccessUserID, "opaque-user"),
				attribute.String(semconv.AttrAccessUserIPAddress, "opaque-address"),
				attribute.Bool(semconv.AttrAccessIdentityInferred, true),
				attribute.String(semconv.AttrAccessIdentityLoginRayID, "opaque-login-ray"),
				attribute.String(semconv.AttrStatusClass, "2xx"),
			}}}
			if err := b.Span(ctx, SpanSpec{Name: semconv.SpanAPIRequest, Start: now.Add(-time.Second), End: now, Attrs: inferred,
				Events: []SpanEvent{{Name: "inferred", Attrs: inferred}}, Logs: []LogRecord{{Name: semconv.EventHTTPRequest, At: now, Body: "inferred", Attrs: inferred}}, Links: links,
			}); err != nil {
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
			for _, bag := range directBags {
				found := false
				for _, a := range bag {
					if a.Key == semconv.AttrAccessUserEmail {
						found = a.Value.GetStringValue() == "opaque-direct"
					}
					if a.Key == semconv.AttrAccessIdentityInferred && !tc.markerKept {
						t.Error("denied marker retained on direct identity")
					}
				}
				if found != tc.directKept {
					t.Error("direct identity did not follow explicit deny policy")
				}
			}
			if len(directBags) != 2 {
				t.Errorf("direct logs missing: %d", len(directBags))
			}
			for _, bag := range bags {
				values := map[string]*commonpb.AnyValue{}
				for _, a := range bag {
					values[a.Key] = a.Value
				}
				if values[semconv.AttrStatusClass].GetStringValue() != "2xx" {
					t.Error("unlisted attribute lost")
				}
				if values[semconv.AttrAccessUserEmail] != nil && !tc.inferredKept {
					t.Error("inferred identity exported without mandatory inference marker")
				}
				if values[semconv.AttrAccessIdentityInferred] != nil && !tc.markerKept {
					t.Error("denied inference marker exported")
				}
				if tc.inferredKept && values[semconv.AttrAccessUserEmail].GetStringValue() != "opaque-inferred" {
					t.Error("allowed inferred identity lost")
				}
				for _, key := range []string{semconv.AttrAccessUserID, semconv.AttrAccessUserIPAddress} {
					if (values[key] != nil) != tc.markerKept {
						t.Errorf("inferred identity field %s did not follow qualifier policy", key)
					}
				}
				if !tc.markerKept && values[semconv.AttrAccessIdentityLoginRayID] != nil {
					t.Error("inference login reference exported without marker")
				}
				if tc.markerKept {
					marker := values[semconv.AttrAccessIdentityInferred]
					if marker.GetStringValue() != "true" && !marker.GetBoolValue() {
						t.Error("allowed inference qualifier lost")
					}
					if values[semconv.AttrAccessIdentityLoginRayID].GetStringValue() != "opaque-login-ray" {
						t.Error("allowed inference reference lost")
					}
				}
			}
			// Gauge, counter, histogram, log, span, event, nested log and link.
			if len(bags) != 8 {
				t.Errorf("inferred signal bags missing: %d", len(bags))
			}
			if inferred[0].Value != "opaque-inferred" || len(links[0].Attributes) != 6 {
				t.Error("caller input mutated")
			}
		})
	}
}
