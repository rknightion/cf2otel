package main

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
)

func TestLoadBalancerDocumentedDetailAndLiveCatalog(t *testing.T) {
	c, err := loadContract("../../spec/cloudflare/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var index = -1
	for i, r := range c.REST {
		if r.Name == "lb-pool-health" {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("health production path lacks explicit contract")
	}
	if err := validateContract(c); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"load-balancer-pools", "lb-pool-health"} {
		if len(restProbeQuery(name, time.Now())) != 0 {
			t.Fatal("invented query parameters")
		}
	}
	api := &lbProbeAPI{fakeAPI: fakeAPI{contract: c}}
	if diffs := probe(context.Background(), api, c); len(diffs) != 0 {
		t.Fatal(diffs)
	}
	if api.details != 0 || api.catalogs == 0 {
		t.Fatal("catalog must be live-probed, health detail must remain unprobed")
	}
	reports := strings.Join(documentedRESTReports(c), "\n")
	if !strings.Contains(reports, "REST lb-pool-health: documented_only, unprobed:") {
		t.Fatal("missing explicit coverage report")
	}
	for _, mutate := range []func(*restContract){
		func(r *restContract) { r.ProbeMode = ""; r.DocumentedReason = "" },
		func(r *restContract) { r.DocumentedReason = " " },
		func(r *restContract) { r.Path = "/accounts/{account}/load_balancers/pools/*/health" },
		func(r *restContract) { r.Single = false },
		func(r *restContract) {
			r.Name = "load-balancer-pools"
			r.Path = "/accounts/{account}/load_balancers/pools"
		},
	} {
		changed := c
		changed.REST = append([]restContract(nil), c.REST...)
		mutate(&changed.REST[index])
		if validateContract(changed) == nil {
			t.Fatal("invalid detail exemption accepted")
		}
	}
	api.missingName = true
	if diffs := probe(context.Background(), api, c); !strings.Contains(strings.Join(diffs, "\n"), "missing field name") {
		t.Fatal("live catalog required-field drift masked", diffs)
	}
}

type lbProbeAPI struct {
	fakeAPI
	details, catalogs int
	missingName       bool
}

func (a *lbProbeAPI) GetPage(ctx context.Context, path string, q url.Values, out *cfapi.Page) error {
	if strings.HasSuffix(path, "/load_balancers/pools") {
		var rows json.RawMessage
		if err := a.Get(ctx, path, q, &rows); err != nil {
			return err
		}
		out.Result = rows
		return nil
	}
	return a.fakeAPI.GetPage(ctx, path, q, out)
}

func (a *lbProbeAPI) Get(ctx context.Context, path string, q url.Values, out any) error {
	if strings.Contains(path, "/load_balancers/pools/") {
		a.details++
		return nil
	}
	if strings.HasSuffix(path, "/load_balancers/pools") {
		a.catalogs++
		if len(q) != 0 {
			return context.Canceled
		}
		if a.missingName {
			return json.Unmarshal([]byte(`[{"id":"opaque-pool"}]`), out)
		}
		return json.Unmarshal([]byte(`[]`), out)
	}
	return a.fakeAPI.Get(ctx, path, q, out)
}
