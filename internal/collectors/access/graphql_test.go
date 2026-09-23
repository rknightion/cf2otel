package access

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/telemetry"
	otellog "go.opentelemetry.io/otel/log"
)

type metricAPI struct {
	requests []cfapi.GraphQLRequest
	failOn   string
}

func (a *metricAPI) Get(context.Context, string, url.Values, any) error        { return nil }
func (a *metricAPI) Accounts(context.Context) ([]cfapi.Account, error)         { return nil, nil }
func (a *metricAPI) Zones(context.Context) ([]cfapi.Zone, error)               { return nil, nil }
func (a *metricAPI) Gateways(context.Context, string) ([]cfapi.Gateway, error) { return nil, nil }
func (a *metricAPI) Query(_ context.Context, r cfapi.GraphQLRequest, out any) error {
	a.requests = append(a.requests, r)
	if r.Dataset == a.failOn {
		return errors.New("query failed")
	}
	var data string
	if r.Dataset == "cf1AccessLoginsRawGroups" {
		data = `[{"dimensions":{"appName":"Demo","idp":"okta","loginType":"login","allowed":"allowed"},"sum":{"logins":7}},{"dimensions":{"appName":"Demo","idp":"nonidentity","loginType":"login","allowed":"allowed"},"sum":{"logins":11}}]`
	} else {
		data = `[{"count":13,"dimensions":{"appId":"app-1","identityProvider":"nonidentity","serviceTokenId":"token-1","isSuccessfulLogin":1}},{"count":2,"dimensions":{"appId":"app-1","identityProvider":"okta","isSuccessfulLogin":1}}]`
	}
	return json.Unmarshal([]byte(data), out)
}
func TestLoginMetricsDoNotEmitBeforeAllQueriesSucceed(t *testing.T) {
	api := &metricAPI{failOn: "accessLoginRequestsAdaptiveGroups"}
	e := &metricEmitter{}
	c := loginMetrics{api: api, accountID: "account"}
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	if _, err := c.CollectWindow(context.Background(), from, from.Add(time.Hour), e); err == nil {
		t.Fatal("expected query failure")
	}
	if len(e.points) != 0 {
		t.Fatalf("emitted %d counters before all queries succeeded", len(e.points))
	}
}

type metricPoint struct {
	name  string
	value float64
	attrs []telemetry.Attr
}
type metricEmitter struct{ points []metricPoint }

func (e *metricEmitter) Counter(_ context.Context, n string, v float64, a ...telemetry.Attr) error {
	e.points = append(e.points, metricPoint{n, v, a})
	return nil
}
func (*metricEmitter) Gauge(context.Context, string, float64, ...telemetry.Attr) error { return nil }
func (*metricEmitter) Histogram(context.Context, string, float64, ...telemetry.Attr) error {
	return nil
}
func (*metricEmitter) LogEvent(context.Context, string, string, time.Time, otellog.Severity, ...telemetry.Attr) error {
	return nil
}
func (*metricEmitter) Span(context.Context, telemetry.SpanSpec) error { return nil }
func TestLoginMetricsKeepSourcesAndIdentitySeparate(t *testing.T) {
	api := &metricAPI{}
	e := &metricEmitter{}
	c := loginMetrics{api: api, accountID: "account"}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(4 * time.Hour)
	high, err := c.CollectWindow(context.Background(), from, to, e)
	if err != nil {
		t.Fatal(err)
	}
	if !high.Equal(to) {
		t.Fatalf("high=%s", high)
	}
	if len(api.requests) != 3 {
		t.Fatalf("requests=%d", len(api.requests))
	}
	for _, r := range api.requests {
		if r.Dataset == "cf1AccessLoginsRawGroups" && r.To.Sub(r.From) > 3*time.Hour {
			t.Fatalf("raw window=%s", r.To.Sub(r.From))
		}
	}
	if len(e.points) != 6 {
		t.Fatalf("points=%+v", e.points)
	}
	for _, p := range e.points {
		for _, a := range p.attrs {
			if a.Value == "token-1" {
				t.Fatal("token ID in metric attributes")
			}
		}
	}
	if e.points[0].name == e.points[4].name {
		t.Fatal("adaptive source merged into login totals")
	}
}
