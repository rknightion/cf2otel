package main

import (
	"bytes"
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
	Name                         string              `json:"name"`
	Scope                        string              `json:"scope"`
	Path                         string              `json:"path"`
	RequiredFields               []string            `json:"required_fields"`
	AllowEmpty                   bool                `json:"allow_empty,omitempty"`
	Single                       bool                `json:"single,omitempty"`
	RawJSON                      bool                `json:"raw_json,omitempty"`
	CheckAllRows                 bool                `json:"check_all_rows,omitempty"`
	OptionalWhenDestinationTypes map[string][]string `json:"optional_when_destination_types,omitempty"`
	RequiredInAnyRow             []string            `json:"required_in_any_row,omitempty"`
	invalidNullRowRule           bool
}

func (r *restContract) UnmarshalJSON(data []byte) error {
	type restContractFields restContract
	var fields restContractFields
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fields); err != nil {
		return err
	}
	*r = restContract(fields)

	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawFields); err != nil {
		return err
	}
	for name, raw := range rawFields {
		if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			continue
		}
		if strings.EqualFold(name, "check_all_rows") || strings.EqualFold(name, "optional_when_destination_types") || strings.EqualFold(name, "required_in_any_row") {
			r.invalidNullRowRule = true
			break
		}
	}
	return nil
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

type pageProbeAPI interface {
	GetPage(context.Context, string, url.Values, any) error
}

var fieldName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)
var datasetName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var restPaths = map[string]string{
	"d1-databases":            "/accounts/{account}/d1/database",
	"kv-namespaces":           "/accounts/{account}/storage/kv/namespaces",
	"queues-list":             "/accounts/{account}/queues",
	"do-namespaces":           "/accounts/{account}/workers/durable_objects/namespaces",
	"warp-devices":            "/accounts/{account}/dex/fleet-status/devices",
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
	"cfd-tunnel-list":         "/accounts/{account}/cfd_tunnel",
	"certificate-packs":       "/zones/{zone}/ssl/certificate_packs",
	"firewall-rulesets":       "/zones/{zone}/rulesets",
	"firewall-ruleset-detail": "/zones/{zone}/rulesets/{id}",
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
		if (g.Scope != cfapi.AccountScope && g.Scope != cfapi.ZoneScope) || !datasetName.MatchString(g.Dataset) || seen[key] || g.MinimumDuration <= 0 || g.MinimumRetention <= 0 || !validFields(g.RequiredFields) || (g.AllowDisabled && (g.Scope != cfapi.ZoneScope || g.Dataset != "firewallEventsAdaptiveGroups" && g.Dataset != "healthCheckEventsAdaptiveGroups")) {
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
		if r.Name == "firewall-ruleset-detail" && (!restSeen["firewall-rulesets"] || !r.Single) {
			return errors.New("firewall ruleset list must precede single detail path")
		}
		path, ok := restPaths[r.Name]
		fieldsValid := validFields(r.RequiredFields)
		if r.RawJSON {
			fieldsValid = len(r.RequiredFields) == 0
		}
		if !ok || r.Path != path || restSeen[r.Name] || !fieldsValid || !validRESTOptionalRules(r) || (r.Scope != "global" && r.Scope != "account" && r.Scope != "zone" && r.Scope != "gateway" && r.Scope != "gateway-log") {
			return errors.New("invalid REST contract")
		}
		if (r.Scope == "zone") != strings.Contains(r.Path, "{zone}") {
			return errors.New("invalid REST zone scope")
		}
		if (r.Scope == "global" || r.Scope == "zone") != !strings.Contains(r.Path, "{account}") {
			return errors.New("invalid REST scope")
		}
		if ((r.Scope == "gateway") || (r.Scope == "gateway-log")) != strings.Contains(r.Path, "{gateway}") {
			return errors.New("invalid gateway scope")
		}
		if (r.Scope == "gateway-log" || r.Name == "firewall-ruleset-detail") != strings.Contains(r.Path, "{id}") || (r.RawJSON && r.Scope != "gateway-log") || (r.Single && r.RawJSON) {
			return errors.New("invalid REST response shape")
		}
		restSeen[r.Name] = true
	}
	if hasGatewayLogPath && gatewayLogListIndex < 0 {
		return errors.New("gateway log detail paths require the gateway log list")
	}
	return nil
}

