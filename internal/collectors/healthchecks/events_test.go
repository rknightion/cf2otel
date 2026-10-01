package healthchecks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

// All fixtures are invented. HTTPClient is real; only its network edge is replaced.
type fixture struct {
	enabled                bool
	fields                 []string
	budget, page, duration int
	countRows, timingRows  []map[string]any
	failTiming             bool
	nullCount, nullTiming  bool
	queries                []string
}

func newFixture() *fixture {
	return &fixture{enabled: true, budget: 10, page: 10000, duration: 3600, fields: []string{"count", "dimensions_datetimeFiveMinutes", "dimensions_healthStatus", "dimensions_failureReason", "dimensions_fqdn", "dimensions_healthCheckName", "avg_rttMs", "avg_timeToFirstByteMs", "avg_tcpConnMs", "avg_tlsHandshakeMs"}}
}
func (f *fixture) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/zones" {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []any{map[string]any{"id": "zone-fixture", "name": "example.com", "account": map[string]any{"id": "account-fixture"}}}})
			return
		}
		if r.Method != "POST" || r.URL.Path != "/graphql" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(400)
			return
		}
		var payload struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		q := payload.Query
		if strings.Contains(q, "originIP") {
			t.Error("originIP selected")
		}
		node := map[string]any{}
		if strings.Contains(q, "settings{") {
			node["settings"] = map[string]any{"healthCheckEventsAdaptiveGroups": map[string]any{"enabled": f.enabled, "availableFields": f.fields, "maxNumberOfFields": f.budget, "maxDuration": f.duration, "notOlderThan": 267840000, "maxPageSize": f.page}}
		} else {
			f.queries = append(f.queries, q)
			if strings.Contains(q, "avg{") {
				if f.failTiming {
					_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": "synthetic failure"}}})
					return
				}
				if strings.Contains(q, "healthStatus") || strings.Contains(q, "failureReason") {
					t.Error("timing was grouped by event status/reason")
				}
				node["healthCheckEventsAdaptiveGroups"] = rowsForQuery(f.timingRows, q)
			} else {
				node["healthCheckEventsAdaptiveGroups"] = rowsForQuery(f.countRows, q)
			}
			if strings.Contains(q, "avg{") && f.nullTiming || !strings.Contains(q, "avg{") && f.nullCount {
				node["healthCheckEventsAdaptiveGroups"] = nil
			}
			// A batch alias names the same source dataset, not a different fixture.
			if strings.Contains(q, "healthcheck:"+dataset) {
				node["healthcheck"] = node["healthCheckEventsAdaptiveGroups"]
				delete(node, "healthCheckEventsAdaptiveGroups")
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"zones": []any{node}}}})
	}))
}

var start = time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)

