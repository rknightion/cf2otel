package cfapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

// Build a normal executable: re-executing a test binary would keep
// testing.Testing true and could not observe the production rejection surface.
func TestProcessRateLimitClockRejectedOutsideTests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// The source must reside within the module for Go's internal-package
	// import rule; a system temporary directory is outside that boundary.
	dir, err := os.MkdirTemp("../..", ".clock-witness-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	source := filepath.Join(dir, "main.go")
	const program = `package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/config"
)

type forbiddenClock struct{}
func (*forbiddenClock) Now() time.Time { panic("rejected clock was used") }
func (*forbiddenClock) NewTimer(time.Duration) cfapi.AdmissionTimer { panic("rejected clock was used") }

func main() {
	if testing.Testing() { panic("witness must be a normal executable") }
	cfg := config.Default().Cloudflare
	changed := cfg.RateLimit
	changed.RequestsPerSecond = 1.5
	clock := &forbiddenClock{}
	if err := cfapi.ConfigureProcessRateLimitWithClock(changed, clock); err == nil {
		panic("production clock installation accepted before traffic")
	}
	if err := cfapi.ConfigureProcessRateLimitWithClock(cfg.RateLimit, nil); err == nil {
		panic("production nil clock accepted")
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte("{\"result\":[]}"))
	}))
	defer server.Close()
	cfg.APIBase = server.URL
	client := cfapi.New(cfg)
	var rows []any
	if err := client.Get(context.Background(), "/first", nil, &rows); err != nil { panic(err) }
	// Active configuration identity proves the refused call did not install
	// its otherwise valid, changed capacity before the first admission.
	if err := cfapi.ConfigureProcessRateLimit(cfg.RateLimit); err != nil { panic(err) }
	if err := cfapi.ConfigureProcessRateLimitWithClock(cfg.RateLimit, clock); err == nil {
		panic("production clock installation accepted after traffic")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := client.Get(ctx, "/second", nil, &rows); !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		panic(fmt.Sprintf("refused clock changed real pacing: err=%v calls=%d", err, calls.Load()))
	}
	fmt.Println("normal executable: clock rejected before/after traffic; configuration, real clock and spent credit unchanged")
}
`
	if err := os.WriteFile(source, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "clock-witness")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, source)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build normal clock witness: %v\n%s", err, output)
	}
	// Neither fixture markers nor test-looking arguments may enable the seam
	// in an executable built without go test.
	cmd := exec.CommandContext(ctx, binary, "-test.v")
	cmd.Env = append(os.Environ(), "CF2OTEL_TEST_DEFAULT_CHILD=1", "CF2OTEL_TEST_PACK_CHILD=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("production clock witness: %v\n%s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}

// A subprocess is the process edge: configuration is immutable after traffic,
// so this exercises a genuinely fresh configured process rather than resetting
// the singleton in a test or changing production defaults for fixtures.
// The former 64-bit collision must either be rejected at the loader boundary
// (the bounded contract) or distinguished after real traffic; it cannot silently
// accept a different active configuration.
func TestProcessRateLimitReviewIntegerCollision(t *testing.T) {
	if os.Getenv("LOOP_TEST_COLLISION_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProcessRateLimitReviewIntegerCollision$", "-test.v")
		cmd.Env = append(os.Environ(), "LOOP_TEST_COLLISION_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("integer collision process: %v\n%s", err, output)
		}
		return
	}
	load := func(burst string) (*config.Config, error) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("cloudflare:\n  rate_limit:\n    requests_per_second: 0.5\n    burst: "+burst+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		return config.Load(path)
	}
	secondBurst := "9223372036854775806"
	first, err := load("9223372036854775807")
	if err != nil {
		if !strings.Contains(err.Error(), "cloudflare.rate_limit.burst") {
			t.Fatal(err)
		}
		if _, err := load("9223372036854775806"); err == nil || !strings.Contains(err.Error(), "cloudflare.rate_limit.burst") {
			t.Fatalf("second oversized burst not rejected: %v", err)
		}
		// Under the bounded contract, also exercise exact integer identity
		// at the largest supported capacity through the same public path.
		first, err = load("400")
		if err != nil {
			t.Fatal(err)
		}
		secondBurst = "399"
	}
	if err := ConfigureProcessRateLimit(first.Cloudflare.RateLimit); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":[]}`))
	}))
	defer server.Close()
	first.Cloudflare.APIBase = server.URL
	var out []any
	if err := New(first.Cloudflare).Get(context.Background(), "/first", nil, &out); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureProcessRateLimit(first.Cloudflare.RateLimit); err != nil {
		t.Fatalf("identical integer burst rejected after HTTP traffic: %v", err)
	}
	second, err := load(secondBurst)
	if err != nil {
		t.Fatal(err)
	}
	if err := ConfigureProcessRateLimit(second.Cloudflare.RateLimit); err == nil {
		t.Fatal("distinct integer bursts collided after HTTP traffic")
	}
}

