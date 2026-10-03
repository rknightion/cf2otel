package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEntitlementBackoffLoadAndValidation(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.Cloudflare.EntitlementBackoff != time.Hour {
		t.Fatalf("default backoff = %v, want 1h", c.Cloudflare.EntitlementBackoff)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("cloudflare:\n  entitlement_backoff: 2h\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err = Load(path)
	if err != nil || c.Cloudflare.EntitlementBackoff != 2*time.Hour {
		t.Fatalf("YAML backoff: %v error=%v", c, err)
	}
	t.Setenv("CF2OTEL_CLOUDFLARE__ENTITLEMENT_BACKOFF", "30m")
	c, err = Load(path)
	if err != nil || c.Cloudflare.EntitlementBackoff != 30*time.Minute {
		t.Fatalf("environment precedence: %v error=%v", c, err)
	}
	for _, value := range []string{"0s", "-1m", "not-duration"} {
		t.Setenv("CF2OTEL_CLOUDFLARE__ENTITLEMENT_BACKOFF", value)
		c, err = Load(path)
		if err == nil {
			err = c.Validate()
		}
		if err == nil || !strings.Contains(err.Error(), "entitlement_backoff") {
			t.Fatalf("invalid duration %q accepted or unrelated error: %v", value, err)
		}
	}
}