func eventRow(count any, status, reason string) map[string]any {
	return map[string]any{"count": count, "dimensions": map[string]any{"datetimeFiveMinutes": start.Format(time.RFC3339), "healthStatus": status, "failureReason": reason}}
}
func timingRow(at time.Time, fqdn, name string, avg map[string]any) map[string]any {
	return map[string]any{"dimensions": map[string]any{"datetimeFiveMinutes": at.Format(time.RFC3339), "fqdn": fqdn, "healthCheckName": name}, "avg": avg}
}
func collectFixture(t *testing.T, f *fixture, change func(*config.Config), to time.Time) (*telemetry.Buffer, time.Time, error) {
	t.Helper()
	s := f.server(t)
	defer s.Close()
	cfg := config.Default()
	cfg.Cloudflare.APIBase = s.URL
	cfg.Cloudflare.AccountID = "account-fixture"
	entry := cfg.Collectors["healthchecks.events"]
	entry.Enabled = true
	cfg.Collectors["healthchecks.events"] = entry
	if change != nil {
		change(&cfg)
	}
	reg := collector.NewRegistry()
	Register(collector.Deps{Config: &cfg, API: cfapi.New(cfg.Cloudflare), Registry: reg})
	if len(reg.Entries()) != 1 {
		t.Fatalf("healthchecks.events was not registered: got %d entries", len(reg.Entries()))
	}
	c, ok := reg.Entries()[0].Collector.(collector.WindowCollector)
	if !ok {
		t.Fatal("healthchecks.events is not windowed")
	}
	out := &telemetry.Buffer{}
	mark, err := c.CollectWindow(context.Background(), start, to, out)
	// This fixture returns domain metrics for the existing timing/count tests.
	// Retain all output on failures so no-partial-output assertions remain strict.
	if err == nil {
		data := make([]telemetry.BufferedMetric, 0, len(out.Metrics))
		for _, m := range out.Metrics {
			switch m.Name {
			case semconv.MetricZonesDiscovered, semconv.MetricZonesFiltered, semconv.MetricZonesProcessed, semconv.MetricZonesSkipped:
				continue
			}
			data = append(data, m)
		}
		out.Metrics = data
	}
	return out, mark, err
}
func attr(m telemetry.BufferedMetric, key string) string {
	for _, a := range m.Attrs {
		if a.Key == key {
			return a.Value
		}
	}
	return ""
}
func TestRegisteredHealthCheckCountsAndSeconds(t *testing.T) {
	f := newFixture()
	f.countRows = []map[string]any{eventRow(7, "healthy", ""), eventRow(3, "unhealthy", "timeout")}
	f.timingRows = []map[string]any{timingRow(start, "origin.example.com", "Primary", map[string]any{"rttMs": 2500, "timeToFirstByteMs": 500, "tcpConnMs": 0, "tlsHandshakeMs": 125})}
	out, mark, err := collectFixture(t, f, nil, start.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(start.Add(5*time.Minute)) || len(out.Metrics) != 6 {
		t.Fatalf("mark=%s metrics=%+v", mark, out.Metrics)
	}
	counts := map[string]float64{}
	times := map[string]float64{}
	for _, m := range out.Metrics {
		if attr(m, semconv.AttrHealthCheckZone) != "example.com" {
			t.Fatalf("zone name missing: %+v", m)
		}
		if m.Kind == "counter" {
			counts[attr(m, semconv.AttrHealthCheckStatus)+":"+attr(m, semconv.AttrHealthCheckFailureReason)] = m.Value
		} else {
			if attr(m, semconv.AttrHealthCheckOrigin) != "origin.example.com" || len(m.Attrs) != 2 {
				t.Fatalf("unsafe timing attributes: %+v", m)
			}
			times[m.Name] = m.Value
		}
	}
	if counts["healthy:none"] != 7 || counts["unhealthy:timeout"] != 3 {
		t.Fatalf("counts=%v", counts)
	}
	for name, value := range map[string]float64{semconv.MetricHealthCheckRTT: 2.5, semconv.MetricHealthCheckTTFB: 0.5, semconv.MetricHealthCheckTCPConnection: 0, semconv.MetricHealthCheckTLSHandshake: 0.125} {
		got, ok := times[name]
		if !ok || got != value {
			t.Errorf("%s = %g, present=%v, want %g", name, got, ok, value)
		}
	}
	if len(f.queries) != 2 {
		t.Fatalf("expected separate count and timing queries, got %d", len(f.queries))
	}
}

// Keep complete-bucket rows at the real HTTP query boundary, not in the collector.
func rowsForQuery(rows []map[string]any, q string) []map[string]any {
	out := []map[string]any{}
	for _, row := range rows {
		dims, _ := row["dimensions"].(map[string]any)
		at, _ := dims["datetimeFiveMinutes"].(string)
		if at == "" || strings.Contains(q, `datetime_geq:"`+at+`"`) {
			out = append(out, row)
		}
	}
	return out
}
func TestDefaultDisabledAndFreeDisabled(t *testing.T) {
	cfg := config.Default()
	reg := collector.NewRegistry()
	Register(collector.Deps{Config: &cfg, Registry: reg})
	if len(reg.Entries()) != 0 {
		t.Fatal("default-disabled health checks registered")
	}
	f := newFixture()
	f.enabled = false
	out, mark, err := collectFixture(t, f, nil, start.Add(5*time.Minute))
	if err != nil || !mark.Equal(start.Add(5*time.Minute)) || len(out.Metrics) != 0 || len(f.queries) != 0 {
		t.Fatalf("disabled dataset queried or emitted: mark=%s err=%v metrics=%v queries=%v", mark, err, out.Metrics, f.queries)
	}
}
func TestEmptyAndUnusableOriginsKeepCountsWithoutFabricatingTimings(t *testing.T) {
	for _, tc := range []struct {
		name      string
		rows      []map[string]any
		wantCount bool
	}{
		{name: "empty"},
		{name: "anonymous", wantCount: true, rows: []map[string]any{timingRow(start, "", "", map[string]any{"rttMs": 100}), timingRow(start, "", "", map[string]any{"rttMs": 200})}},
		{name: "IP literal", wantCount: true, rows: []map[string]any{timingRow(start, "192.0.2.1", "192.0.2.2", map[string]any{"rttMs": 100})}},
		{name: "IP port", wantCount: true, rows: []map[string]any{timingRow(start, "", "192.0.2.1:443", map[string]any{"rttMs": 100})}},
		{name: "IP trailing dot", wantCount: true, rows: []map[string]any{timingRow(start, "", "192.0.2.1.", map[string]any{"rttMs": 100})}},
		{name: "IPv6 literal", wantCount: true, rows: []map[string]any{timingRow(start, "2001:db8::1", "[2001:db8::2]", map[string]any{"rttMs": 100})}},
		{name: "opaque identifier", wantCount: true, rows: []map[string]any{timingRow(start, "", "01234567-89ab-cdef-0123-456789abcdef", map[string]any{"rttMs": 100})}},
		{name: "overlong", wantCount: true, rows: []map[string]any{timingRow(start, "", strings.Repeat("a", 129), map[string]any{"rttMs": 100})}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture()
			f.timingRows = tc.rows
			if tc.wantCount {
				f.countRows = []map[string]any{eventRow(9, "healthy", "")}
			}
			out, _, err := collectFixture(t, f, nil, start.Add(5*time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if tc.wantCount {
				want = 1
			}
			if len(out.Metrics) != want || want == 1 && (out.Metrics[0].Kind != "counter" || out.Metrics[0].Value != 9) {
				t.Fatalf("unexpected output %+v", out.Metrics)
			}
		})
	}
}
func TestTimingIdentityFallbackAndMissingNotApplicable(t *testing.T) {
	f := newFixture()
	f.timingRows = []map[string]any{
		timingRow(start, "192.0.2.1", "Primary probe", map[string]any{"rttMs": 0, "timeToFirstByteMs": nil, "tcpConnMs": -1, "tlsHandshakeMs": "N/A"}),
		timingRow(start, "Origin.Example.COM.", "Ignored", map[string]any{"rttMs": 1000}),
	}
	out, _, err := collectFixture(t, f, nil, start.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Metrics) != 2 {
		t.Fatalf("missing/N/A manufactured values: %+v", out.Metrics)
	}
	origins := map[string]float64{}
	for _, m := range out.Metrics {
		if m.Name != semconv.MetricHealthCheckRTT {
			t.Fatalf("unexpected timing %+v", m)
		}
		origins[attr(m, semconv.AttrHealthCheckOrigin)] = m.Value
	}
	zero, ok := origins["Primary probe"]
	if !ok || zero != 0 || origins["origin.example.com"] != 1 {
		t.Fatalf("origins=%v", origins)
	}
}
func TestLatestCompleteBucketPerOriginNotAverageAcrossStatuses(t *testing.T) {
	f := newFixture()
	f.duration = 300
	f.budget = 7
	early := eventRow(2, "healthy", "")
	late := eventRow(8, "healthy", "")
	late["dimensions"].(map[string]any)["datetimeFiveMinutes"] = start.Add(5 * time.Minute).Format(time.RFC3339)
	f.countRows = []map[string]any{early, late}
	f.timingRows = []map[string]any{
		timingRow(start, "first.example.com", "First", map[string]any{"rttMs": 100}),
		timingRow(start.Add(5*time.Minute), "first.example.com", "First", map[string]any{"rttMs": 900}),
		timingRow(start, "second.example.com", "Second", map[string]any{"rttMs": 300}),
		timingRow(start, "third.example.com", "Third", map[string]any{"rttMs": 700}),
		timingRow(start.Add(5*time.Minute), "third.example.com", "Third", map[string]any{"rttMs": nil}),
	}
	out, mark, err := collectFixture(t, f, nil, start.Add(12*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(start.Add(10*time.Minute)) || len(out.Metrics) != 3 {
		t.Fatalf("mark=%s metrics=%+v", mark, out.Metrics)
	}
	for _, m := range out.Metrics {
		if m.Kind == "counter" {
			if m.Value != 10 {
				t.Errorf("count=%g", m.Value)
			}
			continue
		}
		switch attr(m, semconv.AttrHealthCheckOrigin) {
		case "first.example.com":
			if m.Value != 0.9 {
				t.Errorf("latest timing=%g", m.Value)
			}
		case "second.example.com":
			if m.Value != 0.3 {
				t.Errorf("independent origin=%g", m.Value)
			}
		default:
			t.Fatalf("stale or anonymous timing %+v", m)
		}
	}
	if len(f.queries) != 4 {
		t.Fatalf("queries=%v", f.queries)
	}
	for _, q := range f.queries {
		if strings.Contains(q, "datetime_lt:"+`"`+start.Add(12*time.Minute).Format(time.RFC3339)+`"`) {
			t.Error("partial bucket queried")
		}
	}
}
func TestFailuresBeforeAnyEmission(t *testing.T) {
	for _, name := range []string{"query failure", "missing count", "missing status", "missing reason", "fractional count", "negative count", "missing timestamp", "duplicate origin", "budget", "missing entitlement", "short duration", "page saturation"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			f.countRows = []map[string]any{eventRow(4, "healthy", "")}
			f.timingRows = []map[string]any{timingRow(start, "origin.example.com", "Primary", map[string]any{"rttMs": 100})}
			switch name {
			case "query failure":
				f.failTiming = true
			case "missing count":
				delete(f.countRows[0], "count")
			case "missing status":
				delete(f.countRows[0]["dimensions"].(map[string]any), "healthStatus")
			case "missing reason":
				delete(f.countRows[0]["dimensions"].(map[string]any), "failureReason")
			case "fractional count":
				f.countRows[0]["count"] = 1.5
			case "negative count":
				f.countRows[0]["count"] = -1
			case "missing timestamp":
				delete(f.countRows[0]["dimensions"].(map[string]any), "datetimeFiveMinutes")
			case "duplicate origin":
				f.timingRows = append(f.timingRows, timingRow(start, "origin.example.com", "Other check", map[string]any{"rttMs": 500}))
			case "budget":
				f.budget = 6
			case "missing entitlement":
				f.fields = f.fields[:len(f.fields)-1]
			case "short duration":
				f.duration = 299
			case "page saturation":
				f.page = 1
			}
			out, mark, err := collectFixture(t, f, nil, start.Add(5*time.Minute))
			if err == nil || !mark.Equal(start) || len(out.Metrics) != 0 {
				t.Fatalf("failure emitted partial/zero values: mark=%s err=%v metrics=%+v", mark, err, out.Metrics)
			}
			if (name == "budget" || name == "missing entitlement" || name == "short duration") && len(f.queries) != 0 {
				t.Fatalf("unsafe settings queried: %v", f.queries)
			}
		})
	}
}
func TestRawDatasetAndUnsignedCountFailures(t *testing.T) {
	for _, name := range []string{"null count", "null timing", "both null", "overflow", "uint overflow", "rounded fraction", "missing", "null value", "negative", "fraction", "string"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			f.countRows = []map[string]any{eventRow(4, "healthy", "")}
			f.timingRows = []map[string]any{timingRow(start, "origin.example.com", "Primary", map[string]any{"rttMs": 100})}
			switch name {
			case "null count":
				f.nullCount = true
			case "null timing":
				f.nullTiming = true
			case "both null":
				f.nullCount, f.nullTiming = true, true
			case "overflow":
				f.countRows[0]["count"] = json.Number("1e20")
			case "uint overflow":
				f.countRows[0]["count"] = json.Number("18446744073709551616")
			case "rounded fraction":
				f.countRows[0]["count"] = json.Number("1.00000000000000001")
			case "missing":
				delete(f.countRows[0], "count")
			case "null value":
				f.countRows[0]["count"] = nil
			case "negative":
				f.countRows[0]["count"] = -1
			case "fraction":
				f.countRows[0]["count"] = json.Number("1.5")
			case "string":
				f.countRows[0]["count"] = "4"
			}
			out, mark, err := collectFixture(t, f, nil, start.Add(5*time.Minute))
			if err == nil || !mark.Equal(start) || len(out.Metrics) != 0 {
				t.Fatalf("invalid source advanced/emitted: mark=%s err=%v metrics=%+v", mark, err, out.Metrics)
			}
		})
	}
}