func TestProcessRateLimitRepeatedConfiguration(t *testing.T) {
	if os.Getenv("CF2OTEL_TEST_LIFECYCLE_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProcessRateLimitRepeatedConfiguration$", "-test.v")
		cmd.Env = append(os.Environ(), "CF2OTEL_TEST_LIFECYCLE_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("lifecycle process: %v\n%s", err, output)
		}
		return
	}
	cfg := config.Default().Cloudflare
	if err := ConfigureProcessRateLimit(cfg.RateLimit); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"result":[]}`))
	}))
	defer server.Close()
	cfg.APIBase = server.URL
	var out []any
	if err := New(cfg).Get(context.Background(), "/first", nil, &out); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureProcessRateLimit(cfg.RateLimit); err != nil {
		t.Fatalf("identical active configuration rejected: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := NewObserved(cfg, nil).Get(ctx, "/second", nil, &out); !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatalf("reconfiguration reset active bucket: err=%v calls=%d", err, calls.Load())
	}
	if err := ConfigureProcessRateLimit(config.RateLimitConfig{RequestsPerSecond: 1.5, Burst: 1}); err == nil {
		t.Fatal("different active configuration accepted")
	}
}

func TestConfiguredLimiterPublicPaths(t *testing.T) {
	if os.Getenv("CF2OTEL_TEST_LIMITER_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestConfiguredLimiterPublicPaths$", "-test.v")
		cmd.Env = append(os.Environ(), "CF2OTEL_TEST_LIMITER_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("configured process: %v\n%s", err, output)
		}
		return
	}
	synctest.Test(t, func(t *testing.T) {
		budget := config.RateLimitConfig{RequestsPerSecond: 0.9, Burst: 2}
		if err := ConfigureProcessRateLimit(budget); err != nil {
			t.Fatal(err)
		}
		var calls, retries atomic.Int32
		var received []time.Time
		var mu sync.Mutex
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			mu.Lock()
			received = append(received, time.Now())
			mu.Unlock()
			switch r.URL.Path {
			case "/pages":
				if r.URL.Query().Get("page") == "1" {
					_, _ = w.Write([]byte(`{"result":[{"id":"opaque"}]}`))
				} else {
					_, _ = w.Write([]byte(`{"result":[]}`))
				}
			case "/retry":
				if retries.Add(1) == 1 {
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(429)
					return
				}
				_, _ = w.Write([]byte(`{"result":[]}`))
			case "/redirect":
				http.Redirect(w, r, "/redirect-final", http.StatusTemporaryRedirect)
			case "/redirect-final":
				_, _ = w.Write([]byte(`{"result":[]}`))
			case "/loop":
				http.Redirect(w, r, "/loop", http.StatusFound)
			case "/slow":
				select {
				case <-time.After(60 * time.Millisecond):
				case <-r.Context().Done():
				}
				_, _ = w.Write([]byte(`{"result":[]}`))
			default:
				_, _ = w.Write([]byte(`{"result":[]}`))
			}
		})
		originalTransport := http.DefaultTransport
		http.DefaultTransport = rateTestTransport(func(r *http.Request) (*http.Response, error) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if err := r.Context().Err(); err != nil {
				return nil, err
			}
			return w.Result(), nil
		})
		defer func() { http.DefaultTransport = originalTransport }()
		cfg := config.Default().Cloudflare
		cfg.APIBase = "http://fixture.invalid"
		cfg.Timeout = time.Second
		a, b := New(cfg), NewObserved(cfg, nil)
		var out []any
		started := time.Now()
		if err := a.Pages(context.Background(), "/pages", nil, 1, &out); err != nil {
			t.Fatal(err)
		}
		if len(out) != 1 {
			t.Fatalf("pagination rows=%d", len(out))
		}
		if err := b.Get(context.Background(), "/retry", nil, &out); err != nil {
			t.Fatal(err)
		}
		if err := a.Get(context.Background(), "/redirect", nil, &out); err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 6 || time.Since(started) < time.Duration(4*float64(time.Second)/budget.RequestsPerSecond) {
			t.Fatalf("shared configured pacing: calls=%d elapsed=%s", calls.Load(), time.Since(started))
		}
		mu.Lock()
		for i := 2; i < len(received); i++ {
			if received[i].Sub(received[i-1]) < time.Duration(float64(time.Second)/budget.RequestsPerSecond) {
				t.Errorf("hop %d was not paced: %s", i, received[i].Sub(received[i-1]))
			}
		}
		mu.Unlock()
		if err := ConfigureProcessRateLimit(cfg.RateLimit); err == nil {
			t.Fatal("active budget was reconfigured")
		}
		cfg.Timeout = 10 * time.Millisecond
		c := New(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := c.Get(ctx, "/slow", nil, &out); !os.IsTimeout(err) || ctx.Err() != nil {
			t.Fatalf("network timeout not retained independently of caller context: %v, caller=%v", err, ctx.Err())
		}
		before := calls.Load()
		queued, stop := context.WithTimeout(context.Background(), time.Millisecond)
		defer stop()
		if err := c.Get(queued, "/unused", nil, &out); !errors.Is(err, context.DeadlineExceeded) || calls.Load() != before {
			t.Fatalf("queue cancellation made a network attempt: %v calls=%d before=%d", err, calls.Load(), before)
		}
		before = calls.Load()
		if err := a.Get(context.Background(), "/loop", nil, &out); err == nil || calls.Load()-before != 10 {
			t.Fatalf("redirect chain not bounded: err=%v attempts=%d", err, calls.Load()-before)
		}
		var forwarded atomic.Bool
		http.DefaultTransport = rateTestTransport(func(r *http.Request) (*http.Response, error) {
			w := httptest.NewRecorder()
			if r.URL.Host == "target.invalid" {
				forwarded.Store(r.Header.Get("Authorization") != "")
				_, _ = w.Write([]byte(`{"result":[]}`))
			} else {
				http.Redirect(w, r, "http://target.invalid/final", http.StatusFound)
			}
			return w.Result(), nil
		})
		cfg.APIBase, cfg.APIToken, cfg.Timeout = "http://redirect.invalid", "opaque", time.Second
		if err := New(cfg).Get(context.Background(), "/start", nil, &out); err != nil || forwarded.Load() {
			t.Fatalf("cross-origin redirect: err=%v credentialsForwarded=%v", err, forwarded.Load())
		}
	})
}

// This fixture parses the wire independently of the production planner. It
// supports both original and packed aliases and records exact selection tails.
// No production parser, cache, ticket or clock hook is used by the public witness.
type publicWireNode struct {
	name, key, args, tail string
	children              []publicWireNode
}

type publicWireParser struct {
	text string
	pos  int
}

func (p *publicWireParser) space() {
	for p.pos < len(p.text) && strings.ContainsRune(" \t\n\r,", rune(p.text[p.pos])) {
		p.pos++
	}
}
func (p *publicWireParser) name() string {
	p.space()
	start := p.pos
	for p.pos < len(p.text) && (p.text[p.pos] == '_' || p.text[p.pos] >= 'a' && p.text[p.pos] <= 'z' || p.text[p.pos] >= 'A' && p.text[p.pos] <= 'Z' || p.text[p.pos] >= '0' && p.text[p.pos] <= '9') {
		p.pos++
	}
	return p.text[start:p.pos]
}
func (p *publicWireParser) balanced(open, close byte) (string, error) {
	start := p.pos
	if p.pos >= len(p.text) || p.text[p.pos] != open {
		return "", errors.New("fixture expected delimiter")
	}
	depth, quoted, escaped := 0, false, false
	for p.pos < len(p.text) {
		ch := p.text[p.pos]
		p.pos++
		if quoted {
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				quoted = false
			}
			continue
		}
		if ch == '"' {
			quoted = true
			continue
		}
		if ch == open {
			depth++
		}
		if ch == close {
			depth--
			if depth == 0 {
				return p.text[start:p.pos], nil
			}
		}
	}
	return "", errors.New("fixture unterminated delimiter")
}
func (p *publicWireParser) nodes() ([]publicWireNode, error) {
	p.space()
	if p.pos >= len(p.text) || p.text[p.pos] != '{' {
		return nil, errors.New("fixture expected selection")
	}
	p.pos++
	var nodes []publicWireNode
	for {
		p.space()
		if p.pos >= len(p.text) {
			return nil, errors.New("fixture unterminated selection")
		}
		if p.text[p.pos] == '}' {
			p.pos++
			return nodes, nil
		}
		name := p.name()
		if name == "" {
			return nil, errors.New("fixture expected name")
		}
		key := name
		p.space()
		if p.pos < len(p.text) && p.text[p.pos] == ':' {
			p.pos++
			name = p.name()
		}
		start := p.pos
		node := publicWireNode{name: name, key: key}
		p.space()
		if p.pos < len(p.text) && p.text[p.pos] == '(' {
			var err error
			node.args, err = p.balanced('(', ')')
			if err != nil {
				return nil, err
			}
			p.space()
		}
		if p.pos < len(p.text) && p.text[p.pos] == '{' {
			var err error
			node.children, err = p.nodes()
			if err != nil {
				return nil, err
			}
		}
		node.tail = p.text[start:p.pos]
		nodes = append(nodes, node)
	}
}
func publicWireQuery(q string) ([]publicWireNode, error) {
	p := publicWireParser{text: q}
	nodes, err := p.nodes()
	p.space()
	if err != nil || p.pos != len(q) || len(nodes) != 1 || nodes[0].name != "viewer" {
		return nil, errors.New("fixture invalid query")
	}
	return nodes[0].children, nil
}

var publicFrom = regexp.MustCompile(`datetime_geq:("(?:\\.|[^"\\])*")`)

