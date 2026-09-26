package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
)

type fakeAPI struct {
	contract contract
	broken   string
}

func (f fakeAPI) Accounts(context.Context) ([]cfapi.Account, error) {
	if f.broken == "empty-accounts" {
		return nil, nil
	}
	if f.broken == "account-error" {
		return nil, errors.New("account listing not permitted")
	}
	return []cfapi.Account{{ID: "secret-account-id"}}, nil
}
func (f fakeAPI) Zones(context.Context) ([]cfapi.Zone, error) {
	z := cfapi.Zone{ID: "secret-zone-id"}
	z.Account.ID = "secret-account-id"
	return []cfapi.Zone{z}, nil
}
func (f fakeAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) {
	return []cfapi.Gateway{{ID: "secret-gateway-id"}}, nil
}
func (f fakeAPI) DatasetSettings(_ context.Context, scope cfapi.Scope, _ string, dataset string) (cfapi.DatasetSettings, error) {
	if f.broken == "api-error" {
		return cfapi.DatasetSettings{}, errors.New("token-secret account-secret")
	}
	for _, g := range f.contract.GraphQL {
		if g.Scope == scope && g.Dataset == dataset {
			if f.broken == "unentitled-firewall" && dataset == "firewallEventsAdaptiveGroups" {
				return cfapi.DatasetSettings{Enabled: false}, nil
			}
			fields := append([]string{}, g.RequiredFields...)
			if f.broken == "field" && dataset == "cf1AccessLoginsRawGroups" {
				fields = fields[1:]
			}
			return cfapi.DatasetSettings{Enabled: true, AvailableFields: fields, MaxDuration: g.MinimumDuration, NotOlderThan: g.MinimumRetention, MaxPageSize: 100, MaxNumberOfFields: 80}, nil
		}
	}
	return cfapi.DatasetSettings{}, errors.New("unknown dataset")
}

func TestUnentitledOptionalDatasetDoesNotMaskEnabledDatasetDrift(t *testing.T) {
	c, err := loadContract("../../spec/cloudflare/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	if diffs := probe(context.Background(), fakeAPI{contract: c, broken: "unentitled-firewall"}, c); len(diffs) != 0 {
		t.Fatalf("optional unavailable dataset should not fail: %v", diffs)
	}
	changed := c
	changed.GraphQL = append([]graphContract(nil), c.GraphQL...)
	for i := range changed.GraphQL {
		if changed.GraphQL[i].Dataset == "firewallEventsAdaptiveGroups" {
			changed.GraphQL[i].RequiredFields = append(changed.GraphQL[i].RequiredFields, "newField")
		}
	}
	if diffs := probe(context.Background(), fakeAPI{contract: c}, changed); !strings.Contains(strings.Join(diffs, "\n"), "newField") {
		t.Fatalf("enabled dataset drift was masked: %v", diffs)
	}
}

func TestGatewayLogDetailAndBodiesHaveNoListQuery(t *testing.T) {
	for _, name := range []string{"ai-gateway-log-detail", "ai-gateway-log-request", "ai-gateway-log-response"} {
		if query := restProbeQuery(name, time.Now()); len(query) != 0 {
			t.Fatalf("%s received list pagination: %v", name, query)
		}
	}
}
func (f fakeAPI) Get(_ context.Context, path string, _ url.Values, out any) error {
	if f.broken == "api-error" {
		return errors.New("token-secret account-secret")
	}
	if f.broken == "empty-scim" && strings.HasSuffix(path, "/access/logs/scim/updates") {
		*out.(*[]map[string]any) = nil
		return nil
	}
	entry, found := routeForPath(f.contract, path)
	if !found {
		return errors.New("unrecognized test path")
	}
	row := map[string]any{"id": "secret-row-id", "name": "example", "status": "active", "domain": "example.com", "collect_logs": true, "created_at": "example", "action": "example", "allowed": true, "app_domain": "example.com", "ray_id": "secret-ray-id", "provider": "example", "model": "example", "status_code": 200, "usage_metadata": map[string]any{}, "timings": map[string]any{}}
	for _, field := range entry.RequiredFields {
		setField(row, field, "example")
	}
	setField(row, "id", "secret-row-id")
	if f.broken == "rest-field" && entry.Name == "access-apps" {
		delete(row, "domain")
	}
	if entry.Single {
		*out.(*map[string]any) = row
	} else {
		*out.(*[]map[string]any) = []map[string]any{row}
	}
	return nil
}