func TestSchedulerRejectsRawInvalidSource(t *testing.T) {
	for _, name := range []string{"null count", "null timing", "overflow", "rounded fraction"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			f.countRows = []map[string]any{eventRow(4, "healthy", "")}
			f.timingRows = []map[string]any{timingRow(start, "origin.example.com", "Primary", map[string]any{"rttMs": 100})}
			switch name {
			case "null count":
				f.nullCount = true
			case "null timing":
				f.nullTiming = true
			case "overflow":
				f.countRows[0]["count"] = json.Number("1e20")
			case "rounded fraction":
				f.countRows[0]["count"] = json.Number("1.00000000000000001")
			}
			s := f.server(t)
			defer s.Close()
			cfg := config.Default()
			cfg.Cloudflare.APIBase, cfg.Cloudflare.AccountID = s.URL, "account-fixture"
			entry := cfg.Collectors["healthchecks.events"]
			entry.Enabled = true
			cfg.Collectors["healthchecks.events"] = entry
			reg := collector.NewRegistry()
			Register(collector.Deps{Config: &cfg, API: cfapi.New(cfg.Cloudflare), Registry: reg})
			c := reg.Entries()[0].Collector.(collector.WindowCollector)
			out := &telemetry.Buffer{}
			scheduler := collector.NewScheduler(reg, out, nil)
			if err := scheduler.CollectRange(context.Background(), c, start, start.Add(5*time.Minute)); err == nil || len(out.Metrics) != 0 {
				t.Fatalf("scheduler exported invalid source: err=%v metrics=%+v", err, out.Metrics)
			}
		})
	}
}