func publicWireResponse(q string, ledger map[string]int) ([]byte, int, error) {
	scopes, err := publicWireQuery(q)
	if err != nil {
		return nil, 0, err
	}
	viewer := map[string]any{}
	count := 0
	for _, scope := range scopes {
		node := map[string]any{}
		for _, selection := range scope.children {
			if selection.name == "settings" {
				metadata := map[string]any{}
				for _, dataset := range selection.children {
					count++
					ledger[scope.name+scope.args+"/settings/"+dataset.name+dataset.tail]++
					metadata[dataset.key] = map[string]any{"enabled": true, "availableFields": []string{"datetime", "sum_count"}, "maxNumberOfFields": 64, "maxDuration": 3600, "notOlderThan": 2678400, "maxPageSize": 10000}
				}
				node[selection.key] = metadata
			} else {
				count++
				ledger[scope.name+scope.args+"/data/"+selection.name+selection.tail]++
				match := publicFrom.FindStringSubmatch(selection.args)
				if len(match) != 2 {
					return nil, 0, errors.New("fixture missing source window")
				}
				var from string
				if err := json.Unmarshal([]byte(match[1]), &from); err != nil {
					return nil, 0, err
				}
				node[selection.key] = []any{map[string]any{"datetime": from, "sum": map[string]any{"count": 2}}}
			}
		}
		viewer[scope.key] = []any{node}
	}
	raw, err := json.Marshal(map[string]any{"data": map[string]any{"viewer": viewer}})
	return raw, count, err
}

