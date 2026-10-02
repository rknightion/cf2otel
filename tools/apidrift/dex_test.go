package main

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestDEXDocumentedOnlyValidationAndSkip(t *testing.T) {
	c, err := loadContract("../../spec/cloudflare/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var detail restContract
	err = json.Unmarshal([]byte(`{"name":"dex-http-results","scope":"account","path":"/accounts/{account}/dex/http-tests/{test}","required_fields":["httpStats"],"single":true,"probe_mode":"documented_only","documented_reason":"Fixture-only; no live DEX tests observed."}`), &detail)
	if err != nil {
		t.Fatalf("documented-only detail contract rejected: %v", err)
	}
	entries := []restContract{}
	for _, r := range c.REST {
		if r.Name != "dex-http-results" && r.Name != "dex-traceroute-results" {
			entries = append(entries, r)
		}
	}
	trace := detail
	trace.Name = "dex-traceroute-results"
	trace.Path = "/accounts/{account}/dex/traceroute-tests/{test}"
	trace.RequiredFields = []string{"tracerouteStats"}
	c.REST = append(entries, detail, trace)
	if err := validateContract(c); err != nil {
		t.Fatalf("documented-only detail contract invalid: %v", err)
	}
	api := &dexProbeAPI{fakeAPI: fakeAPI{contract: c}}
	diffs := probe(context.Background(), api, c)
	if len(diffs) != 0 {
		t.Fatalf("canary differences: %v", diffs)
	}
	if api.detailCalls != 0 {
		t.Fatal("documented-only detail issued network request")
	}
	reports := documentedRESTReports(c)
	if len(reports) != 2 || !strings.Contains(reports[0], "documented_only, unprobed") {
		t.Fatalf("documented-only coverage not reported: %v", reports)
	}
	for _, mutate := range []func(*restContract){
		func(r *restContract) { r.ProbeMode = "ignore" },
		func(r *restContract) { r.DocumentedReason = " " },
		func(r *restContract) { r.Path = "/accounts/{account}/dex/*" },
		func(r *restContract) { r.Name = "access-users"; r.Path = "/accounts/{account}/access/users" },
	} {
		changed := c
		changed.REST = append([]restContract(nil), c.REST...)
		mutate(&changed.REST[len(changed.REST)-1])
		if validateContract(changed) == nil {
			t.Fatal("invalid documented-only exemption accepted")
		}
	}
}
func TestDEXRejectUnknownProbeMode(t *testing.T) {
	c, err := loadContract("../../spec/cloudflare/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	c.REST[0].ProbeMode = "ignore"
	if validateContract(c) == nil {
		t.Fatal("unknown probe mode accepted by old validation")
	}
}

type dexProbeAPI struct {
	fakeAPI
	detailCalls int
}

func (a *dexProbeAPI) Get(ctx context.Context, path string, q url.Values, out any) error {
	if strings.Contains(path, "/http-tests/") || strings.Contains(path, "/traceroute-tests/") {
		a.detailCalls++
		return nil
	}
	if strings.HasSuffix(path, "/dex/tests/overview") {
		return json.Unmarshal([]byte(`{"tests":[]}`), out)
	}
	return a.fakeAPI.Get(ctx, path, q, out)
}
