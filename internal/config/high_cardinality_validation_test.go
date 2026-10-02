package config

import (
	"strings"
	"testing"
)

// Catch ambiguous route lookup and unsafe names before polling starts.
func TestHTTPHighCardinalityValidation(t *testing.T) {
	for _, mode := range []string{"valid", "empty-routes", "duplicate-template", "raw-number", "unsafe-name", "hex-name", "query", "wrong-placeholder", "host-port", "limit-zero"} {
		t.Run(mode, func(t *testing.T) {
			c := Default()
			c.Cloudflare.APIToken = "fixture"
			c.Cloudflare.AccountID = "fixture"
			c.OTLP.Endpoint = "https://fixture"
			c.OTLP.GrafanaCloud.InstanceID = "fixture"
			c.OTLP.GrafanaCloud.Token = "fixture"
			c.HTTP.Breakdowns = []string{"error_path", "colo", "asn"}
			c.HTTP.ErrorPathRoutes = map[string]string{"item": "/items/:number"}
			c.HTTP.HighCardinalityHosts = []string{"host-alpha"}
			switch mode {
			case "empty-routes":
				c.HTTP.ErrorPathRoutes = nil
			case "duplicate-template":
				c.HTTP.ErrorPathRoutes["alternate"] = "/items/:number"
			case "raw-number":
				c.HTTP.ErrorPathRoutes["item"] = "/items/123"
			case "unsafe-name":
				c.HTTP.ErrorPathRoutes = map[string]string{"other": "/items/:number"}
			case "hex-name":
				c.HTTP.ErrorPathRoutes = map[string]string{strings.Repeat("a", 8): "/items/:number"}
			case "query":
				c.HTTP.ErrorPathRoutes["item"] = "/items/:number?opaque"
			case "wrong-placeholder":
				c.HTTP.ErrorPathRoutes["item"] = "/items/:id"
			case "host-port":
				c.HTTP.HighCardinalityHosts = []string{"host-alpha:443"}
			case "limit-zero":
				c.HTTP.HighCardinalityLimit = 0
			}
			err := c.Validate()
			if (err == nil) != (mode == "valid") {
				t.Fatalf("configuration validation=%v", err)
			}
			if err != nil && (strings.Contains(err.Error(), "/items/") || strings.Contains(err.Error(), "host-alpha")) {
				t.Fatal("config validation leaked route or host values")
			}
		})
	}
}
