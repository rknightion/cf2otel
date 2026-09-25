package email

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	otellog "go.opentelemetry.io/otel/log"
)

const testAccountID = "acct-00000000000000000000000000000001"

var testFrom = time.Now().UTC().Truncate(5 * time.Minute).Add(-20 * time.Minute).Format(time.RFC3339)

type emailTestAPI struct {
	zones        []cfapi.Zone
	settings     map[string]cfapi.DatasetSettings
	settingCalls []string
	queries      []cfapi.GraphQLRequest
	query        func(cfapi.GraphQLRequest) ([]map[string]any, error)
}

func (a *emailTestAPI) Get(context.Context, string, url.Values, any) error {
	return errors.New("unexpected REST request")
}
func (a *emailTestAPI) Accounts(context.Context) ([]cfapi.Account, error) {
	return nil, errors.New("unexpected Accounts request")
}
func (a *emailTestAPI) Zones(context.Context) ([]cfapi.Zone, error) {
	return append([]cfapi.Zone(nil), a.zones...), nil
}
func (a *emailTestAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) {
	return nil, errors.New("unexpected Gateways request")
}
func (a *emailTestAPI) DatasetSettings(_ context.Context, scope cfapi.Scope, id, dataset string) (cfapi.DatasetSettings, error) {
	key := string(scope) + "/" + id + "/" + dataset
	a.settingCalls = append(a.settingCalls, key)
	return a.settings[key], nil
}
func (a *emailTestAPI) Query(_ context.Context, request cfapi.GraphQLRequest, out any) error {
	a.queries = append(a.queries, request)
	var rows []map[string]any
	var err error
	if a.query != nil {
		rows, err = a.query(request)
	}
	if err != nil {
		return err
	}
	result, ok := out.(*[]map[string]any)
	if !ok {
		return fmt.Errorf("unexpected GraphQL output type %T", out)
	}
	*result = append([]map[string]any(nil), rows...)
	return nil
}

type emailMetric struct {
	name  string
	value float64
	attrs []telemetry.Attr
}

type emailTestEmitter struct {
	metrics []emailMetric
	events  []string
}

func (e *emailTestEmitter) Gauge(context.Context, string, float64, ...telemetry.Attr) error {
	return nil
}
func (e *emailTestEmitter) Counter(_ context.Context, name string, value float64, attrs ...telemetry.Attr) error {
	e.metrics = append(e.metrics, emailMetric{name: name, value: value, attrs: append([]telemetry.Attr(nil), attrs...)})
	return nil
}
func (e *emailTestEmitter) Histogram(context.Context, string, float64, ...telemetry.Attr) error {
	return nil
}
func (e *emailTestEmitter) LogEvent(_ context.Context, name, _ string, _ time.Time, _ otellog.Severity, _ ...telemetry.Attr) error {
	e.events = append(e.events, name)
	return nil
}
func (e *emailTestEmitter) Span(context.Context, telemetry.SpanSpec) error { return nil }

type emailCheckpointStore struct {
	values map[string]time.Time
	writes int
}

func (s *emailCheckpointStore) Get(name string) (time.Time, bool) {
	value, ok := s.values[name]
	return value, ok
}

func (s *emailCheckpointStore) Set(name string, value time.Time) error {
	s.values[name] = value
	s.writes++
	return nil
}

func testZone(id, accountID string) cfapi.Zone {
	var zone cfapi.Zone
	zone.ID = id
	zone.Account.ID = accountID
	return zone
}

func datasetSettings(enabled bool, fields ...string) cfapi.DatasetSettings {
	return cfapi.DatasetSettings{
		Enabled:           enabled,
		AvailableFields:   fields,
		MaxNumberOfFields: 2,
		MaxDuration:       3600,
		NotOlderThan:      86400,
		MaxPageSize:       64,
	}
}

func settingsKey(zoneID, dataset string) string {
	return string(cfapi.ZoneScope) + "/" + zoneID + "/" + dataset
}

