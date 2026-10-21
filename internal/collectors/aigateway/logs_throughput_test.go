package aigateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
)

type workloadTransport func(*http.Request) (*http.Response, error)

func (f workloadTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise registration, scheduling, real quota admission, body decoding and
// checkpointing at 2400 logs/hour with concurrent ~1.1 rps GraphQL traffic.
// Only the upstream network and clock are replaced; no production clock seam.
func TestRegisteredLogsSustainSourceRate(t *testing.T) {
	if os.Getenv("CF2OTEL_TEST_AIGATEWAY_BUDGET_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRegisteredLogsSustainSourceRate$", "-test.v")
		cmd.Env = append(os.Environ(), "CF2OTEL_TEST_AIGATEWAY_BUDGET_CHILD=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("workload process: %v\n%s", err, out)
		}
		return
	}
	synctest.Test(t, func(t *testing.T) {
		from := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
		original := http.DefaultTransport
		http.DefaultTransport = workloadTransport(func(r *http.Request) (*http.Response, error) {
			// The measured mean upstream header latency was about half a second.
			select {
			case <-time.After(500 * time.Millisecond):
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
			body := `{"result":{}}`
			switch {
			case r.Method == http.MethodPost:
				body = `{"data":{"viewer":{"zones":[{"settings":{"events":{"enabled":true,"availableFields":["count"]}}}]}}}`
			case strings.HasSuffix(r.URL.Path, "/logs"):
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				rows := make([]map[string]any, 0)
				for i := (page - 1) * 50; i < page*50 && i < 200; i++ {
					rows = append(rows, map[string]any{"id": fmt.Sprint(i), "created_at": from.Add(time.Duration(i) * time.Second).Format(time.RFC3339), "duration": 1})
				}
				raw, err := json.Marshal(map[string]any{"result": rows})
				if err != nil {
					return nil, err
				}
				body = string(raw)
			case strings.HasSuffix(r.URL.Path, "/request"), strings.HasSuffix(r.URL.Path, "/response"):
				body = `{"messages":[{"role":"user","content":"fixture"}]}`
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})
		defer func() { http.DefaultTransport = original }()
		cfg := config.Default()
		cfg.Cloudflare.AccountID = "fixture"
		cfg.AIGateway.Gateways, cfg.AIGateway.CaptureBodies = []string{"gateway"}, true
		cfg.Collectors = map[string]config.CollectorConfig{"aigateway.logs": {Enabled: true, Interval: 5 * time.Minute, MaxWindow: 5 * time.Minute}}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		graphDone := make(chan int, 1)
		go func() {
			tick := time.NewTicker(900 * time.Millisecond)
			defer tick.Stop()
			completed := 0
			defer func() { graphDone <- completed }()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					_, err := cfapi.New(cfg.Cloudflare).DatasetSettings(ctx, cfapi.ZoneScope, "zone-"+strconv.Itoa(completed%8), "events")
					if err != nil {
						if ctx.Err() == nil {
							t.Error(err)
						}
						return
					}
					completed++
				}
			}
		}()
		registry := collector.NewRegistry()
		Register(collector.Deps{Config: &cfg, API: cfapi.New(cfg.Cloudflare), Registry: registry})
		store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "state.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Set("aigateway.logs", from); err != nil {
			t.Fatal(err)
		}
		out := &fakeEmitter{}
		scheduler := collector.NewScheduler(registry, out, store)
		scheduler.Now = func() time.Time { return from.Add(6 * time.Minute) }
		started := time.Now()
		if err := scheduler.RunOnce(ctx, registry.Entries()[0]); err != nil {
			t.Fatal(err)
		}
		elapsed := time.Since(started)
		cancel()
		graphRequests := <-graphDone
		mark, _ := store.Get("aigateway.logs")
		if elapsed >= 5*time.Minute || !mark.Equal(from.Add(5*time.Minute)) || len(out.spans) != 200 {
			t.Fatalf("source fell behind: elapsed=%s mark=%s rows=%d", elapsed, mark, len(out.spans))
		}
		if float64(graphRequests)/elapsed.Seconds() < 1 {
			t.Fatalf("competing GraphQL traffic starved: %d requests in %s", graphRequests, elapsed)
		}
		t.Logf("200 captured logs committed in %s with %d GraphQL requests", elapsed, graphRequests)
	})
}
