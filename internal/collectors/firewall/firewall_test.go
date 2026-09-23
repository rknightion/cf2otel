package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	otellog "go.opentelemetry.io/otel/log"
)

type firewallFakeAPI struct {
	zones       []cfapi.Zone
	settings    map[string]cfapi.DatasetSettings
	queries     []cfapi.GraphQLRequest
	settingRead []string
	query       func(cfapi.GraphQLRequest) (any, error)
}

func (f *firewallFakeAPI) Get(context.Context, string, url.Values, any) error {
	return errors.New("unexpected REST request")
}

func (f *firewallFakeAPI) Query(_ context.Context, request cfapi.GraphQLRequest, out any) error {
	f.queries = append(f.queries, request)
	if f.query == nil {
		return errors.New("unexpected GraphQL request")
	}
	value, err := f.query(request)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, out)
}

func (f *firewallFakeAPI) DatasetSettings(_ context.Context, _ cfapi.Scope, zoneID, dataset string) (cfapi.DatasetSettings, error) {
	key := zoneID + "/" + dataset
	f.settingRead = append(f.settingRead, key)
	return f.settings[key], nil
}

func (f *firewallFakeAPI) Accounts(context.Context) ([]cfapi.Account, error) {
	return nil, errors.New("unexpected account discovery")
}

func (f *firewallFakeAPI) Zones(context.Context) ([]cfapi.Zone, error) {
	return append([]cfapi.Zone(nil), f.zones...), nil
}

func (f *firewallFakeAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) {
	return nil, errors.New("unexpected gateway discovery")
}

func settings(enabled bool, fields ...string) cfapi.DatasetSettings {
	return cfapi.DatasetSettings{Enabled: enabled, AvailableFields: fields}
}

func row(at time.Time, ray, action string) map[string]any {
	return map[string]any{
		"datetime":           at.UTC().Format(time.RFC3339Nano),
		"rayName":            ray,
		"action":             action,
		"source":             "firewallRule",
		"clientIP":           "192.0.2.42",
		"clientRequestPath":  "/synthetic",
		"clientRequestQuery": "q=synthetic",
		"userAgent":          "synthetic-agent",
	}
}

func TestSeverityForAction(t *testing.T) {
	tests := []struct {
		action string
		want   int
	}{
		{action: "block", want: 13},
		{action: "challenge", want: 12},
		{action: "jsChallenge", want: 12},
		{action: "managed_challenge", want: 12},
		{action: "log", want: 9},
		{action: "allow", want: 5},
		{action: "skip", want: 5},
		{action: "unknown", want: 10},
		{action: "", want: 10},
	}
	for _, test := range tests {
		t.Run(test.action, func(t *testing.T) {
			if got := int(severityForAction(test.action)); got != test.want {
				t.Fatalf("severityForAction(%q) = %d, want %d", test.action, got, test.want)
			}
		})
	}

	if severityForAction("block") != otellog.SeverityWarn || severityForAction("challenge") != otellog.SeverityInfo4 || severityForAction("log") != otellog.SeverityInfo || severityForAction("allow") != otellog.SeverityDebug || severityForAction("unknown") != otellog.SeverityInfo2 {
		t.Fatal("severity mapping no longer matches the OpenTelemetry severity constants")
	}
}

