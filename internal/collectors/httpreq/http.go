package httpreq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collectors/access"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/identity"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	otellog "go.opentelemetry.io/otel/log"
)

var eventFields = []string{
	"datetime", "rayName", "clientRequestHTTPHost", "clientRequestHTTPMethodName",
	"clientRequestPath", "clientRequestQuery", "edgeResponseStatus", "originResponseStatus",
	"clientIP", "userAgent", "cacheStatus", "securityAction", "coloCode",
}

type accessApp struct {
	Domain            string   `json:"domain"`
	SelfHostedDomains []string `json:"self_hosted_domains"`
}
type base struct {
	cfg      *config.Config
	api      cfapi.Client
	identity identity.Index
	hydrate  func(context.Context, time.Time, time.Time) error
}
type events struct{ base }
type metrics struct{ base }

func NewEvents(cfg *config.Config, api cfapi.Client, id identity.Index) interface {
	Name() string
	DefaultInterval() time.Duration
	Lag() time.Duration
	CollectWindow(context.Context, time.Time, time.Time, telemetry.Emitter) (time.Time, error)
} {
	b := base{cfg: cfg, api: api, identity: id}
	if cfg.Identity.Enabled && id != nil {
		b.hydrate = func(ctx context.Context, from, to time.Time) error {
			return access.HydrateIdentity(ctx, cfg, api, id, from.Add(-cfg.Identity.MatchWindow), to)
		}
	}
	return events{b}
}
func NewMetrics(cfg *config.Config, api cfapi.Client) interface {
	Name() string
	DefaultInterval() time.Duration
	Lag() time.Duration
	CollectWindow(context.Context, time.Time, time.Time, telemetry.Emitter) (time.Time, error)
} {
	return metrics{base{cfg: cfg, api: api}}
}

func (events) Name() string                    { return "httpreq.events" }
func (metrics) Name() string                   { return "httpreq.metrics" }
func (events) DefaultInterval() time.Duration  { return 5 * time.Minute }
func (metrics) DefaultInterval() time.Duration { return 5 * time.Minute }
func (events) Lag() time.Duration              { return 2 * time.Minute }
func (metrics) Lag() time.Duration             { return 2 * time.Minute }

func hostOf(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
}
func (b base) hosts(ctx context.Context) (map[string]bool, error) {
	return b.hostsFor(ctx, b.cfg.HTTP.Scope)
}
func (b base) hostsFor(ctx context.Context, scope string) (map[string]bool, error) {
	allowed := map[string]bool{}
	switch scope {
	case "all":
		return nil, nil
	case "hosts":
		for _, s := range b.cfg.HTTP.Hosts {
			if h := hostOf(s); h != "" {
				allowed[h] = true
			}
		}
		return allowed, nil
	case "access_protected", "":
		// Access apps are inventory, so refresh at collection time to pick up new domains.
		path := "/accounts/" + url.PathEscape(b.cfg.Cloudflare.AccountID) + "/access/apps"
		for page := 1; page <= 100; page++ {
			q := url.Values{"page": {strconv.Itoa(page)}, "per_page": {"100"}}
			var apps []accessApp
			if err := b.api.Get(ctx, path, q, &apps); err != nil {
				return nil, fmt.Errorf("access app domains: %w", err)
			}
			for _, app := range apps {
				if h := hostOf(app.Domain); h != "" {
					allowed[h] = true
				}
				for _, d := range app.SelfHostedDomains {
					if h := hostOf(d); h != "" {
						allowed[h] = true
					}
				}
			}
			if len(apps) < 100 {
				return allowed, nil
			}
		}
		return nil, fmt.Errorf("access app pagination exceeded 100 pages")
	default:
		return nil, fmt.Errorf("invalid HTTP scope %q", scope)
	}
}
func (b base) zones(ctx context.Context) ([]cfapi.Zone, error) {
	zones, err := b.api.Zones(ctx)
	if err != nil {
		return nil, err
	}
	wanted := b.cfg.HTTP.Zones
	if len(wanted) == 0 {
		wanted = b.cfg.Cloudflare.Zones
	}
	if len(wanted) == 0 {
		return zones, nil
	}
	selected := make([]cfapi.Zone, 0, len(zones))
	for _, z := range zones {
		for _, s := range wanted {
			if s == z.ID || strings.EqualFold(s, z.Name) {
				selected = append(selected, z)
				break
			}
		}
	}
	return selected, nil
}
func (b base) metricsScope() string {
	if b.cfg.HTTP.MetricsScope != "" {
		return b.cfg.HTTP.MetricsScope
	}
	return b.cfg.HTTP.Scope
}
func (b base) metricZones(ctx context.Context) ([]cfapi.Zone, error) {
	zones, err := b.api.Zones(ctx)
	if err != nil {
		return nil, err
	}
	if len(zones) == 0 {
		return nil, errors.New("HTTP metrics zone discovery returned no zones")
	}
	for _, zone := range zones {
		if zone.ID == "" || zone.Name == "" {
			return nil, errors.New("HTTP metrics zone discovery returned an incomplete zone")
		}
	}
	wanted := b.cfg.HTTP.Zones
	// Only an explicitly configured metrics_scope=all broadens past the
	// legacy Cloudflare.Zones selector. An empty metrics scope inherits the
	// host scope while preserving the old zone subset behavior.
	if len(wanted) == 0 && b.cfg.HTTP.MetricsScope != "all" {
		wanted = b.cfg.Cloudflare.Zones
	}
	if len(wanted) == 0 {
		return zones, nil
	}
	selected := make([]cfapi.Zone, 0, len(zones))
	for _, zone := range zones {
		for _, selector := range wanted {
			if selector == zone.ID || strings.EqualFold(selector, zone.Name) {
				selected = append(selected, zone)
				break
			}
		}
	}
	if len(selected) == 0 {
		return nil, errors.New("HTTP metrics zone selectors matched no discovered zones")
	}
	return selected, nil
}

