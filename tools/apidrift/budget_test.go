package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/config"
)

// Settings use the real HTTP client and process-wide class limiter. The existing
// shape fixture supplies REST rows: this witness isolates the dominant settings
// workload without calling Cloudflare or accelerating the production clock.
type pacedContractAPI struct {
	fakeAPI
	client *cfapi.HTTPClient
}

func (p pacedContractAPI) Zones(context.Context) ([]cfapi.Zone, error) {
	zones := make([]cfapi.Zone, 23)
	for i := range zones {
		zones[i].ID = fmt.Sprintf("opaque-zone-%d", i)
		zones[i].Account.ID = "secret-account-id"
	}
	return zones, nil
}
func (p pacedContractAPI) DatasetSettings(ctx context.Context, scope cfapi.Scope, id, dataset string) (cfapi.DatasetSettings, error) {
	return p.client.DatasetSettings(ctx, scope, id, dataset)
}
func (p pacedContractAPI) QueryBatch(ctx context.Context, selections []cfapi.GraphQLBatchSelection) (map[string]json.RawMessage, error) {
	return p.client.QueryBatch(ctx, selections)
}

func TestBudgetDefaultLimiterContract(t *testing.T) {
	if os.Getenv("APIDRIFT_DEFAULT_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 570*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBudgetDefaultLimiterContract$", "-test.v")
		cmd.Env = append(os.Environ(), "APIDRIFT_DEFAULT_CHILD=1")
		output, err := cmd.CombinedOutput()
		t.Log(string(output))
		if err != nil {
			t.Fatalf("default-limiter witness: %v", err)
		}
		return
	}
	c, err := loadContract("../../spec/cloudflare/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			t.Error("unexpected non-GraphQL fixture request")
			w.WriteHeader(404)
			return
		}
		calls.Add(1)
		var request struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		scope := "accounts"
		if strings.Contains(request.Query, "zones(") {
			scope = "zones"
		}
		node := map[string]any{}
		if strings.Contains(request.Query, "settings{") {
			settings := map[string]any{}
			for _, g := range c.GraphQL {
				if (scope == "accounts") != (g.Scope == cfapi.AccountScope) {
					continue
				}
				fields := append([]string(nil), g.RequiredFields...)
				if g.Dataset == "firewallEventsAdaptiveGroups" {
					for _, field := range firewallGroupFields[1:] {
						fields = append(fields, strings.ReplaceAll(field, ".", "_"))
					}
				}
				settings[g.Dataset] = map[string]any{"enabled": true, "availableFields": fields, "maxNumberOfFields": 80, "maxDuration": g.MinimumDuration, "notOlderThan": g.MinimumRetention, "maxPageSize": 100}
			}
			node["settings"] = settings
		} else {
			node["firewall_canary"] = []any{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{scope: []any{node}}}})
	}))
	defer server.Close()
	cfg := config.Default().Cloudflare
	cfg.APIBase = server.URL
	cfg.Timeout = 10 * time.Second
	api := pacedContractAPI{fakeAPI: fakeAPI{contract: c}, client: cfapi.New(cfg)}
	ctx, cancel := context.WithTimeout(context.Background(), 570*time.Second)
	defer cancel()
	started := time.Now()
	var diffs []string
	if os.Getenv("APIDRIFT_OLD_BUDGET") == "1" {
		old, stop := context.WithTimeout(ctx, 2*time.Minute)
		defer stop()
		diffs = probe(old, api, c)
		if !errors.Is(old.Err(), context.DeadlineExceeded) {
			t.Fatal("baseline did not reach the actual context deadline")
		}
	} else {
		diffs = probeBudgeted(ctx, api, c, cfg.RateLimit)
	}
	duration, err := probeDuration(c, 1, 23, cfg.RateLimit)
	if err != nil {
		t.Fatal(err)
	}
	graphql := cfg.RateLimit.GraphQL
	t.Logf("settings=330 selection=1 class=graphql rate=%g burst=%d budget=%s elapsed=%s physical_requests=%d", graphql.RequestsPerSecond, graphql.Burst, duration, time.Since(started), calls.Load())
	if len(diffs) != 0 {
		t.Fatalf("contract probe differences: %v", diffs)
	}
	if calls.Load() != 331 {
		t.Fatalf("want all 330 settings and singleton selection, got %d", calls.Load())
	}
	// 331 requests through the default process GraphQL bucket: the burst is free,
	// every later request waits 1/rate. Allow 5% for timer slack.
	floor := time.Duration(float64(331-graphql.Burst) / graphql.RequestsPerSecond * 0.95 * float64(time.Second))
	if time.Since(started) < floor {
		t.Fatal("default pacing was bypassed")
	}
}

func TestProbeBudgetCountsAndClasses(t *testing.T) {
	c, err := loadContract("../../spec/cloudflare/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	rest, graph := probeRequestCounts(c, 1, 23)
	if rest != 104 || graph != 331 {
		t.Fatalf("request count REST=%d GraphQL=%d", rest, graph)
	}
	rates := config.Default().Cloudflare.RateLimit
	duration, err := probeDuration(c, 1, 23, rates)
	if err != nil || duration <= 2*time.Minute || duration >= 540*time.Second {
		t.Fatalf("budget=%s err=%v", duration, err)
	}
	slower := rates
	slower.GraphQL.RequestsPerSecond /= 2
	slow, _ := probeDuration(c, 1, 23, slower)
	if slow <= duration {
		t.Fatal("budget does not track configured class rates")
	}
	for _, sample := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("private: %w", context.DeadlineExceeded), "deadline"},
		{errors.Join(context.DeadlineExceeded, &cfapi.GraphQLBudgetError{}), "rate_limited"},
		{&cfapi.HTTPError{Status: 429}, "rate_limited"},
		{&cfapi.HTTPError{Status: 403}, "http_4xx"},
		{&cfapi.HTTPError{Status: 503}, "http_5xx"},
		{errors.New("opaque-token opaque-identifier opaque-response-body"), "envelope"},
	} {
		if got := probeErrorClass(sample.err); got != sample.want {
			t.Fatalf("class=%s want=%s", got, sample.want)
		}
	}
	tiny, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	diffs := probeBudgeted(tiny, fakeAPI{contract: c}, c, rates)
	if len(diffs) != 1 || !strings.Contains(diffs[0], "probe budget exceeds job") {
		t.Fatalf("job budget must fail closed: %v", diffs)
	}
}
