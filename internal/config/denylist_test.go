package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rknightion/cf2otel/internal/semconv"
)

// Allows the public Load regression to execute against the pre-field base.
func loadedDenylist(c *Config, name string) []string {
	field := reflect.ValueOf(c.OTLP).FieldByName(name)
	if !field.IsValid() {
		return nil
	}
	return field.Interface().([]string)
}

func TestDenylistLoadKnownAndEnvironmentPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := "otlp:\n  metric_denylist: [" + semconv.MetricDNSQueries + ", " + semconv.MetricDNSQueries + "]\n  attribute_denylist: [" + semconv.AttrAccessUserID + ", " + semconv.AttrAccessUserID + "]\n"
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("known deny keys must load without credentials or exporter initialization: %v", err)
	}
	check := func(c *Config, m, a []string) {
		t.Helper()
		gotM, gotA := loadedDenylist(c, "MetricDenylist"), loadedDenylist(c, "AttributeDenylist")
		if !reflect.DeepEqual(gotM, m) || !reflect.DeepEqual(gotA, a) {
			t.Fatalf("denylist values=%v/%v", gotM, gotA)
		}
	}
	check(c, []string{semconv.MetricDNSQueries}, []string{semconv.AttrAccessUserID})
	t.Setenv("CF2OTEL_OTLP__METRIC_DENYLIST", semconv.MetricAPIRequests+","+semconv.MetricAPIRequests)
	t.Setenv("CF2OTEL_OTLP__ATTRIBUTE_DENYLIST", semconv.AttrEventName)
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	check(c, []string{semconv.MetricAPIRequests}, []string{semconv.AttrEventName})
	t.Setenv("CF2OTEL_OTLP__METRIC_DENYLIST", "")
	t.Setenv("CF2OTEL_OTLP__ATTRIBUTE_DENYLIST", "")
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	check(c, []string{}, []string{})
}

func TestDenylistLoadRejectsInvalidExactNames(t *testing.T) {
	for _, input := range []struct{ key, value string }{
		{"metric_denylist", "cloudflare.unknown"}, {"attribute_denylist", strings.ToUpper(semconv.AttrAccessUserID)}, {"attribute_denylist", ""}, {"metric_denylist", semconv.MetricDNSQueries + "*"},
	} {
		t.Run(input.key+input.value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("otlp:\n  "+input.key+": [\""+input.value+"\"]\n"), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), "otlp."+input.key) {
				t.Fatalf("invalid deny key did not fail Load: %v", err)
			}
		})
	}
	t.Setenv("CF2OTEL_OTLP__ATTRIBUTE_DENYLIST", semconv.AttrAccessUserID+",")
	if _, err := Load(""); err == nil {
		t.Fatal("empty comma-delimited member accepted")
	}
}

func TestDenylistDefaultsEmpty(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedDenylist(c, "MetricDenylist")) != 0 || len(loadedDenylist(c, "AttributeDenylist")) != 0 {
		t.Fatal("default filtering enabled")
	}
}