var requiredHTTPGroupFields = []string{
	"count", "dimensions.clientRequestHTTPHost", "dimensions.edgeResponseStatus", "dimensions.cacheStatus",
}

type httpGroupSettingsProvider interface {
	DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error)
}

func httpGroupFieldName(field string) string {
	field = strings.ToLower(field)
	for _, prefix := range []string{"dimensions", "avg"} {
		if strings.HasPrefix(field, prefix+"_") {
			return prefix + "." + strings.TrimPrefix(field, prefix+"_")
		}
	}
	return field
}

func httpGroupQueryFields(ctx context.Context, api cfapi.Client, zoneID string) ([]string, error) {
	provider, ok := api.(httpGroupSettingsProvider)
	if !ok {
		return nil, errors.New("HTTP groups settings discovery is unavailable")
	}
	settings, err := provider.DatasetSettings(ctx, cfapi.ZoneScope, zoneID, "httpRequestsAdaptiveGroups")
	if err != nil {
		return nil, fmt.Errorf("HTTP groups settings: %w", err)
	}
	if !settings.Enabled {
		return nil, errors.New("HTTP groups dataset is disabled for a discovered zone")
	}
	available := make(map[string]bool, len(settings.AvailableFields))
	for _, field := range settings.AvailableFields {
		available[httpGroupFieldName(field)] = true
	}
	for _, field := range requiredHTTPGroupFields {
		if !available[httpGroupFieldName(field)] {
			return nil, fmt.Errorf("HTTP groups is missing required field %q", field)
		}
	}
	if settings.MaxNumberOfFields > 0 && settings.MaxNumberOfFields < len(requiredHTTPGroupFields) {
		return nil, fmt.Errorf("HTTP groups field limit %d is below the required field count %d", settings.MaxNumberOfFields, len(requiredHTTPGroupFields))
	}
	fields := append([]string(nil), requiredHTTPGroupFields...)
	const avgField = "avg.originResponseDurationMs"
	if available[httpGroupFieldName(avgField)] && (settings.MaxNumberOfFields <= 0 || settings.MaxNumberOfFields > len(fields)) {
		fields = append(fields, avgField)
	}
	return fields, nil
}

type httpMetricLabels struct {
	zone   string
	host   string
	status string
	cache  string
}

type httpMetricTotals struct {
	requests             float64
	originDurationMS     float64
	originDurationWeight float64
}

