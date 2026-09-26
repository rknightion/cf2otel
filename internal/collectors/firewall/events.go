package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	otellog "go.opentelemetry.io/otel/log"
)

var rawEventFields = []string{
	"datetime", "action", "source", "kind", "clientIP", "clientCountryName",
	"clientAsn", "clientRequestHTTPHost",
	"clientRequestHTTPMethodName", "clientRequestPath", "clientRequestQuery",
	"clientRequestHTTPProtocol", "edgeResponseStatus", "originResponseStatus",
	"wafAttackScoreClass", "ruleId", "rulesetId", "rayName", "coloCode", "userAgent",
}

type events struct {
	cfg   *config.Config
	api   cfapi.Client
	limit int
}

func NewEvents(cfg *config.Config, api cfapi.Client) *events {
	return &events{cfg: cfg, api: api, limit: queryLimit}
}

func (*events) Name() string                   { return "firewall.events" }
func (*events) DefaultInterval() time.Duration { return 5 * time.Minute }
func (*events) Lag() time.Duration             { return 2 * time.Minute }

type firewallEvent struct {
	zone cfapi.Zone
	at   time.Time
	row  map[string]any
}

func (c *events) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return from, errors.New("invalid firewall event window")
	}
	selected, err := zones(ctx, c.cfg, c.api)
	if err != nil {
		return from, err
	}

	var records []firewallEvent
	seen := make(map[string]struct{})
	var retentionGaps []error
	for _, zone := range selected {
		rows, err := c.queryWindow(ctx, zone, from, to)
		if err != nil {
			var gap *cfapi.RetentionGapError
			if errors.As(err, &gap) {
				retentionGaps = append(retentionGaps, fmt.Errorf("zone firewall events: %w", err))
				continue
			}
			return from, fmt.Errorf("zone firewall events: %w", err)
		}
		for _, row := range rows {
			at, err := rowTime(row)
			if err != nil {
				return from, fmt.Errorf("firewall event is missing a valid datetime")
			}
			// Checkpoint windows are [from, to): retaining the lower-bound
			// event avoids a gap between consecutive windows.
			if at.Before(from) || !at.Before(to) {
				continue
			}
			if ray := valueString(row["rayName"]); ray != "" {
				key := ray + "/" + at.UTC().Format(time.RFC3339Nano)
				if _, duplicate := seen[key]; duplicate {
					continue
				}
				seen[key] = struct{}{}
			}
			records = append(records, firewallEvent{zone: zone, at: at, row: row})
		}
	}
	if len(retentionGaps) > 0 {
		return from, errors.Join(retentionGaps...)
	}

	for _, record := range records {
		body, err := json.Marshal(record.row)
		if err != nil {
			return from, fmt.Errorf("encode firewall event: %w", err)
		}
		attrs := []telemetry.Attr{{Key: semconv.AttrFirewallZone, Value: record.zone.Name}}
		for _, field := range firewallLogFields {
			if value, ok := record.row[field.name]; ok && value != nil {
				attrs = append(attrs, telemetry.Attr{Key: field.attr, Value: valueString(value)})
			}
		}
		if err := out.LogEvent(ctx, semconv.EventFirewallEvent, string(body), record.at, severityForAction(valueString(record.row["action"])), attrs...); err != nil {
			return from, err
		}
	}
	return to, nil
}

func (c *events) queryWindow(ctx context.Context, zone cfapi.Zone, from, to time.Time) ([]map[string]any, error) {
	limit := c.limit
	if limit <= 0 {
		limit = queryLimit
	}
	rows, err := collector.Bisect(from, to, time.Second, time.Minute, func(from, to time.Time) ([]map[string]any, bool, error) {
		var rows []map[string]any
		req := cfapi.GraphQLRequest{
			Scope: cfapi.ZoneScope, ScopeID: zone.ID, Dataset: rawDataset,
			WantedFields: rawEventFields, JoinFields: []string{"rayName", "datetime"},
			From: from, To: to, Limit: limit,
		}
		err := c.api.Query(ctx, req, &rows)
		saturated := isSaturationError(err, rawDataset) || (err == nil && len(rows) >= limit)
		if saturated && err == nil {
			err = fmt.Errorf("row count reached requested limit %d", limit)
		}
		return rows, saturated, err
	})
	var window *collector.SaturatedWindowError
	if errors.As(err, &window) {
		if errors.Is(window.Reason, collector.ErrWindowIrreducible) {
			return nil, fmt.Errorf("firewall events still saturate a one-minute query window: %w", window.Cause)
		}
		return nil, fmt.Errorf("cannot split saturated firewall event window %s..%s", window.From.Format(time.RFC3339), window.To.Format(time.RFC3339))
	}
	return rows, err
}

func isSaturationError(err error, dataset string) bool {
	sat, ok := cfapi.AsSaturation(err)
	return ok && sat.Dataset == dataset
}

func rowTime(row map[string]any) (time.Time, error) {
	value, ok := row["datetime"].(string)
	if !ok || value == "" {
		return time.Time{}, errors.New("missing datetime")
	}
	return time.Parse(time.RFC3339Nano, value)
}

func valueString(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func severityForAction(action string) otellog.Severity {
	action = strings.ToLower(strings.TrimSpace(action))
	switch {
	case action == "block":
		return otellog.SeverityWarn
	case strings.Contains(action, "challenge"):
		return otellog.SeverityInfo4
	case action == "log":
		return otellog.SeverityInfo
	case action == "allow" || action == "skip":
		return otellog.SeverityDebug
	default:
		return otellog.SeverityInfo2
	}
}

var firewallLogFields = []struct{ name, attr string }{
	{"action", semconv.AttrFirewallAction},
	{"source", semconv.AttrFirewallSource},
	{"kind", semconv.AttrFirewallKind},
	{"clientIP", semconv.AttrFirewallClientIP},
	{"clientCountryName", semconv.AttrFirewallCountry},
	{"clientAsn", semconv.AttrFirewallASN},
	{"clientAsnDescription", semconv.AttrFirewallASNDescription},
	{"clientRequestHTTPHost", semconv.AttrFirewallHost},
	{"clientRequestHTTPMethodName", semconv.AttrFirewallMethod},
	{"clientRequestPath", semconv.AttrFirewallPath},
	{"clientRequestQuery", semconv.AttrFirewallQuery},
	{"clientRequestHTTPProtocol", semconv.AttrFirewallProtocol},
	{"edgeResponseStatus", semconv.AttrFirewallEdgeResponseStatus},
	{"originResponseStatus", semconv.AttrFirewallOriginResponseStatus},
	{"wafAttackScoreClass", semconv.AttrFirewallAttackScoreClass},
	{"ruleId", semconv.AttrFirewallRuleID},
	{"rulesetId", semconv.AttrFirewallRulesetID},
	{"rayName", semconv.AttrFirewallRayID},
	{"coloCode", semconv.AttrFirewallColo},
	{"userAgent", semconv.AttrFirewallUserAgent},
}

var _ collector.WindowCollector = (*events)(nil)
