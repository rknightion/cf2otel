package inventory

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/telemetry"
	otellog "go.opentelemetry.io/otel/log"
)

type inventoryAPI struct{}

func (*inventoryAPI) Get(_ context.Context, path string, q url.Values, out any) error {
	var payload string
	switch {
	case path == "/accounts/account/access/apps" && q.Get("page") == "1":
		payload = `{"result":[{"id":"app-1","name":"Demo","domain":"https://app.example.com/path","type":"self_hosted"},{"id":"app-2","name":"Demo2","domain":"other.example.com","type":"self_hosted"}],"result_info":{"total_count":0}}`
	case path == "/accounts/account/access/users" && q.Get("page") == "1":
		payload = `{"result":[{"id":"user-1","email":"user@example.com"}],"result_info":{"total_count":0}}`
	default:
		payload = `{"result":[]}`
	}
	var env struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		return err
	}
	return json.Unmarshal(env.Result, out)
}
func (*inventoryAPI) Query(context.Context, cfapi.GraphQLRequest, any) error    { return nil }
func (*inventoryAPI) Accounts(context.Context) ([]cfapi.Account, error)         { return nil, nil }
func (*inventoryAPI) Zones(context.Context) ([]cfapi.Zone, error)               { return nil, nil }
func (*inventoryAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) { return nil, nil }

type gaugePoint struct {
	name  string
	value float64
	attrs []telemetry.Attr
}
type inventoryEmitter struct{ gauges []gaugePoint }

func (e *inventoryEmitter) Gauge(_ context.Context, n string, v float64, a ...telemetry.Attr) error {
	e.gauges = append(e.gauges, gaugePoint{n, v, a})
	return nil
}
func (*inventoryEmitter) Counter(context.Context, string, float64, ...telemetry.Attr) error {
	return nil
}
func (*inventoryEmitter) Histogram(context.Context, string, float64, ...telemetry.Attr) error {
	return nil
}
func (*inventoryEmitter) LogEvent(context.Context, string, string, time.Time, otellog.Severity, ...telemetry.Attr) error {
	return nil
}
func (*inventoryEmitter) Span(context.Context, telemetry.SpanSpec) error { return nil }
func TestInventoryCountsAndCatalog(t *testing.T) {
	cat := NewCatalog()
	e := &inventoryEmitter{}
	c := accessInventory{api: &inventoryAPI{}, accountID: "account", apps: cat, pageSize: 2}
	if err := c.Collect(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if len(e.gauges) != 3 {
		t.Fatalf("gauges=%+v", e.gauges)
	}
	if rec, ok := cat.Lookup("app.example.com"); !ok || rec.ID != "app-1" {
		t.Fatalf("lookup=%+v %v", rec, ok)
	}
	if len(cat.Hosts()) != 2 {
		t.Fatalf("hosts=%v", cat.Hosts())
	}
	for _, g := range e.gauges {
		for _, a := range g.attrs {
			if a.Value == "user@example.com" {
				t.Fatal("email metric attribute")
			}
		}
	}
}
func TestCatalogNormalizesHost(t *testing.T) {
	cat := NewCatalog()
	cat.PutApp(collector.AppRecord{ID: "app", Host: "https://app.example.com/path"})
	if _, ok := cat.Lookup("APP.EXAMPLE.COM"); !ok {
		t.Fatal("normalized host missing")
	}
}
