package access

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
	otellog "go.opentelemetry.io/otel/log"
)

type scimCollector struct {
	api       cfapi.Client
	accountID string
	pageSize  int
}

func (scimCollector) Name() string                   { return "access.scim" }
func (scimCollector) DefaultInterval() time.Duration { return 5 * time.Minute }
func (scimCollector) Lag() time.Duration             { return time.Minute }
func (c scimCollector) CollectWindow(ctx context.Context, from, to time.Time, e telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return to, nil
	}
	if c.api == nil || c.accountID == "" {
		return from, fmt.Errorf("access SCIM requires API and account ID")
	}
	size := c.pageSize
	if size <= 0 {
		size = 100
	}
	seen := map[string]bool{}
	for page := 1; ; page++ {
		q := url.Values{"since": {from.UTC().Format(time.RFC3339Nano)}, "until": {to.UTC().Format(time.RFC3339Nano)}, "limit": {strconv.Itoa(size)}, "page": {strconv.Itoa(page)}, "direction": {"asc"}}

		var rows []struct {
			ResourceID    string `json:"cf_resource_id"`
			Method        string `json:"http_method"`
			RequestMethod string `json:"request_method"`
			IDPID         string `json:"idp_id"`
			LoggedAt      string `json:"logged_at"`
			Type          string `json:"resource_type"`
			Email         string `json:"resource_user_email"`
			Status        string `json:"status"`
		}
		if err := c.api.Get(ctx, "/accounts/"+url.PathEscape(c.accountID)+"/access/logs/scim/updates", q, &rows); err != nil {
			return from, fmt.Errorf("SCIM updates page %d: %w", page, err)
		}
		for _, r := range rows {
			at, err := time.Parse(time.RFC3339Nano, r.LoggedAt)
			if err != nil {
				return from, fmt.Errorf("SCIM logged_at: %w", err)
			}
			if at.Before(from) || !at.Before(to) {
				continue
			}
			method := r.Method
			if method == "" {
				method = r.RequestMethod
			}
			key := r.ResourceID + "|" + r.LoggedAt + "|" + method + "|" + r.Status
			if seen[key] {
				continue
			}
			seen[key] = true
			if err := e.LogEvent(ctx, semconv.EventAccessSCIM, "SCIM update", at, otellog.SeverityInfo, telemetry.Attr{Key: semconv.AttrAccessSCIMResourceType, Value: r.Type}, telemetry.Attr{Key: semconv.AttrAccessSCIMMethod, Value: method}, telemetry.Attr{Key: semconv.AttrAccessSCIMStatus, Value: r.Status}, telemetry.Attr{Key: semconv.AttrAccessSCIMIDPID, Value: r.IDPID}, telemetry.Attr{Key: semconv.AttrAccessSCIMResourceID, Value: r.ResourceID}, telemetry.Attr{Key: semconv.AttrAccessSCIMUserEmail, Value: r.Email}); err != nil {
				return from, err
			}
		}
		if len(rows) < size {
			break
		}
		if page >= 10000 {
			return from, fmt.Errorf("SCIM pagination exceeded 10000 pages")
		}
	}
	return to, nil
}
func registerSCIM(deps collector.Deps) {
	cfg := deps.Config.Collector("access.scim")
	if cfg.Enabled {
		deps.Registry.RegisterWindow(scimCollector{api: deps.API, accountID: deps.Config.Cloudflare.AccountID}, cfg.Interval, cfg.InitialLookback, cfg.MaxWindow)
	}
}
