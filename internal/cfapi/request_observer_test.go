package cfapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

func TestRequestObserverPhysicalRetriesAndRedirects(t *testing.T) {
	var upstream atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.Add(1)
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/retry", http.StatusFound)
			return
		}
		if upstream.Load() == 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"result":[]}`))
	}))
	defer server.Close()
	var observations []RequestObservation
	var names []string
	client := NewRequestObserved(config.CloudflareConfig{APIBase: server.URL, Timeout: time.Second}, func(ctx context.Context, o RequestObservation) {
		observations = append(observations, o)
		names = append(names, RequestCollector(ctx))
	})
	ctx, cancel := context.WithTimeout(WithCollector(context.Background(), "fixture.rest"), 5*time.Second)
	defer cancel()
	var out []any
	if err := client.Get(ctx, "/start", nil, &out); err != nil {
		t.Fatal(err)
	}
	if upstream.Load() != 4 || len(observations) != 4 {
		t.Fatalf("upstream=%d observations=%v", upstream.Load(), observations)
	}
	for i, o := range observations {
		wantStatus := []int{302, 503, 302, 200}[i]
		wantClass := ""
		if i == 1 {
			wantClass = "other"
		}
		if o.Status != wantStatus || o.ErrorClass != wantClass || o.Method != "rest" || names[i] != "fixture.rest" {
			t.Errorf("hop %d: %+v collector=%s", i, o, names[i])
		}
		if o.Started.IsZero() || o.Duration <= 0 || o.LimiterWait < 0 {
			t.Errorf("hop %d timing: %+v", i, o)
		}
	}
}

func TestRequestObserverHTTPAndGraphQLHeaderOutcomes(t *testing.T) {
	for _, status := range []int{200, 401, 403, 404, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"errors":[{"message":"private-upstream-text"}]}`))
			}))
			defer server.Close()
			var events []RequestObservation
			client := NewRequestObserved(config.CloudflareConfig{APIBase: server.URL, Timeout: time.Second}, func(ctx context.Context, o RequestObservation) {
				if RequestCollector(ctx) != "unattributed" {
					t.Error("outside request was attributed")
				}
				events = append(events, o)
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			response, err := client.graph(ctx, "", "", "{viewer{zones{settings}}}")
			if status == 200 {
				if err != nil || gqlErrors(response) == nil {
					t.Fatalf("logical GraphQL error lost: %+v %v", response, err)
				}
			} else if err == nil {
				t.Fatal("HTTP failure lost")
			}
			count := 1
			if status == 429 {
				count = 5
			}
			if len(events) != count {
				t.Fatalf("events=%d want=%d", len(events), count)
			}
			wantClass := map[int]string{200: "", 401: "auth", 403: "auth", 404: "other", 429: "rate_limited", 500: "other"}[status]
			for _, o := range events {
				if o.Method != "graphql" || o.Status != status || o.ErrorClass != wantClass {
					t.Errorf("outcome=%+v", o)
				}
			}
		})
	}
}

func TestRequestObserverTimeoutAndRejectedCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	var events []RequestObservation
	client := NewRequestObserved(config.CloudflareConfig{APIBase: server.URL, Timeout: 50 * time.Millisecond}, func(_ context.Context, o RequestObservation) {
		events = append(events, o)
	})
	var out []any
	if err := client.Get(context.Background(), "/timeout", nil, &out); err == nil {
		t.Fatal("expected timeout")
	}
	if len(events) != 1 || events[0].ErrorClass != "timeout" {
		t.Fatalf("timeout events=%+v", events)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.Get(cancelled, "/cancelled", nil, &out); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled error=%v", err)
	}
	if _, err := client.do(context.Background(), http.MethodDelete, "/guard", nil, nil); err == nil {
		t.Fatal("read-only guard accepted DELETE")
	}
	if len(events) != 1 {
		t.Fatalf("nonphysical calls observed: %+v", events)
	}
}

func TestRequestAttributionContextAndErrorClass(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := WithCollector(parent, "fixture.collector")
	if RequestCollector(parent) != "unattributed" || RequestCollector(ctx) != "fixture.collector" || RequestCollector(context.WithoutCancel(ctx)) != "fixture.collector" {
		t.Fatal("collector context leaked or was lost")
	}
	if RequestCollector(WithCollector(parent, "")) != "unattributed" {
		t.Fatal("empty attribution needs bounded fallback")
	}
	cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("attribution detached cancellation")
	}
	want := []string{"", "other", "timeout", "rate_limited", "auth", "other"}
	got := []string{
		requestErrorClass(200, nil),
		requestErrorClass(0, errors.New("private-upstream-text")),
		requestErrorClass(0, errors.Join(errors.New("private-upstream-text"), context.DeadlineExceeded)),
		requestErrorClass(429, nil), requestErrorClass(401, nil), requestErrorClass(500, nil),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("classes=%v want=%v", got, want)
	}
}
