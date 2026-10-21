package aigateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
)

// Log details are latency-bound, so a dense source second keeps several in
// flight while records are still emitted in source order.
func TestLogDetailsOverlapAndKeepEmissionOrder(t *testing.T) {
	const rows = 8
	from := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	var list []string
	for i := 0; i < rows; i++ {
		list = append(list, fmt.Sprintf(`{"id":"row%d","created_at":%q,"path":"chat/completions","duration":1}`, i, from.Add(time.Duration(i)*time.Millisecond).Format(time.RFC3339Nano)))
	}
	for _, budget := range []int{0, 1 << 30} {
		t.Run(fmt.Sprintf("budget=%d", budget), func(t *testing.T) {
			var mu sync.Mutex
			inFlight, peak := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/logs") {
					if r.URL.Query().Get("page") == "1" {
						_, _ = w.Write([]byte(`{"result":[` + strings.Join(list, ",") + `]}`))
					} else {
						_, _ = w.Write([]byte(`{"result":[]}`))
					}
					return
				}
				mu.Lock()
				inFlight++
				peak = max(peak, inFlight)
				mu.Unlock()
				// Hold each detail long enough for siblings to overlap it.
				time.Sleep(50 * time.Millisecond)
				mu.Lock()
				inFlight--
				mu.Unlock()
				_, _ = w.Write([]byte(`{"result":{"request_head":"{}","request_head_complete":true,"response_head":"{}","response_head_complete":true}}`))
			}))
			defer server.Close()
			cfg := config.Default()
			cfg.Cloudflare.APIBase, cfg.Cloudflare.AccountID = server.URL, "fixture"
			cfg.AIGateway.Gateways, cfg.AIGateway.CaptureBodies = []string{"gateway"}, true
			cfg.Cloudflare.RateLimit.REST.RequestsPerSecond, cfg.Cloudflare.RateLimit.Total.RequestsPerSecond = 1000, 1000
			out := &fakeEmitter{}
			mark, err := NewLogs(&cfg, cfapi.New(cfg.Cloudflare)).CollectWindowBudget(context.Background(), from, from.Add(time.Minute), budget, out)
			if err != nil || !mark.Equal(from.Add(time.Minute)) || len(out.logEvents) != rows {
				t.Fatalf("mark=%s events=%d err=%v", mark, len(out.logEvents), err)
			}
			for i, e := range out.logEvents {
				if got := attr(e.attrs, semconv.AttrAIGatewayLogID); got != fmt.Sprintf("row%d", i) {
					t.Fatalf("event %d is %s: emission order changed", i, got)
				}
			}
			if peak < detailConcurrency {
				t.Fatalf("at most %d log details were in flight", peak)
			}
		})
	}
}
