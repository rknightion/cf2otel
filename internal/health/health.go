package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

// Handler serves a loopback health endpoint without exposing configuration.
func Handler(c *config.Config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if c == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unavailable"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(struct {
			Status string `json:"status"`
		}{"ok"})
	})
	return mux
}

// Listen rejects non-loopback addresses even if the caller skipped config validation.
func Listen(c *config.Config) (net.Listener, error) {
	if c == nil {
		return nil, fmt.Errorf("nil health config")
	}
	host, _, err := net.SplitHostPort(c.Health.Listen)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("health listener must bind loopback")
	}
	return net.Listen("tcp", c.Health.Listen)
}

// Serve runs until ctx is cancelled; the caller owns its goroutine.
func Serve(ctx context.Context, c *config.Config) error {
	l, err := Listen(c)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: Handler(c), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	err = srv.Serve(l)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Probe accepts a base URL and checks the actual /healthz status.
func Probe(ctx context.Context, baseURL string) error {
	if strings.TrimRight(baseURL, "/") != baseURL || strings.Contains(baseURL, "/healthz") || !strings.HasPrefix(baseURL, "http://") {
		return fmt.Errorf("health probe requires an http base URL")
	}
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	r, err := client.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("health probe returned %s", r.Status)
	}
	return nil
}

func ProbeListen(ctx context.Context, listen string) error { return Probe(ctx, "http://"+listen) }
