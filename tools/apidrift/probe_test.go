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
	restRows map[string][]map[string]any
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
	if rows, ok := f.restRows[entry.Name]; ok {
		if entry.Single {
			if len(rows) > 0 {
				*out.(*map[string]any) = rows[0]
			}
		} else {
			*out.(*[]map[string]any) = rows
		}
		return nil
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

func TestAccessAppsContractAllowsOnlyDocumentedDomainlessDestinationTypes(t *testing.T) {
	c := loadTestContract(t)
	entry := restEntry(t, &c, "access-apps")
	if !entry.CheckAllRows {
		t.Fatal("access-apps must check every returned row")
	}
	want := map[string][]string{"domain": {"worker", "all_preview_workers"}}
	if !equalDestinationTypes(entry.OptionalWhenDestinationTypes, want) {
		t.Fatalf("unexpected domain exceptions: got %v, want %v", entry.OptionalWhenDestinationTypes, want)
	}
}

func TestAccessAppsAcceptsDomainAndPureWorkerRowsInEitherOrder(t *testing.T) {
	for _, destinationType := range []string{"worker", "all_preview_workers"} {
		for _, reverse := range []bool{false, true} {
			name := destinationType + "/domain-first"
			if reverse {
				name = destinationType + "/worker-first"
			}
			t.Run(name, func(t *testing.T) {
				c := accessAppsExceptionContract(t)
				domainRow := map[string]any{"id": "domain", "name": "domain app", "domain": "example.com"}
				workerRow := map[string]any{
					"id": "worker", "name": "worker app",
					"destinations": []any{map[string]any{"type": destinationType}},
				}
				rows := []map[string]any{domainRow, workerRow}
				if reverse {
					rows[0], rows[1] = rows[1], rows[0]
				}
				api := fakeAPI{contract: c, restRows: map[string][]map[string]any{"access-apps": rows}}
				if diffs := probe(context.Background(), api, c); hasRESTDiff(diffs, "access-apps") {
					t.Fatalf("valid domain and %s rows failed in order %v: %v", destinationType, reverse, diffs)
				}
			})
		}
	}
}

func TestAccessAppsRequiresDomainForPublicMixedAndMalformedDestinations(t *testing.T) {
	invalidDestinations := []struct {
		name string
		row  map[string]any
	}{
		{name: "public", row: map[string]any{"destinations": []any{map[string]any{"type": "public"}}}},
		{name: "mixed", row: map[string]any{"destinations": []any{map[string]any{"type": "worker"}, map[string]any{"type": "public"}}}},
		{name: "absent", row: map[string]any{"name": "domainless app"}},
		{name: "empty", row: map[string]any{"destinations": []any{}}},
		{name: "not-array", row: map[string]any{"destinations": "worker"}},
		{name: "not-object", row: map[string]any{"destinations": []any{"worker"}}},
		{name: "missing-type", row: map[string]any{"destinations": []any{map[string]any{"kind": "worker"}}}},
		{name: "non-string-type", row: map[string]any{"destinations": []any{map[string]any{"type": 7}}}},
	}
	for _, invalid := range invalidDestinations {
		t.Run(invalid.name, func(t *testing.T) {
			c := accessAppsExceptionContract(t)
			domainRow := map[string]any{"id": "domain", "name": "domain app", "domain": "example.com"}
			workerRow := map[string]any{"id": "invalid", "name": "domainless app"}
			for key, value := range invalid.row {
				workerRow[key] = value
			}
			anotherBadRow := map[string]any{"id": "invalid-again", "name": "another domainless app"}
			for key, value := range invalid.row {
				anotherBadRow[key] = value
			}
			api := fakeAPI{contract: c, restRows: map[string][]map[string]any{"access-apps": {domainRow, workerRow, anotherBadRow}}}
			diffs := probe(context.Background(), api, c)
			want := "REST access-apps scope #1: missing field domain (2 of 3 rows)"
			count := 0
			for _, diff := range diffs {
				if diff == want {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("bad row after valid first row did not produce %q: %v", want, diffs)
			}
		})
	}
}

func TestRESTEntriesWithoutNewRowRulesKeepFirstRowBehaviorAndMessages(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
	}{
		{name: "access-apps", field: "domain"},
		{name: "access-users", field: "id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := loadTestContract(t)
			entry := restEntry(t, &c, tc.name)
			entry.CheckAllRows = false
			entry.OptionalWhenDestinationTypes = nil

			missingFirst := map[string]any{"id": "present", "name": "example", "domain": "example.com"}
			delete(missingFirst, tc.field)
			second := map[string]any{"id": "present", "name": "example", "domain": "example.com"}
			api := fakeAPI{contract: c, restRows: map[string][]map[string]any{tc.name: {missingFirst, second}}}
			want := "REST " + tc.name + " scope #1: missing field " + tc.field
			if diffs := probe(context.Background(), api, c); !containsDiff(diffs, want) {
				t.Fatalf("first-row behavior/message changed: got %v, want %q", diffs, want)
			}
			for _, diff := range probe(context.Background(), fakeAPI{contract: c, restRows: map[string][]map[string]any{tc.name: {second, missingFirst}}}, c) {
				if strings.HasPrefix(diff, "REST "+tc.name+" ") {
					t.Fatalf("later row changed default first-row behavior: %v", diff)
				}
			}
		})
	}
}

