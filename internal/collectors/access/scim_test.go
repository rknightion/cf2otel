package access

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/telemetry"
	otellog "go.opentelemetry.io/otel/log"
)

type scimAPI struct{ queries []url.Values }

func (a *scimAPI) Get(_ context.Context, _ string, q url.Values, out any) error {
	a.queries = append(a.queries, q)
	payload := `{"result":[{"logged_at":"2026-01-01T00:01:00Z","http_method":"PATCH","resource_type":"USER","resource_user_email":"user@example.com","status":"SUCCESS","cf_resource_id":"resource-1"},{"logged_at":"2026-01-01T00:04:00Z","status":"SUCCESS"}],"result_info":{"page":1,"per_page":2}}`
	if q.Get("page") == "2" {
		payload = `{"result":[],"result_info":{"page":2,"per_page":2}}`
	}
	var env struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		return err
	}
	return json.Unmarshal(env.Result, out)
}
func (a *scimAPI) Query(context.Context, cfapi.GraphQLRequest, any) error    { return nil }
func (a *scimAPI) Accounts(context.Context) ([]cfapi.Account, error)         { return nil, nil }
func (a *scimAPI) Zones(context.Context) ([]cfapi.Zone, error)               { return nil, nil }
func (a *scimAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) { return nil, nil }

type scimEvent struct {
	at    time.Time
	attrs []telemetry.Attr
}
type scimEmitter struct{ events []scimEvent }

func (*scimEmitter) Gauge(context.Context, string, float64, ...telemetry.Attr) error     { return nil }
func (*scimEmitter) Counter(context.Context, string, float64, ...telemetry.Attr) error   { return nil }
func (*scimEmitter) Histogram(context.Context, string, float64, ...telemetry.Attr) error { return nil }
func (e *scimEmitter) LogEvent(_ context.Context, _, _ string, at time.Time, _ otellog.Severity, a ...telemetry.Attr) error {
	e.events = append(e.events, scimEvent{at, a})
	return nil
}
func (*scimEmitter) Span(context.Context, telemetry.SpanSpec) error { return nil }
func TestSCIMWindowFiltersExclusiveHighWater(t *testing.T) {
	api := &scimAPI{}
	e := &scimEmitter{}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(4 * time.Minute)
	high, err := (scimCollector{api: api, accountID: "account", pageSize: 2}).CollectWindow(context.Background(), from, to, e)
	if err != nil {
		t.Fatal(err)
	}
	if !high.Equal(to) {
		t.Fatalf("high=%s", high)
	}
	if len(e.events) != 1 {
		t.Fatalf("events=%d", len(e.events))
	}
	if len(api.queries) != 2 {
		t.Fatalf("pages=%d", len(api.queries))
	}
	if api.queries[0].Get("since") != "2026-01-01T00:00:00Z" || api.queries[0].Get("until") != "2026-01-01T00:04:00Z" {
		t.Fatalf("window=%v", api.queries[0])
	}
}
