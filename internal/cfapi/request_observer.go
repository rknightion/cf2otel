package cfapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

// RequestObservation describes one physical HTTP exchange, including retry and
// redirect hops. It contains no URL, scope identifier, body or error text.
// Duration measures Do through response headers (not body decoding), excluding
// LimiterWait. HTTP 2xx envelopes are not classified as transport failures.
type RequestObservation struct {
	Method      string // "rest" or "graphql", never an HTTP verb or path
	Status      int
	Started     time.Time
	Duration    time.Duration
	LimiterWait time.Duration
	ErrorClass  string // absent on success; auth, rate_limited, timeout or other
}

// RequestObserver is called synchronously once per physical HTTP request, after
// headers or a transport error. Implementations must be concurrency-safe.
// Cancellation while acquiring quota emits nothing: no HTTP request was made.
type RequestObserver func(context.Context, RequestObservation)

// NewRequestObserved injects self-observability without coupling cfapi to OTLP.
// NewObserved retains its legacy route/status callback for existing consumers.
func NewRequestObserved(cfg config.CloudflareConfig, observer RequestObserver) *HTTPClient {
	client := New(cfg)
	client.requestObserver = observer
	return client
}

type collectorContextKey struct{}

// WithCollector attributes requests to the scheduler's registered collector.
// Only trusted scheduling code should set this value, never upstream data.
func WithCollector(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, collectorContextKey{}, name)
}

// RequestCollector returns the scheduler context's name, or the bounded fallback
// for startup discovery, explore and callers outside the scheduler.
func RequestCollector(ctx context.Context) string {
	if name, ok := ctx.Value(collectorContextKey{}).(string); ok && name != "" {
		return name
	}
	return "unattributed"
}

func requestErrorClass(status int, err error) string {
	if err != nil {
		var network net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout()) {
			return "timeout"
		}
		return "other"
	}
	switch status {
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusUnauthorized, http.StatusForbidden:
		return "auth"
	}
	if status >= http.StatusBadRequest {
		return "other"
	}
	return ""
}
