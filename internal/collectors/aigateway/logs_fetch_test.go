package aigateway

import (
	"context"
	"errors"
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
