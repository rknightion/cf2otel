package healthchecks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

const dataset = "healthCheckEventsAdaptiveGroups"
const bucketSize = 5 * time.Minute

var countFields = []string{"count", "dimensions.datetimeFiveMinutes", "dimensions.healthStatus", "dimensions.failureReason"}
var timingFields = []string{"dimensions.datetimeFiveMinutes", "dimensions.fqdn", "dimensions.healthCheckName", "avg.rttMs", "avg.timeToFirstByteMs", "avg.tcpConnMs", "avg.tlsHandshakeMs"}
var timings = []struct{ field, metric string }{
	{"rttMs", semconv.MetricHealthCheckRTT}, {"timeToFirstByteMs", semconv.MetricHealthCheckTTFB},
	{"tcpConnMs", semconv.MetricHealthCheckTCPConnection}, {"tlsHandshakeMs", semconv.MetricHealthCheckTLSHandshake},
}

type settingsReader interface {
	DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error)
}
type events struct {
	cfg *config.Config
	api cfapi.Client
}

func (*events) Name() string                   { return "healthchecks.events" }
func (*events) DefaultInterval() time.Duration { return bucketSize }
func (*events) Lag() time.Duration             { return 10 * time.Minute }

type eventKey struct{ zone, status, reason string }
type originKey struct{ zone, origin string }
type observation struct {
	at     time.Time
	values map[string]any
}