func newEmailTestAPI(dataset string, zones []cfapi.Zone, byZone map[string]cfapi.DatasetSettings) *emailTestAPI {
	settings := make(map[string]cfapi.DatasetSettings, len(byZone))
	for zoneID, value := range byZone {
		settings[settingsKey(zoneID, dataset)] = value
	}
	return &emailTestAPI{zones: zones, settings: settings}
}

func registeredCollector(t *testing.T, cfg *config.Config, api *emailTestAPI, name string) collector.WindowCollector {
	t.Helper()
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: cfg, API: api, Registry: registry})
	for _, entry := range registry.Entries() {
		if entry.Collector.Name() == name {
			window, ok := entry.Collector.(collector.WindowCollector)
			if !ok {
				t.Fatalf("collector %s is not a window collector", name)
			}
			return window
		}
	}
	t.Fatalf("Register did not register %s", name)
	return nil
}

func fixedTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestRegisterInstallsRoutingAndSendingWindowCollectors(t *testing.T) {
	cfg := config.Default()
	registry := collector.NewRegistry()
	Register(collector.Deps{Config: &cfg, API: &emailTestAPI{}, Registry: registry})

	entries := registry.Entries()
	if len(entries) != 2 {
		t.Fatalf("registered %d collectors, want routing and sending", len(entries))
	}
	for _, name := range []string{"email.routing", "email.sending"} {
		found := false
		for _, entry := range entries {
			if entry.Collector.Name() == name {
				found = true
				if entry.Interval != 5*time.Minute {
					t.Errorf("%s interval = %s, want 5m", name, entry.Interval)
				}
				if entry.InitialLookback <= 0 || entry.MaxWindow <= 0 {
					t.Errorf("%s has invalid window configuration: %+v", name, entry)
				}
			}
		}
		if !found {
			t.Errorf("collector %s is not registered", name)
		}
	}
}

func TestCollectorsAggregateOwnedZonesAndEmitUnlabeledCounters(t *testing.T) {
	zones := []cfapi.Zone{
		testZone("zone-00000000000000000000000000000001", testAccountID),
		testZone("zone-00000000000000000000000000000002", testAccountID),
		testZone("zone-00000000000000000000000000000003", testAccountID),
		testZone("zone-00000000000000000000000000000004", "acct-foreign"),
	}
	for _, test := range []struct {
		collector string
		dataset   string
		metric    string
	}{
		{collector: "email.routing", dataset: "emailRoutingAdaptiveGroups", metric: semconv.MetricEmailRoutingEvents},
		{collector: "email.sending", dataset: "emailSendingAdaptiveGroups", metric: semconv.MetricEmailSendingEvents},
	} {
		t.Run(test.collector, func(t *testing.T) {
			api := newEmailTestAPI(test.dataset, zones, map[string]cfapi.DatasetSettings{
				zones[0].ID: datasetSettings(true, "count", "dimensions_datetimeFiveMinutes"),
				zones[1].ID: datasetSettings(true, "count", "dimensions_datetimeFiveMinutes"),
				zones[2].ID: datasetSettings(false),
				zones[3].ID: datasetSettings(true, "count", "dimensions_datetimeFiveMinutes"),
			})
			api.query = func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
				count := 2.0
				if request.ScopeID == zones[1].ID {
					count = 5
				}
				return []map[string]any{{
					"count":      count,
					"dimensions": map[string]any{"datetimeFiveMinutes": testFrom},
				}}, nil
			}
			cfg := config.Default()
			cfg.Cloudflare.AccountID = testAccountID
			window := registeredCollector(t, &cfg, api, test.collector)
			emitter := &emailTestEmitter{}
			from := fixedTime(t, testFrom)
			to := from.Add(5 * time.Minute)

			highWater, err := window.CollectWindow(context.Background(), from, to, emitter)
			if err != nil {
				t.Fatalf("CollectWindow: %v", err)
			}
			if !highWater.Equal(to) {
				t.Errorf("high-water = %s, want %s", highWater, to)
			}
			if len(emitter.metrics) != 1 {
				t.Fatalf("emitted %d metrics, want one account aggregate", len(emitter.metrics))
			}
			if got := emitter.metrics[0]; got.name != test.metric || got.value != 7 || len(got.attrs) != 0 {
				t.Errorf("metric = %+v, want %s=7 without attributes", got, test.metric)
			}
			if len(api.queries) != 2 {
				t.Fatalf("queried %d zones, want two enabled owned zones", len(api.queries))
			}
			for _, request := range api.queries {
				if request.Scope != cfapi.ZoneScope || request.ScopeID == zones[3].ID {
					t.Errorf("query crossed the account boundary: %+v", request)
				}
				if len(request.WantedFields) != 2 || request.WantedFields[0] != "count" || request.WantedFields[1] != "dimensions.datetimeFiveMinutes" {
					t.Errorf("selected fields = %v, want count and datetimeFiveMinutes", request.WantedFields)
				}
			}
		})
	}
}

