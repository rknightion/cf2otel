package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
)

type contract struct {
	Version int             `json:"version"`
	GraphQL []graphContract `json:"graphql"`
	REST    []restContract  `json:"rest"`
}

type graphContract struct {
	Scope            cfapi.Scope `json:"scope"`
	Dataset          string      `json:"dataset"`
	RequiredFields   []string    `json:"required_fields"`
	MinimumDuration  int64       `json:"minimum_max_duration_seconds"`
	MinimumRetention int64       `json:"minimum_not_older_than_seconds"`
	AllowDisabled    bool        `json:"allow_disabled,omitempty"`
}

type restContract struct {
	Name           string   `json:"name"`
	Scope          string   `json:"scope"`
	Path           string   `json:"path"`
	RequiredFields []string `json:"required_fields"`
	AllowEmpty     bool     `json:"allow_empty,omitempty"`
	Single         bool     `json:"single,omitempty"`
	RawJSON        bool     `json:"raw_json,omitempty"`
}

type probeAPI interface {
	Accounts(context.Context) ([]cfapi.Account, error)
	Zones(context.Context) ([]cfapi.Zone, error)
	Gateways(context.Context, string) ([]cfapi.Gateway, error)
	DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error)
	Get(context.Context, string, url.Values, any) error
}

type rawProbeAPI interface {
	GetRaw(context.Context, string, url.Values, any) error
}

var fieldName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)
var datasetName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var restPaths = map[string]string{
	"zone-list":               "/zones",
	"access-apps":             "/accounts/{account}/access/apps",
	"access-users":            "/accounts/{account}/access/users",
	"access-logins":           "/accounts/{account}/access/logs/access_requests",
	"access-scim":             "/accounts/{account}/access/logs/scim/updates",
	"ai-gateways":             "/accounts/{account}/ai-gateway/gateways",
	"ai-gateway-logs":         "/accounts/{account}/ai-gateway/gateways/{gateway}/logs",
	"ai-gateway-log-detail":   "/accounts/{account}/ai-gateway/gateways/{gateway}/logs/{id}",
	"ai-gateway-log-request":  "/accounts/{account}/ai-gateway/gateways/{gateway}/logs/{id}/request",
	"ai-gateway-log-response": "/accounts/{account}/ai-gateway/gateways/{gateway}/logs/{id}/response",
	"audit-logs":              "/accounts/{account}/logs/audit",
}

func loadContract(path string) (contract, error) {
	var c contract
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	if d.Decode(new(any)) != io.EOF {
		return c, errors.New("contract has trailing content")
	}
	return c, validateContract(c)
}

func validateContract(c contract) error {
	if c.Version != 1 || len(c.GraphQL) == 0 || len(c.GraphQL) > 64 || len(c.REST) == 0 || len(c.REST) > len(restPaths) {
		return errors.New("invalid contract version or probe count")
	}
	seen := map[string]bool{}
	for _, g := range c.GraphQL {
		key := string(g.Scope) + "/" + g.Dataset
		if (g.Scope != cfapi.AccountScope && g.Scope != cfapi.ZoneScope) || !datasetName.MatchString(g.Dataset) || seen[key] || g.MinimumDuration <= 0 || g.MinimumRetention <= 0 || !validFields(g.RequiredFields) || (g.AllowDisabled && (g.Scope != cfapi.ZoneScope || g.Dataset != "firewallEventsAdaptiveGroups")) {
			return errors.New("invalid GraphQL contract")
		}
		seen[key] = true
	}
	restSeen := map[string]bool{}
	gatewayLogListIndex := -1
	hasGatewayLogPath := false
	for index, r := range c.REST {
		if r.Name == "ai-gateway-logs" {
			gatewayLogListIndex = index
		}
		if r.Scope == "gateway-log" {
			hasGatewayLogPath = true
			if gatewayLogListIndex < 0 {
				return errors.New("gateway log list must precede detail and body paths")
			}
		}
		path, ok := restPaths[r.Name]
		fieldsValid := validFields(r.RequiredFields)
		if r.RawJSON {
			fieldsValid = len(r.RequiredFields) == 0
		}
		if !ok || r.Path != path || restSeen[r.Name] || !fieldsValid || (r.Scope != "global" && r.Scope != "account" && r.Scope != "gateway" && r.Scope != "gateway-log") {
			return errors.New("invalid REST contract")
		}
		if (r.Scope == "global") != !strings.Contains(r.Path, "{account}") {
			return errors.New("invalid REST scope")
		}
		if ((r.Scope == "gateway") || (r.Scope == "gateway-log")) != strings.Contains(r.Path, "{gateway}") {
			return errors.New("invalid gateway scope")
		}
		if (r.Scope == "gateway-log") != strings.Contains(r.Path, "{id}") || (r.RawJSON && r.Scope != "gateway-log") || (r.Single && r.RawJSON) {
			return errors.New("invalid REST response shape")
		}
		restSeen[r.Name] = true
	}
	if hasGatewayLogPath && gatewayLogListIndex < 0 {
		return errors.New("gateway log detail paths require the gateway log list")
	}
	return nil
}

