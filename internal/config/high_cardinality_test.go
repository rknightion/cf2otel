package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Catch an accepted opt-in configuration that silently loses its feature budget.
func TestHTTPHighCardinalityLoad(t *testing.T) {
	t.Setenv("CF2OTEL_CLOUDFLARE__API_TOKEN", "fixture-token")
	t.Setenv("CF2OTEL_HTTP__HIGH_CARDINALITY_LIMIT", "2")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("http:\n  high_cardinality_limit: 7\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := c.RedactedJSON()
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["http"].(map[string]any)["high_cardinality_limit"] != float64(2) {
		t.Fatal("HTTP high-cardinality limit missing or environment precedence lost")
	}
}
