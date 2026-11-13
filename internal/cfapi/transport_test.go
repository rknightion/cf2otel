package cfapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

func TestClientReplacesHTTP2ConnectionSilencedByOutage(t *testing.T) {
	var http2 atomic.Bool
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http2.Store(r.ProtoMajor == 2)
		_, _ = w.Write([]byte(`{"success":true,"result":{}}`))
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	original := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	defer func() { http.DefaultTransport = original }()
	proxy := newBlackholeProxy(t, srv.Listener.Addr().String())
	c := New(config.CloudflareConfig{APIBase: "https://" + proxy.Addr(), APIToken: "test", Timeout: time.Second})
	get := func() error {
		var out struct{}
		return c.Get(context.Background(), "/zones", nil, &out)
	}
	if err := get(); err != nil {
		t.Fatal(err)
	}
	if !http2.Load() {
		t.Fatal("fixture did not negotiate HTTP/2")
	}
	proxy.Outage()
	if err := get(); err == nil {
		t.Fatal("request succeeded during the outage")
	}
	proxy.Restore()
	deadline := time.Now().Add(30 * time.Second)
	for err := get(); err != nil; err = get() {
		if time.Now().After(deadline) {
			t.Fatalf("client still failing 30s after the network returned: %v", err)
		}
	}
}