func TestConfiguredZoneMissingFromDiscoveryDoesNotAdvance(t *testing.T) {
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	for _, name := range []string{"events", "metrics"} {
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{Cloudflare: config.CloudflareConfig{Zones: []string{"present.example.test", "missing.example.test"}}}
			api := &firewallFakeAPI{
				zones:    []cfapi.Zone{{ID: "zone-present", Name: "present.example.test"}},
				settings: map[string]cfapi.DatasetSettings{"zone-present/" + groupsDataset: settings(true, "count")},
				query:    func(cfapi.GraphQLRequest) (any, error) { return []map[string]any{}, nil },
			}
			var c collector.WindowCollector
			if name == "events" {
				c = NewEvents(cfg, api)
			} else {
				c = NewMetrics(cfg, api)
			}
			out := &telemetry.Buffer{}
			mark, err := c.CollectWindow(context.Background(), from, to, out)
			if err == nil || !strings.Contains(err.Error(), "configured firewall zone") {
				t.Fatalf("error = %v, want unresolved configured zone", err)
			}
			if !mark.Equal(from) || len(out.Records) != 0 || len(out.Metrics) != 0 || len(api.queries) != 0 {
				t.Fatalf("mark=%s records=%d metrics=%d queries=%d, want no progress", mark, len(out.Records), len(out.Metrics), len(api.queries))
			}
		})
	}
}

func TestEventsFilterWindowBoundariesAndDeduplicate(t *testing.T) {
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	want := row(from.Add(15*time.Second), "synthetic-ray", "block")
	api := &firewallFakeAPI{
		zones: []cfapi.Zone{{ID: "zone-a", Name: "example.test"}},
		query: func(request cfapi.GraphQLRequest) (any, error) {
			if request.Dataset != rawDataset {
				t.Fatalf("queried %q, want the raw firewall dataset", request.Dataset)
			}
			return []map[string]any{
				row(from, "lower-bound-ray", "allow"),
				want,
				want,
				row(to, "upper-bound-ray", "skip"),
			}, nil
		},
	}
	out := &telemetry.Buffer{}
	mark, err := NewEvents(&config.Config{}, api).CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) {
		t.Fatalf("mark = %s, want %s", mark, to)
	}
	if len(out.Records) != 2 {
		t.Fatalf("records = %d, want 2 after half-open boundary filtering and ray/time dedupe", len(out.Records))
	}
	if !out.Records[0].At.Equal(from) {
		t.Fatalf("lower-bound event timestamp = %s, want %s", out.Records[0].At, from)
	}
	got := out.Records[1]
	if got.Event != semconv.EventFirewallEvent || got.Severity != otellog.SeverityWarn {
		t.Fatalf("event=%q severity=%d", got.Event, got.Severity)
	}
	if !got.At.Equal(from.Add(15 * time.Second)) {
		t.Fatalf("timestamp = %s, want interior event", got.At)
	}
	attrs := attrMap(got.Attrs)
	for _, key := range []string{semconv.AttrFirewallClientIP, semconv.AttrFirewallPath, semconv.AttrFirewallQuery, semconv.AttrFirewallUserAgent, semconv.AttrFirewallRayID} {
		if _, ok := attrs[key]; !ok {
			t.Errorf("log attributes do not include %q", key)
		}
	}
	for _, field := range rawEventFields {
		if field == "datetime" || field == "rayName" {
			continue
		}
		found := false
		for _, requested := range api.queries[0].WantedFields {
			if requested == field {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("raw field superset omitted %q", field)
		}
	}
	for _, requested := range api.queries[0].WantedFields {
		if requested == "clientAsnDescription" {
			t.Fatal("live schema rejects clientAsnDescription despite settings advertisement")
		}
	}
}

