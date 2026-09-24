package dns

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type fakeAPI struct {
	zones        []cfapi.Zone
	settings     map[string]cfapi.DatasetSettings
	rows         map[string][]map[string]any
	queryErrors  map[string]error
	queries      []cfapi.GraphQLRequest
	readSettings []string
}

func (f *fakeAPI) Get(context.Context, string, url.Values, any) error {
	return errors.New("unexpected REST request")
}

func (f *fakeAPI) Query(_ context.Context, request cfapi.GraphQLRequest, out any) error {
	f.queries = append(f.queries, request)
	key := request.ScopeID + "/" + request.Dataset
	if err := f.queryErrors[key]; err != nil {
		return err
	}
	body, err := json.Marshal(f.rows[key])
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

func (f *fakeAPI) Accounts(context.Context) ([]cfapi.Account, error) {
	return nil, errors.New("unexpected account discovery")
}

func (f *fakeAPI) Zones(context.Context) ([]cfapi.Zone, error) {
	return append([]cfapi.Zone(nil), f.zones...), nil
}

func (f *fakeAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) {
	return nil, errors.New("unexpected gateway discovery")
}

func (f *fakeAPI) DatasetSettings(_ context.Context, _ cfapi.Scope, zoneID, dataset string) (cfapi.DatasetSettings, error) {
	key := zoneID + "/" + dataset
	f.readSettings = append(f.readSettings, key)
	settings, ok := f.settings[key]
	if !ok {
		return cfapi.DatasetSettings{}, errors.New("unexpected settings request")
	}
	return settings, nil
}

func dnsSettings(maxFields int, fields ...string) cfapi.DatasetSettings {
	return cfapi.DatasetSettings{Enabled: true, AvailableFields: fields, MaxNumberOfFields: maxFields}
}

func TestEventsEmitRawDNSQueryFields(t *testing.T) {
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	row := map[string]any{
		"datetime": from.Format(time.RFC3339Nano), "queryName": "www.example.com", "queryType": "A", "responseCode": "NOERROR",
		"responseCached": true, "responseStale": false, "protocol": "UDP", "coloName": "test-colo-one",
		"sourceIP": "192.0.2.4", "destinationIP": "2001:db8::4", "upstreamIP": "198.51.100.8",
		"ipVersion": "IPv4", "sampleInterval": 1, "querySize": 24, "responseSize": 100,
	}
	upperBoundRow := make(map[string]any, len(row))
	for key, value := range row {
		upperBoundRow[key] = value
	}
	upperBoundRow["datetime"] = to.Format(time.RFC3339Nano)
	api := &fakeAPI{
		zones: []cfapi.Zone{{ID: "zone-one", Name: "example.test"}},
		settings: map[string]cfapi.DatasetSettings{
			"zone-one/dnsAnalyticsAdaptive": dnsSettings(70,
				"datetime", "queryName", "queryType", "responseCode", "responseCached", "responseStale", "protocol", "coloName",
				"sourceIP", "destinationIP", "upstreamIP", "ipVersion", "sampleInterval", "querySize", "responseSize"),
		},
		rows: map[string][]map[string]any{"zone-one/dnsAnalyticsAdaptive": {row, upperBoundRow}},
	}
	out := &telemetry.Buffer{}
	mark, err := NewEvents(&config.Config{}, api).CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) {
		t.Fatalf("mark = %s, want %s", mark, to)
	}
	if len(out.Records) != 1 || len(out.Metrics) != 0 {
		t.Fatalf("records=%d metrics=%d, want one raw log and no metrics", len(out.Records), len(out.Metrics))
	}
	record := out.Records[0]
	if record.Event != semconv.EventDNSQuery || !record.At.Equal(from) {
		t.Fatalf("event=%q at=%s", record.Event, record.At)
	}
	for _, attr := range []telemetry.Attr{
		{Key: semconv.AttrDNSZone, Value: "example.test"},
		{Key: semconv.AttrDNSQueryName, Value: "www.example.com"},
		{Key: semconv.AttrDNSQueryType, Value: "A"},
		{Key: semconv.AttrDNSResponseCode, Value: "NOERROR"},
		{Key: semconv.AttrDNSResponseCached, Value: "true"},
		{Key: semconv.AttrDNSProtocol, Value: "UDP"},
		{Key: semconv.AttrDNSColo, Value: "test-colo-one"},
	} {
		if !hasDNSAttr(record.Attrs, attr.Key, attr.Value) {
			t.Errorf("log attribute %q=%q is missing: %+v", attr.Key, attr.Value, record.Attrs)
		}
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(record.Body), &body); err != nil {
		t.Fatalf("raw event body is invalid JSON: %v", err)
	}
	if body["queryName"] != "www.example.com" || body["sourceIP"] != "192.0.2.4" {
		t.Fatalf("raw event body omitted DNS fields: %+v", body)
	}
	if len(api.queries) != 1 || api.queries[0].Dataset != "dnsAnalyticsAdaptive" || api.queries[0].ScopeID != "zone-one" {
		t.Fatalf("queries = %+v", api.queries)
	}
	for _, field := range []string{"datetime", "queryName", "queryType", "responseCode", "responseCached", "protocol", "coloName"} {
		if !containsField(api.queries[0].WantedFields, field) {
			t.Errorf("raw DNS request omitted %q: %v", field, api.queries[0].WantedFields)
		}
	}
}

