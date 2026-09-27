package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
)

type fakeAPI struct {
	contract        contract
	broken          string
	restRows        map[string][]map[string]any
	queryLog        map[string][]url.Values
	resultTotals    map[string]int
	omitResultTotal map[string]bool
	pageCaps        map[string]int
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
func (f fakeAPI) Get(_ context.Context, path string, query url.Values, out any) error {
	if f.broken == "api-error" {
		return errors.New("token-secret account-secret")
	}
	entry, found := routeForPath(f.contract, path)
	if !found {
		return errors.New("unrecognized test path")
	}
	f.recordQuery(entry.Name, query)
	rows := f.rowsFor(entry)
	if entry.Single {
		if len(rows) > 0 {
			*out.(*map[string]any) = rows[0]
		}
		return nil
	}
	*out.(*[]map[string]any) = paginateFakeRows(rows, query, f.pageCaps[entry.Name])
	return nil
}

func (f fakeAPI) GetPage(_ context.Context, path string, query url.Values, out any) error {
	if f.broken == "api-error" {
		return errors.New("token-secret account-secret")
	}
	entry, found := routeForPath(f.contract, path)
	if !found {
		return errors.New("unrecognized test path")
	}
	f.recordQuery(entry.Name, query)
	rows := f.rowsFor(entry)
	rows = paginateFakeRows(rows, query, f.pageCaps[entry.Name])
	result, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	page := cfapi.Page{Result: result}
	if !f.omitResultTotal[entry.Name] {
		total, ok := f.resultTotals[entry.Name]
		if !ok {
			total = len(f.rowsFor(entry))
		}
		page.ResultInfo.TotalCount = total
	}
	*out.(*cfapi.Page) = page
	return nil
}

func (f fakeAPI) recordQuery(name string, query url.Values) {
	if f.queryLog == nil {
		return
	}
	f.queryLog[name] = append(f.queryLog[name], query.Clone())
}

func (f fakeAPI) rowsFor(entry restContract) []map[string]any {
	if f.broken == "empty-scim" && entry.Name == "access-scim" {
		return nil
	}
	if rows, ok := f.restRows[entry.Name]; ok {
		return rows
	}
	row := map[string]any{"id": "secret-row-id", "name": "example", "status": "active", "domain": "example.com", "collect_logs": true, "created_at": "example", "action": "example", "allowed": true, "app_domain": "example.com", "ray_id": "secret-ray-id", "provider": "example", "model": "example", "status_code": 200, "usage_metadata": map[string]any{}, "timings": map[string]any{}}
	for _, field := range entry.RequiredFields {
		setField(row, field, "example")
	}
	if entry.Name == "access-scim" {
		row["resource_user_email"] = "example"
	}
	setField(row, "id", "secret-row-id")
	if f.broken == "rest-field" && entry.Name == "access-apps" {
		delete(row, "domain")
	}
	return []map[string]any{row}
}

func paginateFakeRows(rows []map[string]any, query url.Values, serverCap int) []map[string]any {
	limit := 0
	for _, key := range []string{"per_page", "limit"} {
		if value := query.Get(key); value != "" {
			limit, _ = strconv.Atoi(value)
			break
		}
	}
	if serverCap > 0 && (limit == 0 || limit > serverCap) {
		limit = serverCap
	}
	if limit <= 0 {
		return rows
	}
	page := 1
	if value := query.Get("page"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			page = parsed
		}
	}
	start := (page - 1) * limit
	if start >= len(rows) {
		return nil
	}
	end := start + limit
	if end > len(rows) {
		end = len(rows)
	}
	return rows[start:end]
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
	fields := restJSONEntry(t, c, "access-apps")
	var checkAllRows bool
	if err := json.Unmarshal(fields["check_all_rows"], &checkAllRows); err != nil || !checkAllRows {
		t.Fatal("access-apps must check every returned row")
	}
	want := map[string][]string{"domain": {"worker", "all_preview_workers"}}
	var got map[string][]string
	if err := json.Unmarshal(fields["optional_when_destination_types"], &got); err != nil || !equalDestinationTypes(got, want) {
		t.Fatalf("unexpected domain exceptions: got %v, want %v", got, want)
	}
}

