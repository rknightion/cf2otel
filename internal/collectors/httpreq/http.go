package httpreq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
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

var groupFields = []string{
	"count", "dimensions.clientRequestHTTPHost", "dimensions.edgeResponseStatus",
	"dimensions.cacheStatus", "avg.originResponseDurationMs",
}
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
	allowed := map[string]bool{}
	switch b.cfg.HTTP.Scope {
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
		return nil, fmt.Errorf("invalid HTTP scope %q", b.cfg.HTTP.Scope)
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
	allowed, err := c.hosts(ctx)
	if err != nil {
		return from, err
	}
	zones, err := c.zones(ctx)
	if err != nil {
		return from, err
	}
	var retentionGaps []error
	for _, zone := range zones {
		var rows []map[string]any
		req := cfapi.GraphQLRequest{Scope: cfapi.ZoneScope, ScopeID: zone.ID, Dataset: "httpRequestsAdaptiveGroups", WantedFields: groupFields, From: from, To: to, Limit: 10000}
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
			if row["count"] == nil || field(dims, "clientRequestHTTPHost") == nil {
				return from, fmt.Errorf("HTTP group missing required count or host field")
			}
			host := str(field(dims, "clientRequestHTTPHost"))
			if !selected(host, allowed) {
				continue
			}
			count := number(row["count"])
			if count <= 0 {
				continue
			}
			attrs := []telemetry.Attr{{Key: semconv.AttrHTTPZone, Value: zone.Name},
				{Key: semconv.AttrHTTPHost, Value: host},
				{Key: semconv.AttrStatusClass, Value: statusClass(field(dims, "edgeResponseStatus"))},
				{Key: semconv.AttrHTTPCacheStatus, Value: str(field(dims, "cacheStatus"))}}
			if err := e.Counter(ctx, semconv.MetricHTTPRequests, count, attrs...); err != nil {
				return from, err
			}
			avg, _ := row["avg"].(map[string]any)
			if v, ok := avg["originResponseDurationMs"]; ok && v != nil {
				if err := e.Gauge(ctx, semconv.MetricHTTPOriginDuration, number(v)/1000, attrs...); err != nil {
					return from, err
				}
			}
		}
	}
	if len(retentionGaps) > 0 {
		return from, errors.Join(retentionGaps...)
	}
	return to, nil
}