func TestRawDatasetEmptyAndUnsignedControls(t *testing.T) {
	for _, value := range []string{"empty", "0", "7", "18446744073709551615"} {
		t.Run(value, func(t *testing.T) {
			f := newFixture()
			if value != "empty" {
				f.countRows = []map[string]any{eventRow(json.Number(value), "healthy", "")}
			}
			out, mark, err := collectFixture(t, f, nil, start.Add(5*time.Minute))
			if err != nil || !mark.Equal(start.Add(5*time.Minute)) {
				t.Fatalf("valid source failed: mark=%s err=%v", mark, err)
			}
			if value == "empty" {
				if len(out.Metrics) != 0 {
					t.Fatalf("empty fabricated output: %+v", out.Metrics)
				}
			} else {
				want, _ := json.Number(value).Float64()
				if len(out.Metrics) != 1 || out.Metrics[0].Value != want {
					t.Fatalf("valid count lost: %+v", out.Metrics)
				}
			}
		})
	}
}

func TestMetricSeriesCapAndZoneSelection(t *testing.T) {
	f := newFixture()
	f.countRows = []map[string]any{eventRow(4, "healthy", ""), eventRow(1, "unhealthy", "timeout")}
	f.timingRows = []map[string]any{timingRow(start, "origin.example.com", "Primary", map[string]any{"rttMs": 100})}
	out, _, err := collectFixture(t, f, func(cfg *config.Config) { cfg.Platform.MaxMetricSeriesPerWindow = 1 }, start.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Metrics) != 1 {
		t.Fatalf("series cap not applied: %+v", out.Metrics)
	}
	f = newFixture()
	out, _, err = collectFixture(t, f, func(cfg *config.Config) { cfg.Cloudflare.Zones = []string{"other.example.com"} }, start.Add(5*time.Minute))
	if err != nil || len(out.Metrics) != 0 || len(f.queries) != 0 {
		t.Fatalf("unselected zone queried/emitted: err=%v metrics=%v queries=%v", err, out.Metrics, f.queries)
	}
}
