package aigateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
)

// Body reads the detail could not satisfy must overlap upstream latency while
// the ordinary client still meters every physical request. The detail comes
// first because its heads decide which bodies need a read.
func TestCapturedLogReadsOverlapThroughHTTPClient(t *testing.T) {
	var started, details atomic.Int32
	ready := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/accounts/fixture/ai-gateway/gateways/gateway/logs" {
			if r.URL.Query().Get("page") == "1" {
				_, _ = w.Write([]byte(`{"result":[{"id":"row","created_at":"2026-10-20T00:00:00Z","duration":1}]}`))
			} else {
				_, _ = w.Write([]byte(`{"result":[]}`))
			}
			return
		}
		if r.URL.Path == "/accounts/fixture/ai-gateway/gateways/gateway/logs/row" {
			details.Add(1)
			_, _ = w.Write([]byte(`{"result":{"request_head_complete":false,"response_head_complete":false}}`))
			return
		}
		if started.Add(1) == 2 {
			close(ready)
		}
		select {
		case <-ready:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte(`{"messages":[{"role":"user","content":"fixture"}]}`))
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Cloudflare.APIBase, cfg.Cloudflare.AccountID = server.URL, "fixture"
	cfg.AIGateway.Gateways, cfg.AIGateway.CaptureBodies = []string{"gateway"}, true
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	from := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	out := &fakeEmitter{}
	mark, err := NewLogs(&cfg, cfapi.New(cfg.Cloudflare)).CollectWindow(ctx, from, from.Add(time.Minute), out)
	if err != nil || !mark.Equal(from.Add(time.Minute)) || len(out.spans) != 1 || started.Load() != 2 || details.Load() != 1 {
		t.Fatalf("body reads did not overlap: mark=%s spans=%d bodies=%d details=%d err=%v", mark, len(out.spans), started.Load(), details.Load(), err)
	}
}

type canceledCaptureAPI struct {
	fakeAPI
	started, canceled, release chan struct{}
	failure                    error
}

// The detail has no complete head, so both bodies need a read.
func (a *canceledCaptureAPI) Get(context.Context, string, url.Values, any) error { return nil }

// The request read fails once the response read is in flight; the response
// read only returns after cancellation and the test's release.
func (a *canceledCaptureAPI) GetRaw(ctx context.Context, path string, _ url.Values, _ any) error {
	if strings.HasSuffix(path, "/request") {
		<-a.started
		return a.failure
	}
	a.started <- struct{}{}
	<-ctx.Done()
	a.canceled <- struct{}{}
	<-a.release
	return ctx.Err()
}

func TestCaptureFailureCancelsAndJoinsSiblingReads(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		api := &canceledCaptureAPI{started: make(chan struct{}, 1), canceled: make(chan struct{}, 1), release: make(chan struct{}), failure: errors.New("fixture request body failure")}
		cfg := config.Default()
		cfg.AIGateway.CaptureBodies = true
		out := &fakeEmitter{}
		done := make(chan error, 1)
		go func() {
			duration := 1.0
			done <- NewLogs(&cfg, api).emit(context.Background(), "gateway", logRow{ID: "row", CreatedAt: time.Now(), Duration: &duration}, out)
		}()
		<-api.canceled
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("capture returned before siblings joined: %v", err)
		default:
		}
		close(api.release)
		if err := <-done; !errors.Is(err, api.failure) {
			t.Fatalf("capture lost the initiating failure: %v", err)
		}
		if len(out.spans) != 0 || out.logs != 0 {
			t.Fatal("failed capture emitted partial records")
		}
	})
}

// An explicit payload-disabled flag makes raw reads futile; unknown flags must
// still try the raw endpoints when complete heads are absent.
func TestPayloadCollectionFlagAvoidsUnavailableBodyReads(t *testing.T) {
	for _, tc := range []struct {
		name, listFlag, detailFlag string
		bodies                     int32
	}{
		{"list disabled", `,"collect_log_payload":false`, "", 0},
		{"detail disabled", "", `,"collect_log_payload":false`, 0},
		{"unknown", `,"collect_log_payload":null`, "", 2},
		{"enabled", `,"collect_log_payload":true`, `,"collect_log_payload":true`, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var details, bodies atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/logs"):
					if r.URL.Query().Get("page") == "1" {
						_, _ = fmt.Fprintf(w, `{"result":[{"id":"row","created_at":"2026-10-20T00:00:00Z","duration":1%s}]}`, tc.listFlag)
					} else {
						_, _ = w.Write([]byte(`{"result":[]}`))
					}
				case strings.HasSuffix(r.URL.Path, "/logs/row"):
					details.Add(1)
					_, _ = fmt.Fprintf(w, `{"result":{"request_head":null,"response_head":null,"request_head_complete":true,"response_head_complete":true,"request_size":123%s}}`, tc.detailFlag)
				default:
					bodies.Add(1)
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":7002,"message":"not found"}]}`))
				}
			}))
			defer server.Close()
			cfg := config.Default()
			cfg.Cloudflare.APIBase, cfg.Cloudflare.AccountID = server.URL, "fixture"
			cfg.AIGateway.Gateways, cfg.AIGateway.CaptureBodies = []string{"gateway"}, true
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			from := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
			out := &fakeEmitter{}
			mark, err := NewLogs(&cfg, cfapi.New(cfg.Cloudflare)).CollectWindowBudget(ctx, from, from.Add(time.Minute), 1<<20, out)
			if err != nil || !mark.Equal(from.Add(time.Minute)) || len(out.spans) != 1 || details.Load() != 1 || bodies.Load() != tc.bodies {
				t.Fatalf("mark=%s spans=%d details=%d bodies=%d want=%d err=%v", mark, len(out.spans), details.Load(), bodies.Load(), tc.bodies, err)
			}
			attrs := out.spans[0].Attrs
			if attr(attrs, semconv.AttrAIGatewayRequestBodyUnavailable) != "true" || attr(attrs, semconv.AttrAIGatewayResponseBodyUnavailable) != "true" || attr(attrs, semconv.AttrAIGatewayRequestSize) != "123" {
				t.Fatalf("missing unavailable flags or detail metadata: %v", attrs)
			}
		})
	}
}
