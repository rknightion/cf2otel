package cfapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

func TestPollRequestsAreConcurrentAndIsolated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			w.WriteHeader(404)
		}
		_, _ = w.Write([]byte(`{"result":[]}`))
	}))
	defer server.Close()
	cfg := config.Default().Cloudflare
	cfg.APIBase = server.URL
	api := New(cfg)
	parent, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx, poll := WithPollRequests(parent)
	other, otherPoll := WithPollRequests(parent)
	var wg sync.WaitGroup
	for _, role := range []RequestRole{RequestRoleList, RequestRoleDetail, RequestRoleBody} {
		wg.Go(func() { var rows []any; _ = api.Get(WithRequestRole(ctx, role), "/ok", nil, &rows) })
	}
	wg.Go(func() { var rows []any; _ = api.Get(other, "/bad", nil, &rows) })
	wg.Wait()
	got := poll.Snapshot()
	if got.Requests != 3 || got.List != 1 || got.Detail != 1 || got.Body != 1 || got.Failures != 0 || got.HeaderSeconds <= 0 {
		t.Fatalf("poll totals: %+v", got)
	}
	got = otherPoll.Snapshot()
	if got.Requests != 1 || got.Other != 1 || got.Failures != 1 {
		t.Fatalf("other poll totals: %+v", got)
	}
}
