package aigateway

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

func TestNormalizeModel(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"openai/gpt-4o", "gpt-4o"},
		{"gpt-4o", "gpt-4o"},
		{"google-ai-studio/gemini-2.5-pro", "gemini-2.5-pro"},
		{"aws-bedrock/anthropic.claude-v2:1", "anthropic.claude-v2:1"},
		{"workers-ai/@cf/meta/llama-3.1-8b-instruct", "@cf/meta/llama-3.1-8b-instruct"},
		{"@cf/meta/llama-3.1-8b-instruct", "@cf/meta/llama-3.1-8b-instruct"},
		{"", "unknown"},
		{"openai/", "unknown"},
		{"/gpt-4o", "unknown"},
		{"openai//gpt-4o", "unknown"},
		{"openai/..", "unknown"},
		{"Rate limited", "unknown"},
		{"https://example.com/model", "unknown"},
		{"{\"model\":\"gpt-4o\"}", "unknown"},
		{"null", "unknown"},
		{"openai/undefined", "unknown"},
		{"---", "unknown"},
		{"gpt-4o\n", "unknown"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			if got := normalizeModel(tc.raw); got != tc.want {
				t.Fatalf("normalizeModel(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestProviderName(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"openai", "openai"},
		{"GOOGLE-AI-STUDIO", "gcp.gemini"},
		{"google-vertex-ai", "gcp.vertex_ai"},
		{"azure-openai", "azure.ai.openai"},
		{"aws-bedrock", "aws.bedrock"},
		{"mistral", "mistral_ai"},
		{"xai", "x_ai"},
		{"", ""},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			if got := providerName(tc.raw); got != tc.want {
				t.Fatalf("providerName(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestRegisteredLogsNormalizeModel(t *testing.T) {
	for _, tc := range []struct{ name, model, want string }{
		{"prefixed", "openai/gpt-4o", "gpt-4o"},
		{"empty suffix", "openai/", "unknown"},
		{"non model", "Rate limited", "unknown"},
		{"unprefixed", "gpt-4o", "gpt-4o"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeAPI{
				list:   fmt.Sprintf(`{"result":[{"id":"fixture-log","created_at":"2026-09-23T10:00:00Z","provider":"openai","model":%q,"path":"chat/completions","duration":1,"tokens_in":2,"tokens_out":3,"cost":0.01}]}`, tc.model),
				detail: `{}`, request: `{"messages":[{"role":"user","content":"fixture"}]}`, response: `{"choices":[{"message":{"role":"assistant","content":"fixture"}}]}`,
			}
			cfg := config.Default()
			cfg.Cloudflare.AccountID = "fixture-account"
			cfg.AIGateway = config.AIGatewayConfig{Gateways: []string{"fixture-gateway"}, CaptureBodies: true, MaxBodyBytes: 4096}
			registry := collector.NewRegistry()
			Register(collector.Deps{Config: &cfg, API: api, Registry: registry})
			var window collector.WindowCollector
			for _, entry := range registry.Entries() {
				if entry.Collector.Name() == "aigateway.logs" {
					window = entry.Collector.(collector.WindowCollector)
				}
			}
			if window == nil {
				t.Fatal("logs collector not registered")
			}
			from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
			out := &fakeEmitter{}
			mark, err := window.CollectWindow(context.Background(), from, from.Add(time.Minute), out)
			if err != nil {
				t.Fatal(err)
			}
			if !mark.Equal(from.Add(time.Minute)) || len(out.spans) != 1 || len(out.logEvents) != 1 {
				t.Fatal("metadata or checkpoint absent")
			}
			check := func(surface string, attrs []telemetry.Attr) {
				t.Helper()
				if got := attr(attrs, semconv.AttrGenAIModel); got != tc.want {
					t.Errorf("%s model = %q, want %q", surface, got, tc.want)
				}
				if got := attr(attrs, semconv.AttrGenAIProvider); got != "openai" {
					t.Errorf("%s provider = %q", surface, got)
				}
			}
			check("request log", out.logEvents[0].attrs)
			check("span", out.spans[0].Attrs)
			if got := out.spans[0].Name; got != "chat "+tc.want {
				t.Errorf("span name = %q, want %q", got, "chat "+tc.want)
			}
			if len(out.spans[0].Logs) != 2 {
				t.Fatal("content logs absent")
			}
			for _, log := range out.spans[0].Logs {
				check("content log", log.Attrs)
			}
			for _, metric := range out.counters {
				check(metric.name, metric.attrs)
			}
			for _, metric := range out.histograms {
				check(metric.name, metric.attrs)
			}
		})
	}
}
