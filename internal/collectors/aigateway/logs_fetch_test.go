package aigateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/config"
)

// The three independent per-log reads must overlap upstream latency while
// the ordinary client still meters every physical request.
func TestCapturedLogReadsOverlapThroughHTTPClient(t *testing.T) {
	var started atomic.Int32
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
		if started.Add(1) == 3 {
			close(ready)
		}
		select {
		case <-ready:
		case <-r.Context().Done():
			return
		}
		if r.URL.Path == "/accounts/fixture/ai-gateway/gateways/gateway/logs/row" {
			_, _ = w.Write([]byte(`{"result":{}}`))
		} else {
			_, _ = w.Write([]byte(`{"messages":[{"role":"user","content":"fixture"}]}`))
		}
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
	if err != nil || !mark.Equal(from.Add(time.Minute)) || len(out.spans) != 1 || started.Load() != 3 {
		t.Fatalf("independent capture reads did not overlap: mark=%s spans=%d reads=%d err=%v", mark, len(out.spans), started.Load(), err)
	}
}

type canceledCaptureAPI struct {
	fakeAPI
	started, canceled, release chan struct{}
	failure                    error
}

func (a *canceledCaptureAPI) Get(context.Context, string, url.Values, any) error {
	<-a.started
	<-a.started
	return a.failure
}

func (a *canceledCaptureAPI) GetRaw(ctx context.Context, _ string, _ url.Values, _ any) error {
	a.started <- struct{}{}
	<-ctx.Done()
	a.canceled <- struct{}{}
	<-a.release
	return ctx.Err()
}

func TestCaptureFailureCancelsAndJoinsSiblingReads(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		api := &canceledCaptureAPI{started: make(chan struct{}, 2), canceled: make(chan struct{}, 2), release: make(chan struct{}), failure: errors.New("fixture detail failure")}
		cfg := config.Default()
		cfg.AIGateway.CaptureBodies = true
		out := &fakeEmitter{}
		done := make(chan error, 1)
		go func() {
			duration := 1.0
			done <- NewLogs(&cfg, api).emit(context.Background(), "gateway", logRow{ID: "row", CreatedAt: time.Now(), Duration: &duration}, out)
		}()
		<-api.canceled
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
