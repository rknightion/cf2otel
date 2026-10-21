package aigateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type usageBodyAPI struct{ fakeAPI }

func (a *usageBodyAPI) GetRaw(_ context.Context, path string, _ url.Values, out any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	body := a.request
	if strings.HasSuffix(path, "/response") {
		body = a.response
	}
	*out.(*json.RawMessage) = append(json.RawMessage(nil), body...)
	return nil
}

func TestResponseUsageReachesEverySignal(t *testing.T) {
	for _, tc := range []struct {
		name, gateway, body          string
		cap                          int64
		disabled                     bool
		input, output, total, source string
	}{
		{name: "workers_zero", gateway: `"tokens_in":0,"tokens_out":0,"usage_metadata":{"input_tokens":0,"output_tokens":0,"total_tokens":0},`, body: `{"result":{"usage":{"input_tokens":1234,"output_tokens":56}}}`, input: "1234", output: "56", total: "1290", source: "response_body"},
		{name: "workers_missing", body: `{"result":{"usage":{"input_tokens":1234,"output_tokens":56}}}`, input: "1234", output: "56", total: "1290", source: "response_body"},
		{name: "openai_chat", body: `{"usage":{"prompt_tokens":80,"completion_tokens":20,"total_tokens":100}}`, input: "80", output: "20", total: "100", source: "response_body"},
		{name: "responses", body: `{"usage":{"input_tokens":70,"output_tokens":30}}`, input: "70", output: "30", total: "100", source: "response_body"},
		{name: "preserve_positive", gateway: `"tokens_in":7,"tokens_out":3,"usage_metadata":{"total_tokens":10},`, body: `{"usage":{"input_tokens":70,"output_tokens":30}}`, input: "7", output: "3", total: "10"},
		{name: "zero_metadata_preserves_gateway", gateway: `"tokens_in":7,"tokens_out":3,"usage_metadata":{"input_tokens":0,"output_tokens":0,"total_tokens":10},`, body: `{"usage":{"input_tokens":70,"output_tokens":30}}`, input: "7", output: "3", total: "10"},
		{name: "mixed", gateway: `"tokens_in":7,"tokens_out":0,`, body: `{"usage":{"input_tokens":70,"output_tokens":30}}`, input: "7", output: "30", total: "37", source: "response_body"},
		{name: "truncated", gateway: `"tokens_in":0,"tokens_out":0,`, input: "0", output: "0", cap: 16, body: `{"usage":{"input_tokens":70,"output_tokens":30}}`},
		{name: "non_json", body: `data: {"usage":{"input_tokens":70,"output_tokens":30}}`},
		{name: "fractional", body: `{"usage":{"input_tokens":1.5,"output_tokens":30}}`},
		{name: "negative", body: `{"usage":{"input_tokens":-1,"output_tokens":30}}`},
		{name: "overflow", body: `{"usage":{"input_tokens":9223372036854775808,"output_tokens":30}}`},
		{name: "no_usage", body: `{"result":{"answer":"fixture"}}`},
		{name: "capture_off", disabled: true, body: `{"usage":{"input_tokens":70,"output_tokens":30}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Cloudflare.AccountID = "fixture"
			cfg.AIGateway.Gateways = []string{"gateway"}
			cfg.AIGateway.CaptureBodies = !tc.disabled
			if tc.cap > 0 {
				cfg.AIGateway.MaxBodyBytes = tc.cap
			}
			api := &usageBodyAPI{fakeAPI: fakeAPI{list: fmt.Sprintf(`{"result":[{%s"id":"row","created_at":"2026-10-20T00:00:00Z","duration":1,"model":"fixture"}]}`, tc.gateway), detail: `{}`, request: `{}`, response: tc.body}}
			out := &fakeEmitter{}
			from := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
			mark, err := NewLogs(&cfg, api).CollectWindow(context.Background(), from, from.Add(time.Minute), out)
			if err != nil || !mark.Equal(from.Add(time.Minute)) || len(out.spans) != 1 || len(out.logEvents) != 1 {
				t.Fatalf("collection mark=%s spans=%d logs=%d err=%v", mark, len(out.spans), len(out.logEvents), err)
			}
			for _, attrs := range [][]telemetry.Attr{out.spans[0].Attrs, out.logEvents[0].attrs} {
				for _, want := range []struct{ key, value string }{{semconv.AttrGenAIInputTokens, tc.input}, {semconv.AttrGenAIOutputTokens, tc.output}, {semconv.AttrAIGatewayTotalTokens, tc.total}, {semconv.AttrAIGatewayUsageSource, tc.source}} {
					count := 0
					for _, a := range attrs {
						if a.Key == want.key {
							count++
						}
					}
					if got := attr(attrs, want.key); got != want.value || count > 1 {
						t.Errorf("%s=%q (%d copies), want %q", want.key, got, count, want.value)
					}
				}
			}
			for _, points := range [][]observedMetric{out.counters, out.histograms} {
				for _, point := range points {
					for _, a := range point.attrs {
						if a.Key == semconv.AttrAIGatewayUsageSource {
							t.Fatal("usage source became a metric attribute")
						}
					}
				}
			}
			for _, tokens := range []struct{ value, counter, histogram string }{{tc.input, semconv.MetricGenAIInputTokens, semconv.MetricGenAIInputOperationTokens}, {tc.output, semconv.MetricGenAIOutputTokens, semconv.MetricGenAIOutputOperationTokens}} {
				for _, signal := range []struct {
					name   string
					points []observedMetric
				}{{tokens.counter, out.counters}, {tokens.histogram, out.histograms}} {
					count := 0
					for _, point := range signal.points {
						if point.name != signal.name {
							continue
						}
						count++
						want, _ := strconv.ParseFloat(tokens.value, 64)
						if tokens.value == "" || point.value != want {
							t.Errorf("%s=%v, want %q", signal.name, point.value, tokens.value)
						}
					}
					wantCount := 0
					if tokens.value != "" {
						wantCount = 1
					}
					if count != wantCount {
						t.Errorf("%s has %d points, want %d", signal.name, count, wantCount)
					}
				}
			}
		})
	}
}
