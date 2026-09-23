package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	o, err := Parse([]string{"-config", "a.yaml", "-once", "-datasets", "access.logins,httpreq.events", "-since", "2026-09-23T10:00:00Z", "-before", "2026-09-23T11:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.Once || o.Config != "a.yaml" || len(o.Datasets) != 2 || o.Since.IsZero() || o.Before.IsZero() {
		t.Fatalf("%+v", o)
	}
	if _, err := Parse([]string{"-since", "2026-09-23T11:00:00Z", "-before", "2026-09-23T10:00:00Z"}); err == nil {
		t.Fatal("accepted reversed window")
	}
	if _, err := Parse([]string{"-healthcheck", "-once"}); err == nil {
		t.Fatal("accepted mixed mode")
	}
}

func TestPermissionMessage(t *testing.T) {
	if got := MissingPermission(403, "GET /accounts/x/ai-gateway/gateways/x/logs/x/request"); !strings.Contains(got, "AI Gateway Read") {
		t.Fatal(got)
	}
	if got := MissingPermission(200, "anything"); got != "" {
		t.Fatal(got)
	}
	if got := ExploreSummary("aiGatewayLogs", 403, "GET /accounts/x/ai-gateway/gateways/x/logs/x/request"); !strings.Contains(got, "aiGatewayLogs: 403: missing AI Gateway Read") {
		t.Fatal(got)
	}
}

func TestClampInterval(t *testing.T) {
	if got := ClampInterval(time.Second); got != MinimumInterval {
		t.Fatal(got)
	}
	if got := ClampInterval(time.Minute); got != time.Minute {
		t.Fatal(got)
	}
}

func TestConfigPermissionAdvisory(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ConfigPermissionAdvisory(p); got != "" {
		t.Fatal(got)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ConfigPermissionAdvisory(p); !strings.Contains(got, "0600") {
		t.Fatal(got)
	}
}
