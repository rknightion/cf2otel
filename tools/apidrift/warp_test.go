package main

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestWARPContractAndFirstPageProbe(t *testing.T) {
	c, err := loadContract("../../spec/cloudflare/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range c.REST {
		if r.Name == "warp-devices" {
			found = true
			if !r.AllowEmpty || !r.CheckAllRows || r.Scope != "account" || len(r.RequiredFields) != 7 {
				t.Fatal("incorrect WARP contract")
			}
		}
	}
	if !found {
		t.Fatal("missing WARP device contract")
	}
	api := &fakeAPI{contract: c, queryLog: map[string][]url.Values{}}
	if diffs := probe(context.Background(), api, c); len(diffs) != 0 {
		t.Fatalf("valid shapes %v", diffs)
	}
	queries := api.queryLog["warp-devices"]
	if len(queries) != 1 || queries[0].Get("source") != "last_seen" || queries[0].Get("page") != "1" || queries[0].Get("per_page") != "50" {
		t.Fatalf("wrong bounded probe %v", queries)
	}
	q := queries[0]
	from, e1 := time.Parse("2006-01-02T15:04:05.000Z", q.Get("from"))
	to, e2 := time.Parse("2006-01-02T15:04:05.000Z", q.Get("to"))
	if e1 != nil || e2 != nil || to.Sub(from) != 15*time.Minute {
		t.Fatal("incorrect probe time bounds")
	}
	valid := map[string]any{"colo": "fixture-colo", "deviceId": "opaque-a", "mode": "fixture-mode", "platform": "fixture-platform", "status": "fixture-status", "timestamp": "opaque-time", "version": "fixture-version"}
	broken := fakeAPI{contract: c, restRows: map[string][]map[string]any{"warp-devices": {valid, {"deviceId": "opaque-b"}}}}
	if diffs := probe(context.Background(), broken, c); !strings.Contains(strings.Join(diffs, "\n"), "REST warp-devices scope #1: missing field colo") {
		t.Fatalf("later-row missing field not detected %v", diffs)
	}
	empty := fakeAPI{contract: c, restRows: map[string][]map[string]any{"warp-devices": {}}}
	if diffs := probe(context.Background(), empty, c); len(diffs) != 0 {
		t.Fatalf("genuine empty rejected %v", diffs)
	}
}
