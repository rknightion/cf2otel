package config

import (
	"strings"
	"testing"
)

func TestLogConfigurationIsValidated(t *testing.T) {
	for _, tc := range []struct {
		level, format, want string
	}{
		{"verbose", "json", "log.level must be one of debug, info, warn, error"},
		{"info", "logfmt", "log.format must be json or text"},
	} {
		c := Default()
		c.Log = LogConfig{Level: tc.level, Format: tc.format}
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%+v not rejected with %q: %v", c.Log, tc.want, err)
		}
	}
	for _, level := range []string{"debug", "info", "warn", "error"} {
		for _, format := range []string{"json", "text"} {
			c := Default()
			c.Log = LogConfig{Level: level, Format: format}
			if err := c.Validate(); err != nil && strings.Contains(err.Error(), "log.") {
				t.Fatalf("%s/%s rejected: %v", level, format, err)
			}
		}
	}
}
