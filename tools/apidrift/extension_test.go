package main

import (
	"context"
	"strings"
	"testing"

	"github.com/rknightion/cf2otel/internal/cfapi"
)

type healthSettingsAPI struct {
	fakeAPI
	disabled bool
	missing  bool
}

func (f healthSettingsAPI) DatasetSettings(ctx context.Context, scope cfapi.Scope, id, dataset string) (cfapi.DatasetSettings, error) {
	if dataset == "healthCheckEventsAdaptiveGroups" {
		if f.disabled {
			return cfapi.DatasetSettings{Enabled: false}, nil
		}
		s, err := f.fakeAPI.DatasetSettings(ctx, scope, id, dataset)
		if f.missing {
			s.AvailableFields = []string{"count"}
		}
		return s, err
	}
	return f.fakeAPI.DatasetSettings(ctx, scope, id, dataset)
}

func TestHealthCheckDisabledPlanAndEnabledDrift(t *testing.T) {
	c, err := loadContract("../../spec/cloudflare/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	// Isolate the plan-gating contract while retaining the existing strict REST rules.
	graphs := make([]graphContract, 0, len(c.GraphQL))
	for _, g := range c.GraphQL {
		if g.Dataset != "healthCheckEventsAdaptiveGroups" {
			graphs = append(graphs, g)
		}
	}
	c.GraphQL = append(graphs, graphContract{Scope: cfapi.ZoneScope, Dataset: "healthCheckEventsAdaptiveGroups", RequiredFields: []string{"count", "avg_rttMs"}, MinimumDuration: 3600, MinimumRetention: 3600, AllowDisabled: true})
	if err := validateContract(c); err != nil {
		t.Fatalf("health checks must allow disabled Free plans: %v", err)
	}
	if diffs := probe(context.Background(), healthSettingsAPI{fakeAPI: fakeAPI{contract: c}, disabled: true}, c); len(diffs) != 0 {
		t.Fatalf("disabled health checks should skip field and duration checks: %v", diffs)
	}
	if diffs := probe(context.Background(), healthSettingsAPI{fakeAPI: fakeAPI{contract: c}, missing: true}, c); !strings.Contains(strings.Join(diffs, "\n"), "missing field avg_rttMs") {
		t.Fatalf("enabled health-check drift was masked: %v", diffs)
	}
	c.GraphQL[len(c.GraphQL)-1].Dataset = "unrelatedDataset"
	if err := validateContract(c); err == nil {
		t.Fatal("disabled exemption widened to unrelated dataset")
	}
}