func TestConfiguredOwnedZoneNameSelectsAndCounts(t *testing.T) {
	zone := testZone("zone-00000000000000000000000000000001", testAccountID)
	zone.Name = "owned.invalid"
	api := newEmailTestAPI(emailDatasetRouting, []cfapi.Zone{zone}, map[string]cfapi.DatasetSettings{
		zone.ID: datasetSettings(true, "count", "dimensions_datetimeFiveMinutes"),
	})
	api.query = func(cfapi.GraphQLRequest) ([]map[string]any, error) {
		return []map[string]any{{"count": 3.0, "dimensions": map[string]any{"datetimeFiveMinutes": testFrom}}}, nil
	}
	cfg := config.Default()
	cfg.Cloudflare.AccountID = testAccountID
	cfg.Cloudflare.Zones = []string{"OWNED.INVALID"}
	window := registeredCollector(t, &cfg, api, "email.routing")
	emitter := &emailTestEmitter{}
	from := fixedTime(t, testFrom)
	to := from.Add(emailBucket)

	highWater, err := window.CollectWindow(context.Background(), from, to, emitter)
	if err != nil || !highWater.Equal(to) || len(api.queries) != 1 || api.queries[0].ScopeID != zone.ID {
		t.Fatalf("configured owned zone was not queried: high-water=%s err=%v queries=%v", highWater, err, api.queries)
	}
	if len(emitter.metrics) != 1 || emitter.metrics[0].value != 3 {
		t.Errorf("emitted metrics = %+v, want one count of 3", emitter.metrics)
	}
}

func TestZoneSelectionAndEntitlementGapsFailWithoutAdvance(t *testing.T) {
	owned := testZone("zone-00000000000000000000000000000001", testAccountID)
	owned.Name = "owned.invalid"
	foreign := testZone("zone-00000000000000000000000000000002", "acct-foreign")
	foreign.Name = "foreign.invalid"
	for _, test := range []struct {
		name       string
		zones      []cfapi.Zone
		configured []string
		enabled    bool
	}{
		{name: "unmatched name", zones: []cfapi.Zone{owned}, configured: []string{"missing.invalid"}, enabled: true},
		{name: "foreign ID", zones: []cfapi.Zone{owned, foreign}, configured: []string{foreign.ID}, enabled: true},
		{name: "no owned zones", zones: []cfapi.Zone{foreign}, enabled: true},
		{name: "all datasets disabled", zones: []cfapi.Zone{owned}, enabled: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			api := newEmailTestAPI(emailDatasetRouting, test.zones, map[string]cfapi.DatasetSettings{
				owned.ID: datasetSettings(test.enabled, "count", "dimensions_datetimeFiveMinutes"),
			})
			cfg := config.Default()
			cfg.Cloudflare.AccountID = testAccountID
			cfg.Cloudflare.Zones = test.configured
			window := registeredCollector(t, &cfg, api, "email.routing")
			emitter := &emailTestEmitter{}
			from := fixedTime(t, testFrom)

			highWater, err := window.CollectWindow(context.Background(), from, from.Add(emailBucket), emitter)
			if err == nil || !highWater.Equal(from) || len(api.queries) != 0 || len(emitter.metrics) != 0 {
				t.Errorf("selection gap advanced or emitted: high-water=%s err=%v queries=%v metrics=%v", highWater, err, api.queries, emitter.metrics)
			}
		})
	}
}