func (f fakeAPI) GetRaw(_ context.Context, _ string, _ url.Values, out any) error {
	if f.broken == "body-not-found" {
		return &cfapi.HTTPError{Status: 404, Code: 7002}
	}
	if f.broken == "body-error" {
		return &cfapi.HTTPError{Status: 500, Code: 9999}
	}
	if f.broken == "body-invalid" {
		*out.(*json.RawMessage) = json.RawMessage("not json")
		return nil
	}
	*out.(*json.RawMessage) = json.RawMessage(`{"ok":true}`)
	return nil
}

func routeForPath(c contract, path string) (restContract, bool) {
	for _, entry := range c.REST {
		want, got := strings.Split(entry.Path, "/"), strings.Split(path, "/")
		if len(want) != len(got) {
			continue
		}
		matches := true
		for index, part := range want {
			if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
				continue
			}
			if part != got[index] {
				matches = false
				break
			}
		}
		if matches {
			return entry, true
		}
	}
	return restContract{}, false
}

func setField(row map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	current := row
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}

func TestProbeMatchesAndReportsSanitizedDrift(t *testing.T) {
	c, err := loadContract("../../spec/cloudflare/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	if diffs := probe(context.Background(), fakeAPI{contract: c}, c); len(diffs) != 0 {
		t.Fatalf("unexpected drift: %v", diffs)
	}
	changed := c
	changed.GraphQL = append([]graphContract(nil), c.GraphQL...)
	changed.GraphQL[0].RequiredFields = append(append([]string(nil), c.GraphQL[0].RequiredFields...), "deliberateContractEdit")
	if diffs := probe(context.Background(), fakeAPI{contract: c}, changed); len(diffs) == 0 || !strings.Contains(strings.Join(diffs, "\n"), "deliberateContractEdit") {
		t.Fatalf("contract edit did not produce a readable diff: %v", diffs)
	}
	for _, broken := range []string{"field", "rest-field", "api-error"} {
		diffs := probe(context.Background(), fakeAPI{contract: c, broken: broken}, c)
		if len(diffs) == 0 {
			t.Fatalf("%s did not fail", broken)
		}
		joined := strings.Join(diffs, "\n")
		for _, secret := range []string{"secret-account-id", "secret-zone-id", "secret-gateway-id", "secret-row-id", "secret-ray-id", "token-secret", "account-secret"} {
			if strings.Contains(joined, secret) {
				t.Fatalf("%s leaked %s", broken, secret)
			}
		}
	}
	if diffs := probe(context.Background(), fakeAPI{contract: c, broken: "empty-accounts"}, c); len(diffs) != 0 {
		t.Fatalf("zone-derived account failed: %v", diffs)
	}
	if diffs := probe(context.Background(), fakeAPI{contract: c, broken: "account-error"}, c); len(diffs) != 0 {
		t.Fatalf("zone-derived account after listing error failed: %v", diffs)
	}
	if diffs := probe(context.Background(), fakeAPI{contract: c, broken: "empty-scim"}, c); len(diffs) != 0 {
		t.Fatalf("empty SCIM window failed: %v", diffs)
	}
	if diffs := probe(context.Background(), fakeAPI{contract: c, broken: "body-not-found"}, c); len(diffs) != 0 {
		t.Fatalf("documented unavailable body failed: %v", diffs)
	}
	for _, broken := range []string{"body-error", "body-invalid"} {
		if diffs := probe(context.Background(), fakeAPI{contract: c, broken: broken}, c); len(diffs) == 0 {
			t.Fatalf("%s did not fail", broken)
		}
	}
}

func TestContractRejectsUnknownRoute(t *testing.T) {
	c, err := loadContract("../../spec/cloudflare/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	c.REST[0].Path = "/accounts/{account}/secrets"
	if err := validateContract(c); err == nil {
		t.Fatal("accepted arbitrary REST path")
	}
}

func TestContractRequiresGatewayLogListBeforeDetails(t *testing.T) {
	c, err := loadContract("../../spec/cloudflare/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	listIndex, detailIndex := -1, -1
	for index, entry := range c.REST {
		switch entry.Name {
		case "ai-gateway-logs":
			listIndex = index
		case "ai-gateway-log-detail":
			detailIndex = index
		}
	}
	if listIndex < 0 || detailIndex < 0 {
		t.Fatal("contract omitted gateway log list or detail route")
	}
	c.REST[listIndex], c.REST[detailIndex] = c.REST[detailIndex], c.REST[listIndex]
	if err := validateContract(c); err == nil {
		t.Fatal("accepted gateway detail before the log list it depends on")
	}
}
