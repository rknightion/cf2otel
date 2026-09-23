package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	otellog "go.opentelemetry.io/otel/log"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type fakeAPI struct {
	calls                           []string
	listQueries                     []url.Values
	list, detail, request, response string
}

func (f *fakeAPI) Get(_ context.Context, path string, q url.Values, out any) error {
	f.calls = append(f.calls, path)
	value := f.detail
	switch {
	case strings.HasSuffix(path, "/request"):
		value = f.request
	case strings.HasSuffix(path, "/response"):
		value = f.response
	case strings.HasSuffix(path, "/logs"):
		f.listQueries = append(f.listQueries, q)
		if q.Get("page") != "1" {
			value = "[]"
			break
		}
		var envelope struct {
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal([]byte(f.list), &envelope); err != nil {
			return err
		}
		value = string(envelope.Result)
	}
	return json.Unmarshal([]byte(value), out)
}
func (*fakeAPI) Query(context.Context, cfapi.GraphQLRequest, any) error { return errors.New("unused") }
func (*fakeAPI) Accounts(context.Context) ([]cfapi.Account, error)      { return nil, errors.New("unused") }
func (*fakeAPI) Zones(context.Context) ([]cfapi.Zone, error)            { return nil, errors.New("unused") }
func (*fakeAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) {
	return nil, errors.New("unused")
}

type observedMetric struct {
	name  string
	value float64
	attrs []telemetry.Attr
}
type fakeEmitter struct {
	logs                 int
	spans                []telemetry.SpanSpec
	counters, histograms []observedMetric
}

func (*fakeEmitter) Gauge(context.Context, string, float64, ...telemetry.Attr) error { return nil }
func (e *fakeEmitter) Counter(_ context.Context, n string, v float64, a ...telemetry.Attr) error {
	e.counters = append(e.counters, observedMetric{n, v, a})
	return nil
}
func (e *fakeEmitter) Histogram(_ context.Context, n string, v float64, a ...telemetry.Attr) error {
	e.histograms = append(e.histograms, observedMetric{n, v, a})
	return nil
}
func (e *fakeEmitter) LogEvent(context.Context, string, string, time.Time, otellog.Severity, ...telemetry.Attr) error {
	e.logs++
	return nil
}
func (e *fakeEmitter) Span(_ context.Context, s telemetry.SpanSpec) error {
	e.spans = append(e.spans, s)
	return nil
}

func TestLogsWindowDedupeAndUsage(t *testing.T) {
	api := &fakeAPI{list: `{"result":[
 {"id":"A","created_at":"2026-09-23T10:00:00Z","provider":"google-ai-studio","model":"gemini","path":"chat/completions","duration":1500,"status_code":200,"success":true,"cached":true,"tokens_in":99,"tokens_out":88,"usage_metadata":{"input_tokens":10,"output_tokens":2,"input_cached_tokens":3,"output_reasoning_tokens":1},"cost":0.5},
 {"id":"A","created_at":"2026-09-23T10:00:00Z","provider":"google-ai-studio","model":"gemini","path":"chat/completions","duration":1500},
 {"id":"B","created_at":"2026-09-23T10:01:00Z","duration":1}
 ],"result_info":{"per_page":100}}`}
	cfg := &config.Config{Cloudflare: config.CloudflareConfig{AccountID: "example"}, AIGateway: config.AIGatewayConfig{Gateways: []string{"gateway"}}}
	out := &fakeEmitter{}
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	mark, err := NewLogs(cfg, api).CollectWindow(context.Background(), from, from.Add(time.Minute), out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(from.Add(time.Minute)) || out.logs != 1 || len(out.spans) != 1 {
		t.Fatalf("mark %s logs %d spans %d", mark, out.logs, len(out.spans))
	}
	span := out.spans[0]
	if !span.Start.Equal(from.Add(-1500*time.Millisecond)) || !span.End.Equal(from) {
		t.Fatalf("span times %s %s", span.Start, span.End)
	}
	if got := attr(span.Attrs, semconv.AttrGenAIProvider); got != "gcp.gemini" {
		t.Fatalf("provider %q", got)
	}
	if got := attr(span.Attrs, semconv.AttrGenAIInputTokens); got != "10" {
		t.Fatalf("input %q", got)
	}
	if len(api.calls) != 1 {
		t.Fatalf("default fetched detail/body: %v", api.calls)
	}
	if len(api.listQueries) != 1 || api.listQueries[0].Get("start_date") != from.Format(time.RFC3339Nano) || api.listQueries[0].Get("end_date") != from.Add(time.Minute).Format(time.RFC3339Nano) {
		t.Fatalf("list window filters: %v", api.listQueries)
	}
	if got := metric(out.counters, semconv.MetricGenAIInputTokens); got != 10 {
		t.Fatalf("input metric %v", got)
	}
	if got := metric(out.counters, semconv.MetricGenAICacheReadTokens); got != 3 {
		t.Fatalf("cache read metric %v", got)
	}
	if got := metric(out.histograms, semconv.MetricGenAIDuration); got != 1.5 {
		t.Fatalf("duration %v", got)
	}
}

func TestContentCapAndTraceLink(t *testing.T) {
	api := &fakeAPI{
		list:     `{"result":[{"id":"A","created_at":"2026-09-23T10:00:00Z","path":"chat/completions","duration":1}],"result_info":{"per_page":100}}`,
		detail:   `{"request_head":{"traceparent":"00-0123456789abcdef0123456789abcdef-0123456789abcdef-01","authorization":"Bearer secret"},"response_head":{"set-cookie":"secret"},"request_content_type":"application/json"}`,
		request:  `{"messages":[{"role":"user","content":"hello"}]}`,
		response: `{"choices":[{"message":{"role":"assistant","content":"world"}}]}`,
	}
	cfg := &config.Config{Cloudflare: config.CloudflareConfig{AccountID: "example"}, AIGateway: config.AIGatewayConfig{Gateways: []string{"gateway"}, CaptureBodies: true, MaxBodyBytes: 1024, LinkCallerTraces: true}}
	out := &fakeEmitter{}
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	_, err := NewLogs(cfg, api).CollectWindow(context.Background(), from, from.Add(time.Minute), out)
	if err != nil {
		t.Fatal(err)
	}
	if len(api.calls) != 5 || len(out.spans) != 1 || len(out.spans[0].Links) != 1 {
		t.Fatalf("calls %d spans %d", len(api.calls), len(out.spans))
	}
	s := out.spans[0]
	if attr(s.Attrs, semconv.AttrGenAIInputMessages) == "" || attr(s.Attrs, semconv.AttrGenAIOutputMessages) == "" {
		t.Fatal("structured messages missing")
	}
	if strings.Contains(attr(s.Attrs, semconv.AttrAIGatewayRequestHead), "secret") {
		t.Fatal("secret header exported")
	}
	cfg.AIGateway.MaxBodyBytes = 8
	out = &fakeEmitter{}
	api.calls = nil
	_, err = NewLogs(cfg, api).CollectWindow(context.Background(), from, from.Add(time.Minute), out)
	if err != nil {
		t.Fatal(err)
	}
	if attr(out.spans[0].Attrs, semconv.AttrGenAIInputMessages) != "" {
		t.Fatal("truncated body became message")
	}
	if got := attr(out.spans[0].Events[0].Attrs, semconv.AttrAIGatewayRequestBodyTruncated); got != "true" {
		t.Fatalf("truncated=%q", got)
	}
}

func TestOpaqueMetadataRedactsValuesAndKeys(t *testing.T) {
	got := redactedJSON(json.RawMessage(`{"team":"private value","api_token":"private token","count":2}`), 4096)
	if strings.Contains(got, "private") || strings.Contains(got, "api_token") || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("unexpected metadata redaction: %s", got)
	}
}

func attr(attrs []telemetry.Attr, key string) string {
	for _, a := range attrs {
		if a.Key == key {
			return a.Value
		}
	}
	return ""
}
func metric(metrics []observedMetric, name string) float64 {
	for _, m := range metrics {
		if m.name == name {
			return m.value
		}
	}
	return -1
}