func TestSchedulerDoesNotSkipEmailRetentionGap(t *testing.T) {
	zone := testZone("zone-00000000000000000000000000000001", testAccountID)
	api := newEmailTestAPI(emailDatasetRouting, []cfapi.Zone{zone}, map[string]cfapi.DatasetSettings{
		zone.ID: datasetSettings(true, "count", "dimensions_datetimeFiveMinutes"),
	})
	cfg := config.Default()
	cfg.Cloudflare.AccountID = testAccountID
	window := registeredCollector(t, &cfg, api, "email.routing")
	now := time.Now().UTC().Truncate(5 * time.Minute)
	initial := now.Add(-26 * time.Hour)
	checkpoints := &emailCheckpointStore{values: map[string]time.Time{"email.routing": initial}}
	emitter := &emailTestEmitter{}
	scheduler := collector.NewScheduler(nil, emitter, checkpoints)
	scheduler.Now = func() time.Time { return now }
	entry := collector.Entry{Collector: window, Interval: 5 * time.Minute, MaxWindow: time.Hour}

	err := scheduler.RunOnce(context.Background(), entry)
	if err == nil {
		t.Error("RunOnce succeeded for an email window older than dataset retention")
	}
	if got, _ := checkpoints.Get(window.Name()); !got.Equal(initial) || checkpoints.writes != 0 {
		t.Errorf("email retention failure advanced checkpoint from %s to %s (%d writes)", initial, got, checkpoints.writes)
	}
	if len(emitter.events) != 0 || len(emitter.metrics) != 0 {
		t.Errorf("email retention failure emitted events or metrics: events=%v metrics=%v", emitter.events, emitter.metrics)
	}
	if len(api.queries) != 0 {
		t.Errorf("email retention failure queried data: %+v", api.queries)
	}
}

func TestMissingRequiredSettingsFieldFailsWithoutEmissionOrCheckpointAdvance(t *testing.T) {
	for _, missing := range []string{"count", "dimensions_datetimeFiveMinutes"} {
		t.Run(missing, func(t *testing.T) {
			zone := testZone("zone-00000000000000000000000000000001", testAccountID)
			fields := []string{"count", "dimensions_datetimeFiveMinutes"}
			if missing == fields[0] {
				fields = fields[1:]
			} else {
				fields = fields[:1]
			}
			api := newEmailTestAPI("emailRoutingAdaptiveGroups", []cfapi.Zone{zone}, map[string]cfapi.DatasetSettings{
				zone.ID: datasetSettings(true, fields...),
			})
			cfg := config.Default()
			cfg.Cloudflare.AccountID = testAccountID
			window := registeredCollector(t, &cfg, api, "email.routing")
			emitter := &emailTestEmitter{}
			from := fixedTime(t, testFrom)
			to := from.Add(5 * time.Minute)

			highWater, err := window.CollectWindow(context.Background(), from, to, emitter)
			if err == nil {
				t.Fatal("CollectWindow succeeded without a required field")
			}
			if !highWater.Equal(from) || len(emitter.metrics) != 0 || len(api.queries) != 0 {
				t.Errorf("failed window advanced or emitted: high-water=%s metrics=%v queries=%v", highWater, emitter.metrics, api.queries)
			}
		})
	}
}