func TestMetricsUsePerZoneGroupsAndBoundedDimensions(t *testing.T) {
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	api := &fakeAPI{
		zones: []cfapi.Zone{{ID: "zone-one", Name: "one.example.test"}, {ID: "zone-two", Name: "two.example.test"}},
		settings: map[string]cfapi.DatasetSettings{
			"zone-one/dnsAnalyticsAdaptiveGroups": dnsSettings(3, "count", "dimensions_queryType", "dimensions_responseCode", "dimensions_responseCached", "dimensions_responseStale", "dimensions_protocol", "dimensions_coloName"),
			"zone-two/dnsAnalyticsAdaptiveGroups": dnsSettings(70, "count", "dimensions_queryType", "dimensions_responseCode", "dimensions_responseCached", "dimensions_responseStale", "dimensions_protocol", "dimensions_coloName"),
		},
		rows: map[string][]map[string]any{
			"zone-one/dnsAnalyticsAdaptiveGroups": {{"count": 11, "dimensions": map[string]any{"queryType": "A", "responseCode": "NOERROR", "responseCached": true, "responseStale": false, "protocol": "UDP", "coloName": "test-colo-one", "queryName": "private.example", "sourceIP": "192.0.2.5"}}},
			"zone-two/dnsAnalyticsAdaptiveGroups": {{"count": 7, "dimensions": map[string]any{"queryType": "AAAA", "responseCode": "NXDOMAIN", "responseCached": false, "responseStale": false, "protocol": "DoH", "coloName": "test-colo-two", "queryName": "private.example", "sourceIP": "192.0.2.6"}}},
		},
	}
	out := &telemetry.Buffer{}
	mark, err := NewMetrics(&config.Config{}, api).CollectWindow(context.Background(), from, to, out)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(to) || len(out.Records) != 0 || len(out.Metrics) != 2 {
		t.Fatalf("mark=%s records=%d metrics=%d", mark, len(out.Records), len(out.Metrics))
	}
	if out.Metrics[0].Name != semconv.MetricDNSQueries || out.Metrics[0].Value != 11 || out.Metrics[1].Value != 7 {
		t.Fatalf("metrics = %+v", out.Metrics)
	}
	if !hasDNSAttr(out.Metrics[0].Attrs, semconv.AttrDNSZone, "one.example.test") || !hasDNSAttr(out.Metrics[1].Attrs, semconv.AttrDNSZone, "two.example.test") {
		t.Fatalf("zone dimensions missing: %+v", out.Metrics)
	}
	if !hasDNSAttr(out.Metrics[1].Attrs, semconv.AttrDNSQueryType, "AAAA") || !hasDNSAttr(out.Metrics[1].Attrs, semconv.AttrDNSResponseCode, "NXDOMAIN") || !hasDNSAttr(out.Metrics[1].Attrs, semconv.AttrDNSProtocol, "DoH") {
		t.Fatalf("bounded group dimensions missing: %+v", out.Metrics[1].Attrs)
	}
	for _, metric := range out.Metrics {
		for _, attr := range metric.Attrs {
			if attr.Key == semconv.AttrDNSQueryName || attr.Key == semconv.AttrDNSSourceIP || attr.Key == semconv.AttrDNSDestinationIP || attr.Key == semconv.AttrDNSUpstreamIP {
				t.Fatalf("high-cardinality field %q used as a metric attribute", attr.Key)
			}
		}
	}
	if len(api.queries) != 2 || api.queries[0].Dataset != "dnsAnalyticsAdaptiveGroups" || api.queries[1].Dataset != "dnsAnalyticsAdaptiveGroups" {
		t.Fatalf("queries = %+v", api.queries)
	}
	if want := []string{"count", "dimensions.queryType", "dimensions.responseCode"}; !reflect.DeepEqual(api.queries[0].WantedFields, want) {
		t.Fatalf("first zone fields = %v, want maxNumberOfFields-limited %v", api.queries[0].WantedFields, want)
	}
	if containsField(api.queries[1].WantedFields, "dimensions.queryName") || containsField(api.queries[1].WantedFields, "dimensions.sourceIP") {
		t.Fatalf("high-cardinality fields requested for metrics: %v", api.queries[1].WantedFields)
	}
	if !reflect.DeepEqual(api.readSettings, []string{"zone-one/dnsAnalyticsAdaptiveGroups", "zone-two/dnsAnalyticsAdaptiveGroups"}) {
		t.Fatalf("dataset settings were not negotiated per zone: %v", api.readSettings)
	}
}