// Portable to the named base: only the existing exported client/config API and
// a real process limiter under synctest are exercised. The same responder and
// semantic assertions run before the physical-count assertion on both versions.
func TestPublicClientPollingPacking(t *testing.T) {
	if runDefaultBudgetProcess(t) {
		return
	}
	synctest.Test(t, func(t *testing.T) {
		cfg := config.Default().Cloudflare
		if err := ConfigureProcessRateLimit(cfg.RateLimit); err != nil {
			t.Fatal(err)
		}
		cfg.APIBase = "http://fixture.invalid"
		ledger := map[string]int{}
		expected := map[string]int{}
		var attempts []time.Time
		physical := 0
		var mu sync.Mutex
		original := http.DefaultTransport
		http.DefaultTransport = rateTestTransport(func(r *http.Request) (*http.Response, error) {
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			var body struct {
				Query string `json:"query"`
			}
			if err = json.Unmarshal(raw, &body); err != nil {
				return nil, err
			}
			mu.Lock()
			defer mu.Unlock()
			response, nodes, err := publicWireResponse(body.Query, ledger)
			if err != nil {
				return nil, err
			}
			if nodes > 8 || len(raw) > 32<<10 {
				return nil, fmt.Errorf("envelope cap: nodes=%d bytes=%d", nodes, len(raw))
			}
			physical++
			attempts = append(attempts, time.Now())
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(response))), Request: r}, nil
		})
		defer func() { http.DefaultTransport = original }()
		client := New(cfg)
		started := time.Now()
		// Two identical-sized recurring cycles, all 23 scopes and eight datasets,
		// cold discovery in cycle zero, unchanged explicit five-minute windows.
		for cycle := 0; cycle < 2; cycle++ {
			var wg sync.WaitGroup
			for zone := 0; zone < 23; zone++ {
				for dataset := 0; dataset < 8; dataset++ {
					id, name := fmt.Sprintf("opaque-zone-%02d", zone), fmt.Sprintf("events%d", dataset)
					from := started.Add(time.Duration(cycle-2) * 5 * time.Minute)
					to := from.Add(5 * time.Minute)
					tag := `quoted } ) : cfpack_n0 \ " marker`
					filter := fmt.Sprintf(`{datetime_geq:%q,datetime_lt:%q,tag:%q}`, from.Format(time.RFC3339), to.Format(time.RFC3339), tag)
					args := fmt.Sprintf(`(filter:{zoneTag:%q})`, id)
					expected["zones"+args+"/data/"+name+"(limit:100,filter:"+filter+"){datetime sum{count}}"]++
					if cycle == 0 {
						expected["zones"+args+"/settings/"+name+"{enabled availableFields maxNumberOfFields maxDuration notOlderThan maxPageSize}"]++
					}
					wg.Go(func() {
						var rows []struct {
							Datetime string `json:"datetime"`
							Sum      struct {
								Count int `json:"count"`
							} `json:"sum"`
						}
						err := client.Query(context.Background(), GraphQLRequest{Scope: ZoneScope, ScopeID: id, Dataset: name, WantedFields: []string{"datetime", "sum.count"}, From: from, To: to, Limit: 100, Filter: map[string]any{"tag": tag}}, &rows)
						if err != nil || len(rows) != 1 || rows[0].Datetime != from.Format(time.RFC3339) || rows[0].Sum.Count != 2 {
							t.Errorf("semantic delivery %s/%s: rows=%+v err=%v", id, name, rows, err)
						}
					})
				}
			}
			wg.Wait()
		}
		mu.Lock()
		defer mu.Unlock()
		if len(ledger) != len(expected) {
			t.Fatalf("selection ledger size=%d expected=%d", len(ledger), len(expected))
		}
		for key, count := range expected {
			if ledger[key] != count {
				t.Fatalf("selection changed: %q got=%d want=%d", key, ledger[key], count)
			}
		}
		keys := make([]string, 0, len(ledger))
		for key := range ledger {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var summary strings.Builder
		for _, key := range keys {
			fmt.Fprintf(&summary, "%s=%d\n", key, ledger[key])
		}
		t.Logf("semantic selections=%d delivered rows=368 sha256=%x physical=%d elapsed=%s", len(ledger), sha256.Sum256([]byte(summary.String())), physical, time.Since(started))
		graphRate := 0.49
		for i, at := range attempts {
			end := i
			for end < len(attempts) && attempts[end].Before(at.Add(300*time.Second)) {
				end++
			}
			if end-i > 148 {
				t.Errorf("rolling GraphQL budget=%d >148", end-i)
			}
			if i > 0 && at.Sub(attempts[i-1]) < time.Duration(float64(time.Second)/graphRate) {
				t.Errorf("GraphQL spacing=%s violates 0.49rps burst1", at.Sub(attempts[i-1]))
			}
		}
		if physical >= 552 || physical > 100 {
			t.Errorf("packing failed: physical=%d for identical 552 logical selections; want <=100", physical)
		}
	})
}

