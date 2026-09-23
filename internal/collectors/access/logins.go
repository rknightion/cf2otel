package access

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	otellog "go.opentelemetry.io/otel/log"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/identity"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

const loginPageSize = 1000

type loginRow struct {
	UserEmail  string    `json:"user_email"`
	UserID     string    `json:"user_id"`
	IPAddress  string    `json:"ip_address"`
	Country    string    `json:"country"`
	AppUID     string    `json:"app_uid"`
	AppName    string    `json:"app_name"`
	AppDomain  string    `json:"app_domain"`
	AppType    string    `json:"app_type"`
	Action     string    `json:"action"`
	Connection string    `json:"connection"`
	Allowed    bool      `json:"allowed"`
	RayID      string    `json:"ray_id"`
	CreatedAt  time.Time `json:"created_at"`
}

type loginPage struct {
	Result     []loginRow `json:"result"`
	ResultInfo struct {
		PerPage int `json:"per_page"`
	} `json:"result_info"`
}

type logins struct{ deps collector.Deps }

func newLogins(deps collector.Deps) *logins    { return &logins{deps: deps} }
func (*logins) Name() string                   { return "access.logins" }
func (*logins) DefaultInterval() time.Duration { return 5 * time.Minute }
func (*logins) Lag() time.Duration             { return time.Minute }

// CollectWindow uses an exclusive upper bound locally. The REST API may include
// both ends, so advancing the persisted time checkpoint cannot re-emit a row.
func (c *logins) CollectWindow(ctx context.Context, from, to time.Time, out telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return time.Time{}, fmt.Errorf("access logins: invalid window")
	}
	if c.deps.Config == nil || c.deps.API == nil {
		return time.Time{}, fmt.Errorf("access logins: missing dependencies")
	}
	path := "/accounts/" + url.PathEscape(c.deps.Config.Cloudflare.AccountID) + "/access/logs/access_requests"
	seen := make(map[string]struct{})
	for pageNum := 1; pageNum <= 10000; pageNum++ {
		if err := ctx.Err(); err != nil {
			return time.Time{}, err
		}
		query := url.Values{
			"since":    {from.UTC().Format(time.RFC3339Nano)},
			"until":    {to.UTC().Format(time.RFC3339Nano)},
			"page":     {strconv.Itoa(pageNum)},
			"per_page": {strconv.Itoa(loginPageSize)},
		}
		var page loginPage
		get := c.deps.API.Get
		if pg, ok := c.deps.API.(cfapi.PageGetter); ok {
			get = pg.GetPage
		}
		if err := get(ctx, path, query, &page); err != nil {
			return time.Time{}, fmt.Errorf("access logins page %d: %w", pageNum, err)
		}
		fresh := 0
		for _, row := range page.Result {
			if row.CreatedAt.IsZero() {
				return time.Time{}, fmt.Errorf("access logins page %d: missing created_at", pageNum)
			}
			if row.RayID == "" {
				return time.Time{}, fmt.Errorf("access logins page %d: missing ray_id", pageNum)
			}
			key := row.RayID + "\x00" + row.CreatedAt.UTC().Format(time.RFC3339Nano)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			fresh++
			if row.CreatedAt.Before(from) || !row.CreatedAt.Before(to) {
				continue
			}
			if !c.deps.Config.Access.IncludeServiceTokens && row.Connection == "nonidentity" {
				continue
			}
			if err := c.emit(ctx, row, out); err != nil {
				return time.Time{}, err
			}
		}
		// total_count is observed as zero with nonempty results. The page's
		// actual length, rather than total_count, determines continuation.
		perPage := page.ResultInfo.PerPage
		if perPage <= 0 {
			perPage = loginPageSize
		}
		if len(page.Result) < perPage {
			return to, nil
		}
		if fresh == 0 {
			return time.Time{}, fmt.Errorf("access logins page %d: full page without new rows", pageNum)
		}
	}
	return time.Time{}, fmt.Errorf("access logins: page limit reached")
}

func (c *logins) emit(ctx context.Context, row loginRow, out telemetry.Emitter) error {
	host, path := splitAppDomain(row.AppDomain)
	attrs := []telemetry.Attr{
		{Key: semconv.AttrAccessUserEmail, Value: row.UserEmail},
		{Key: semconv.AttrAccessUserID, Value: row.UserID},
		{Key: semconv.AttrAccessUserIPAddress, Value: row.IPAddress},
		{Key: semconv.AttrAccessCountry, Value: row.Country},
		{Key: semconv.AttrAccessAppID, Value: row.AppUID},
		{Key: semconv.AttrAccessApp, Value: row.AppName},
		{Key: semconv.AttrAccessAppType, Value: row.AppType},
		{Key: semconv.AttrAccessHost, Value: host},
		{Key: semconv.AttrAccessPath, Value: path},
		{Key: semconv.AttrAccessAction, Value: row.Action},
		{Key: semconv.AttrAccessConnection, Value: row.Connection},
		{Key: semconv.AttrAccessAllowed, Value: strconv.FormatBool(row.Allowed)},
		{Key: semconv.AttrAccessRayID, Value: row.RayID},
	}
	if err := out.LogEvent(ctx, semconv.EventAccessLogin, "Access login", row.CreatedAt, otellog.SeverityInfo, attrs...); err != nil {
		return err
	}
	if row.Allowed && row.UserEmail != "" && c.deps.Identity != nil {
		c.deps.Identity.Observe(identity.Login{ClientIP: row.IPAddress, Host: host, UserEmail: row.UserEmail, RayID: row.RayID, At: row.CreatedAt})
	}
	return nil
}

func splitAppDomain(domain string) (host, path string) {
	if strings.Contains(domain, "://") {
		if u, err := url.Parse(domain); err == nil && u.Host != "" {
			return u.Hostname(), u.EscapedPath()
		}
	}
	host, path, _ = strings.Cut(domain, "/")
	if path != "" {
		path = "/" + path
	}
	return host, path
}

// HydrateIdentity replays recent Access rows into the in-memory identity index
// without emitting duplicate login records or advancing the login checkpoint.
// The REST log has only about a day of retention; older windows fail closed.
func HydrateIdentity(ctx context.Context, cfg *config.Config, api cfapi.Client, idx identity.Index, from, to time.Time) error {
	if from.Before(time.Now().Add(-23 * time.Hour)) {
		return fmt.Errorf("access identity hydration exceeds the REST retention safety window")
	}
	deps := collector.Deps{Config: cfg, API: api, Identity: idx}
	_, err := newLogins(deps).CollectWindow(ctx, from, to, telemetry.NewNoopEmitter())
	return err
}