func TestConfiguredZoneAbsenceFailsClosed(t *testing.T) {
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	for _, collectorName := range []string{"events", "metrics"} {
		t.Run(collectorName, func(t *testing.T) {
			api := &fakeAPI{zones: []cfapi.Zone{{ID: "zone-one", Name: "one.example.test"}}}
			cfg := &config.Config{Cloudflare: config.CloudflareConfig{Zones: []string{"one.example.test", "missing.example.test"}}}
			out := &telemetry.Buffer{}
			var mark time.Time
			var err error
			if collectorName == "events" {
				mark, err = NewEvents(cfg, api).CollectWindow(context.Background(), from, to, out)
			} else {
				mark, err = NewMetrics(cfg, api).CollectWindow(context.Background(), from, to, out)
			}
			if err == nil {
				t.Fatal("missing configured zone was accepted")
			}
			if !mark.Equal(from) || len(api.queries) != 0 || len(out.Records) != 0 || len(out.Metrics) != 0 {
				t.Fatalf("mark=%s queries=%d records=%d metrics=%d; want fail closed", mark, len(api.queries), len(out.Records), len(out.Metrics))
			}
		})
	}
}

func TestMissingRequiredFieldsFailClosed(t *testing.T) {
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	for _, tc := range []struct {
		name, dataset string
		fields        []string
		collect       func(*config.Config, cfapi.Client) (time.Time, error)
	}{
		{name: "raw response cached", dataset: "dnsAnalyticsAdaptive", fields: []string{"datetime", "queryName", "queryType", "responseCode", "protocol", "coloName"}, collect: func(cfg *config.Config, api cfapi.Client) (time.Time, error) {
			return NewEvents(cfg, api).CollectWindow(context.Background(), from, to, &telemetry.Buffer{})
		}},
		{name: "Groups count", dataset: "dnsAnalyticsAdaptiveGroups", fields: []string{"dimensions_queryType"}, collect: func(cfg *config.Config, api cfapi.Client) (time.Time, error) {
			return NewMetrics(cfg, api).CollectWindow(context.Background(), from, to, &telemetry.Buffer{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeAPI{
				zones:    []cfapi.Zone{{ID: "zone-one", Name: "one.example.test"}},
				settings: map[string]cfapi.DatasetSettings{"zone-one/" + tc.dataset: dnsSettings(70, tc.fields...)},
			}
			mark, err := tc.collect(&config.Config{}, api)
			if err == nil || !mark.Equal(from) {
				t.Fatalf("mark=%s error=%v, want required-field failure without progress", mark, err)
			}
			if len(api.queries) != 0 {
				t.Fatalf("issued query without required fields: %+v", api.queries)
			}
		})
	}
}

func TestEmptyZoneDiscoveryDoesNotAdvanceDNS(t *testing.T) {
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	for _, tc := range []struct {
		name    string
		collect func(*config.Config, cfapi.Client) (time.Time, error)
	}{
		{"events", func(cfg *config.Config, api cfapi.Client) (time.Time, error) {
			return NewEvents(cfg, api).CollectWindow(context.Background(), from, to, &telemetry.Buffer{})
		}},
		{"metrics", func(cfg *config.Config, api cfapi.Client) (time.Time, error) {
			return NewMetrics(cfg, api).CollectWindow(context.Background(), from, to, &telemetry.Buffer{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeAPI{}
			mark, err := tc.collect(&config.Config{}, api)
			if err == nil || !mark.Equal(from) || len(api.queries) != 0 {
				t.Fatalf("empty discovery: mark=%s error=%v queries=%d, want no checkpoint progress", mark, err, len(api.queries))
			}
		})
	}
}

func hasDNSAttr(attrs []telemetry.Attr, key, value string) bool {
	for _, attr := range attrs {
		if attr.Key == key && attr.Value == value {
			return true
		}
	}
	return false
}

func containsField(fields []string, wanted string) bool {
	for _, field := range fields {
		if field == wanted {
			return true
		}
	}
	return false
}
