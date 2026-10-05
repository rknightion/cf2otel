package loadbalancers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

const requestMetric = "cloudflare.loadbalancers.pool.requests"

func requestPoints(t *testing.T, r *sdkmetric.ManualReader) map[string]float64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := r.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != requestMetric {
				continue
			}
			if m.Unit != "{request}" {
				t.Fatalf("unexpected unit %q", m.Unit)
			}
			g, ok := m.Data.(metricdata.Gauge[float64])
			if !ok {
				t.Fatal("request window count must be a snapshot gauge")
			}
			for _, p := range g.DataPoints {
				if p.Attributes.Len() != 1 {
					t.Fatal("only public pool name may be labelled")
				}
				name, ok := p.Attributes.Value(attribute.Key("cloudflare.loadbalancers.pool.name"))
				if !ok {
					t.Fatal("missing pool name")
				}
				got[name.AsString()] = p.Value
			}
		}
	}
	return got
}

type trafficFixture struct {
	t                                *testing.T
	mode                             string
	pools                            bool
	fields                           []string
	queries, discovery, lists, other int
	data                             []any
	settings                         map[string]any
}

func (f *trafficFixture) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/pools"):
		f.lists++
		if f.pools {
			respond(w, []any{row("opaque-a", "alpha"), row("opaque-b", "beta")})
		} else {
			respond(w, []any{})
		}
	case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/health"):
		f.other++
		respond(w, map[string]any{"pop_health": map[string]any{"healthy": true}})
	case r.Method == "GET" && r.URL.Path == "/zones":
		f.discovery++
		respond(w, []any{
			map[string]any{"id": "opaque-zone", "account": map[string]string{"id": "opaque-account"}},
			map[string]any{"id": "foreign-zone", "account": map[string]string{"id": "foreign-account"}},
		})
	case r.Method == "POST" && r.URL.Path == "/graphql":
		var payload struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			f.t.Fatal(err)
		}
		if strings.Contains(payload.Query, "foreign-zone") {
			f.t.Error("queried zone outside configured account")
		}
		node := map[string]any{}
		if strings.Contains(payload.Query, "settings{") {
			settings := map[string]any{}
			if f.mode != "absent" {
				s := map[string]any{"enabled": f.mode != "disabled", "availableFields": f.fields, "maxNumberOfFields": 2, "maxDuration": 3600, "notOlderThan": 86400, "maxPageSize": 100}
				for k, v := range f.settings {
					s[k] = v
				}
				settings["loadBalancingRequestsAdaptiveGroups"] = s
			}
			node["settings"] = settings
		} else {
			f.queries++
			if !strings.Contains(payload.Query, "count") || !strings.Contains(payload.Query, "selectedPoolName") || strings.Contains(payload.Query, "coloCode") || strings.Contains(payload.Query, "avg") {
				f.t.Errorf("selection not built from minimal advertised documented fields: %s", payload.Query)
			}
			if !strings.Contains(payload.Query, "datetime_geq:") || !strings.Contains(payload.Query, "datetime_lt:") {
				f.t.Error("missing half-open window")
			}
			if f.mode == "denied" {
				_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": "not entitled to field 'loadBalancingRequestsAdaptiveGroups'"}}})
				return
			}
			if f.mode == "failure" {
				http.Error(w, "fixture failure", 400)
				return
			}
			// The strict singleton batch uses an alias without changing the source shape.
			node["pool_requests"] = f.data
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"zones": []any{node}}}})
	default:
		f.t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", 400)
	}
}
func newTrafficFixture(t *testing.T) *trafficFixture {
	return &trafficFixture{t: t, pools: true, fields: []string{"count", "dimensions_selectedPoolName"}, data: []any{
		map[string]any{"count": 4, "dimensions": map[string]any{"selectedPoolName": "alpha"}},
		map[string]any{"count": 6, "dimensions": map[string]any{"selectedPoolName": "beta"}},
	}}
}
func TestRegisteredPoolRequestsDocumentedShape(t *testing.T) {
	f := newTrafficFixture(t)
	c, e, r := setupAPI(t, f.serve, time.Minute, 500)
	if err := run(c, e); err != nil {
		t.Fatal(err)
	}
	got := requestPoints(t, r)
	if len(got) != 2 || got["alpha"] != 4 || got["beta"] != 6 || f.queries != 1 {
		t.Fatalf("documented pool counts missing: %v; queries=%d", got, f.queries)
	}
	// A snapshot refresh replaces counts, never increments a counter from overlapping polls.
	f.data = []any{map[string]any{"count": 2, "dimensions": map[string]any{"selectedPoolName": "alpha"}}}
	if err := run(c, e); err != nil {
		t.Fatal(err)
	}
	if got := requestPoints(t, r); len(got) != 1 || got["alpha"] != 2 {
		t.Fatal("overlapping snapshot was accumulated or stale pool retained", got)
	}
	f.data = []any{}
	if err := run(c, e); err != nil {
		t.Fatal(err)
	}
	if len(requestPoints(t, r)) != 0 {
		t.Fatal("valid empty traffic snapshot did not clear prior counts")
	}
}
func TestPoolRequestsNoPoolsOnlyLists(t *testing.T) {
	f := newTrafficFixture(t)
	f.pools = false
	c, e, r := setupAPI(t, f.serve, time.Minute, 500)
	if err := run(c, e); err != nil {
		t.Fatal(err)
	}
	if f.lists != 1 || f.queries != 0 || f.discovery != 0 || f.other != 0 || len(requestPoints(t, r)) != 0 {
		t.Fatalf("no pools must make only pool-list GET: %+v", f)
	}
}
func TestPoolRequestsEntitlementNoOp(t *testing.T) {
	for _, mode := range []string{"absent", "disabled", "missing-count", "missing-pool", "denied"} {
		t.Run(mode, func(t *testing.T) {
			f := newTrafficFixture(t)
			f.mode = mode
			if mode == "missing-count" {
				f.fields = []string{"dimensions_selectedPoolName"}
			}
			if mode == "missing-pool" {
				f.fields = []string{"count"}
			}
			c, e, r := setupAPI(t, f.serve, time.Minute, 500)
			if err := run(c, e); err != nil {
				t.Fatal("entitlement should be a no-op", err)
			}
			if len(requestPoints(t, r)) != 0 {
				t.Fatal("unentitled traffic fabricated values")
			}
			expected := 0
			if mode == "denied" {
				expected = 1
			}
			if f.queries != expected {
				t.Fatalf("data queried without advertised selection: %d", f.queries)
			}
		})
	}
}
func TestPoolRequestsFailurePreservesExpiryAndCap(t *testing.T) {
	f := newTrafficFixture(t)
	c, e, r := setupAPI(t, f.serve, 400*time.Millisecond, 3)
	if err := run(c, e); err != nil {
		t.Fatal(err)
	}
	got := requestPoints(t, r)
	// Health uses two series; the remaining one traffic slot is a summed remainder.
	if len(got) != 1 || got["other"] != 10 {
		t.Fatal("combined series cap failed to conserve counts", got)
	}
	f.mode = "failure"
	if err := run(c, e); err == nil {
		t.Fatal("transport/auth failure must not be treated as unentitled")
	}
	if len(requestPoints(t, r)) != 1 {
		t.Fatal("failed poll replaced previous snapshot")
	}
	time.Sleep(1500 * time.Millisecond)
	if len(requestPoints(t, r)) != 0 {
		t.Fatal("failure refreshed snapshot expiry")
	}
}
func TestPoolRequestsRejectMalformedAndSaturatedRows(t *testing.T) {
	for _, mode := range []string{"missing-count", "negative", "fractional", "missing-pool", "bad-name", "null-rows", "saturated", "field-limit", "duration-limit", "retention-limit"} {
		t.Run(mode, func(t *testing.T) {
			f := newTrafficFixture(t)
			switch mode {
			case "missing-count":
				f.data = []any{map[string]any{"dimensions": map[string]any{"selectedPoolName": "alpha"}}}
			case "negative":
				f.data = []any{map[string]any{"count": -1, "dimensions": map[string]any{"selectedPoolName": "alpha"}}}
			case "fractional":
				f.data = []any{map[string]any{"count": 1.5, "dimensions": map[string]any{"selectedPoolName": "alpha"}}}
			case "missing-pool":
				f.data = []any{map[string]any{"count": 4, "dimensions": map[string]any{}}}
			case "bad-name":
				f.data = []any{map[string]any{"count": 4, "dimensions": map[string]any{"selectedPoolName": "bad\nname"}}}
			case "null-rows":
				f.data = nil
			case "saturated":
				for len(f.data) < 100 {
					f.data = append(f.data, f.data[0])
				}
			case "field-limit":
				f.settings = map[string]any{"maxNumberOfFields": 1}
			case "duration-limit":
				f.settings = map[string]any{"maxDuration": 1}
			case "retention-limit":
				f.settings = map[string]any{"notOlderThan": 1}
			}
			c, e, r := setupAPI(t, f.serve, time.Minute, 500)
			if err := run(c, e); err == nil {
				t.Fatal("malformed/incomplete traffic must fail closed")
			}
			if len(requestPoints(t, r)) != 0 {
				t.Fatal("failed traffic exported partial values")
			}
		})
	}
}
