package promexport

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestListenerBindAndCancellation(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Reader.Shutdown(context.Background()) }()
	server, err := e.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Listen(server.Addr().String()); err == nil {
		t.Fatal("occupied port accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	client := &http.Client{Timeout: time.Second}
	res, err := client.Get("http://" + server.Addr().String() + "/metrics")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	_ = res.Body.Close()
	// No provider is registered here; the endpoint still binds and serves.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not complete")
	}
	conn, err := net.DialTimeout("tcp", server.Addr().String(), time.Second)
	if err == nil {
		_ = conn.Close()
		t.Fatal("listener survived cancellation")
	}
}