func validRESTOptionalRules(r restContract) bool {
	if r.invalidNullRowRule {
		return false
	}
	if r.RawJSON || r.Single {
		return !r.CheckAllRows && r.OptionalWhenDestinationTypes == nil && r.RequiredInAnyRow == nil
	}
	required := make(map[string]bool, len(r.RequiredFields))
	for _, field := range r.RequiredFields {
		required[field] = true
	}
	if r.RequiredInAnyRow != nil {
		if !validFields(r.RequiredInAnyRow) {
			return false
		}
		for _, field := range r.RequiredInAnyRow {
			if required[field] {
				return false
			}
		}
	}
	if r.OptionalWhenDestinationTypes == nil {
		return true
	}
	if len(r.OptionalWhenDestinationTypes) == 0 {
		return false
	}
	for field, destinationTypes := range r.OptionalWhenDestinationTypes {
		if !required[field] || len(destinationTypes) == 0 {
			return false
		}
		for _, destinationType := range destinationTypes {
			if strings.TrimSpace(destinationType) == "" {
				return false
			}
		}
	}
	return true
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
	firewallRulesetIDs := map[string]string{}
	gatewayLogIDs := map[string]string{}
	now := time.Now().UTC()
	for _, r := range c.REST {
		type target struct{ account, zone, gateway, id string }
		targets := []target{{}}
		if r.Scope == "zone" {
			targets = targets[:0]
			for _, zone := range zones {
				if r.Name == "firewall-ruleset-detail" && firewallRulesetIDs[zone.ID] == "" {
					continue
				}
				targets = append(targets, target{zone: zone.ID, id: firewallRulesetIDs[zone.ID]})
			}
		} else if r.Scope != "global" {
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
			path = strings.ReplaceAll(path, "{zone}", url.PathEscape(t.zone))
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
			var totalCount int
			if r.Single {
				var row map[string]any
				if err := api.Get(ctx, path, query, &row); err != nil {
					diffs = append(diffs, label+": read failed")
					continue
				}
				if row != nil {
					rows = append(rows, row)
				}
			} else if r.CheckAllRows {
				if getter, ok := api.(pageProbeAPI); ok {
					var page cfapi.Page
					if err := getter.GetPage(ctx, path, query, &page); err != nil || json.Unmarshal(page.Result, &rows) != nil {
						diffs = append(diffs, label+": read failed")
						continue
					}
					totalCount = page.ResultInfo.TotalCount
				} else if err := api.Get(ctx, path, query, &rows); err != nil {
					diffs = append(diffs, label+": read failed")
					continue
				}
			} else if err := api.Get(ctx, path, query, &rows); err != nil {
				diffs = append(diffs, label+": read failed")
				continue
			}
			if r.Name == "firewall-rulesets" {
				for _, row := range rows {
					phase, _ := row["phase"].(string)
					id, _ := row["id"].(string)
					if id != "" && (phase == "http_request_firewall_custom" || phase == "http_request_firewall_managed") {
						firewallRulesetIDs[t.zone] = id
						break
					}
				}
			}
			if r.CheckAllRows && totalCount > len(rows) {
				diffs = append(diffs, fmt.Sprintf("%s: checked %d of %d rows", label, len(rows), totalCount))
			}
			if len(rows) == 0 {
				if !r.AllowEmpty {
					diffs = append(diffs, label+": no row available for shape check")
				}
				continue
			}
			rowsToCheck := rows[:1]
			if r.CheckAllRows {
				rowsToCheck = rows
			}
			for _, field := range r.RequiredFields {
				missing := 0
				for _, row := range rowsToCheck {
					allowedTypes, conditional := r.OptionalWhenDestinationTypes[field]
					present := hasField(row, field)
					if conditional {
						present = hasNonNullField(row, field)
					}
					if present {
						continue
					}
					if conditional && destinationsHaveOnlyTypes(row, allowedTypes) {
						continue
					}
					missing++
				}
				if missing == 0 {
					continue
				}
				diff := label + ": missing field " + field
				if r.CheckAllRows {
					diff += fmt.Sprintf(" (%d of %d rows)", missing, len(rows))
				}
				diffs = append(diffs, diff)
			}
			for _, field := range r.RequiredInAnyRow {
				found := false
				for _, row := range rows {
					if hasNonNullField(row, field) {
						found = true
						break
					}
				}
				if !found {
					diffs = append(diffs, fmt.Sprintf("%s: field %s absent from all %d rows", label, field, len(rows)))
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
	if name == "firewall-rulesets" || name == "firewall-ruleset-detail" {
		return nil
	}
	if strings.HasPrefix(name, "ai-gateway-log-") {
		return nil
	}
	from := now.Add(-time.Hour).Format(time.RFC3339Nano)
	to := now.Format(time.RFC3339Nano)
	switch name {
	case "d1-databases", "kv-namespaces", "queues-list", "do-namespaces":
		return url.Values{"page": {"1"}, "per_page": {"50"}}
	case "warp-devices":
		return url.Values{"page": {"1"}, "per_page": {"50"}, "source": {"last_seen"}, "from": {now.Add(-15 * time.Minute).UTC().Format("2006-01-02T15:04:05.000Z")}, "to": {now.UTC().Format("2006-01-02T15:04:05.000Z")}}
	case "access-logins":
		return url.Values{"since": {from}, "until": {to}, "page": {"1"}, "per_page": {"1"}}
	case "access-scim":
		return url.Values{"since": {from}, "until": {to}, "page": {"1"}, "limit": {"50"}, "direction": {"asc"}}
	case "cfd-tunnel-list":
		return url.Values{"page": {"1"}, "per_page": {"1"}, "is_deleted": {"false"}}
	case "certificate-packs":
		return url.Values{"page": {"1"}, "per_page": {"5"}, "status": {"all"}}
	case "access-apps":
		return url.Values{"page": {"1"}, "per_page": {"1000"}}
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
	_, ok := fieldValue(row, path)
	return ok
}

func hasNonNullField(row map[string]any, path string) bool {
	value, ok := fieldValue(row, path)
	return ok && value != nil
}

func fieldValue(row map[string]any, path string) (any, bool) {
	var current any = row
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func destinationsHaveOnlyTypes(row map[string]any, allowedTypes []string) bool {
	destinationsValue, ok := row["destinations"]
	if !ok {
		return false
	}
	destinations, ok := destinationsValue.([]any)
	if !ok || len(destinations) == 0 {
		return false
	}
	allowed := make(map[string]bool, len(allowedTypes))
	for _, destinationType := range allowedTypes {
		allowed[destinationType] = true
	}
	for _, destinationValue := range destinations {
		destination, ok := destinationValue.(map[string]any)
		if !ok {
			return false
		}
		destinationType, ok := destination["type"].(string)
		if !ok || !allowed[destinationType] {
			return false
		}
	}
	return true
}
