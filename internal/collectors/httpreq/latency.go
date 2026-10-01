package httpreq

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

var httpLatencyFields = []struct{ field, metric, statistic string }{
	{"avg.edgeTimeToFirstByteMs", semconv.MetricHTTPEdgeTTFB, "avg"},
	{"quantiles.edgeTimeToFirstByteMsP50", semconv.MetricHTTPEdgeTTFB, "p50"},
	{"quantiles.edgeTimeToFirstByteMsP95", semconv.MetricHTTPEdgeTTFB, "p95"},
	{"quantiles.edgeTimeToFirstByteMsP99", semconv.MetricHTTPEdgeTTFB, "p99"},
	{"quantiles.originResponseDurationMsP50", semconv.MetricHTTPOriginResponseTime, "p50"},
	{"quantiles.originResponseDurationMsP95", semconv.MetricHTTPOriginResponseTime, "p95"},
	{"quantiles.originResponseDurationMsP99", semconv.MetricHTTPOriginResponseTime, "p99"},
}

type httpLatencyPoint struct {
	name    string
	seconds float64
	attrs   []telemetry.Attr
}

func (c metrics) requestSourceFilter() map[string]any {
	if c.cfg.HTTP.RequestSource == "all" {
		return nil
	}
	return map[string]any{"requestSource": "eyeball"}
}

// Quantiles must be calculated by Cloudflare at host level, not averaged from
// status/cache groups: percentiles cannot be combined into a host percentile.
func (c metrics) collectLatencies(ctx context.Context, zone cfapi.Zone, from, to time.Time, fields []string, allowed map[string]bool, hostsByZone map[string]map[string]struct{}) ([]httpLatencyPoint, int, error) {
	req := cfapi.GraphQLRequest{Scope: cfapi.ZoneScope, ScopeID: zone.ID, Dataset: "httpRequestsAdaptiveGroups", WantedFields: fields, From: from, To: to, Limit: 10000, Filter: c.requestSourceFilter()}
	var rows []map[string]any
	if err := c.api.Query(ctx, req, &rows); err != nil {
		return nil, 0, fmt.Errorf("zone HTTP latencies: %w", err)
	}
	if len(rows) >= req.Limit {
		return nil, 0, errors.New("zone HTTP latencies reached the query limit; narrow the window")
	}
	wanted := map[string]bool{}
	for _, field := range fields {
		wanted[field] = true
	}
	type hostGroup struct {
		row   map[string]any
		raw   string
		count float64
	}
	groups := map[string]hostGroup{}
	var hosts []string
	variants := 0
	var points []httpLatencyPoint
	for _, row := range rows {
		dims, _ := row["dimensions"].(map[string]any)
		raw := str(dims["clientRequestHTTPHost"])
		host := hostOf(raw)
		if host == "" {
			return nil, 0, errors.New("HTTP latency group has an empty host field")
		}
		if !selected(host, allowed) {
			continue
		}
		if net.ParseIP(host) != nil {
			return nil, 0, errors.New("HTTP latency host is an IP address; refusing it as a metric label")
		}
		count, valid := metricNumber(row["count"])
		if !valid || count < 0 {
			return nil, 0, errors.New("HTTP latency group has invalid count")
		}
		if previous, exists := groups[host]; exists {
			variants++
			// Never merge percentiles. Prefer the largest source population;
			// the canonical raw host wins equal-count ties. Lexical fallback
			// makes ties between noncanonical variants independent of row order.
			if count < previous.count || count == previous.count &&
				(previous.raw == host || raw != host && raw >= previous.raw) {
				continue
			}
		} else {
			hosts = append(hosts, host)
		}
		groups[host] = hostGroup{row: row, raw: raw, count: count}
		zoneHosts := hostsByZone[zone.ID]
		if zoneHosts == nil {
			zoneHosts = map[string]struct{}{}
			hostsByZone[zone.ID] = zoneHosts
		}
		zoneHosts[host] = struct{}{}
		if len(zoneHosts) > c.cfg.HTTP.MaxMetricHostsPerZone {
			return nil, 0, fmt.Errorf("HTTP metric host count exceeds the per-zone limit %d", c.cfg.HTTP.MaxMetricHostsPerZone)
		}
	}
	for _, host := range hosts {
		row := groups[host].row
		for _, latency := range httpLatencyFields {
			if !wanted[latency.field] {
				continue
			}
			part, name, _ := strings.Cut(latency.field, ".")
			values, _ := row[part].(map[string]any)
			// Null timings have no samples; -1 is Cloudflare's missing-value sentinel.
			if values[name] == nil {
				continue
			}
			milliseconds, valid := metricNumber(values[name])
			if !valid || milliseconds < -1 {
				return nil, 0, fmt.Errorf("HTTP group has invalid latency %s", latency.field)
			}
			if milliseconds == -1 {
				continue
			}
			points = append(points, httpLatencyPoint{name: latency.metric, seconds: milliseconds / 1000, attrs: []telemetry.Attr{{Key: semconv.AttrHTTPZone, Value: zone.Name}, {Key: semconv.AttrHTTPHost, Value: host}, {Key: semconv.AttrStatistic, Value: latency.statistic}}})
		}
	}
	return points, variants, nil
}