func TestPublicClientFIFOAdmission(t *testing.T) {
	if runDefaultBudgetProcess(t) {
		return
	}
	synctest.Test(t, func(t *testing.T) {
		cfg := config.Default().Cloudflare
		if err := ConfigureProcessRateLimit(cfg.RateLimit); err != nil {
			t.Fatal(err)
		}
		cfg.APIBase = "http://fixture.invalid"
		var mu sync.Mutex
		var paths []string
		var attempts []time.Time
		original := http.DefaultTransport
		http.DefaultTransport = rateTestTransport(func(r *http.Request) (*http.Response, error) {
			mu.Lock()
			paths = append(paths, r.URL.Path)
			attempts = append(attempts, time.Now())
			mu.Unlock()
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"result":[]}`)), Request: r}, nil
		})
		defer func() { http.DefaultTransport = original }()
		client := New(cfg)
		get := func(ctx context.Context, path string) error { var rows []any; return client.Get(ctx, path, nil, &rows) }
		if err := get(context.Background(), "/initial"); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		launch := func(ctx context.Context, path string) {
			wg.Go(func() {
				if err := get(ctx, path); err != nil && !errors.Is(err, context.Canceled) {
					t.Errorf("%s: %v", path, err)
				}
			})
			synctest.Wait()
		}
		launch(context.Background(), "/A")
		ctx, cancel := context.WithCancel(context.Background())
		launch(ctx, "/B")
		cancel()
		synctest.Wait()
		launch(context.Background(), "/C")
		for i := 0; i < 20; i++ {
			launch(context.Background(), fmt.Sprintf("/new-%02d", i))
		}
		wg.Wait()
		mu.Lock()
		defer mu.Unlock()
		if len(paths) != 23 || paths[1] != "/A" || paths[2] != "/C" {
			t.Fatalf("FIFO/starvation: paths=%v", paths)
		}
		for i := 3; i < len(paths); i++ {
			if paths[i] != fmt.Sprintf("/new-%02d", i-3) {
				t.Fatalf("FIFO overtaking: %v", paths)
			}
		}
		for i := 1; i < len(attempts); i++ {
			if attempts[i].Sub(attempts[i-1]) < time.Duration(float64(time.Second)/cfg.RateLimit.RequestsPerSecond) {
				t.Errorf("aggregate budget spacing=%s", attempts[i].Sub(attempts[i-1]))
			}
		}
		// Cancellation consumes no credit: one refill between each survivor.
		if attempts[2].Sub(attempts[0]) > time.Duration(2*float64(time.Second)/cfg.RateLimit.RequestsPerSecond)+2*time.Nanosecond {
			t.Fatal("queued cancellation spent credit")
		}
		t.Logf("no starved waiter: %v; elapsed=%s", paths, attempts[len(attempts)-1].Sub(attempts[0]))
	})
}

type rateTestTransport func(*http.Request) (*http.Response, error)

func (f rateTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// This independent wall-clock witness retains real localhost network timing.
func TestReviewIndependentNetworkTimeout(t *testing.T) {
	if runDefaultBudgetProcess(t) {
		return
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(60 * time.Millisecond):
		case <-r.Context().Done():
		}
		_, _ = w.Write([]byte(`{"result":[]}`))
	}))
	defer server.Close()
	cfg := config.Default().Cloudflare
	cfg.APIBase = server.URL
	cfg.Timeout = 10 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var out []any
	if err := New(cfg).Get(ctx, "/slow", nil, &out); !os.IsTimeout(err) || ctx.Err() != nil {
		t.Fatalf("independent network timeout: err=%v caller=%v", err, ctx.Err())
	}
}

func runDefaultBudgetProcess(t *testing.T) bool {
	t.Helper()
	if os.Getenv("CF2OTEL_TEST_DEFAULT_CHILD") == "1" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$", "-test.v")
	cmd.Env = append(os.Environ(), "CF2OTEL_TEST_DEFAULT_CHILD=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("default process: %v\n%s", err, output)
	}
	return true
}

func TestReviewSharedBudgetPublicBoundary(t *testing.T) {
	if runDefaultBudgetProcess(t) {
		return
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"success":true,"result":[]}`))
	}))
	defer server.Close()
	cfg := config.Default().Cloudflare
	cfg.APIBase = server.URL
	a, b := New(cfg), NewObserved(cfg, nil)
	var out []any
	if err := a.Get(context.Background(), "/zones", nil, &out); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := b.Get(ctx, "/accounts", nil, &out)
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatalf("shared limiter missing: err=%v upstreamAttempts=%d; want deadline and exactly one attempt", err, calls.Load())
	}
}

