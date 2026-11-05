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

// Sparse and dense traffic overlap two physical reads while emitting in order.
// Raw-body fallback fanout must share the same cap as detail reads.
func TestLogDetailsOverlapAndKeepEmissionOrder(t *testing.T) {
	const rows = 8
	from := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	for _, spacing := range []time.Duration{time.Millisecond, time.Second} {
		var list []string
		for i := 0; i < rows; i++ {
			list = append(list, fmt.Sprintf(`{"id":"row%d","created_at":%q,"path":"chat/completions","duration":1}`, i, from.Add(time.Duration(i)*spacing).Format(time.RFC3339Nano)))
		}
		for _, complete := range []bool{true, false} {
			for _, budget := range []int{0, 1 << 30} {
				t.Run(fmt.Sprintf("spacing=%s/heads=%t/budget=%d", spacing, complete, budget), func(t *testing.T) {
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
						if complete {
							_, _ = w.Write([]byte(`{"result":{"request_head":"{}","request_head_complete":true,"response_head":"{}","response_head_complete":true}}`))
						} else if strings.HasSuffix(r.URL.Path, "/request") || strings.HasSuffix(r.URL.Path, "/response") {
							_, _ = w.Write([]byte(`{}`))
						} else {
							_, _ = w.Write([]byte(`{"result":{}}`))
						}
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
					if peak != 2 {
						t.Fatalf("peak physical reads=%d, want two concurrent and never more", peak)
					}
				})
			}
		}
	}

}

func TestBudgetBoundaryReusesCompletedDetail(t *testing.T) {
	from := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	reads := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/logs") {
			lower, _ := time.Parse(time.RFC3339Nano, r.URL.Query().Get("start_date"))
			var list []string
			if r.URL.Query().Get("page") == "1" {
				for i := 0; i < 4; i++ {
					at := from.Add(time.Duration(i) * time.Second)
					if !at.Before(lower) {
						list = append(list, fmt.Sprintf(`{"id":"row%d","created_at":%q,"duration":1}`, i, at.Format(time.RFC3339)))
					}
				}
			}
			_, _ = fmt.Fprintf(w, `{"result":[%s]}`, strings.Join(list, ","))
			return
		}
		mu.Lock()
		reads[r.URL.Path]++
		mu.Unlock()
		_, _ = w.Write([]byte(`{"result":{"request_head":"{}","request_head_complete":true,"response_head":"{}","response_head_complete":true}}`))
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Cloudflare.APIBase, cfg.Cloudflare.AccountID = server.URL, "fixture"
	cfg.AIGateway.Gateways, cfg.AIGateway.CaptureBodies = []string{"gateway"}, true
	c := NewLogs(&cfg, cfapi.New(cfg.Cloudflare))
	out := &fakeEmitter{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mark, err := c.CollectWindowBudget(ctx, from, from.Add(4*time.Second), 1, out)
	if err != nil || !mark.Equal(from.Add(time.Second)) {
		t.Fatalf("first mark=%s err=%v", mark, err)
	}
	next, err := c.CollectWindowBudget(ctx, mark, from.Add(4*time.Second), 1, out)
	if err != nil || !next.Equal(from.Add(2*time.Second)) {
		t.Fatalf("next mark=%s err=%v", next, err)
	}
	mu.Lock()
	n := reads["/accounts/fixture/ai-gateway/gateways/gateway/logs/row1"]
	mu.Unlock()
	if n != 1 {
		t.Fatalf("completed boundary detail read %d times", n)
	}
	// A failed downstream flush retries the old cursor, so prior retained data
	// must be discarded rather than treating collection as a committed window.
	_, err = c.CollectWindowBudget(ctx, from, from.Add(4*time.Second), 1, &fakeEmitter{})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if n := reads["/accounts/fixture/ai-gateway/gateways/gateway/logs/row1"]; n != 2 {
		t.Fatalf("retry did not refetch boundary detail: %d reads", n)
	}
	if len(out.logEvents) != 2 {
		t.Fatalf("emitted %d rows", len(out.logEvents))
	}
}
