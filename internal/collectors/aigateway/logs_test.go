package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
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
	missingResponse                 bool
}

func (f *fakeAPI) Get(_ context.Context, path string, q url.Values, out any) error {
	f.calls = append(f.calls, path)
	if strings.HasSuffix(path, "/request") || strings.HasSuffix(path, "/response") {
		return errors.New("body endpoint has no result envelope")
	}
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
func (f *fakeAPI) GetRaw(_ context.Context, path string, _ url.Values, out any) error {
	f.calls = append(f.calls, path)
	if strings.HasSuffix(path, "/request") {
		return json.Unmarshal([]byte(f.request), out)
	}
	if strings.HasSuffix(path, "/response") {
		if f.missingResponse {
			return &cfapi.HTTPError{Status: 404, Code: 7002}
		}
		return json.Unmarshal([]byte(f.response), out)
	}
	return errors.New("unexpected raw path")
}

func TestMissingResponseBodyKeepsRequest(t *testing.T) {
	api := &fakeAPI{
		list:            `{"result":[{"id":"A","created_at":"2026-09-23T10:00:00Z","path":"chat/completions","duration":1}]}`,
		detail:          `{}`,
		request:         `{"messages":[{"role":"user","content":"fixture"}]}`,
		missingResponse: true,
	}
	cfg := &config.Config{Cloudflare: config.CloudflareConfig{AccountID: "example"}, AIGateway: config.AIGatewayConfig{Gateways: []string{"gateway"}, CaptureBodies: true, MaxBodyBytes: 1024}}
	out := &fakeEmitter{}
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	if _, err := NewLogs(cfg, api).CollectWindow(context.Background(), from, from.Add(time.Minute), out); err != nil {
		t.Fatal(err)
	}
	if len(out.spans) != 1 || attr(out.spans[0].Attrs, semconv.AttrGenAIInputMessages) == "" || attr(out.spans[0].Attrs, semconv.AttrAIGatewayResponseBodyUnavailable) != "true" {
		t.Fatal("request span or missing-body marker absent")
	}
	if len(out.spans[0].Events) != 1 || attr(out.spans[0].Events[0].Attrs, semconv.AttrAIGatewayResponseBodyUnavailable) != "true" {
		t.Fatal("content event missing unavailable-body marker")
	}
	if len(out.spans[0].Logs) != 1 || attr(out.spans[0].Logs[0].Attrs, semconv.AttrAIGatewayContentSide) != "request" {
		t.Fatal("expected only the available request content log")
	}
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
	if len(span.Logs) != 0 {
		t.Fatal("body logs emitted while capture was disabled")
	}
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
	if len(api.listQueries) != 1 || api.listQueries[0].Get("per_page") != "50" || api.listQueries[0].Get("start_date") != from.Format(time.RFC3339Nano) || api.listQueries[0].Get("end_date") != from.Add(time.Minute).Format(time.RFC3339Nano) {
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

func TestContentLogRecords(t *testing.T) {
	const requestMessages = `{"messages":[{"role":"user","content":"hello"}]}`
	const responseMessages = `{"choices":[{"message":{"role":"assistant","content":"world"}}]}`
	const parsedRequest = `[{"parts":[{"content":"hello","type":"text"}],"role":"user"}]`
	const parsedResponse = `[{"parts":[{"content":"world","type":"text"}],"role":"assistant"}]`
	rawRequest := `{"x":"a"}`
	rawResponse := `{"y":"b"}`
	cases := []struct {
		name               string
		capture            bool
		max                int64
		request, response  string
		missingResponse    bool
		wantBodies         []string
		wantTruncated      bool
		wantEventTruncated bool
	}{
		{name: "both parsed bodies", capture: true, max: 1024, request: requestMessages, response: responseMessages, wantBodies: []string{parsedRequest, parsedResponse}},
		{name: "response unavailable", capture: true, max: 1024, request: requestMessages, missingResponse: true, wantBodies: []string{parsedRequest}},
		{name: "capture disabled", capture: false, max: 1024, request: requestMessages, response: responseMessages},
		{name: "at cap", capture: true, max: 9, request: rawRequest, response: rawResponse, wantBodies: []string{rawRequest, rawResponse}},
		{name: "above cap", capture: true, max: 8, request: rawRequest, response: rawResponse, wantBodies: []string{rawRequest[:8], rawResponse[:8]}, wantTruncated: true, wantEventTruncated: true},
		{name: "expanded parsed output falls back to source JSON", capture: true, max: int64(len(parsedRequest) - 1), request: requestMessages, missingResponse: true, wantBodies: []string{requestMessages}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeAPI{
				list:            `{"result":[{"id":"content-id","created_at":"2026-09-23T10:00:00Z","provider":"openai","model":"fixture-model","path":"chat/completions","duration":1}]}`,
				detail:          `{}`,
				request:         tc.request,
				response:        tc.response,
				missingResponse: tc.missingResponse,
			}
			cfg := &config.Config{
				Cloudflare: config.CloudflareConfig{AccountID: "example"},
				AIGateway:  config.AIGatewayConfig{Gateways: []string{"gateway"}, CaptureBodies: tc.capture, MaxBodyBytes: tc.max},
			}
			out := &fakeEmitter{}
			end := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
			if _, err := NewLogs(cfg, api).CollectWindow(context.Background(), end, end.Add(time.Minute), out); err != nil {
				t.Fatal(err)
			}
			if len(out.spans) != 1 {
				t.Fatalf("spans=%d, want 1", len(out.spans))
			}
			logs := out.spans[0].Logs
			if len(logs) != len(tc.wantBodies) {
				t.Fatalf("content logs=%d, want %d", len(logs), len(tc.wantBodies))
			}
			for i, record := range logs {
				if record.Name != semconv.EventGenAIContent || record.Body != tc.wantBodies[i] {
					t.Fatalf("record %d name=%q body=%q", i, record.Name, record.Body)
				}
				if len(record.Body) > int(tc.max) {
					t.Fatalf("record %d body length %d exceeds cap %d", i, len(record.Body), tc.max)
				}
				if !record.At.Equal(end) || record.Severity != otellog.SeverityInfo {
					t.Fatalf("record %d timestamp=%s severity=%v", i, record.At, record.Severity)
				}
				wantSide := "request"
				flagKey := semconv.AttrAIGatewayRequestBodyTruncated
				if i == 1 {
					wantSide = "response"
					flagKey = semconv.AttrAIGatewayResponseBodyTruncated
				}
				if got := attr(record.Attrs, semconv.AttrAIGatewayName); got != "gateway" {
					t.Errorf("gateway=%q", got)
				}
				if got := attr(record.Attrs, semconv.AttrAIGatewayLogID); got != "content-id" {
					t.Errorf("log id=%q", got)
				}
				if got := attr(record.Attrs, semconv.AttrGenAIOperation); got != "chat" {
					t.Errorf("operation=%q", got)
				}
				if got := attr(record.Attrs, semconv.AttrGenAIModel); got != "fixture-model" {
					t.Errorf("model=%q", got)
				}
				if got := attr(record.Attrs, semconv.AttrGenAIProvider); got != "openai" {
					t.Errorf("provider=%q", got)
				}
				if got := attr(record.Attrs, semconv.AttrAIGatewayContentSide); got != wantSide {
					t.Errorf("side=%q, want %q", got, wantSide)
				}
				if got := attr(record.Attrs, semconv.AttrAIGatewayContentLength); got != strconv.Itoa(len(record.Body)) {
					t.Errorf("content length=%q, want %d", got, len(record.Body))
				}
				if got := attr(record.Attrs, flagKey); got != strconv.FormatBool(tc.wantTruncated) {
					t.Errorf("%s=%q, want %t", flagKey, got, tc.wantTruncated)
				}
			}
			if tc.missingResponse && attr(out.spans[0].Attrs, semconv.AttrAIGatewayResponseBodyUnavailable) != "true" {
				t.Fatal("response unavailable marker missing from span")
			}
			if tc.capture && len(out.spans[0].Events) > 0 {
				if got := attr(out.spans[0].Events[0].Attrs, semconv.AttrAIGatewayRequestBodyTruncated); got != strconv.FormatBool(tc.wantEventTruncated) {
					t.Errorf("span event request truncation=%q, want %t", got, tc.wantEventTruncated)
				}
			}
		})
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