func TestQueryLimitSaturationBisectsBucketsAndHonorsSettings(t *testing.T) {
	zone := testZone("zone-00000000000000000000000000000001", testAccountID)
	settings := datasetSettings(true, "count", "dimensions_datetimeFiveMinutes")
	settings.MaxDuration = 1200
	settings.MaxPageSize = 11
	api := newEmailTestAPI("emailRoutingAdaptiveGroups", []cfapi.Zone{zone}, map[string]cfapi.DatasetSettings{zone.ID: settings})
	api.query = func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
		if request.To.Sub(request.From) > 10*time.Minute {
			return nil, fmt.Errorf("GraphQL dataset %s window saturated limit %d", request.Dataset, request.Limit)
		}
		var rows []map[string]any
		for bucket := request.From; bucket.Before(request.To); bucket = bucket.Add(5 * time.Minute) {
			rows = append(rows, map[string]any{
				"count":      1.0,
				"dimensions": map[string]any{"datetimeFiveMinutes": bucket.Format(time.RFC3339)},
			})
		}
		return rows, nil
	}
	cfg := config.Default()
	cfg.Cloudflare.AccountID = testAccountID
	window := registeredCollector(t, &cfg, api, "email.routing")
	emitter := &emailTestEmitter{}
	from := fixedTime(t, testFrom)
	to := from.Add(20 * time.Minute)

	highWater, err := window.CollectWindow(context.Background(), from, to, emitter)
	if err != nil {
		t.Fatalf("CollectWindow: %v", err)
	}
	if !highWater.Equal(to) {
		t.Errorf("high-water = %s, want %s", highWater, to)
	}
	if len(emitter.metrics) != 1 || emitter.metrics[0].value != 4 {
		t.Errorf("metrics = %+v, want one counter with value 4", emitter.metrics)
	}
	if len(api.queries) != 3 {
		t.Fatalf("query count = %d, want one saturated request plus two halves", len(api.queries))
	}
	for _, request := range api.queries {
		if request.Limit != 11 {
			t.Errorf("query limit = %d, want configured maxPageSize 11", request.Limit)
		}
		if request.From.Minute()%5 != 0 || request.From.Second() != 0 || request.To.Minute()%5 != 0 || request.To.Second() != 0 {
			t.Errorf("query split cut a five-minute bucket: %s..%s", request.From, request.To)
		}
	}
}

func TestIrreducibleBucketSaturationDoesNotAdvanceOrEmit(t *testing.T) {
	zone := testZone("zone-00000000000000000000000000000001", testAccountID)
	api := newEmailTestAPI("emailSendingAdaptiveGroups", []cfapi.Zone{zone}, map[string]cfapi.DatasetSettings{
		zone.ID: datasetSettings(true, "count", "dimensions_datetimeFiveMinutes"),
	})
	api.query = func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
		return nil, fmt.Errorf("GraphQL dataset %s window saturated limit %d", request.Dataset, request.Limit)
	}
	cfg := config.Default()
	cfg.Cloudflare.AccountID = testAccountID
	window := registeredCollector(t, &cfg, api, "email.sending")
	emitter := &emailTestEmitter{}
	from := fixedTime(t, testFrom)

	highWater, err := window.CollectWindow(context.Background(), from, from.Add(5*time.Minute), emitter)
	if err == nil {
		t.Fatal("CollectWindow succeeded for an irreducibly saturated bucket")
	}
	if !highWater.Equal(from) || len(emitter.metrics) != 0 {
		t.Errorf("saturated bucket advanced or emitted: high-water=%s metrics=%v", highWater, emitter.metrics)
	}
}

