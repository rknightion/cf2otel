package aigateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	otellog "go.opentelemetry.io/otel/log"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

// burstAPI serves a dense burst: rowsPerSecond log rows in each of seconds
// consecutive source seconds, every row with a request and a response body.
type burstAPI struct {
	fakeAPI
	from          time.Time
	seconds       int
	rowsPerSecond int
	bodyFetches   map[string]int
}

func (b *burstAPI) Get(_ context.Context, path string, q url.Values, out any) error {
	if !strings.HasSuffix(path, "/logs") {
		return json.Unmarshal([]byte(`{}`), out) // per-row detail
	}
	start, err := time.Parse(time.RFC3339Nano, q.Get("start_date"))
	if err != nil {
		return err
	}
	end, err := time.Parse(time.RFC3339Nano, q.Get("end_date"))
	if err != nil {
		return err
	}
	rows := []map[string]any{}
	if q.Get("page") == "1" {
		for s := 0; s < b.seconds; s++ {
			for r := 0; r < b.rowsPerSecond; r++ {
				at := b.from.Add(time.Duration(s)*time.Second + time.Duration(r+1)*100*time.Millisecond)
				if at.Before(start) || !at.Before(end) {
					continue
				}
				rows = append(rows, map[string]any{"id": fmt.Sprintf("row-%d-%d", s, r), "created_at": at.Format(time.RFC3339Nano), "provider": "openai", "model": "example", "path": "chat/completions", "duration": 1, "status_code": 200, "success": true})
			}
		}
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (b *burstAPI) GetRaw(_ context.Context, path string, _ url.Values, out any) error {
	if b.bodyFetches == nil {
		b.bodyFetches = map[string]int{}
	}
	b.bodyFetches[path]++
	return json.Unmarshal([]byte(`{"messages":[{"role":"user","content":"example"}]}`), out)
}

// slowExport stands in for an OTLP endpoint that delivers at most capacity
// spans inside one aggregate commit budget: a larger commit blocks until the
// budget expires, as the live exporter did.
type slowExport struct {
	mu        sync.Mutex
	capacity  int
	pending   int
	spans     map[string]int
	requests  float64
	delivered int
}

func (e *slowExport) Gauge(context.Context, string, float64, ...telemetry.Attr) error { return nil }
func (e *slowExport) Counter(_ context.Context, name string, v float64, _ ...telemetry.Attr) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if name == semconv.MetricAIGatewayRequests {
		e.requests += v
	}
	return nil
}
func (e *slowExport) Histogram(context.Context, string, float64, ...telemetry.Attr) error {
	return nil
}
func (e *slowExport) LogEvent(context.Context, string, string, time.Time, otellog.Severity, ...telemetry.Attr) error {
	return nil
}
func (e *slowExport) Span(_ context.Context, s telemetry.SpanSpec) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pending++
	e.spans[attr(s.Attrs, semconv.AttrAIGatewayLogID)]++
	return nil
}
func (e *slowExport) BeginCommit() uint64 { return 0 }
func (e *slowExport) FlushCommit(ctx context.Context, _ uint64) error {
	e.mu.Lock()
	pending := e.pending
	e.pending = 0
	e.mu.Unlock()
	if pending > e.capacity {
		<-ctx.Done()
		return &telemetry.ExportFailure{Signal: "traces", Err: ctx.Err()}
	}
	e.mu.Lock()
	e.delivered += pending
	e.mu.Unlock()
	return nil
}

// A burst far denser than one commit can deliver must still advance within
// the scheduled run, without fetching every body again for each attempt.
func TestDenseBurstAdvancesWithinOneRun(t *testing.T) {
	from := time.Date(2026, 9, 29, 13, 50, 0, 0, time.UTC)
	const seconds, rowsPerSecond = 4, 3
	api := &burstAPI{from: from, seconds: seconds, rowsPerSecond: rowsPerSecond}
	cfg := &config.Config{Cloudflare: config.CloudflareConfig{AccountID: "example"}, AIGateway: config.AIGatewayConfig{Gateways: []string{"gateway"}, CaptureBodies: true, MaxBodyBytes: 4096}}
	store, err := collector.NewFileStore(filepath.Join(t.TempDir(), "checkpoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	logs := NewLogs(cfg, api)
	if err := store.Set(logs.Name(), from); err != nil {
		t.Fatal(err)
	}
	export := &slowExport{capacity: rowsPerSecond, spans: map[string]int{}}
	s := collector.NewScheduler(nil, export, store)
	s.Flusher = export
	s.CommitTimeout = 200 * time.Millisecond
	// One byte is below any row, so every commit carries exactly one complete
	// source second: the smallest slice a budgeted collector may return.
	s.CommitBudgetBytes = 1
	to := from.Add(seconds * time.Second)
	s.Now = func() time.Time { return to.Add(logs.Lag()) }
	entry := collector.Entry{Collector: logs, Interval: 5 * time.Minute, MaxWindow: 15 * time.Minute}

	if err := s.RunOnce(context.Background(), entry); err != nil {
		t.Fatalf("dense burst did not deliver inside one run: %v", err)
	}
	mark, _ := store.Get(logs.Name())
	if !mark.Equal(to) {
		t.Fatalf("checkpoint %s, want the whole burst committed to %s", mark, to)
	}
	total := seconds * rowsPerSecond
	if export.delivered != total || len(export.spans) != total || export.requests != float64(total) {
		t.Fatalf("delivered %d spans (%d distinct), %v request increments; want %d of each", export.delivered, len(export.spans), export.requests, total)
	}
	for id, n := range export.spans {
		if n != 1 {
			t.Fatalf("log %s exported %d times", id, n)
		}
	}
	// Each slice may look one source second past its mark before it stops, so
	// a body is read at most twice; a whole-window retry read each one per attempt.
	for path, n := range api.bodyFetches {
		if n > 2 {
			t.Fatalf("%s fetched %d times", path, n)
		}
	}
}

var _ cfapi.RawGetter = (*burstAPI)(nil)