func validFields(fields []string) bool {
	if len(fields) == 0 || len(fields) > 80 {
		return false
	}
	seen := map[string]bool{}
	for _, field := range fields {
		if !fieldName.MatchString(field) || seen[field] {
			return false
		}
		seen[field] = true
	}
	return true
}

// probe returns only contract names, field names and numeric limits. It never
// formats scope IDs, response values, tokens or Cloudflare error bodies.
func probe(ctx context.Context, api probeAPI, c contract) []string {
	var diffs []string
	// Schema-only tokens may read zones but be denied account listing. A zone's
	// account.id supplies the same scope identifier without widening token access.
	accounts, _ := api.Accounts(ctx)
	zones, err := api.Zones(ctx)
	if err != nil {
		return []string{"zone discovery failed"}
	}
	if len(accounts) == 0 {
		seen := map[string]bool{}
		for _, zone := range zones {
			if zone.Account.ID != "" && !seen[zone.Account.ID] {
				accounts = append(accounts, cfapi.Account{ID: zone.Account.ID})
				seen[zone.Account.ID] = true
			}
		}
	}
	if len(accounts) == 0 || len(accounts) > 4 || len(zones) == 0 || len(zones) > 32 {
		return []string{"scope count outside bounded probe range"}
	}
	for _, g := range c.GraphQL {
		ids := make([]string, 0)
		if g.Scope == cfapi.AccountScope {
			for _, account := range accounts {
				ids = append(ids, account.ID)
			}
		} else {
			for _, zone := range zones {
				ids = append(ids, zone.ID)
			}
		}
		for index, id := range ids {
			label := fmt.Sprintf("GraphQL %s/%s scope #%d", g.Scope, g.Dataset, index+1)
			s, err := api.DatasetSettings(ctx, g.Scope, id, g.Dataset)
			if err != nil {
				diffs = append(diffs, label+": settings request failed")
				continue
			}
			if !s.Enabled {
				if g.AllowDisabled {
					continue
				}
				diffs = append(diffs, label+": dataset disabled")
			}
			for _, field := range g.RequiredFields {
				if !hasAvailableField(s.AvailableFields, field) {
					diffs = append(diffs, label+": missing field "+field)
				}
			}
			if s.MaxDuration < g.MinimumDuration {
				diffs = append(diffs, fmt.Sprintf("%s: maxDuration=%d below minimum %d", label, s.MaxDuration, g.MinimumDuration))
			}
			if s.NotOlderThan < g.MinimumRetention {
				diffs = append(diffs, fmt.Sprintf("%s: notOlderThan=%d below minimum %d", label, s.NotOlderThan, g.MinimumRetention))
			}
			if s.MaxPageSize < 100 || s.MaxNumberOfFields < len(g.RequiredFields) {
				diffs = append(diffs, label+": page or field limit below collector requirement")
			}
		}
	}
	gatewayLogIDs := map[string]string{}
	now := time.Now().UTC()
	for _, r := range c.REST {
		type target struct{ account, gateway, id string }
		targets := []target{{}}
		if r.Scope != "global" {
			targets = targets[:0]
			for _, account := range accounts {
				if r.Scope == "account" {
					targets = append(targets, target{account: account.ID})
					continue
				}
				gateways, err := api.Gateways(ctx, account.ID)
				if err != nil || len(gateways) > 4 {
					diffs = append(diffs, "REST "+r.Name+": gateway discovery failed or count outside bound")
					continue
				}
				for _, gateway := range gateways {
					if r.Scope == "gateway-log" {
						if id := gatewayLogIDs[account.ID+"/"+gateway.ID]; id != "" {
							targets = append(targets, target{account: account.ID, gateway: gateway.ID, id: id})
						}
						continue
					}
					targets = append(targets, target{account: account.ID, gateway: gateway.ID})
				}
			}
		}
		if len(targets) == 0 && r.AllowEmpty {
			continue
		}
		for index, t := range targets {
			label := fmt.Sprintf("REST %s scope #%d", r.Name, index+1)
			path := strings.ReplaceAll(r.Path, "{account}", url.PathEscape(t.account))
			path = strings.ReplaceAll(path, "{gateway}", url.PathEscape(t.gateway))
			path = strings.ReplaceAll(path, "{id}", url.PathEscape(t.id))
			query := restProbeQuery(r.Name, now)
			if r.RawJSON {
				getter, ok := api.(rawProbeAPI)
				if !ok {
					diffs = append(diffs, label+": raw JSON reader unavailable")
					continue
				}
				var body json.RawMessage
				err := getter.GetRaw(ctx, path, query, &body)
				if isUnavailableBody(err) {
					continue
				}
				if err != nil {
					diffs = append(diffs, label+": read failed")
					continue
				}
				if len(body) == 0 || !json.Valid(body) {
					diffs = append(diffs, label+": invalid raw JSON response")
				}
				continue
			}
			var rows []map[string]any
			if r.Single {
				var row map[string]any
				if err := api.Get(ctx, path, query, &row); err != nil {
					diffs = append(diffs, label+": read failed")
					continue
				}
				if row != nil {
					rows = append(rows, row)
				}
			} else if err := api.Get(ctx, path, query, &rows); err != nil {
				diffs = append(diffs, label+": read failed")
				continue
			}
			if len(rows) == 0 {
				if !r.AllowEmpty {
					diffs = append(diffs, label+": no row available for shape check")
				}
				continue
			}
			for _, field := range r.RequiredFields {
				if !hasField(rows[0], field) {
					diffs = append(diffs, label+": missing field "+field)
				}
			}
			if r.Name == "ai-gateway-logs" {
				if id, ok := rows[0]["id"].(string); ok && id != "" {
					gatewayLogIDs[t.account+"/"+t.gateway] = id
				}
			}
		}
	}
	return diffs
}