func TestReviewRedirectQueueOutsideNetworkTimeout(t *testing.T) {
	if runDefaultBudgetProcess(t) {
		return
	}
	// Allow the shared default burst to refill after earlier tests; do not replace
	// the process budget or bypass the real constructors.
	time.Sleep(2 * time.Second)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/zones", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"result":[]}`))
	}))
	defer server.Close()
	cfg := config.Default().Cloudflare
	cfg.APIBase = server.URL
	cfg.Timeout = 40 * time.Millisecond
	api := New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var out []any
	started := time.Now()
	err := api.Get(ctx, "/redirect", nil, &out)
	if err != nil || calls.Load() != 2 || time.Since(started) < time.Duration(float64(time.Second)/cfg.RateLimit.RequestsPerSecond)*9/10 {
		t.Fatalf("quota waiting consumed network timeout: err=%v attempts=%d elapsed=%s callerContext=%v; want successful paced redirect within caller deadline", err, calls.Load(), time.Since(started), ctx.Err())
	}
}

// GraphQL is the only permitted POST path. Redirecting it must never convert
// it to GET or replay its body to another API path or origin.
func TestGraphQLRedirectFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		crossOrigin bool
	}{
		{"moved", http.StatusMovedPermanently, false},
		{"found", http.StatusFound, true},
		{"see_other", http.StatusSeeOther, false},
		{"temporary", http.StatusTemporaryRedirect, false},
		{"permanent_cross_origin", http.StatusPermanentRedirect, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var first, redirected atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				redirected.Add(1)
				_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[]}}}`))
			}))
			defer target.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/graphql" {
					redirected.Add(1)
					_, _ = w.Write([]byte(`{"data":{"viewer":{"zones":[]}}}`))
					return
				}
				first.Add(1)
				if r.Method != http.MethodPost {
					t.Errorf("initial GraphQL method=%s; want POST", r.Method)
				}
				location := "/arbitrary-api-path"
				if tc.crossOrigin {
					location = target.URL + location
				}
				w.Header().Set("Location", location)
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			cfg := config.Default().Cloudflare
			cfg.APIBase = server.URL
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := New(cfg).DatasetSettings(ctx, ZoneScope, "opaque", "events")
			var httpErr *HTTPError
			if !errors.As(err, &httpErr) || httpErr.Status != tc.status || first.Load() != 1 || redirected.Load() != 0 {
				t.Fatalf("GraphQL redirect did not fail closed: err=%v first=%d redirected=%d; want HTTP %d and no second upstream request", err, first.Load(), redirected.Load(), tc.status)
			}
		})
	}
}
