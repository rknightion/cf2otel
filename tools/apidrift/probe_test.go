package main

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

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
			fields := append([]string{}, g.RequiredFields...)
			if f.broken == "field" && dataset == "cf1AccessLoginsRawGroups" {
				fields = fields[1:]
			}
			return cfapi.DatasetSettings{Enabled: true, AvailableFields: fields, MaxDuration: g.MinimumDuration, NotOlderThan: g.MinimumRetention, MaxPageSize: 100, MaxNumberOfFields: 80}, nil
		}
	}
	return cfapi.DatasetSettings{}, errors.New("unknown dataset")
}
func (f fakeAPI) Get(_ context.Context, path string, _ url.Values, out any) error {
	if f.broken == "api-error" {
		return errors.New("token-secret account-secret")
	}
	row := map[string]any{"id": "secret-row-id", "name": "example", "status": "active", "domain": "example.com", "collect_logs": true, "created_at": "example", "action": "example", "allowed": true, "app_domain": "example.com", "ray_id": "secret-ray-id", "provider": "example", "model": "example", "status_code": 200, "usage_metadata": map[string]any{}, "timings": map[string]any{}}
	if f.broken == "rest-field" && strings.HasSuffix(path, "/access/apps") {
		delete(row, "domain")
	}
	*out.(*[]map[string]any) = []map[string]any{row}
	return nil
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
