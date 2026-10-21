package aigateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
	"github.com/rknightion/cf2otel/internal/semconv"
)

// headDetail builds a log detail in the live shape: each head is the body
// text as a JSON string, with its own completeness flag.
func headDetail(t *testing.T, request, response string, requestComplete, responseComplete bool) string {
	t.Helper()
	detail, err := json.Marshal(map[string]any{
		"request_head": request, "request_head_complete": requestComplete,
		"response_head": response, "response_head_complete": responseComplete,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(detail)
}

func bodyCalls(calls []string) (request, response int) {
	for _, call := range calls {
		switch {
		case strings.HasSuffix(call, "/request"):
			request++
		case strings.HasSuffix(call, "/response"):
			response++
		}
	}
	return request, response
}

func TestCompleteHeadsReplaceBodyReads(t *testing.T) {
	headRequest := `{"messages":[{"role":"user","content":"from head"}]}`
	headResponse := `{"result":{"response":"from head","usage":{"prompt_tokens":7,"completion_tokens":3}}}`
	api := &fakeAPI{
		list:     `{"result":[{"id":"A","created_at":"2026-09-23T10:00:00Z","path":"chat/completions","duration":1,"tokens_in":0,"tokens_out":0}]}`,
		detail:   headDetail(t, headRequest, headResponse, true, true),
		request:  `{"messages":[{"role":"user","content":"from body read"}]}`,
		response: `{"result":{"response":"from body read"}}`,
	}
	cfg := &config.Config{Cloudflare: config.CloudflareConfig{AccountID: "example"}, AIGateway: config.AIGatewayConfig{Gateways: []string{"gateway"}, CaptureBodies: true, MaxBodyBytes: 4096}}
	out := &fakeEmitter{}
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	if _, err := NewLogs(cfg, api).CollectWindow(context.Background(), from, from.Add(time.Minute), out); err != nil {
		t.Fatal(err)
	}
	if request, response := bodyCalls(api.calls); request != 0 || response != 0 {
		t.Fatalf("complete heads still read bodies: request=%d response=%d calls=%v", request, response, api.calls)
	}
	if len(out.spans) != 1 {
		t.Fatalf("spans=%d", len(out.spans))
	}
	s := out.spans[0]
	if got := attr(s.Attrs, semconv.AttrGenAIInputMessages); !strings.Contains(got, "from head") {
		t.Fatalf("input messages not taken from the request head: %q", got)
	}
	if attr(s.Attrs, semconv.AttrGenAIInputTokens) != "7" || attr(s.Attrs, semconv.AttrAIGatewayUsageSource) != "response_body" {
		t.Fatalf("usage not recovered from the complete response head: %v", s.Attrs)
	}
}

func TestIncompleteHeadReadsOnlyThatBody(t *testing.T) {
	api := &fakeAPI{
		list:     `{"result":[{"id":"A","created_at":"2026-09-23T10:00:00Z","path":"chat/completions","duration":1}]}`,
		detail:   headDetail(t, `{"messages":[{"role":"user","content":"from head"}]}`, `{"choices":[{"message":{"role":"assist`, true, false),
		request:  `{"messages":[{"role":"user","content":"from body read"}]}`,
		response: `{"choices":[{"message":{"role":"assistant","content":"from body read"}}]}`,
	}
	cfg := &config.Config{Cloudflare: config.CloudflareConfig{AccountID: "example"}, AIGateway: config.AIGatewayConfig{Gateways: []string{"gateway"}, CaptureBodies: true, MaxBodyBytes: 4096}}
	out := &fakeEmitter{}
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	if _, err := NewLogs(cfg, api).CollectWindow(context.Background(), from, from.Add(time.Minute), out); err != nil {
		t.Fatal(err)
	}
	if request, response := bodyCalls(api.calls); request != 0 || response != 1 {
		t.Fatalf("want only the incomplete response read: request=%d response=%d", request, response)
	}
	s := out.spans[0]
	if !strings.Contains(attr(s.Attrs, semconv.AttrGenAIInputMessages), "from head") || !strings.Contains(attr(s.Attrs, semconv.AttrGenAIOutputMessages), "from body read") {
		t.Fatalf("bodies came from the wrong source: %v", s.Attrs)
	}
}

// pagedAPI serves a sorted log list in pages of the requested size.
type pagedAPI struct {
	fakeAPI
	rows  []string
	pages int
}

func (p *pagedAPI) Get(ctx context.Context, path string, q url.Values, out any) error {
	if !strings.HasSuffix(path, "/logs") {
		return p.fakeAPI.Get(ctx, path, q, out)
	}
	p.mu.Lock()
	p.pages++
	p.mu.Unlock()
	// Like the live API, both date bounds are inclusive.
	from, _ := time.Parse(time.RFC3339Nano, q.Get("start_date"))
	to, _ := time.Parse(time.RFC3339Nano, q.Get("end_date"))
	var window []string
	for _, row := range p.rows {
		var r logRow
		if err := json.Unmarshal([]byte(row), &r); err != nil {
			return err
		}
		if !r.CreatedAt.Before(from) && !r.CreatedAt.After(to) {
			window = append(window, row)
		}
	}
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("per_page"))
	start, end := min((page-1)*size, len(window)), min(page*size, len(window))
	return json.Unmarshal([]byte("["+strings.Join(window[start:end], ",")+"]"), out)
}

func TestBudgetedListingStopsAtRowCapAndDeliversEveryRowOnce(t *testing.T) {
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	var rows []string
	add := func(second, count int) {
		for i := 0; i < count; i++ {
			at := from.Add(time.Duration(second)*time.Second + time.Duration(i)*time.Millisecond)
			rows = append(rows, fmt.Sprintf(`{"id":"r%06d","created_at":%q,"path":"chat/completions","duration":1}`, len(rows), at.Format(time.RFC3339Nano)))
		}
	}
	// A run of one second crosses page and cap boundaries, then a long tail.
	add(0, 3)
	add(1, listRowCap+2*logPageSize+7)
	for second := 2; len(rows) < 4*listRowCap; second++ {
		add(second, 3)
	}
	api := &pagedAPI{rows: rows, fakeAPI: fakeAPI{detail: `{}`}}
	cfg := &config.Config{Cloudflare: config.CloudflareConfig{AccountID: "example"}, AIGateway: config.AIGatewayConfig{Gateways: []string{"gateway"}}}
	logs := NewLogs(cfg, api)
	seen := map[string]int{}
	cursor := from
	for calls := 0; cursor.Before(to); calls++ {
		if calls > 20 {
			t.Fatal("budgeted collection did not converge")
		}
		api.pages = 0
		out := &fakeEmitter{}
		mark, err := logs.CollectWindowBudget(context.Background(), cursor, to, 1<<30, out)
		if err != nil {
			t.Fatal(err)
		}
		if !mark.After(cursor) || !mark.Equal(mark.Truncate(time.Second)) {
			t.Fatalf("mark %s did not advance to a whole second from %s", mark, cursor)
		}
		// The cap plus the longest single-second run, plus the confirming page.
		if limit := (listRowCap+listRowCap+2*logPageSize+7)/logPageSize + 2; api.pages > limit {
			t.Fatalf("one call listed %d pages, limit %d", api.pages, limit)
		}
		for _, e := range out.logEvents {
			seen[attr(e.attrs, semconv.AttrAIGatewayLogID)]++
		}
		cursor = mark
	}
	if len(seen) != len(rows) {
		t.Fatalf("delivered %d of %d rows", len(seen), len(rows))
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("row %s delivered %d times", id, n)
		}
	}
}