func TestAccessAppsChecksAThirdPublicRowBeyondTheFirstPage(t *testing.T) {
	c := loadTestContract(t)
	rows := []map[string]any{
		{"id": "domain-1", "name": "domain app", "domain": "one.example"},
		{"id": "domain-2", "name": "another domain app", "domain": "two.example"},
		{"id": "public", "name": "public app", "destinations": []any{map[string]any{"type": "public"}}},
	}
	api := fakeAPI{
		contract: c,
		restRows: map[string][]map[string]any{"access-apps": rows},
		queryLog: map[string][]url.Values{},
	}
	diffs := probe(context.Background(), api, c)
	want := "REST access-apps scope #1: missing field domain (1 of 3 rows)"
	if !containsDiff(diffs, want) {
		t.Fatalf("domainless public row at position 3 was not checked: got %v, want %q", diffs, want)
	}
	if queries := api.queryLog["access-apps"]; len(queries) != 1 || queries[0].Get("per_page") != "1000" {
		t.Fatalf("Access apps query did not request 1000 rows: %v", queries)
	}
	if queries := api.queryLog["access-users"]; len(queries) != 1 || queries[0].Get("per_page") != "1" {
		t.Fatalf("Access users query changed unexpectedly: %v", queries)
	}
}

func TestAccessAppsReportsTruncationAndStillChecksReturnedRows(t *testing.T) {
	c := loadTestContract(t)
	rows := []map[string]any{
		{"id": "public", "name": "public app", "destinations": []any{map[string]any{"type": "public"}}},
		{"id": "domain", "name": "domain app", "domain": "example.com"},
	}
	api := fakeAPI{
		contract:        c,
		restRows:        map[string][]map[string]any{"access-apps": rows},
		resultTotals:    map[string]int{"access-apps": 3},
		pageCaps:        map[string]int{"access-apps": 1},
		omitResultTotal: map[string]bool{},
	}
	diffs := probe(context.Background(), api, c)
	if !containsDiff(diffs, "REST access-apps scope #1: checked 1 of 3 rows") {
		t.Fatalf("server truncation was not reported: %v", diffs)
	}
	if !containsDiff(diffs, "REST access-apps scope #1: missing field domain (1 of 1 rows)") {
		t.Fatalf("returned row was not checked after truncation: %v", diffs)
	}
}

func TestAccessAppsMissingTotalCountDoesNotReportTruncation(t *testing.T) {
	c := loadTestContract(t)
	api := fakeAPI{
		contract:        c,
		restRows:        map[string][]map[string]any{"access-apps": {{"id": "domain", "name": "domain app", "domain": "example.com"}}},
		omitResultTotal: map[string]bool{"access-apps": true},
	}
	for _, diff := range probe(context.Background(), api, c) {
		if strings.HasPrefix(diff, "REST access-apps scope #1: checked ") {
			t.Fatalf("missing total_count was treated as truncation: %v", diff)
		}
	}
}

func TestAccessSCIMAcceptsMixedGroupAndUserRowsInEitherOrder(t *testing.T) {
	for _, userFirst := range []bool{false, true} {
		name := "group-first"
		if userFirst {
			name = "user-first"
		}
		t.Run(name, func(t *testing.T) {
			c := accessSCIMAnyRowContract(t)
			group := scimUpdateRow("group", "group-row", false)
			user := scimUpdateRow("user", "user-row", true)
			rows := []map[string]any{group, user}
			if userFirst {
				rows[0], rows[1] = rows[1], rows[0]
			}
			api := fakeAPI{
				contract: c,
				restRows: map[string][]map[string]any{"access-scim": rows},
			}
			if diffs := probe(context.Background(), api, c); hasRESTDiff(diffs, "access-scim") {
				t.Fatalf("mixed GROUP and USER rows failed in order %s: %v", name, diffs)
			}
		})
	}
}