func hasAvailableField(available []string, required string) bool {
	required = strings.ToLower(strings.ReplaceAll(required, ".", "_"))
	for _, field := range available {
		field = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(field), ".", "_"))
		if field == required {
			return true
		}
	}
	return false
}

func restProbeQuery(name string, now time.Time) url.Values {
	if strings.HasPrefix(name, "ai-gateway-log-") {
		return nil
	}
	from := now.Add(-time.Hour).Format(time.RFC3339Nano)
	to := now.Format(time.RFC3339Nano)
	switch name {
	case "access-logins":
		return url.Values{"since": {from}, "until": {to}, "page": {"1"}, "per_page": {"1"}}
	case "access-scim":
		return url.Values{"since": {from}, "until": {to}, "page": {"1"}, "limit": {"1"}, "direction": {"asc"}}
	case "audit-logs":
		return url.Values{"since": {from}, "before": {to}, "limit": {"1"}}
	case "ai-gateway-logs":
		return url.Values{"page": {"1"}, "per_page": {"50"}, "order_by": {"created_at"}, "order_by_direction": {"desc"}}
	default:
		return url.Values{"page": {"1"}, "per_page": {"1"}}
	}
}

func isUnavailableBody(err error) bool {
	var httpErr *cfapi.HTTPError
	return errors.As(err, &httpErr) && httpErr.Status == 404 && httpErr.Code == 7002
}

func hasField(row map[string]any, path string) bool {
	var current any = row
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return false
		}
		current, ok = object[part]
		if !ok {
			return false
		}
	}
	return true
}