func metricNumber(v any) (float64, bool) {
	var value float64
	switch n := v.(type) {
	case float64:
		value = n
	case float32:
		value = float64(n)
	case int:
		value = float64(n)
	case int64:
		value = float64(n)
	case json.Number:
		parsed, err := n.Float64()
		if err != nil {
			return 0, false
		}
		value = parsed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return 0, false
		}
		value = parsed
	default:
		return 0, false
	}
	return value, !math.IsNaN(value) && !math.IsInf(value, 0)
}

func metricCacheStatus(v any) string {
	status := strings.ToLower(strings.TrimSpace(str(v)))
	switch status {
	case "hit", "miss", "expired", "stale", "updating", "revalidated", "ignoredbypolicy", "bypass", "dynamic", "none", "uncacheable", "unknown":
		return status
	default:
		return "other"
	}
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		f, _ := strconv.ParseFloat(str(v), 64)
		return f
	}
}
func field(m map[string]any, k string) any {
	if m == nil {
		return nil
	}
	return m[k]
}
func inWindow(row map[string]any, from, to time.Time) (time.Time, bool) {
	at, err := time.Parse(time.RFC3339Nano, str(row["datetime"]))
	return at, err == nil && !at.Before(from) && at.Before(to)
}
func selected(host string, allowed map[string]bool) bool {
	if allowed == nil {
		return true
	}
	host = strings.ToLower(host)
	if allowed[host] {
		return true
	}
	for pattern := range allowed {
		if strings.HasPrefix(pattern, "*.") && strings.HasSuffix(host, pattern[1:]) {
			return true
		}
	}
	return false
}
func statusClass(status any) string {
	n := int(number(status))
	if n < 100 || n > 599 {
		return "unknown"
	}
	return strconv.Itoa(n/100) + "xx"
}

func (c events) CollectWindow(ctx context.Context, from, to time.Time, e telemetry.Emitter) (time.Time, error) {
	if c.hydrate != nil {
		if err := c.hydrate(ctx, from, to); err != nil {
			return from, fmt.Errorf("HTTP identity hydration: %w", err)
		}
	}
	allowed, err := c.hosts(ctx)
	if err != nil {
		return from, err
	}
	zones, err := c.zones(ctx)
	if err != nil {
		return from, err
	}
	seen := map[string]bool{}
	var retentionGaps []error
	for _, zone := range zones {
		var rows []map[string]any
		req := cfapi.GraphQLRequest{Scope: cfapi.ZoneScope, ScopeID: zone.ID, Dataset: "httpRequestsAdaptive", WantedFields: eventFields, JoinFields: []string{"rayName", "datetime"}, From: from, To: to, Limit: 10000}
		if err := c.api.Query(ctx, req, &rows); err != nil {
			var gap *cfapi.RetentionGapError
			if errors.As(err, &gap) {
				retentionGaps = append(retentionGaps, fmt.Errorf("zone HTTP events: %w", err))
				continue
			}
			return from, fmt.Errorf("zone HTTP events: %w", err)
		}
		if len(rows) >= req.Limit {
			return from, fmt.Errorf("zone HTTP events reached the query limit; narrow the window")
		}
		for _, row := range rows {
			at, ok := inWindow(row, from, to)
			if row["datetime"] == nil || row["clientRequestHTTPHost"] == nil {
				return from, fmt.Errorf("HTTP event missing required datetime or host field")
			}
			if !ok {
				continue
			}
			host := strings.ToLower(str(row["clientRequestHTTPHost"]))
			if !selected(host, allowed) {
				continue
			}
			key := zone.ID + "/" + str(row["rayName"]) + "/" + at.Format(time.RFC3339Nano)
			if row["rayName"] != nil && seen[key] {
				continue
			}
			seen[key] = true
			body, err := json.Marshal(row)
			if err != nil {
				return from, err
			}
			attrs := []telemetry.Attr{{Key: semconv.AttrHTTPZone, Value: zone.Name}}
			for _, f := range []struct{ source, key string }{
				{"clientRequestHTTPHost", semconv.AttrHTTPHost}, {"clientRequestHTTPMethodName", semconv.AttrHTTPMethod},
				{"clientRequestPath", semconv.AttrHTTPPath}, {"clientRequestQuery", semconv.AttrHTTPQuery},
				{"edgeResponseStatus", semconv.AttrHTTPStatusCode}, {"originResponseStatus", semconv.AttrHTTPOriginStatusCode},
				{"clientIP", semconv.AttrHTTPClientIP}, {"userAgent", semconv.AttrHTTPUserAgent},
				{"rayName", semconv.AttrHTTPRayID}, {"cacheStatus", semconv.AttrHTTPCacheStatus},
				{"securityAction", semconv.AttrHTTPSecurityAction}, {"coloCode", semconv.AttrHTTPColo},
			} {
				if v, ok := row[f.source]; ok && v != nil {
					attrs = append(attrs, telemetry.Attr{Key: f.key, Value: str(v)})
				}
			}
			if c.cfg.Identity.Enabled && c.identity != nil {
				match := c.identity.Lookup(str(row["clientIP"]), host, at)
				if match.Inferred && !match.Ambiguous && match.UserEmail != "" {
					attrs = append(attrs, telemetry.Attr{Key: semconv.AttrAccessUserEmail, Value: match.UserEmail}, telemetry.Attr{Key: semconv.AttrAccessIdentityInferred, Value: "true"})
					if match.LoginRayID != "" {
						attrs = append(attrs, telemetry.Attr{Key: semconv.AttrAccessIdentityLoginRayID, Value: match.LoginRayID})
					}
				}
			}
			if err := e.LogEvent(ctx, semconv.EventHTTPRequest, string(body), at, otellog.SeverityInfo, attrs...); err != nil {
				return from, err
			}
		}
	}
	if len(retentionGaps) > 0 {
		return from, errors.Join(retentionGaps...)
	}
	return to, nil
}

