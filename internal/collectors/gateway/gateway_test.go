package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type gatewayFakeAPI struct {
	settings    cfapi.DatasetSettings
	settingsErr error
	rows        any
	queryErr    error
	requests    []cfapi.GraphQLRequest
}

func (*gatewayFakeAPI) Get(context.Context, string, url.Values, any) error {
	return errors.New("unexpected REST request")
}

func (f *gatewayFakeAPI) Query(_ context.Context, request cfapi.GraphQLRequest, out any) error {
	f.requests = append(f.requests, request)
	if f.queryErr != nil {
		return f.queryErr
	}
	encoded, err := json.Marshal(f.rows)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, out)
}

func (f *gatewayFakeAPI) DatasetSettings(_ context.Context, scope cfapi.Scope, _ string, dataset string) (cfapi.DatasetSettings, error) {
	if scope != cfapi.AccountScope || dataset != gatewayDNSDataset {
		return cfapi.DatasetSettings{}, errors.New("unexpected dataset settings request")
	}
	return f.settings, f.settingsErr
}

func (*gatewayFakeAPI) Accounts(context.Context) ([]cfapi.Account, error) {
	return nil, errors.New("unexpected account discovery")
}

func (*gatewayFakeAPI) Zones(context.Context) ([]cfapi.Zone, error) {
	return nil, errors.New("unexpected zone discovery")
}

func (*gatewayFakeAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) {
	return nil, errors.New("unexpected gateway discovery")
}

func gatewaySettings() cfapi.DatasetSettings {
	return cfapi.DatasetSettings{
		Enabled: true,
		AvailableFields: []string{
			"sum_queries", "dimensions_queryType", "dimensions_resolverDecision", "dimensions_country",
		},
		MaxNumberOfFields: 10,
		MaxDuration:       3600,
		NotOlderThan:      31 * 24 * 60 * 60,
		MaxPageSize:       100,
	}
}

func gatewayConfig(accountID string) *config.Config {
	return &config.Config{Cloudflare: config.CloudflareConfig{AccountID: accountID}}
}

func gatewayRow(queries any, queryType, decision, country string) map[string]any {
	return map[string]any{
		"sum":        map[string]any{"queries": queries},
		"dimensions": map[string]any{"queryType": queryType, "resolverDecision": decision, "country": country},
	}
}

func TestDNSGroupsSumUsesHalfOpenRequestAndBoundedAttributes(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(30 * time.Minute)
	first := gatewayRow(2, "A", "allow", "GB")
	first["dimensions"].(map[string]any)["queryType"] = 1
	api := &gatewayFakeAPI{
		settings: gatewaySettings(),
		rows: []map[string]any{
			first,
			gatewayRow(7, "AAAA", "block", "US"),
		},
	}
	out := &telemetry.Buffer{}

	mark, err := NewDNSMetrics(gatewayConfig("synthetic-account"), api).CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) {
		t.Fatalf("mark = %s, want the window end", mark)
	}
	if len(api.requests) != 1 {
		t.Fatalf("query count = %d, want one", len(api.requests))
	}
	request := api.requests[0]
	if request.Scope != cfapi.AccountScope || request.ScopeID != "synthetic-account" || request.Dataset != gatewayDNSDataset {
		t.Fatal("query did not use the configured account and Gateway DNS Groups dataset")
	}
	if !request.From.Equal(from) || !request.To.Equal(to) {
		t.Fatalf("query window = %s..%s, want the exact half-open request window", request.From, request.To)
	}
	for _, field := range gatewayDNSFields {
		if !stringIn(request.WantedFields, field) {
			t.Errorf("query omitted negotiated field %q", field)
		}
	}
	if request.Limit != gatewayDNSQueryLimit {
		t.Fatalf("query limit = %d, want the bounded collector limit", request.Limit)
	}
	if len(out.Metrics) != 2 {
		t.Fatalf("metric count = %d, want one observation per returned group", len(out.Metrics))
	}
	var total float64
	for i, metric := range out.Metrics {
		if metric.Kind != "counter" || metric.Name != semconv.MetricGatewayDNSQueries {
			t.Fatalf("metric = %q %q, want the Gateway DNS query counter", metric.Kind, metric.Name)
		}
		if i == 0 && metric.Attrs[0].Value != "A" {
			t.Fatal("numeric queryType dimension was not mapped to its bounded label")
		}
		total += metric.Value
		if len(metric.Attrs) != 3 {
			t.Fatalf("attribute count = %d, want only the three bounded dimensions", len(metric.Attrs))
		}
	}
	if total != 9 {
		t.Fatalf("sum of emitted query counts = %v, want 9 from sum.queries", total)
	}
}

