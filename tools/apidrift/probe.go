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
	"slices"
	"strings"

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
}

type restContract struct {
	Name           string   `json:"name"`
	Scope          string   `json:"scope"`
	Path           string   `json:"path"`
	RequiredFields []string `json:"required_fields"`
}

type probeAPI interface {
	Accounts(context.Context) ([]cfapi.Account, error)
	Zones(context.Context) ([]cfapi.Zone, error)
	Gateways(context.Context, string) ([]cfapi.Gateway, error)
	DatasetSettings(context.Context, cfapi.Scope, string, string) (cfapi.DatasetSettings, error)
	Get(context.Context, string, url.Values, any) error
}

var fieldName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)
var datasetName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var restPaths = map[string]string{
	"account-list":    "/accounts",
	"zone-list":       "/zones",
	"access-apps":     "/accounts/{account}/access/apps",
	"access-logins":   "/accounts/{account}/access/logs/access_requests",
	"ai-gateways":     "/accounts/{account}/ai-gateway/gateways",
	"ai-gateway-logs": "/accounts/{account}/ai-gateway/gateways/{gateway}/logs",
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
	if c.Version != 1 || len(c.GraphQL) == 0 || len(c.GraphQL) > 16 || len(c.REST) == 0 || len(c.REST) > len(restPaths) {
		return errors.New("invalid contract version or probe count")
	}
	seen := map[string]bool{}
	for _, g := range c.GraphQL {
		key := string(g.Scope) + "/" + g.Dataset
		if (g.Scope != cfapi.AccountScope && g.Scope != cfapi.ZoneScope) || !datasetName.MatchString(g.Dataset) || seen[key] || g.MinimumDuration <= 0 || g.MinimumRetention <= 0 || !validFields(g.RequiredFields) {
			return errors.New("invalid GraphQL contract")
		}
		seen[key] = true
	}
	for _, r := range c.REST {
		path, ok := restPaths[r.Name]
		if !ok || r.Path != path || seen[r.Name] || !validFields(r.RequiredFields) || (r.Scope != "global" && r.Scope != "account" && r.Scope != "gateway") {
			return errors.New("invalid REST contract")
		}
		if (r.Scope == "global") != !strings.Contains(r.Path, "{account}") {
			return errors.New("invalid REST scope")
		}
		if (r.Scope == "gateway") != strings.Contains(r.Path, "{gateway}") {
			return errors.New("invalid gateway scope")
		}
		seen[r.Name] = true
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
	accounts, err := api.Accounts(ctx)
	if err != nil {
		return []string{"account discovery failed"}
	}
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
				diffs = append(diffs, label+": dataset disabled")
			}
			for _, field := range g.RequiredFields {
				if !slices.Contains(s.AvailableFields, field) {
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
	for _, r := range c.REST {
		type target struct{ account, gateway string }
		targets := []target{{}}
		if r.Scope != "global" {
			targets = targets[:0]
			for _, account := range accounts {
				if r.Scope == "account" {
					targets = append(targets, target{account: account.ID})
					continue
				}
				gateways, err := api.Gateways(ctx, account.ID)
				if err != nil || len(gateways) == 0 || len(gateways) > 4 {
					diffs = append(diffs, "REST "+r.Name+": gateway discovery failed or count outside bound")
					continue
				}
				for _, gateway := range gateways {
					targets = append(targets, target{account: account.ID, gateway: gateway.ID})
				}
			}
		}
		for index, t := range targets {
			label := fmt.Sprintf("REST %s scope #%d", r.Name, index+1)
			path := strings.ReplaceAll(r.Path, "{account}", url.PathEscape(t.account))
			path = strings.ReplaceAll(path, "{gateway}", url.PathEscape(t.gateway))
			var rows []map[string]any
			if err := api.Get(ctx, path, url.Values{"page": {"1"}, "per_page": {"1"}}, &rows); err != nil {
				diffs = append(diffs, label+": read failed")
				continue
			}
			if len(rows) == 0 {
				diffs = append(diffs, label+": no row available for shape check")
				continue
			}
			for _, field := range r.RequiredFields {
				if !hasField(rows[0], field) {
					diffs = append(diffs, label+": missing field "+field)
				}
			}
		}
	}
	return diffs
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