func TestAdjacentAndInitiallyUnalignedWindowsUseCompleteBucketsOnly(t *testing.T) {
	zone := testZone("zone-00000000000000000000000000000001", testAccountID)
	api := newEmailTestAPI("emailRoutingAdaptiveGroups", []cfapi.Zone{zone}, map[string]cfapi.DatasetSettings{
		zone.ID: datasetSettings(true, "count", "dimensions_datetimeFiveMinutes"),
	})
	api.query = func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
		var rows []map[string]any
		for bucket := request.From; bucket.Before(request.To); bucket = bucket.Add(5 * time.Minute) {
			rows = append(rows, map[string]any{"count": 1.0, "dimensions": map[string]any{"datetimeFiveMinutes": bucket.Format(time.RFC3339)}})
		}
		return rows, nil
	}
	cfg := config.Default()
	cfg.Cloudflare.AccountID = testAccountID
	window := registeredCollector(t, &cfg, api, "email.routing")
	bucketStart := time.Now().UTC().Truncate(emailBucket).Add(-30 * time.Minute)
	from := bucketStart.Add(2 * time.Minute)
	firstTo := bucketStart.Add(17 * time.Minute)
	firstEmitter := &emailTestEmitter{}
	firstHigh, err := window.CollectWindow(context.Background(), from, firstTo, firstEmitter)
	if err != nil {
		t.Fatalf("first CollectWindow: %v", err)
	}
	if want := bucketStart.Add(15 * time.Minute); !firstHigh.Equal(want) {
		t.Errorf("first high-water = %s, want %s", firstHigh, want)
	}
	secondTo := bucketStart.Add(27 * time.Minute)
	secondEmitter := &emailTestEmitter{}
	secondHigh, err := window.CollectWindow(context.Background(), firstHigh, secondTo, secondEmitter)
	if err != nil {
		t.Fatalf("second CollectWindow: %v", err)
	}
	if want := bucketStart.Add(25 * time.Minute); !secondHigh.Equal(want) {
		t.Errorf("second high-water = %s, want %s", secondHigh, want)
	}
	if len(api.queries) != 2 || !api.queries[0].From.Equal(bucketStart.Add(5*time.Minute)) || !api.queries[0].To.Equal(firstHigh) || !api.queries[1].From.Equal(firstHigh) || !api.queries[1].To.Equal(secondHigh) {
		t.Errorf("adjacent query windows have a gap or overlap: %+v", api.queries)
	}
	if len(firstEmitter.metrics) != 1 || firstEmitter.metrics[0].value != 2 || len(secondEmitter.metrics) != 1 || secondEmitter.metrics[0].value != 2 {
		t.Errorf("adjacent metric totals = %v then %v, want 2 and 2", firstEmitter.metrics, secondEmitter.metrics)
	}
}

func TestNoCompleteBucketDoesNotAdvanceCheckpointOrEmit(t *testing.T) {
	zone := testZone("zone-00000000000000000000000000000001", testAccountID)
	api := newEmailTestAPI("emailRoutingAdaptiveGroups", []cfapi.Zone{zone}, nil)
	cfg := config.Default()
	cfg.Cloudflare.AccountID = testAccountID
	window := registeredCollector(t, &cfg, api, "email.routing")
	emitter := &emailTestEmitter{}
	from := fixedTime(t, "2026-09-25T10:01:00Z")

	highWater, err := window.CollectWindow(context.Background(), from, fixedTime(t, "2026-09-25T10:04:00Z"), emitter)
	if err != nil {
		t.Fatalf("CollectWindow: %v", err)
	}
	if !highWater.Equal(from) || len(api.settingCalls) != 0 || len(api.queries) != 0 || len(emitter.metrics) != 0 {
		t.Errorf("incomplete bucket caused work or advancement: high-water=%s settings=%v queries=%v metrics=%v", highWater, api.settingCalls, api.queries, emitter.metrics)
	}
}

func TestSeriesCapAllowsOnlyTheUnlabeledAggregate(t *testing.T) {
	zone := testZone("zone-00000000000000000000000000000001", testAccountID)
	api := newEmailTestAPI("emailRoutingAdaptiveGroups", []cfapi.Zone{zone}, map[string]cfapi.DatasetSettings{
		zone.ID: datasetSettings(true, "count", "dimensions_datetimeFiveMinutes"),
	})
	api.query = func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
		return []map[string]any{{"count": 1.0, "dimensions": map[string]any{"datetimeFiveMinutes": testFrom}}}, nil
	}
	cfg := config.Default()
	cfg.Cloudflare.AccountID = testAccountID
	cfg.Platform.MaxMetricSeriesPerWindow = 1
	window := registeredCollector(t, &cfg, api, "email.routing")
	emitter := &emailTestEmitter{}
	from := fixedTime(t, testFrom)
	if _, err := window.CollectWindow(context.Background(), from, from.Add(5*time.Minute), emitter); err != nil {
		t.Fatalf("CollectWindow at one-series cap: %v", err)
	}
	if len(emitter.metrics) != 1 || len(emitter.metrics[0].attrs) != 0 {
		t.Fatalf("series cap emitted non-aggregate points: %+v", emitter.metrics)
	}

	cfg.Platform.MaxMetricSeriesPerWindow = 0
	emitter = &emailTestEmitter{}
	window = registeredCollector(t, &cfg, api, "email.routing")
	highWater, err := window.CollectWindow(context.Background(), from, from.Add(5*time.Minute), emitter)
	if err == nil || !highWater.Equal(from) || len(emitter.metrics) != 0 {
		t.Errorf("zero series cap was ignored: high-water=%s err=%v metrics=%v", highWater, err, emitter.metrics)
	}
}