func TestEventsSplitSaturatedWindowsToOneMinute(t *testing.T) {
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	to := from.Add(4 * time.Minute)
	api := &firewallFakeAPI{
		zones: []cfapi.Zone{{ID: "zone-a", Name: "example.test"}},
		query: func(request cfapi.GraphQLRequest) (any, error) {
			if request.To.Sub(request.From) > time.Minute {
				return nil, fmt.Errorf("GraphQL dataset %s window %s..%s saturated limit 10", request.Dataset, request.From.Format(time.RFC3339), request.To.Format(time.RFC3339))
			}
			return []map[string]any{row(request.From.Add(10*time.Second), request.From.Format(time.RFC3339), "block")}, nil
		},
	}
	events := NewEvents(&config.Config{}, api)
	events.limit = 10
	out := &telemetry.Buffer{}
	mark, err := events.CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) {
		t.Fatalf("mark = %s, want %s", mark, to)
	}
	if len(api.queries) != 7 {
		t.Fatalf("queries = %d, want three saturated parents and four one-minute leaves", len(api.queries))
	}
	durations := make(map[time.Duration]int)
	for _, request := range api.queries {
		durations[request.To.Sub(request.From)]++
	}
	if durations[4*time.Minute] != 1 || durations[2*time.Minute] != 2 || durations[time.Minute] != 4 {
		t.Fatalf("query durations = %#v, want 1x4m, 2x2m, 4x1m", durations)
	}
	if len(out.Records) != 4 {
		t.Fatalf("records = %d, want one per successful minute leaf", len(out.Records))
	}
}

func TestEventsErrorWhenOneMinuteStillSaturates(t *testing.T) {
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Minute)
	api := &firewallFakeAPI{
		zones: []cfapi.Zone{{ID: "zone-a", Name: "example.test"}},
		query: func(request cfapi.GraphQLRequest) (any, error) {
			return nil, fmt.Errorf("GraphQL dataset %s window %s..%s saturated limit 10", request.Dataset, request.From.Format(time.RFC3339), request.To.Format(time.RFC3339))
		},
	}
	events := NewEvents(&config.Config{}, api)
	events.limit = 10
	out := &telemetry.Buffer{}
	_, err := events.CollectWindow(context.Background(), from, to, out)
	if err == nil || !strings.Contains(err.Error(), "one-minute query window") {
		t.Fatalf("error = %v, want one-minute saturation error", err)
	}
	if len(api.queries) != 2 {
		t.Fatalf("queries = %d, want saturated two-minute query plus its saturated one-minute child", len(api.queries))
	}
	if len(out.Records) != 0 {
		t.Fatalf("records = %d, want no partial events after saturation", len(out.Records))
	}
}

func TestMetricsChooseGroupsDatasetPerZoneAndLimitAttributes(t *testing.T) {
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	api := &firewallFakeAPI{
		zones: []cfapi.Zone{{ID: "pro", Name: "pro.example.test"}, {ID: "free", Name: "free.example.test"}},
		settings: map[string]cfapi.DatasetSettings{
			"pro/" + groupsDataset:        settings(true, "count", "dimensions_action", "dimensions_source"),
			"free/" + groupsDataset:       settings(false),
			"free/" + byTimeGroupsDataset: settings(true, "count", "dimensions_action", "dimensions_source"),
		},
		query: func(request cfapi.GraphQLRequest) (any, error) {
			return []map[string]any{{"count": 5, "dimensions": map[string]any{"action": "block", "source": "firewallRule"}}}, nil
		},
	}
	out := &telemetry.Buffer{}
	mark, err := NewMetrics(&config.Config{}, api).CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) {
		t.Fatalf("mark = %s, want %s", mark, to)
	}
	if len(api.queries) != 2 || api.queries[0].Dataset != groupsDataset || api.queries[1].Dataset != byTimeGroupsDataset {
		t.Fatalf("dataset selection = %#v, want Pro Groups and Free ByTimeGroups", []string{api.queries[0].Dataset, api.queries[1].Dataset})
	}
	if len(api.queries[1].WantedFields) != 1 || api.queries[1].WantedFields[0] != "count" {
		t.Fatalf("ByTimeGroups selected %v; live schema supports only count", api.queries[1].WantedFields)
	}
	if len(out.Metrics) != 2 {
		t.Fatalf("metrics = %d, want one per zone", len(out.Metrics))
	}
	for _, metric := range out.Metrics {
		if metric.Name != semconv.MetricFirewallEvents || metric.Value != 5 {
			t.Errorf("metric = %#v", metric)
		}
		for _, attr := range metric.Attrs {
			switch attr.Key {
			case semconv.AttrFirewallZone, semconv.AttrFirewallAction, semconv.AttrFirewallSource:
			default:
				t.Errorf("unbounded or unexpected firewall metric attribute %q", attr.Key)
			}
		}
		attrs := attrMap(metric.Attrs)
		if attrs[semconv.AttrFirewallZone] == "pro.example.test" && (attrs[semconv.AttrFirewallAction] != "block" || attrs[semconv.AttrFirewallSource] != "firewallRule") {
			t.Errorf("Pro metric attrs = %#v", attrs)
		}
		if attrs[semconv.AttrFirewallZone] == "free.example.test" && (attrs[semconv.AttrFirewallAction] != "" || attrs[semconv.AttrFirewallSource] != "") {
			t.Errorf("Free metric has unsupported dimensions = %#v", attrs)
		}
		for _, forbidden := range []string{"ip", "path", "ray"} {
			for key := range attrs {
				if strings.Contains(strings.ToLower(key), forbidden) {
					t.Errorf("metric attribute %q contains forbidden dimension %q", key, forbidden)
				}
			}
		}
	}
}

