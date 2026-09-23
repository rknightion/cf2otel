package telemetry

import (
	"context"
	"strings"
	"testing"
)

func TestCredentialEndpointRequiresHTTPS(t *testing.T) {
	_, err := NewProviders(context.Background(), ProviderOptions{Endpoint: "http://example.com/otlp", Protocol: "http", InstanceID: "test", Token: "secret"})
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("expected HTTPS error, got %v", err)
	}
}
