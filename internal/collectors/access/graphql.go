package access

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type loginMetrics struct {
	api       cfapi.Client
	accountID string
}

func (loginMetrics) Name() string                   { return "access.login_metrics" }
func (loginMetrics) DefaultInterval() time.Duration { return 5 * time.Minute }
func (loginMetrics) Lag() time.Duration             { return 2 * time.Minute }
func (c loginMetrics) CollectWindow(ctx context.Context, from, to time.Time, e telemetry.Emitter) (time.Time, error) {
	if !from.Before(to) {
		return to, nil
	}
	if c.api == nil || c.accountID == "" {
		return from, fmt.Errorf("access login metrics requires API and account ID")
	}
	type point struct {
		name  string
		value float64
		attrs []telemetry.Attr
	}
	var pending []point
	for start := from; start.Before(to); {
		end := start.Add(3 * time.Hour)
		if end.After(to) {
			end = to
		}
		var rows []struct {
			Dimensions struct {
				AppName   string `json:"appName"`
				IDP       string `json:"idp"`
				LoginType string `json:"loginType"`
				Allowed   string `json:"allowed"`
			} `json:"dimensions"`
			Sum struct {
				Logins float64 `json:"logins"`
			} `json:"sum"`
		}
		err := c.api.Query(ctx, cfapi.GraphQLRequest{Scope: cfapi.AccountScope, ScopeID: c.accountID, Dataset: "cf1AccessLoginsRawGroups", WantedFields: []string{"dimensions.appName", "dimensions.idp", "dimensions.loginType", "dimensions.allowed", "sum.logins"}, From: start, To: end, Limit: 10000}, &rows)
		if err != nil {
			return from, fmt.Errorf("access login groups: %w", err)
		}
		for _, r := range rows {
			if r.Sum.Logins <= 0 {
				continue
			}
			pending = append(pending, point{semconv.MetricAccessLogins, r.Sum.Logins, []telemetry.Attr{{Key: semconv.AttrAccessApp, Value: r.Dimensions.AppName}, {Key: semconv.AttrAccessIdentityProvider, Value: identityClass(r.Dimensions.IDP)}, {Key: semconv.AttrAccessLoginType, Value: r.Dimensions.LoginType}, {Key: semconv.AttrAccessAllowed, Value: r.Dimensions.Allowed}}})
		}
		start = end
	}
	var requests []struct {
		Count      float64 `json:"count"`
		Dimensions struct {
			AppID            string `json:"appId"`
			IdentityProvider string `json:"identityProvider"`
			ServiceTokenID   string `json:"serviceTokenId"`
			Successful       int    `json:"isSuccessfulLogin"`
		} `json:"dimensions"`
	}
	err := c.api.Query(ctx, cfapi.GraphQLRequest{Scope: cfapi.AccountScope, ScopeID: c.accountID, Dataset: "accessLoginRequestsAdaptiveGroups", WantedFields: []string{"count", "dimensions.appId", "dimensions.identityProvider", "dimensions.serviceTokenId", "dimensions.isSuccessfulLogin"}, From: from, To: to, Limit: 10000}, &requests)
	if err != nil {
		return from, fmt.Errorf("access request groups: %w", err)
	}
	for _, r := range requests {
		if r.Count <= 0 {
			continue
		}
		token := "false"
		if r.Dimensions.ServiceTokenID != "" {
			token = "true"
		}
		success := "false"
		if r.Dimensions.Successful != 0 {
			success = "true"
		}
		pending = append(pending, point{semconv.MetricAccessRequests, r.Count, []telemetry.Attr{{Key: semconv.AttrAccessAppID, Value: r.Dimensions.AppID}, {Key: semconv.AttrAccessIdentityProvider, Value: identityClass(r.Dimensions.IdentityProvider)}, {Key: semconv.AttrAccessServiceToken, Value: token}, {Key: semconv.AttrAccessAllowed, Value: success}}})
	}
	for _, p := range pending {
		if err := e.Counter(ctx, p.name, p.value, p.attrs...); err != nil {
			return from, err
		}
	}
	return to, nil
}
func identityClass(s string) string {
	if strings.EqualFold(s, "nonidentity") || s == "" {
		return "nonidentity"
	}
	return s
}
func registerLoginMetrics(deps collector.Deps) {
	cfg := deps.Config.Collector("access.login_metrics")
	if cfg.Enabled {
		deps.Registry.RegisterWindow(loginMetrics{api: deps.API, accountID: deps.Config.Cloudflare.AccountID}, cfg.Interval, cfg.InitialLookback, cfg.MaxWindow)
	}
}
