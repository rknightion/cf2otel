package dex

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type testsCollector struct {
	api             cfapi.Client
	account         string
	window          time.Duration
	interval        time.Duration
	maxTests, limit int
	mu              sync.Mutex
	admitted        map[series]bool
}

func (*testsCollector) Name() string                   { return semconv.CollectorNameDEXTests }
func (*testsCollector) DefaultInterval() time.Duration { return 5 * time.Minute }

type testRow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}
type average struct {
	Avg *float64 `json:"avg"`
}
type httpStats struct {
	Fetch        *average `json:"resourceFetchTimeMs"`
	Availability *average `json:"availabilityPct"`
}
type traceStats struct {
	RTT          *average `json:"roundTripTimeMs"`
	Hops         *average `json:"hopsCount"`
	Loss         *average `json:"packetLossPct"`
	Availability *average `json:"availabilityPct"`
}
type series struct{ metric, name, kind string }
type measurement struct {
	key   series
	value float64
}

var safeName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 _-]{0,127}$`)

func (c *testsCollector) catalog(ctx context.Context) ([]testRow, error) {
	var all []testRow
	ids := map[string]bool{}
	names := map[[2]string]bool{}
	path := "/accounts/" + url.PathEscape(c.account) + "/dex/tests/overview"
	// One extra page proves an exactly-full bound ended, rather than truncating.
	for page := 1; page <= c.maxTests/50+1; page++ {
		var result struct {
			Tests []testRow `json:"tests"`
		}
		if err := c.api.Get(ctx, path, url.Values{"page": {strconv.Itoa(page)}, "per_page": {"50"}}, &result); err != nil {
			return nil, fmt.Errorf("DEX catalog page %d read failed", page)
		}
		if result.Tests == nil || len(result.Tests) > 50 {
			return nil, fmt.Errorf("DEX catalog page %d invalid tests array", page)
		}
		for _, row := range result.Tests {
			if row.ID == "" || strings.TrimSpace(row.Name) == "" || (row.Kind != "http" && row.Kind != "traceroute") {
				return nil, fmt.Errorf("DEX catalog invalid test identity or kind")
			}
			if ids[row.ID] {
				return nil, fmt.Errorf("DEX catalog repeated test or page")
			}
			key := [2]string{row.Name, row.Kind}
			if names[key] {
				return nil, fmt.Errorf("DEX catalog ambiguous test name and kind")
			}
			ids[row.ID] = true
			names[key] = true
			if len(all) >= c.maxTests {
				return nil, fmt.Errorf("DEX catalog exceeds max_tests")
			}
			all = append(all, row)
		}
		if len(result.Tests) < 50 {
			return all, nil
		}
	}
	return nil, fmt.Errorf("DEX catalog pagination bound exhausted")
}

func (c *testsCollector) results(ctx context.Context, row testRow, q url.Values) ([]measurement, error) {
	var metrics []measurement
	add := func(metric string, stat *average) error {
		if stat == nil || stat.Avg == nil {
			return nil
		}
		value := *stat.Avg
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || ((metric == semconv.MetricDEXPacketLoss || metric == semconv.MetricDEXAvailability) && value > 100) {
			return fmt.Errorf("DEX invalid provider average")
		}
		metrics = append(metrics, measurement{series{metric, row.Name, row.Kind}, value})
		return nil
	}
	account := url.PathEscape(c.account)
	test := url.PathEscape(row.ID)
	if row.Kind == "http" {
		var result struct {
			Stats *httpStats `json:"httpStats"`
		}
		if err := c.api.Get(ctx, "/accounts/"+account+"/dex/http-tests/"+test, q, &result); err != nil {
			return nil, fmt.Errorf("DEX HTTP result read failed")
		}
		if result.Stats == nil {
			return nil, fmt.Errorf("DEX HTTP result missing stats")
		}
		if err := add(semconv.MetricDEXHTTPFetchTime, result.Stats.Fetch); err != nil {
			return nil, err
		}
		if err := add(semconv.MetricDEXAvailability, result.Stats.Availability); err != nil {
			return nil, err
		}
	} else {
		var result struct {
			Stats *traceStats `json:"tracerouteStats"`
		}
		if err := c.api.Get(ctx, "/accounts/"+account+"/dex/traceroute-tests/"+test, q, &result); err != nil {
			return nil, fmt.Errorf("DEX traceroute result read failed")
		}
		if result.Stats == nil {
			return nil, fmt.Errorf("DEX traceroute result missing stats")
		}
		for _, item := range []struct {
			metric string
			stat   *average
		}{{semconv.MetricDEXTracerouteRTT, result.Stats.RTT}, {semconv.MetricDEXTracerouteHops, result.Stats.Hops}, {semconv.MetricDEXPacketLoss, result.Stats.Loss}, {semconv.MetricDEXAvailability, result.Stats.Availability}} {
			if err := add(item.metric, item.stat); err != nil {
				return nil, err
			}
		}
	}
	return metrics, nil
}

func (c *testsCollector) Collect(ctx context.Context, e telemetry.Emitter) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	batch, ok := e.(telemetry.SnapshotBatchEmitter)
	if !ok {
		return fmt.Errorf("DEX requires expiring snapshot batch emitter")
	}
	if c.api == nil || c.account == "" || c.window < time.Hour || c.window > 168*time.Hour || c.maxTests < 1 || c.maxTests > 10000 || c.limit < 6 || c.limit > 5000 || c.interval <= 0 {
		return fmt.Errorf("invalid DEX snapshot configuration")
	}
	rows, err := c.catalog(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	q := url.Values{"from": {now.Add(-c.window).Format("2006-01-02T15:04:05.000Z")}, "to": {now.Format("2006-01-02T15:04:05.000Z")}, "interval": {"minute"}}
	var values []measurement
	successes, failed := 0, 0
	for _, row := range rows {
		result, err := c.results(ctx, row, q)
		if err != nil {
			failed++
			continue
		}
		successes++
		values = append(values, result...)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(rows) > 0 && successes == 0 {
		return fmt.Errorf("DEX all %d test result requests failed", failed)
	}
	sort.Slice(values, func(i, j int) bool {
		a, b := values[i].key, values[j].key
		if a.name != b.name {
			return a.name < b.name
		}
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		return a.metric < b.metric
	})
	// Sticky admission bounds named point identities across polls until restart.
	// Reserve two HTTP plus four traceroute remainder identities in the total cap.
	admitted := make(map[series]bool, len(c.admitted))
	for key := range c.admitted {
		admitted[key] = true
	}
	snapshots := map[string][]telemetry.GaugePoint{}
	for _, name := range []string{semconv.MetricDEXHTTPFetchTime, semconv.MetricDEXTracerouteRTT, semconv.MetricDEXTracerouteHops, semconv.MetricDEXPacketLoss, semconv.MetricDEXAvailability} {
		snapshots[name] = nil
	}
	type mean struct {
		value float64
		count int
	}
	remainders := map[series]mean{}
	for _, m := range values {
		named := safeName.MatchString(m.key.name) && m.key.name != "other"
		if named && !admitted[m.key] && len(admitted) < c.limit-6 {
			admitted[m.key] = true
		}
		if named && admitted[m.key] {
			snapshots[m.key.metric] = append(snapshots[m.key.metric], point(m.key, m.value))
			continue
		}
		key := series{m.key.metric, "other", m.key.kind}
		a := remainders[key]
		a.count++
		// Incremental arithmetic mean avoids overflowing a sum of finite averages.
		a.value = a.value*(float64(a.count-1)/float64(a.count)) + m.value/float64(a.count)
		remainders[key] = a
	}
	for key, a := range remainders {
		snapshots[key.metric] = append(snapshots[key.metric], point(key, a.value))
	}
	if err := batch.GaugeSnapshots(ctx, 3*c.interval, snapshots); err != nil {
		return err
	}
	c.admitted = admitted
	if failed > 0 {
		return fmt.Errorf("DEX %d of %d test result requests failed", failed, len(rows))
	}
	return nil
}
func point(key series, value float64) telemetry.GaugePoint {
	return telemetry.GaugePoint{Value: value, Attrs: []telemetry.Attr{{Key: semconv.AttrDEXTestName, Value: key.name}, {Key: semconv.AttrDEXTestKind, Value: key.kind}}}
}