func TestContractRejectsInvalidOptionalDestinationRules(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*contract)
	}{
		{
			name: "field outside required fields",
			mutate: func(c *contract) {
				restEntry(t, c, "access-apps").OptionalWhenDestinationTypes = map[string][]string{"unused": {"worker"}}
			},
		},
		{
			name: "empty allowed type list",
			mutate: func(c *contract) {
				restEntry(t, c, "access-apps").OptionalWhenDestinationTypes = map[string][]string{"domain": {}}
			},
		},
		{
			name: "empty allowed type string",
			mutate: func(c *contract) {
				restEntry(t, c, "access-apps").OptionalWhenDestinationTypes = map[string][]string{"domain": {""}}
			},
		},
		{
			name: "blank allowed type string",
			mutate: func(c *contract) {
				restEntry(t, c, "access-apps").OptionalWhenDestinationTypes = map[string][]string{"domain": {"  "}}
			},
		},
		{
			name: "single response conditional field",
			mutate: func(c *contract) {
				r := restEntry(t, c, "access-apps")
				r.Single = true
				r.OptionalWhenDestinationTypes = map[string][]string{"domain": {"worker"}}
			},
		},
		{
			name: "single response all-row check",
			mutate: func(c *contract) {
				r := restEntry(t, c, "access-apps")
				r.Single = true
				r.CheckAllRows = true
			},
		},
		{
			name: "raw JSON all-row check",
			mutate: func(c *contract) {
				r := restEntry(t, c, "ai-gateway-log-request")
				r.CheckAllRows = true
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := loadTestContract(t)
			c.REST = append([]restContract(nil), c.REST...)
			tc.mutate(&c)
			if err := validateContract(c); err == nil {
				t.Fatal("accepted invalid optional destination rule")
			}
		})
	}
}

func loadTestContract(t *testing.T) contract {
	t.Helper()
	c, err := loadContract("../../spec/cloudflare/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func accessAppsExceptionContract(t *testing.T) contract {
	t.Helper()
	c := loadTestContract(t)
	entry := restEntry(t, &c, "access-apps")
	entry.CheckAllRows = true
	entry.OptionalWhenDestinationTypes = map[string][]string{"domain": {"worker", "all_preview_workers"}}
	return c
}

func restEntry(t *testing.T, c *contract, name string) *restContract {
	t.Helper()
	for index := range c.REST {
		if c.REST[index].Name == name {
			return &c.REST[index]
		}
	}
	t.Fatalf("REST entry %q not found", name)
	return nil
}

func hasRESTDiff(diffs []string, name string) bool {
	for _, diff := range diffs {
		if strings.HasPrefix(diff, "REST "+name+" ") {
			return true
		}
	}
	return false
}

func containsDiff(diffs []string, want string) bool {
	for _, diff := range diffs {
		if diff == want {
			return true
		}
	}
	return false
}

func equalDestinationTypes(got, want map[string][]string) bool {
	if len(got) != len(want) {
		return false
	}
	for field, wantTypes := range want {
		gotTypes := got[field]
		if len(gotTypes) != len(wantTypes) {
			return false
		}
		for i := range wantTypes {
			if gotTypes[i] != wantTypes[i] {
				return false
			}
		}
	}
	return true
}