func (c metrics) CollectWindow(ctx context.Context, from, to time.Time, e telemetry.Emitter) (time.Time, error) {
	scope := c.metricsScope()
	allowed, err := c.hostsFor(ctx, scope)
	if err != nil {
		return from, err
	}
	zones, err := c.metricZones(ctx)
	if err != nil {
		return from, err
	}
	if c.cfg.HTTP.MaxMetricHostsPerZone <= 0 {
		return from, errors.New("HTTP metric host limit must be positive")
	}
	if c.cfg.HTTP.MaxMetricSeriesPerWindow <= 0 {
		return from, errors.New("HTTP metric series limit must be positive")
	}

	// Buffer a complete window before touching the emitter. A failure in any
	// zone leaves the caller's checkpoint unchanged without producing partial
	// metric points. Host labels in all scope remain bounded by both limits.
	totals := make(map[httpMetricLabels]httpMetricTotals)
	hostsByZone := make(map[string]map[string]struct{}, len(zones))
	var retentionGaps []error
	for _, zone := range zones {
		fields, err := httpGroupQueryFields(ctx, c.api, zone.ID)
		if err != nil {
			return from, err
		}
		includeOriginDuration := false
		for _, field := range fields {
			if field == "avg.originResponseDurationMs" {
				includeOriginDuration = true
				break
			}
		}
		var rows []map[string]any
		req := cfapi.GraphQLRequest{Scope: cfapi.ZoneScope, ScopeID: zone.ID, Dataset: "httpRequestsAdaptiveGroups", WantedFields: fields, From: from, To: to, Limit: 10000}
		if err := c.api.Query(ctx, req, &rows); err != nil {
			var gap *cfapi.RetentionGapError
			if errors.As(err, &gap) {
				retentionGaps = append(retentionGaps, fmt.Errorf("zone HTTP groups: %w", err))
				continue
			}
			return from, fmt.Errorf("zone HTTP groups: %w", err)
		}
		if len(rows) >= req.Limit {
			return from, fmt.Errorf("zone HTTP groups reached the query limit; narrow the window")
		}
		for _, row := range rows {
			dims, _ := row["dimensions"].(map[string]any)
			if row["count"] == nil || field(dims, "clientRequestHTTPHost") == nil || field(dims, "edgeResponseStatus") == nil || field(dims, "cacheStatus") == nil {
				return from, errors.New("HTTP group missing required count, host, status, or cache field")
			}
			host := hostOf(str(field(dims, "clientRequestHTTPHost")))
			if host == "" {
				return from, errors.New("HTTP group has an empty host field")
			}
			if !selected(host, allowed) {
				continue
			}
			if net.ParseIP(host) != nil {
				return from, errors.New("HTTP group host is an IP address; refusing it as a metric label")
			}
			count, ok := metricNumber(row["count"])
			if !ok || count < 0 {
				return from, errors.New("HTTP group has an invalid count field")
			}
			if count == 0 {
				continue
			}
			zoneHosts := hostsByZone[zone.ID]
			if zoneHosts == nil {
				zoneHosts = make(map[string]struct{})
				hostsByZone[zone.ID] = zoneHosts
			}
			zoneHosts[host] = struct{}{}
			if len(zoneHosts) > c.cfg.HTTP.MaxMetricHostsPerZone {
				return from, fmt.Errorf("HTTP metric host count exceeds the per-zone limit %d", c.cfg.HTTP.MaxMetricHostsPerZone)
			}

			labels := httpMetricLabels{
				zone:   zone.Name,
				host:   host,
				status: statusClass(field(dims, "edgeResponseStatus")),
				cache:  metricCacheStatus(field(dims, "cacheStatus")),
			}
			aggregate := totals[labels]
			aggregate.requests += count
			if math.IsInf(aggregate.requests, 0) || math.IsNaN(aggregate.requests) {
				return from, errors.New("HTTP metric request total is invalid")
			}
			avg, _ := row["avg"].(map[string]any)
			if v, ok := avg["originResponseDurationMs"]; includeOriginDuration && ok && v != nil {
				milliseconds, valid := metricNumber(v)
				if !valid || milliseconds < 0 {
					return from, errors.New("HTTP group has an invalid origin duration")
				}
				aggregate.originDurationMS += milliseconds * count
				aggregate.originDurationWeight += count
				if math.IsInf(aggregate.originDurationMS, 0) || math.IsNaN(aggregate.originDurationMS) || math.IsInf(aggregate.originDurationWeight, 0) || math.IsNaN(aggregate.originDurationWeight) {
					return from, errors.New("HTTP metric origin duration total is invalid")
				}
			}
			totals[labels] = aggregate
		}
	}
	if len(retentionGaps) > 0 {
		return from, errors.Join(retentionGaps...)
	}
	series := 0
	for _, aggregate := range totals {
		series++ // request counter
		if aggregate.originDurationWeight > 0 {
			series++ // origin duration gauge has a distinct metric name
		}
	}
	if series > c.cfg.HTTP.MaxMetricSeriesPerWindow {
		return from, fmt.Errorf("HTTP metric series count %d exceeds the per-window limit %d", series, c.cfg.HTTP.MaxMetricSeriesPerWindow)
	}
	labels := make([]httpMetricLabels, 0, len(totals))
	for label := range totals {
		labels = append(labels, label)
	}
	sort.Slice(labels, func(i, j int) bool {
		if labels[i].zone != labels[j].zone {
			return labels[i].zone < labels[j].zone
		}
		if labels[i].host != labels[j].host {
			return labels[i].host < labels[j].host
		}
		if labels[i].status != labels[j].status {
			return labels[i].status < labels[j].status
		}
		return labels[i].cache < labels[j].cache
	})
	for _, label := range labels {
		aggregate := totals[label]
		attrs := []telemetry.Attr{
			{Key: semconv.AttrHTTPZone, Value: label.zone},
			{Key: semconv.AttrHTTPHost, Value: label.host},
			{Key: semconv.AttrStatusClass, Value: label.status},
			{Key: semconv.AttrHTTPCacheStatus, Value: label.cache},
		}
		if err := e.Counter(ctx, semconv.MetricHTTPRequests, aggregate.requests, attrs...); err != nil {
			return from, err
		}
		if aggregate.originDurationWeight > 0 {
			seconds := aggregate.originDurationMS / aggregate.originDurationWeight / 1000
			if err := e.Gauge(ctx, semconv.MetricHTTPOriginDuration, seconds, attrs...); err != nil {
				return from, err
			}
		}
	}
	return to, nil
}