func TestAccessSCIMRequestsFiftyRowsAndKeepsTheWindow(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 20, 30, 0, time.UTC)
	query := restProbeQuery("access-scim", now)
	want := map[string]string{
		"since":     now.Add(-time.Hour).Format(time.RFC3339Nano),
		"until":     now.Format(time.RFC3339Nano),
		"page":      "1",
		"limit":     "50",
		"direction": "asc",
	}
	for key, value := range want {
		if got := query.Get(key); got != value {
			t.Errorf("SCIM query %s = %q, want %q (query %v)", key, got, value, query)
		}
	}
}

func TestAccessSCIMReportsWhenNoRowHasUserEmail(t *testing.T) {
	c := accessSCIMAnyRowContract(t)
	rows := []map[string]any{
		scimUpdateRow("group", "group-row", false),
		scimUpdateRow("user", "user-row", false),
	}
	api := fakeAPI{contract: c, restRows: map[string][]map[string]any{"access-scim": rows}}
	want := "REST access-scim scope #1: field resource_user_email absent from all 2 rows"
	if diffs := probe(context.Background(), api, c); !containsDiff(diffs, want) {
		t.Fatalf("missing email in every row was not reported: got %v, want %q", diffs, want)
	}
}

func TestContractRejectsInvalidRequiredInAnyRowRules(t *testing.T) {
	tests := []struct {
		name   string
		entry  string
		fields map[string]any
	}{
		{
			name: "empty list", entry: "access-scim",
			fields: map[string]any{"required_in_any_row": []string{}},
		},
		{
			name: "empty field", entry: "access-scim",
			fields: map[string]any{"required_in_any_row": []string{""}},
		},
		{
			name: "duplicate field", entry: "access-scim",
			fields: map[string]any{"required_in_any_row": []string{"extra", "extra"}},
		},
		{
			name: "overlaps required fields", entry: "access-scim",
			fields: map[string]any{"required_in_any_row": []string{"resource_type"}},
		},
		{
			name: "raw JSON entry", entry: "ai-gateway-log-request",
			fields: map[string]any{"required_in_any_row": []string{"extra"}},
		},
		{
			name: "single entry", entry: "ai-gateway-log-detail",
			fields: map[string]any{"single": true, "required_in_any_row": []string{"extra"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := loadTestContract(t)
			c = withRESTJSONFields(t, c, tc.entry, tc.fields)
			if err := validateContract(c); err == nil {
				t.Fatal("accepted invalid required_in_any_row rule")
			}
		})
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
			c = withRESTJSONFields(t, c, tc.name, nil, "check_all_rows", "optional_when_destination_types")

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

func scimUpdateRow(resourceType, id string, includeEmail bool) map[string]any {
	row := map[string]any{
		"cf_resource_id": id,
		"http_method":    "PUT",
		"idp_id":         "synthetic-idp",
		"logged_at":      "2026-09-27T10:00:00Z",
		"resource_type":  resourceType,
		"status":         "success",
	}
	if includeEmail {
		row["resource_user_email"] = "present"
	}
	return row
}

func TestContractRejectsInvalidOptionalDestinationRules(t *testing.T) {
	tests := []struct {
		name   string
		entry  string
		fields map[string]any
	}{
		{
			name: "field outside required fields", entry: "access-apps",
			fields: map[string]any{"optional_when_destination_types": map[string][]string{"unused": {"worker"}}},
		},
		{
			name: "empty allowed type list", entry: "access-apps",
			fields: map[string]any{"optional_when_destination_types": map[string][]string{"domain": {}}},
		},
		{
			name: "empty allowed type string", entry: "access-apps",
			fields: map[string]any{"optional_when_destination_types": map[string][]string{"domain": {""}}},
		},
		{
			name: "blank allowed type string", entry: "access-apps",
			fields: map[string]any{"optional_when_destination_types": map[string][]string{"domain": {"  "}}},
		},
		{
			name: "single response conditional field", entry: "access-apps",
			fields: map[string]any{"single": true, "optional_when_destination_types": map[string][]string{"domain": {"worker"}}},
		},
		{
			name: "single response all-row check", entry: "access-apps",
			fields: map[string]any{"single": true, "check_all_rows": true},
		},
		{
			name: "raw JSON all-row check", entry: "ai-gateway-log-request",
			fields: map[string]any{"check_all_rows": true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := loadTestContract(t)
			c = withRESTJSONFields(t, c, tc.entry, tc.fields)
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
	return withRESTJSONFields(t, c, "access-apps", map[string]any{
		"check_all_rows":                  true,
		"optional_when_destination_types": map[string][]string{"domain": {"worker", "all_preview_workers"}},
	})
}

func accessSCIMAnyRowContract(t *testing.T) contract {
	t.Helper()
	c := loadTestContract(t)
	required := make([]string, 0)
	for _, field := range restEntry(t, &c, "access-scim").RequiredFields {
		if field != "resource_user_email" {
			required = append(required, field)
		}
	}
	return withRESTJSONFields(t, c, "access-scim", map[string]any{
		"required_fields":     required,
		"check_all_rows":      true,
		"required_in_any_row": []string{"resource_user_email"},
	})
}

func withRESTJSONFields(t *testing.T, c contract, name string, fields map[string]any, remove ...string) contract {
	t.Helper()
	encoded := marshalContractWithRESTFields(t, c, name, fields, remove...)
	var updated contract
	if err := json.Unmarshal(encoded, &updated); err != nil {
		t.Fatal(err)
	}
	return updated
}

func marshalContractWithRESTFields(t *testing.T, c contract, name string, fields map[string]any, remove ...string) []byte {
	t.Helper()
	encoded, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(document["rest"], &entries); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		var entryName string
		if err := json.Unmarshal(entry["name"], &entryName); err != nil {
			t.Fatal(err)
		}
		if entryName != name {
			continue
		}
		found = true
		for _, key := range remove {
			delete(entry, key)
		}
		for key, value := range fields {
			encodedValue, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			entry[key] = encodedValue
		}
		break
	}
	if !found {
		t.Fatalf("REST entry %q not found in JSON contract", name)
	}
	document["rest"], err = json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func restJSONEntry(t *testing.T, c contract, name string) map[string]json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		REST []map[string]json.RawMessage `json:"rest"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	for _, entry := range document.REST {
		var entryName string
		if err := json.Unmarshal(entry["name"], &entryName); err != nil {
			t.Fatal(err)
		}
		if entryName == name {
			return entry
		}
	}
	t.Fatalf("REST entry %q not found in JSON contract", name)
	return nil
}

func TestContractRejectsNullRESTRowRules(t *testing.T) {
	tests := []struct {
		name  string
		entry string
		field string
	}{
		{name: "check_all_rows", entry: "access-apps", field: "check_all_rows"},
		{name: "CHECK_ALL_ROWS key", entry: "access-apps", field: "CHECK_ALL_ROWS"},
		{name: "optional_when_destination_types", entry: "access-apps", field: "optional_when_destination_types"},
		{name: "required_in_any_row", entry: "access-scim", field: "required_in_any_row"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := loadTestContract(t)
			encoded := marshalContractWithRESTFields(t, c, tc.entry, map[string]any{tc.field: nil})
			if _, err := loadContract(writeContractFixture(t, encoded)); err == nil {
				t.Fatalf("accepted null %s on %s", tc.field, tc.entry)
			}
		})
	}
}

func TestContractStillRejectsUnknownRESTFields(t *testing.T) {
	c := loadTestContract(t)
	encoded := marshalContractWithRESTFields(t, c, "access-apps", map[string]any{"unknown_contract_field": true})
	if _, err := loadContract(writeContractFixture(t, encoded)); err == nil {
		t.Fatal("accepted an unknown REST contract field")
	}
}

func writeContractFixture(t *testing.T, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "contract.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
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