func TestGatewayDNSResolverDecisionValues(t *testing.T) {
	cases := map[string]string{
		"allowedOnNoRule":        "allow",
		"allowedOnNoLocation":    "allow",
		"allowedOnNoPolicyMatch": "allow",
		"allowedRule":            "allow",
		"4":                      "allow",
		"5":                      "allow",
		"10":                     "allow",
		"blockedByCategory":      "block",
		"blockedAlwaysCategory":  "block",
		"blockedRule":            "block",
		"3":                      "block",
		"6":                      "block",
		"9":                      "block",
		"overrideRule":           "override",
		"overrideApplied":        "override",
		"8":                      "override",
		"overrideForSafeSearch":  "safe_search",
		"7":                      "safe_search",
		"unrecognized":           gatewayDNSOther,
	}
	for input, want := range cases {
		if got := boundedGatewayDNSDecision(input); got != want {
			t.Errorf("decision %q mapped to %q, want %q", input, got, want)
		}
	}
}

func TestDNSGroupsSelectOnlyAdvertisedOptionalDimension(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	settings := gatewaySettings()
	settings.AvailableFields = []string{"sum_queries", "dimensions_country"}
	api := &gatewayFakeAPI{
		settings: settings,
		rows: []map[string]any{{
			"sum":        map[string]any{"queries": 5},
			"dimensions": map[string]any{"country": "GB"},
		}},
	}
	out := &telemetry.Buffer{}
	mark, err := NewDNSMetrics(gatewayConfig("synthetic-account"), api).CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal("collection failed with the count and one advertised dimension")
	}
	if !mark.Equal(to) || len(api.requests) != 1 {
		t.Fatal("partial-dimension collection did not complete one window")
	}
	wanted := api.requests[0].WantedFields
	if len(wanted) != 2 || wanted[0] != "sum.queries" || wanted[1] != "dimensions.country" {
		t.Fatal("query did not select the count and its only available dimension")
	}
	if len(out.Metrics) != 1 || out.Metrics[0].Value != 5 || len(out.Metrics[0].Attrs) != 1 || out.Metrics[0].Attrs[0].Key != semconv.AttrGatewayDNSCountry {
		t.Fatal("collector did not emit the complete sum with only the selected dimension")
	}
}

func TestDNSGroupsSelectCountAloneWhenOnlyCountIsAdvertised(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	settings := gatewaySettings()
	settings.AvailableFields = []string{"sum_queries"}
	settings.MaxNumberOfFields = 1
	api := &gatewayFakeAPI{settings: settings, rows: []map[string]any{{"sum": map[string]any{"queries": 11}}}}
	out := &telemetry.Buffer{}
	mark, err := NewDNSMetrics(gatewayConfig("synthetic-account"), api).CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal("count-only collection failed")
	}
	if !mark.Equal(to) || len(api.requests) != 1 || len(api.requests[0].WantedFields) != 1 || api.requests[0].WantedFields[0] != "sum.queries" {
		t.Fatal("count-only request did not select just sum.queries")
	}
	if len(out.Metrics) != 1 || out.Metrics[0].Value != 11 || len(out.Metrics[0].Attrs) != 0 {
		t.Fatal("count-only collection did not emit the complete sum without attributes")
	}
}

func TestDNSGroupsFieldLimitOneKeepsCountAndOmitsDimensions(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	settings := gatewaySettings()
	settings.MaxNumberOfFields = 1
	api := &gatewayFakeAPI{settings: settings, rows: []map[string]any{gatewayRow(13, "A", "allow", "GB")}}
	out := &telemetry.Buffer{}
	mark, err := NewDNSMetrics(gatewayConfig("synthetic-account"), api).CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal("field limit one rejected the required count")
	}
	if !mark.Equal(to) || len(api.requests) != 1 || len(api.requests[0].WantedFields) != 1 || api.requests[0].WantedFields[0] != "sum.queries" {
		t.Fatal("field limit one did not retain only the required count field")
	}
	if len(out.Metrics) != 1 || out.Metrics[0].Value != 13 || len(out.Metrics[0].Attrs) != 0 {
		t.Fatal("field limit one did not emit a complete count-only metric")
	}
}

func TestDNSGroupsSelectOptionalDimensionsInStableOrder(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	settings := gatewaySettings()
	settings.MaxNumberOfFields = 2
	api := &gatewayFakeAPI{settings: settings, rows: []map[string]any{gatewayRow(17, "A", "allow", "GB")}}
	out := &telemetry.Buffer{}
	mark, err := NewDNSMetrics(gatewayConfig("synthetic-account"), api).CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal("limited collection failed with one optional dimension")
	}
	if !mark.Equal(to) || len(api.requests) != 1 {
		t.Fatal("limited collection did not complete one window")
	}
	wanted := api.requests[0].WantedFields
	if len(wanted) != 2 || wanted[0] != "sum.queries" || wanted[1] != "dimensions.queryType" {
		t.Fatal("optional dimensions were not selected in their stable order")
	}
	if len(out.Metrics) != 1 || out.Metrics[0].Value != 17 || len(out.Metrics[0].Attrs) != 1 || out.Metrics[0].Attrs[0].Key != semconv.AttrGatewayDNSQueryType {
		t.Fatal("collector did not emit the complete sum with only its selected dimension")
	}
}

