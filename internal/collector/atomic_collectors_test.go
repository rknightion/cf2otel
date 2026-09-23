package collector_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	otellog "go.opentelemetry.io/otel/log"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/collectors/aigateway"
	"github.com/rknightion/cf2otel/internal/collectors/httpreq"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type partialAPI struct{ mode string }

func (a *partialAPI) Get(_ context.Context, path string, q url.Values, out any) error {
	if a.mode != "ai" {
		return errors.New("unexpected REST request")
	}
	var raw string
	if strings.HasSuffix(path, "/logs") {
		if q.Get("page") == "1" {
			raw = `[{"id":"A","created_at":"2026-09-23T11:00:00Z","duration":1},{"id":"B","created_at":"2026-09-23T11:00:01Z","duration":1}]`
		} else {
			raw = `[]`
		}
	} else if strings.HasSuffix(path, "/B") {
		return errors.New("second AI detail fetch failed")
	} else {
		raw = `{}`
	}
	return json.Unmarshal([]byte(raw), out)
}
func (a *partialAPI) Query(_ context.Context, r cfapi.GraphQLRequest, out any) error {
	if a.mode != "http" {
		return errors.New("unexpected GraphQL request")
	}
	if r.ScopeID == "zone-b" {
		return errors.New("second HTTP zone fetch failed")
	}
	return json.Unmarshal([]byte(`[{"datetime":"2026-09-23T11:00:00Z","clientRequestHTTPHost":"example.com","rayName":"invented"}]`), out)
}
func (*partialAPI) Accounts(context.Context) ([]cfapi.Account, error) { return nil, nil }
func (*partialAPI) Zones(context.Context) ([]cfapi.Zone, error) {
	return []cfapi.Zone{{ID: "zone-a", Name: "a.example.com"}, {ID: "zone-b", Name: "b.example.com"}}, nil
}
func (*partialAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) { return nil, nil }

type countEmitter struct{ logs, spans, counters int }

func (*countEmitter) Gauge(context.Context, string, float64, ...telemetry.Attr) error { return nil }
func (e *countEmitter) Counter(context.Context, string, float64, ...telemetry.Attr) error {
	e.counters++
	return nil
}
func (*countEmitter) Histogram(context.Context, string, float64, ...telemetry.Attr) error { return nil }
func (e *countEmitter) LogEvent(context.Context, string, string, time.Time, otellog.Severity, ...telemetry.Attr) error {
	e.logs++
	return nil
}
func (e *countEmitter) Span(context.Context, telemetry.SpanSpec) error { e.spans++; return nil }

func TestPartialFetchDiscardsCollectorWindow(t *testing.T) {
	for _, mode := range []string{"ai", "http"} {
		t.Run(mode, func(t *testing.T) {
			cfg := config.Default()
			cfg.Cloudflare.AccountID = "invented"
			cfg.Identity.Enabled = false
			cfg.HTTP.Scope = "all"
			cfg.AIGateway.Gateways = []string{"gateway"}
			cfg.AIGateway.LinkCallerTraces = true
			api := &partialAPI{mode: mode}
			var window collector.WindowCollector
			if mode == "ai" {
				window = aigateway.NewLogs(&cfg, api)
			} else {
				window = httpreq.NewEvents(&cfg, api, nil).(collector.WindowCollector)
			}
			store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			emitter := &countEmitter{}
			s := collector.NewScheduler(nil, emitter, store)
			s.Now = func() time.Time { return time.Date(2026, 9, 23, 11, 5, 0, 0, time.UTC) }
			entry := collector.Entry{Collector: window, Interval: time.Minute, InitialLookback: 10 * time.Minute}
			if err := s.RunOnce(context.Background(), entry); err == nil {
				t.Fatal("expected second fetch failure")
			}
			if emitter.logs != 0 || emitter.spans != 0 || emitter.counters != 0 {
				t.Fatalf("partial output leaked: logs=%d spans=%d counters=%d", emitter.logs, emitter.spans, emitter.counters)
			}
			mark, ok := store.Get(window.Name())
			want := s.Now().Add(-window.Lag()).Truncate(time.Second).Add(-entry.InitialLookback)
			if !ok || !mark.Equal(want) {
				t.Fatalf("checkpoint=%s, want initial lower cursor %s", mark, want)
			}
		})
	}
}
