package collector_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/collectors/dns"
	"github.com/rknightion/cf2otel/internal/collectors/httpreq"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type zoneFixture struct {
	cfapi.Client
	zones    []cfapi.Zone
	queried  []string
	disabled string
}

func (f *zoneFixture) Zones(context.Context) ([]cfapi.Zone, error) { return f.zones, nil }
func (f *zoneFixture) DatasetSettings(_ context.Context, _ cfapi.Scope, id, _ string) (cfapi.DatasetSettings, error) {
	return cfapi.DatasetSettings{Enabled: id != f.disabled, AvailableFields: []string{"count"}, MaxNumberOfFields: 1}, nil
}
func (f *zoneFixture) Query(_ context.Context, req cfapi.GraphQLRequest, out any) error {
	f.queried = append(f.queried, req.ScopeID)
	return json.Unmarshal([]byte("[]"), out)
}

// YAML decoding deliberately uses the real schema without strict unknown-key
// rejection, so this fixture also exercises the legacy collector on the base.
func TestRegisteredZoneExclusionAndPollMetrics(t *testing.T) {
	for _, domain := range []string{"httpreq.events", "dns.metrics"} {
		for _, tc := range []struct {
			name, include, exclude           string
			want                             []string
			filteredInclude, filteredExclude float64
		}{
			{"legacy", "", "", []string{"opaque-a", "opaque-b", "opaque-c"}, 0, 0},
			{"exclusion_miss", "", "[missing]", []string{"opaque-a", "opaque-b", "opaque-c"}, 0, 0},
			{"include_overlap", "[opaque-a, opaque-b]", "[OPAQUE-A, Fixture-B, missing]", []string{"opaque-a"}, 1, 1},
			{"all_excluded", "", "[opaque-a, FIXTURE-B, opaque-c]", nil, 0, 3},
		} {
			t.Run(domain+"/"+tc.name, func(t *testing.T) {
				cfg := config.Default()
				cfg.HTTP.Scope = "all"
				cfg.Collectors = map[string]config.CollectorConfig{domain: {Enabled: true}}
				body := "cloudflare:\n  zones: " + tc.include + "\nzones:\n  exclude: " + tc.exclude + "\n"
				parsed, err := yaml.Parser().Unmarshal([]byte(body))
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(parsed)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(encoded, &cfg); err != nil {
					t.Fatal(err)
				}
				api := &zoneFixture{zones: []cfapi.Zone{{ID: "opaque-a", Name: "fixture-a"}, {ID: "opaque-b", Name: "fixture-b"}, {ID: "opaque-c", Name: "fixture-c"}}}
				registry := collector.NewRegistry()
				deps := collector.Deps{Config: &cfg, API: api, Registry: registry}
				httpreq.Register(deps)
				dns.Register(deps)
				if len(registry.Entries()) != 1 {
					t.Fatal("collector was not registered")
				}
				out := &telemetry.Buffer{}
				from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
				to := from.Add(time.Minute)
				mark, err := registry.Entries()[0].Collector.(collector.WindowCollector).CollectWindow(context.Background(), from, to, out)
				if err != nil || !mark.Equal(to) {
					t.Fatalf("mark=%v error=%v", mark, err)
				}
				if !reflect.DeepEqual(api.queried, tc.want) {
					t.Fatalf("queried=%v want=%v", api.queried, tc.want)
				}
				assertZoneGauge(t, out, domain, "cf2otel.zones.discovered", "", 3)
				assertZoneGauge(t, out, domain, "cf2otel.zones.processed", "", float64(len(tc.want)))
				assertZoneGauge(t, out, domain, "cf2otel.zones.filtered", "include", tc.filteredInclude)
				assertZoneGauge(t, out, domain, "cf2otel.zones.filtered", "exclude", tc.filteredExclude)
				for _, reason := range []string{"include", "exclude", "unentitled", "no_account", "other"} {
					assertZoneGauge(t, out, domain, "cf2otel.zones.skipped", reason, 0)
				}
			})
		}
	}
}
func TestRegisteredDNSZoneEntitlementSkip(t *testing.T) {
	cfg := config.Default()
	cfg.Collectors = map[string]config.CollectorConfig{"dns.metrics": {Enabled: true}}
	api := &zoneFixture{zones: []cfapi.Zone{{ID: "opaque-a", Name: "fixture-a"}, {ID: "opaque-b", Name: "fixture-b"}}, disabled: "opaque-b"}
	registry := collector.NewRegistry()
	dns.Register(collector.Deps{Config: &cfg, API: api, Registry: registry})
	out := &telemetry.Buffer{}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mark, err := registry.Entries()[0].Collector.(collector.WindowCollector).CollectWindow(context.Background(), from, from.Add(time.Minute), out)
	if err != nil || !mark.Equal(from.Add(time.Minute)) {
		t.Fatalf("mark=%v error=%v", mark, err)
	}
	if !reflect.DeepEqual(api.queried, []string{"opaque-a"}) {
		t.Fatalf("queried %v", api.queried)
	}
	assertZoneGauge(t, out, "dns.metrics", "cf2otel.zones.processed", "", 1)
	assertZoneGauge(t, out, "dns.metrics", "cf2otel.zones.skipped", "unentitled", 1)
}
func assertZoneGauge(t *testing.T, out *telemetry.Buffer, domain, name, reason string, want float64) {
	t.Helper()
	for _, m := range out.Metrics {
		if m.Name != name {
			continue
		}
		attrs := map[string]string{}
		for _, a := range m.Attrs {
			attrs[a.Key] = a.Value
		}
		if attrs["cf2otel.collector"] != domain || attrs["cf2otel.zone.reason"] != reason {
			continue
		}
		if len(attrs) != 1 && (reason == "" || len(attrs) != 2) {
			t.Fatalf("unexpected labels %v", attrs)
		}
		if m.Kind != "gauge" || m.Value != want {
			t.Fatalf("%s/%s = %v want %v", name, reason, m.Value, want)
		}
		return
	}
	t.Fatalf("missing gauge %s/%s", name, reason)
}