func TestDNSGroupsFailClosedOnMissingAccountSettingsOrRowFields(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	base := gatewaySettings()
	truncated := make([]any, gatewayDNSQueryLimit)
	tests := []struct {
		name     string
		account  string
		settings cfapi.DatasetSettings
		rows     any
	}{
		{name: "missing account", settings: base, rows: []any{}},
		{name: "dataset disabled", account: "synthetic-account", settings: func() cfapi.DatasetSettings { s := base; s.Enabled = false; return s }(), rows: []any{}},
		{name: "missing required available field", account: "synthetic-account", settings: func() cfapi.DatasetSettings { s := base; s.AvailableFields = s.AvailableFields[1:]; return s }(), rows: []any{}},
		{name: "zero field limit", account: "synthetic-account", settings: func() cfapi.DatasetSettings { s := base; s.MaxNumberOfFields = 0; return s }(), rows: []any{}},
		{name: "missing page-size limit", account: "synthetic-account", settings: func() cfapi.DatasetSettings { s := base; s.MaxPageSize = 0; return s }(), rows: []any{}},
		{name: "missing duration limit", account: "synthetic-account", settings: func() cfapi.DatasetSettings { s := base; s.MaxDuration = 0; return s }(), rows: []any{}},
		{name: "missing retention limit", account: "synthetic-account", settings: func() cfapi.DatasetSettings { s := base; s.NotOlderThan = 0; return s }(), rows: []any{}},
		{name: "truncated result", account: "synthetic-account", settings: base, rows: truncated},
		{name: "missing count", account: "synthetic-account", settings: base, rows: []any{map[string]any{"dimensions": map[string]any{"queryType": "A", "resolverDecision": "allow", "country": "GB"}}}},
		{name: "zero count group", account: "synthetic-account", settings: base, rows: []any{gatewayRow(0, "A", "allow", "GB")}},
		{name: "missing required dimension", account: "synthetic-account", settings: base, rows: []any{map[string]any{"sum": map[string]any{"queries": 3}, "dimensions": map[string]any{"queryType": "A", "resolverDecision": "allow"}}}},
		{name: "malformed selected dimension", account: "synthetic-account", settings: base, rows: []any{map[string]any{"sum": map[string]any{"queries": 3}, "dimensions": map[string]any{"queryType": true, "resolverDecision": "allow", "country": "GB"}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			api := &gatewayFakeAPI{settings: test.settings, rows: test.rows}
			out := &telemetry.Buffer{}
			mark, err := NewDNSMetrics(gatewayConfig(test.account), api).CollectWindow(context.Background(), from, to, out)
			if err == nil {
				t.Fatal("collection succeeded; want fail-closed error")
			}
			if !mark.Equal(from) || len(out.Metrics) != 0 {
				t.Fatal("failed collection advanced its mark or emitted partial metrics")
			}
		})
	}
}

func TestDNSGroupsPresentEmptyDimensionsUseBoundedFallbacks(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	row := gatewayRow(7, "A", "allow", "GB")
	dimensions := row["dimensions"].(map[string]any)
	dimensions["queryType"] = nil
	dimensions["resolverDecision"] = "  "
	dimensions["country"] = ""
	api := &gatewayFakeAPI{settings: gatewaySettings(), rows: []map[string]any{row}}
	out := &telemetry.Buffer{}
	mark, err := NewDNSMetrics(gatewayConfig("synthetic-account"), api).CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) || len(out.Metrics) != 1 || out.Metrics[0].Value != 7 {
		t.Fatal("present empty dimensions lost the complete query sum")
	}
	attrs := map[string]string{}
	for _, attr := range out.Metrics[0].Attrs {
		attrs[attr.Key] = attr.Value
	}
	if attrs[semconv.AttrGatewayDNSQueryType] != gatewayDNSOther || attrs[semconv.AttrGatewayDNSDecision] != gatewayDNSOther || attrs[semconv.AttrGatewayDNSCountry] != gatewayDNSUnknownCountry {
		t.Fatal("present empty dimensions were not normalized to bounded fallback labels")
	}
}

