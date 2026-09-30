package main

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/rknightion/cf2otel/internal/cfapi"
)

// Remove a named collector requirement from settings independently of whether
// the persisted contract includes it; an omitted contract entry must fail too.
type loop14API struct {
	fakeAPI
	missingDataset string
	missingField   string
	paths          []string
}

func (f *loop14API) DatasetSettings(ctx context.Context, scope cfapi.Scope, id, dataset string) (cfapi.DatasetSettings, error) {
	s, err := f.fakeAPI.DatasetSettings(ctx, scope, id, dataset)
	if dataset == f.missingDataset {
		fields := s.AvailableFields[:0]
		for _, field := range s.AvailableFields {
			if field != f.missingField {
				fields = append(fields, field)
			}
		}
		s.AvailableFields = fields
	}
	return s, err
}

func (f *loop14API) Get(ctx context.Context, path string, query url.Values, out any) error {
	f.paths = append(f.paths, path)
	return f.fakeAPI.Get(ctx, path, query, out)
}

func TestLoop14GraphQLDriftIsDetected(t *testing.T) {
	c, err := loadContract("../../spec/cloudflare/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ dataset, field string }{
		{"httpRequestsAdaptiveGroups", "sum_edgeResponseBytes"},
		{"httpRequestsAdaptiveGroups", "quantiles_originResponseDurationMsP50"},
		{"httpRequestsAdaptiveGroups", "quantiles_originResponseDurationMsP95"},
		{"httpRequestsAdaptiveGroups", "quantiles_originResponseDurationMsP99"},
		{"workersInvocationsAdaptive", "sum_requests"},
		{"workersInvocationsAdaptive", "quantiles_cpuTimeP999"},
	} {
		t.Run(tc.dataset+"/"+tc.field, func(t *testing.T) {
			api := &loop14API{fakeAPI: fakeAPI{contract: c}, missingDataset: tc.dataset, missingField: tc.field}
			diffs := probe(context.Background(), api, c)
			if !strings.Contains(strings.Join(diffs, "\n"), "missing field "+tc.field) {
				t.Fatalf("missing collector field was not detected: %v", diffs)
			}
		})
	}
}

func TestLoop14RESTProbeUsesDiscoveredScopesAndFilters(t *testing.T) {
	c, err := loadContract("../../spec/cloudflare/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	api := &loop14API{fakeAPI: fakeAPI{contract: c, queryLog: map[string][]url.Values{}}}
	if diffs := probe(context.Background(), api, c); len(diffs) != 0 {
		t.Fatalf("valid shapes failed: %v", diffs)
	}
	for _, tc := range []struct{ name, path, key, value, pageSize string }{
		{"cfd-tunnel-list", "/accounts/secret-account-id/cfd_tunnel", "is_deleted", "false", "1"},
		{"certificate-packs", "/zones/secret-zone-id/ssl/certificate_packs", "status", "all", "5"},
	} {
		found := false
		for _, path := range api.paths {
			if path == tc.path {
				found = true
			}
		}
		if !found {
			t.Errorf("%s was not read at its discovered scope", tc.name)
		}
		queries := api.queryLog[tc.name]
		if len(queries) != 1 || queries[0].Get(tc.key) != tc.value || queries[0].Get("per_page") != tc.pageSize || queries[0].Get("page") != "1" {
			t.Errorf("%s did not use bounded filtered first-page read: %v", tc.name, queries)
		}
		t.Run(tc.name+"/shape", func(t *testing.T) {
			broken := fakeAPI{contract: c, restRows: map[string][]map[string]any{tc.name: {{"id": "invented-row"}}}}
			if diffs := probe(context.Background(), broken, c); !strings.Contains(strings.Join(diffs, "\n"), "REST "+tc.name+" scope #1: missing field status") {
				t.Fatalf("missing status was not detected: %v", diffs)
			}
			empty := fakeAPI{contract: c, restRows: map[string][]map[string]any{tc.name: nil}}
			if diffs := probe(context.Background(), empty, c); len(diffs) != 0 {
				t.Fatalf("valid empty list rejected: %v", diffs)
			}
		})
	}
}