func (c *events) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	end := to.UTC().Truncate(bucketSize)
	begin := from.UTC().Truncate(bucketSize)
	if begin.Before(from) {
		begin = begin.Add(bucketSize)
	}
	if !begin.Before(end) {
		return from, errors.New("health-check window contains no complete five-minute bucket")
	}
	if c.cfg == nil || c.api == nil || c.cfg.Cloudflare.AccountID == "" {
		return from, errors.New("health-check analytics requires an account and API client")
	}
	cap := c.cfg.Platform.MaxMetricSeriesPerWindow
	if cap <= 0 {
		return from, errors.New("health-check analytics requires a positive metric series cap")
	}
	reader, ok := c.api.(settingsReader)
	if !ok {
		return from, errors.New("health-check client does not expose dataset settings")
	}
	zones, err := c.api.Zones(ctx)
	if err != nil {
		return from, fmt.Errorf("discover health-check zones: %w", err)
	}
	counts := map[eventKey]float64{}
	latest := map[originKey]observation{}
	now := time.Now()
	seenZones := map[string]bool{}
	for _, zone := range zones {
		if zone.Account.ID != c.cfg.Cloudflare.AccountID || !selectedZone(zone, c.cfg.Cloudflare.Zones) {
			continue
		}
		if zone.ID == "" || zone.Name == "" {
			return from, errors.New("health-check zone discovery returned an incomplete zone")
		}
		if seenZones[zone.ID] {
			continue
		}
		seenZones[zone.ID] = true
		settings, err := reader.DatasetSettings(ctx, cfapi.ZoneScope, zone.ID, dataset)
		if err != nil {
			return from, fmt.Errorf("read health-check settings: %w", err)
		}
		// Disabled datasets (including Free zones) are expected, not query errors.
		if !settings.Enabled {
			continue
		}
		if err := validateSettings(settings); err != nil {
			return from, err
		}
		if begin.Before(now.Add(-time.Duration(settings.NotOlderThan) * time.Second)) {
			return from, &cfapi.RetentionGapError{Dataset: dataset, Floor: now.Add(-time.Duration(settings.NotOlderThan) * time.Second)}
		}
		limit := min(10000, settings.MaxPageSize)
		// Query exactly one complete bucket at a time. Splitting a bucket at a
		// duration/page boundary would make its source averages non-composable.
		for at := begin; at.Before(end); at = at.Add(bucketSize) {
			countRows, err := c.query(ctx, zone.ID, countFields, at, limit)
			if err != nil {
				return from, err
			}
			for _, row := range countRows {
				dims, err := rowDimensions(row, at)
				if err != nil {
					return from, err
				}
				status, ok := dims["healthStatus"].(string)
				if !ok || strings.TrimSpace(status) == "" {
					return from, errors.New("health-check group has no health status")
				}
				reason, ok := dims["failureReason"].(string)
				if !ok {
					return from, errors.New("health-check group has no failure reason")
				}
				reason = bounded(reason, 128)
				if reason == "" {
					reason = "none"
				}
				n, ok := number(row["count"])
				if !ok || math.Trunc(n) != n {
					return from, errors.New("health-check group has an invalid event count")
				}
				key := eventKey{zone.Name, bounded(status, 128), reason}
				counts[key] += n
				if math.IsInf(counts[key], 0) {
					return from, errors.New("health-check event count overflow")
				}
			}
			timingRows, err := c.query(ctx, zone.ID, timingFields, at, limit)
			if err != nil {
				return from, err
			}
			seen := map[originKey]bool{}
			for _, row := range timingRows {
				dims, err := rowDimensions(row, at)
				if err != nil {
					return from, err
				}
				origin := originIdentity(dims)
				if origin == "" {
					continue
				} // Never merge unrelated unnamed origins.
				key := originKey{zone.Name, origin}
				if seen[key] {
					return from, errors.New("health-check timing has ambiguous duplicate origin bucket")
				}
				seen[key] = true
				avg, _ := row["avg"].(map[string]any)
				// The most recent complete observation replaces the earlier bucket,
				// even when its optional timings are absent or N/A.
				latest[key] = observation{at: at, values: avg}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return from, err
	}
	series := make([]telemetry.BufferedMetric, 0, len(counts)+len(latest)*len(timings))
	for key, n := range counts {
		series = append(series, telemetry.BufferedMetric{Kind: "counter", Name: semconv.MetricHealthCheckEvents, Value: n, Attrs: []telemetry.Attr{{Key: semconv.AttrHealthCheckZone, Value: key.zone}, {Key: semconv.AttrHealthCheckStatus, Value: key.status}, {Key: semconv.AttrHealthCheckFailureReason, Value: key.reason}}})
	}
	for key, obs := range latest {
		for _, timing := range timings {
			if value, ok := number(obs.values[timing.field]); ok {
				series = append(series, telemetry.BufferedMetric{Kind: "gauge", Name: timing.metric, Value: value / 1000, Attrs: []telemetry.Attr{{Key: semconv.AttrHealthCheckZone, Value: key.zone}, {Key: semconv.AttrHealthCheckOrigin, Value: key.origin}}})
			}
		}
	}
	sort.Slice(series, func(i, j int) bool {
		if series[i].Name != series[j].Name {
			return series[i].Name < series[j].Name
		}
		for k := range series[i].Attrs {
			if series[i].Attrs[k].Value != series[j].Attrs[k].Value {
				return series[i].Attrs[k].Value < series[j].Attrs[k].Value
			}
		}
		return false
	})
	if len(series) > cap {
		slog.WarnContext(ctx, "platform metric series dropped", "collector", c.Name(), "dropped_series", len(series)-cap)
		series = series[:cap]
	}
	for _, metric := range series {
		if err := metric.Replay(ctx, out); err != nil {
			return from, err
		}
	}
	return end, nil
}
func selectedZone(zone cfapi.Zone, selectors []string) bool {
	if len(selectors) == 0 {
		return true
	}
	for _, selector := range selectors {
		if selector == zone.ID || strings.EqualFold(selector, zone.Name) {
			return true
		}
	}
	return false
}
func validateSettings(s cfapi.DatasetSettings) error {
	if s.MaxDuration < 300 || s.NotOlderThan <= 0 || s.MaxPageSize <= 0 {
		return errors.New("health-check dataset is missing complete-bucket duration, retention or page limits")
	}
	if s.MaxNumberOfFields < len(timingFields) || s.MaxNumberOfFields < len(countFields) {
		return errors.New("health-check field budget cannot fit independent selections")
	}
	for _, field := range append(append([]string{}, countFields...), timingFields...) {
		found := false
		for _, available := range s.AvailableFields {
			if strings.EqualFold(available, field) || strings.EqualFold(available, strings.ReplaceAll(field, ".", "_")) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("health-check dataset is missing required field %s", field)
		}
	}
	return nil
}
func (c *events) query(ctx context.Context, zone string, fields []string, at time.Time, limit int) ([]map[string]any, error) {
	var rows []map[string]any
	err := c.api.Query(ctx, cfapi.GraphQLRequest{Scope: cfapi.ZoneScope, ScopeID: zone, Dataset: dataset, WantedFields: append([]string{}, fields...), From: at, To: at.Add(bucketSize), Limit: limit}, &rows)
	if err != nil {
		return nil, fmt.Errorf("query health-check complete bucket: %w", err)
	}
	if len(rows) >= limit {
		return nil, errors.New("health-check complete bucket saturated; cannot combine partial averages")
	}
	return rows, nil
}
func rowDimensions(row map[string]any, at time.Time) (map[string]any, error) {
	dims, ok := row["dimensions"].(map[string]any)
	if !ok {
		return nil, errors.New("health-check row has no dimensions")
	}
	raw, ok := dims["datetimeFiveMinutes"].(string)
	if !ok {
		return nil, errors.New("health-check row has no bucket timestamp")
	}
	timestamp, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || !timestamp.Equal(at) {
		return nil, errors.New("health-check row is outside its complete bucket")
	}
	return dims, nil
}
func number(value any) (float64, bool) {
	n, ok := value.(float64)
	return n, ok && n >= 0 && !math.IsNaN(n) && !math.IsInf(n, 0)
}
func bounded(s string, limit int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > limit {
		s = string(r[:limit])
	}
	return s
}
func originIdentity(dims map[string]any) string {
	fqdn, _ := dims["fqdn"].(string)
	fqdn = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(fqdn), "."))
	if validFQDN(fqdn) {
		return fqdn
	}
	name, _ := dims["healthCheckName"].(string)
	name = strings.TrimSpace(name)
	// Reject rather than truncate identity: truncation can merge two origins.
	if name == "" || len([]rune(name)) > 128 || isIP(name) || opaqueID(name) {
		return ""
	}
	for _, r := range name {
		if unicode.IsControl(r) || strings.ContainsRune("/@\\", r) {
			return ""
		}
	}
	return name
}
func isIP(s string) bool {
	s = strings.TrimSuffix(strings.TrimSpace(s), ".")
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	if _, err := netip.ParseAddr(strings.Trim(s, "[]")); err == nil {
		return true
	}
	// Also reject noncanonical numeric dotted IP spellings as identities.
	if !strings.Contains(s, ".") {
		return false
	}
	for _, r := range s {
		if r != '.' && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
func opaqueID(s string) bool {
	flat := strings.ReplaceAll(s, "-", "")
	if len(flat) != 32 {
		return false
	}
	for _, r := range flat {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}
func validFQDN(s string) bool {
	if len(s) > 128 || !strings.Contains(s, ".") || isIP(s) {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	// A numeric dotted literal must not sneak through as a DNS name.
	allNumeric := true
	for _, r := range s {
		if r != '.' && (r < '0' || r > '9') {
			allNumeric = false
		}
	}
	return !allNumeric
}

var _ collector.WindowCollector = (*events)(nil)