func TestDNSGroupsBoundUnexpectedDimensionValuesAndIgnoreOtherFields(t *testing.T) {
	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	row := gatewayRow(1, "unbounded-query-type", "unbounded-decision", "unbounded-country")
	row["unboundedSource"] = "unbounded-sensitive-value"
	api := &gatewayFakeAPI{settings: gatewaySettings(), rows: []map[string]any{row}}
	out := &telemetry.Buffer{}
	_, err := NewDNSMetrics(gatewayConfig("synthetic-account"), api).CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Metrics) != 1 || len(out.Metrics[0].Attrs) != 3 {
		t.Fatal("collector emitted unexpected metric attributes")
	}
	attrs := map[string]string{}
	for _, attr := range out.Metrics[0].Attrs {
		attrs[attr.Key] = attr.Value
	}
	if attrs[semconv.AttrGatewayDNSQueryType] != gatewayDNSOther || attrs[semconv.AttrGatewayDNSDecision] != gatewayDNSOther || attrs[semconv.AttrGatewayDNSCountry] != gatewayDNSUnknownCountry {
		t.Fatal("unrecognized values were not replaced with bounded labels")
	}
	if strings.Contains(fmt.Sprint(attrs), "unbounded-sensitive-value") {
		t.Fatal("unbounded source value was emitted as a metric attribute")
	}
}

func TestDNSGraphQLFiltersAreHalfOpenAndRespectSettingsLimits(t *testing.T) {
	from := time.Now().UTC().Truncate(time.Second).Add(-90 * time.Minute)
	to := from.Add(50 * time.Minute)
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error("could not decode GraphQL request")
			return
		}
		queries = append(queries, request.Query)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(request.Query, "settings{") {
			_, _ = w.Write([]byte(`{"data":{"viewer":{"accounts":[{"settings":{"cf1GatewayDnsRawGroups":{"enabled":true,"availableFields":["sum_queries","dimensions_queryType","dimensions_resolverDecision","dimensions_country"],"maxNumberOfFields":10,"maxDuration":1200,"notOlderThan":86400,"maxPageSize":100}}}]}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"viewer":{"accounts":[{"cf1GatewayDnsRawGroups":[{"sum":{"queries":1},"dimensions":{"queryType":"A","resolverDecision":"allow","country":"GB"}}]}]}}}`))
	}))
	defer server.Close()
	api := cfapi.New(config.CloudflareConfig{APIBase: server.URL, Timeout: time.Second, MaxResponseBytes: 4096})
	out := &telemetry.Buffer{}
	mark, err := NewDNSMetrics(gatewayConfig("synthetic-account"), api).CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal("collection through cfapi failed")
	}
	if !mark.Equal(to) {
		t.Fatal("collection did not return the end of the requested window")
	}
	if len(queries) != 4 { // one settings query and three bounded data windows
		t.Fatalf("GraphQL request count = %d, want settings plus three maxDuration windows", len(queries))
	}
	filterStart := from
	for i, query := range queries[1:] {
		filterEnd := filterStart.Add(20 * time.Minute)
		if filterEnd.After(to) {
			filterEnd = to
		}
		startFilter := fmt.Sprintf(`datetime_geq:%q`, filterStart.Format(time.RFC3339))
		endFilter := fmt.Sprintf(`datetime_lt:%q`, filterEnd.Format(time.RFC3339))
		if !strings.Contains(query, startFilter) || !strings.Contains(query, endFilter) {
			t.Fatalf("GraphQL data window %d did not carry the exact half-open bounds", i+1)
		}
		if !strings.Contains(query, "limit:100") {
			t.Fatalf("GraphQL data window %d did not respect maxPageSize", i+1)
		}
		filterStart = filterEnd
	}
	if !filterStart.Equal(to) || len(out.Metrics) != 3 {
		t.Fatal("bounded windows did not cover exactly the requested interval")
	}
}

func TestRegisterInstallsGatewayDNSOnlyWhenEnabled(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		cfg := gatewayConfig("synthetic-account")
		cfg.Collectors = map[string]config.CollectorConfig{
			"gateway.dns": {Enabled: enabled, Interval: time.Minute, InitialLookback: 30 * time.Minute, MaxWindow: time.Hour},
		}
		registry := collector.NewRegistry()
		Register(collector.Deps{Config: cfg, API: &gatewayFakeAPI{}, Registry: registry})
		entries := registry.Entries()
		if !enabled {
			if len(entries) != 0 {
				t.Fatal("disabled Gateway DNS collector was registered")
			}
			continue
		}
		if len(entries) != 1 || entries[0].Collector.Name() != "gateway.dns" || entries[0].Interval != time.Minute || entries[0].InitialLookback != 30*time.Minute || entries[0].MaxWindow != time.Hour {
			t.Fatal("enabled Gateway DNS collector was not registered with its configured window")
		}
		if _, ok := entries[0].Collector.(collector.WindowCollector); !ok {
			t.Fatal("Gateway DNS registration did not install a window collector")
		}
	}
}

func stringIn(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
