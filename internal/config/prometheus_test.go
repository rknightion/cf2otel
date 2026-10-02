package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrometheusConfigLoadAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("prometheus:\n  enabled: true\n  listen: 127.0.0.1:9466\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CF2OTEL_PROMETHEUS__LISTEN", ":9465")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.RedactedJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !c.Prometheus.Enabled {
		t.Fatal("YAML opt-in lost")
	}
	if !strings.Contains(string(b), `"prometheus"`) || !strings.Contains(string(b), `":9465"`) {
		t.Fatalf("missing effective Prometheus config: %s", b)
	}
	for _, listen := range []string{"", "127.0.0.1", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:http", "127.0.0.1:+9465", "http://127.0.0.1:9465", "user@127.0.0.1:9465", "/socket"} {
		c.Prometheus.Listen = listen
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "prometheus.listen") {
			t.Errorf("invalid listen %q not rejected: %v", listen, err)
		}
	}
	if Default().Prometheus.Enabled {
		t.Fatal("pull endpoint enabled by default")
	}
}
