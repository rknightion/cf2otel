package access

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	lognoop "go.opentelemetry.io/otel/log/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// Exercise public registration, the real REST client and SDK metric output;
// only Cloudflare's process edge is replaced by a local HTTP server.
func TestRegisteredSeatsSnapshot(t *testing.T) {
	for _, mode := range []string{"multipage", "empty", "missing", "null", "wrong-type", "missing-gateway", "null-gateway", "wrong-gateway", "second-page-failure", "second-page-schema", "malformed", "null-result", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.EscapedPath() != "/accounts/opaque%2Faccount/access/users" || r.Header.Get("Authorization") != "Bearer fixture-token" || r.URL.Query().Get("per_page") != "100" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				switch r.URL.Query().Get("page") {
				case "1":
					switch mode {
					case "empty":
						fmt.Fprint(w, `{"success":true,"result":[]}`)
					case "null-result":
						fmt.Fprint(w, `{"result":null}`)
					case "malformed":
						fmt.Fprint(w, `{"result":[`)
					case "missing":
						fmt.Fprint(w, `{"result":[{"gateway_seat":false}]}`)
					case "null":
						fmt.Fprint(w, `{"result":[{"access_seat":null,"gateway_seat":false}]}`)
					case "wrong-type":
						fmt.Fprint(w, `{"result":[{"access_seat":"true","gateway_seat":false}]}`)
					case "missing-gateway":
						fmt.Fprint(w, `{"result":[{"access_seat":false}]}`)
					case "null-gateway":
						fmt.Fprint(w, `{"result":[{"access_seat":false,"gateway_seat":null}]}`)
					case "wrong-gateway":
						fmt.Fprint(w, `{"result":[{"access_seat":false,"gateway_seat":1}]}`)
					default:
						rows := make([]string, 100)
						for i := range rows {
							rows[i] = `{"access_seat":false,"gateway_seat":false}`
						}
						rows[0] = `{"access_seat":true,"gateway_seat":true,"id":"opaque-fixture"}`
						rows[1] = `{"access_seat":true,"gateway_seat":false}`
						rows[2] = `{"access_seat":false,"gateway_seat":true}`
						fmt.Fprintf(w, `{"result":[%s]}`, strings.Join(rows, ","))
					}
				case "2":
					if mode == "second-page-failure" {
						w.WriteHeader(http.StatusForbidden)
						fmt.Fprint(w, `{"errors":[{"code":9109}]}`)
						return
					}
					if mode == "second-page-schema" {
						fmt.Fprint(w, `{"result":[{"access_seat":true}]}`)
						return
					}
					fmt.Fprint(w, `{"result":[{"access_seat":true,"gateway_seat":false}]}`)
				default:
					t.Errorf("unexpected page %s", r.URL.Query().Get("page"))
				}
			}))
			defer server.Close()
			if mode == "disabled" {
				t.Setenv("CF2OTEL_COLLECTORS__ACCESS_SEATS__ENABLED", "false")
			}
			cfg, err := config.Load("")
			if err != nil {
				t.Fatal(err)
			}
			cfg.Cloudflare.APIBase = server.URL
			cfg.Cloudflare.AccountID = "opaque/account"
			cfg.Cloudflare.APIToken = config.Secret("fixture-token")
			registry := collector.NewRegistry()
			Register(collector.Deps{Config: cfg, Registry: registry, API: cfapi.New(cfg.Cloudflare)})
			var snapshot collector.SnapshotCollector
			var seatEntry collector.Entry
			for _, entry := range registry.Entries() {
				if entry.Collector.Name() == "access.seats" {
					seatEntry = entry
					var ok bool
					snapshot, ok = entry.Collector.(collector.SnapshotCollector)
					if !ok {
						t.Fatal("seats must use snapshot scheduling")
					}
					if entry.Interval != 15*time.Minute {
						t.Fatalf("interval=%s, want 15m", entry.Interval)
					}
				}
			}
			if mode == "disabled" {
				if snapshot != nil || calls != 0 {
					t.Fatal("disabled seats registered or called Cloudflare")
				}
				return
			}
			if snapshot == nil {
				t.Fatal("default-enabled access.seats snapshot is not registered")
			}
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() {
				if err := provider.Shutdown(context.Background()); err != nil {
					t.Error(err)
				}
			})
			emitter := telemetry.NewEmitter(provider.Meter("test"), lognoop.NewLoggerProvider().Logger("test"), tracenoop.NewTracerProvider().Tracer("test"))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			// A nil checkpoint store is intentional: snapshot scheduling must not use it.
			err = collector.NewScheduler(registry, emitter, nil).RunOnce(ctx, seatEntry)
			failed := mode != "multipage" && mode != "empty"
			if (err != nil) != failed {
				t.Fatalf("error=%v, want failure=%v", err, failed)
			}
			var output metricdata.ResourceMetrics
			if err := reader.Collect(ctx, &output); err != nil {
				t.Fatal(err)
			}
			points := map[string]float64{}
			for _, scope := range output.ScopeMetrics {
				for _, metric := range scope.Metrics {
					if metric.Name != "cloudflare.access.seats" {
						t.Fatalf("unexpected metric %s", metric.Name)
					}
					if metric.Unit != "1" {
						t.Fatalf("unit=%s", metric.Unit)
					}
					gauge, ok := metric.Data.(metricdata.Gauge[float64])
					if !ok {
						t.Fatalf("not a gauge: %T", metric.Data)
					}
					for _, point := range gauge.DataPoints {
						attrs := point.Attributes.ToSlice()
						if len(attrs) != 1 || string(attrs[0].Key) != "cloudflare.access.seat.type" || attrs[0].Value.Type() != attribute.STRING {
							t.Fatalf("unbounded or non-string attributes: %v", attrs)
						}
						kind := attrs[0].Value.AsString()
						if kind != "access" && kind != "gateway" {
							t.Fatalf("unexpected seat type %q", kind)
						}
						if _, duplicate := points[kind]; duplicate {
							t.Fatalf("duplicate type %s", kind)
						}
						points[kind] = point.Value
					}
				}
			}
			if failed {
				if len(points) != 0 {
					t.Fatalf("partial counts published: %v", points)
				}
				return
			}
			wantAccess, wantGateway := float64(3), float64(2)
			if mode == "empty" {
				wantAccess, wantGateway = 0, 0
			}
			if len(points) != 2 || points["access"] != wantAccess || points["gateway"] != wantGateway {
				t.Fatalf("counts=%v, want access=%g gateway=%g", points, wantAccess, wantGateway)
			}
			if mode == "multipage" && calls != 2 {
				t.Fatalf("did not consume full paginated list: calls=%d", calls)
			}
		})
	}
}
