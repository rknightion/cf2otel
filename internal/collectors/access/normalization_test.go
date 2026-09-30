package access

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
)

type normalizationAPI struct{ loginAPI }

func (*normalizationAPI) Query(_ context.Context, r cfapi.GraphQLRequest, out any) error {
	var data string
	switch r.Dataset {
	case "cf1AccessLoginsRawGroups":
		data = `[{"dimensions":{"appName":"Example","idp":"example-idp","loginType":"login","allowed":"allowed"},"sum":{"logins":7}},{"dimensions":{"appName":"Example","idp":"example-idp","loginType":"login","allowed":"denied"},"sum":{"logins":3}}]`
	case "accessLoginRequestsAdaptiveGroups":
		data = `[{"count":5,"dimensions":{"appId":"app-example","identityProvider":"example-idp","isSuccessfulLogin":1}},{"count":2,"dimensions":{"appId":"app-example","identityProvider":"nonidentity","serviceTokenId":"token-example","isSuccessfulLogin":0}}]`
	default:
		panic("unexpected dataset: " + r.Dataset)
	}
	return json.Unmarshal([]byte(data), out)
}

func TestRegisteredAccessNormalizesDecisionsAndCountry(t *testing.T) {
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	cfg := config.Default()
	cfg.Cloudflare.AccountID = "test-account"
	api := &normalizationAPI{loginAPI{rows: []loginRow{
		{RayID: "ray-allowed", CreatedAt: from.Add(time.Second), Country: "gb", Connection: "example-idp", Allowed: true},
		{RayID: "ray-denied", CreatedAt: from.Add(2 * time.Second), Country: "uS", Connection: "example-idp", Allowed: false},
	}}}
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: &cfg, API: api, Registry: registry})
	found := map[string]bool{}
	for _, entry := range registry.Entries() {
		switch entry.Collector.Name() {
		case "access.logins":
			found[entry.Collector.Name()] = true
			t.Run("logins", func(t *testing.T) {
				out := &loginEmitter{}
				if _, err := entry.Collector.(collector.WindowCollector).CollectWindow(context.Background(), from, from.Add(time.Minute), out); err != nil {
					t.Fatal(err)
				}
				if len(out.logs) != 2 || len(out.metrics) != 2 {
					t.Fatalf("logs=%d metrics=%d, want 2 each", len(out.logs), len(out.metrics))
				}
				for i, want := range []struct{ allowed, country string }{{"true", "GB"}, {"false", "US"}} {
					attrs := attrsMap(out.logs[i].attrs)
					if attrs[semconv.AttrAccessAllowed] != want.allowed || attrs[semconv.AttrAccessCountry] != want.country {
						t.Errorf("login %d: allowed=%q country=%q, want %q %q", i, attrs[semconv.AttrAccessAllowed], attrs[semconv.AttrAccessCountry], want.allowed, want.country)
					}
					if got := attrsMap(out.metrics[i].attrs)[semconv.AttrAccessAllowed]; got != want.allowed {
						t.Errorf("identity login %d: allowed=%q, want %q", i, got, want.allowed)
					}
				}
			})
		case "access.login_metrics":
			found[entry.Collector.Name()] = true
			t.Run("login_metrics", func(t *testing.T) {
				out := &metricEmitter{}
				if _, err := entry.Collector.(collector.WindowCollector).CollectWindow(context.Background(), from, from.Add(time.Minute), out); err != nil {
					t.Fatal(err)
				}
				if len(out.points) != 4 {
					t.Fatalf("points=%d, want 4", len(out.points))
				}
				for i, want := range []struct {
					name, allowed string
					value         float64
				}{{semconv.MetricAccessLogins, "true", 7}, {semconv.MetricAccessLogins, "false", 3}, {semconv.MetricAccessRequests, "true", 5}, {semconv.MetricAccessRequests, "false", 2}} {
					point := out.points[i]
					if got := attrsMap(point.attrs)[semconv.AttrAccessAllowed]; point.name != want.name || point.value != want.value || got != want.allowed {
						t.Errorf("point %d: name=%q count=%v allowed=%q, want %q %v %q", i, point.name, point.value, got, want.name, want.value, want.allowed)
					}
				}
			})
		}
	}
	if !found["access.logins"] || !found["access.login_metrics"] {
		t.Fatalf("required Access collectors not registered: %v", found)
	}
}