type captureHandler struct {
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, record slog.Record) error {
	h.records = append(h.records, record.Clone())
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func TestMetricsReportMissingDimensionsAndEmitAvailableOnes(t *testing.T) {
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	api := &firewallFakeAPI{
		zones: []cfapi.Zone{{ID: "pro", Name: "pro.example.test"}},
		settings: map[string]cfapi.DatasetSettings{
			"pro/" + groupsDataset: settings(true, "count", "dimensions_action"),
		},
		query: func(cfapi.GraphQLRequest) (any, error) {
			return []map[string]any{{"count": 7, "dimensions": map[string]any{"action": "block"}}}, nil
		},
	}
	previous := slog.Default()
	handler := &captureHandler{}
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(previous) })

	out := &telemetry.Buffer{}
	if _, err := NewMetrics(&config.Config{}, api).CollectWindow(context.Background(), from, to, out); err != nil {
		t.Fatal(err)
	}
	if len(out.Metrics) != 1 {
		t.Fatalf("metrics = %d, want available metric", len(out.Metrics))
	}
	attrs := attrMap(out.Metrics[0].Attrs)
	if attrs[semconv.AttrFirewallAction] != "block" || attrs[semconv.AttrFirewallSource] != "" {
		t.Fatalf("attrs = %#v, want action without unavailable source", attrs)
	}
	var warnings []slog.Record
	for _, record := range handler.records {
		if record.Level >= slog.LevelWarn {
			warnings = append(warnings, record)
		}
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "dimensions are unavailable") {
		t.Fatalf("gap warnings = %#v", warnings)
	}
}

func TestRegisterInstallsOnlyEnabledCollectors(t *testing.T) {
	cfg := config.Default()
	for name, entry := range cfg.Collectors {
		entry.Enabled = false
		cfg.Collectors[name] = entry
	}
	reg := collector.NewRegistry()
	Register(collector.Deps{Config: &cfg, API: &firewallFakeAPI{}, Registry: reg})
	if got := reg.Entries(); len(got) != 0 {
		t.Fatalf("disabled collectors registered: %#v", got)
	}

	for _, name := range []string{"firewall.events", "firewall.metrics"} {
		entry := cfg.Collectors[name]
		entry.Enabled = true
		cfg.Collectors[name] = entry
	}
	reg = collector.NewRegistry()
	Register(collector.Deps{Config: &cfg, API: &firewallFakeAPI{}, Registry: reg})
	entries := reg.Entries()
	if len(entries) != 2 || entries[0].Collector.Name() != "firewall.events" || entries[1].Collector.Name() != "firewall.metrics" {
		t.Fatalf("registered collectors = %#v", entries)
	}
}

func attrMap(attrs []telemetry.Attr) map[string]string {
	result := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		result[attr.Key] = attr.Value
	}
	return result
}