func TestRetentionAndMaximumDurationSettingsAreRespected(t *testing.T) {
	zone := testZone("zone-00000000000000000000000000000001", testAccountID)
	settings := datasetSettings(true, "count", "dimensions_datetimeFiveMinutes")
	settings.MaxDuration = 600
	settings.NotOlderThan = 3600
	api := newEmailTestAPI("emailSendingAdaptiveGroups", []cfapi.Zone{zone}, map[string]cfapi.DatasetSettings{zone.ID: settings})
	api.query = func(request cfapi.GraphQLRequest) ([]map[string]any, error) {
		if request.To.Sub(request.From) > 10*time.Minute {
			return nil, errors.New("request exceeded maxDuration")
		}
		var rows []map[string]any
		for bucket := request.From; bucket.Before(request.To); bucket = bucket.Add(5 * time.Minute) {
			rows = append(rows, map[string]any{"count": 1.0, "dimensions": map[string]any{"datetimeFiveMinutes": bucket.Format(time.RFC3339)}})
		}
		return rows, nil
	}
	cfg := config.Default()
	cfg.Cloudflare.AccountID = testAccountID
	window := registeredCollector(t, &cfg, api, "email.sending")
	emitter := &emailTestEmitter{}
	from := time.Now().UTC().Truncate(5 * time.Minute).Add(-20 * time.Minute)
	to := from.Add(20 * time.Minute)
	highWater, err := window.CollectWindow(context.Background(), from, to, emitter)
	if err != nil {
		t.Fatalf("CollectWindow respecting maxDuration: %v", err)
	}
	if !highWater.Equal(to) || len(api.queries) != 2 || len(emitter.metrics) != 1 || emitter.metrics[0].value != 4 {
		t.Errorf("maxDuration windows = high-water %s, queries %d, metrics %+v", highWater, len(api.queries), emitter.metrics)
	}
	for _, request := range api.queries {
		if request.To.Sub(request.From) > 10*time.Minute {
			t.Errorf("request exceeded maxDuration: %s", request.To.Sub(request.From))
		}
	}

	staleFrom := time.Now().UTC().Truncate(5 * time.Minute).Add(-2 * time.Hour)
	staleTo := staleFrom.Add(5 * time.Minute)
	api.queries = nil
	emitter = &emailTestEmitter{}
	highWater, err = window.CollectWindow(context.Background(), staleFrom, staleTo, emitter)
	if err == nil || !highWater.Equal(staleFrom) || len(api.queries) != 0 || len(emitter.metrics) != 0 {
		t.Errorf("retention floor was ignored: high-water=%s err=%v queries=%v metrics=%v", highWater, err, api.queries, emitter.metrics)
	}
	var gap *emailRetentionError
	if !errors.As(err, &gap) || gap.dataset != "emailSendingAdaptiveGroups" || !gap.floor.After(staleFrom) {
		t.Errorf("retention failure must identify the email dataset and floor: gap=%v err=%v", gap, err)
	}
	var schedulerGap *cfapi.RetentionGapError
	if errors.As(err, &schedulerGap) {
		t.Errorf("email retention failure must not be eligible for scheduler skip: %v", schedulerGap)
	}
}
