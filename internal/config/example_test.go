package config

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/knadh/koanf/parsers/yaml"
)

func TestPublishedExamplesCoverConfigSurface(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	example := readYAMLMap(t, filepath.Join(repoRoot, "config.example.yaml"))
	chart := readYAMLMap(t, filepath.Join(repoRoot, "charts", "cf2otel", "values.yaml"))

	collectors := nestedYAMLMap(t, example, "collectors")
	var missingCollectors []string
	for name := range Default().Collectors {
		if _, ok := collectors[name]; !ok {
			missingCollectors = append(missingCollectors, name)
		}
	}
	sort.Strings(missingCollectors)
	if len(missingCollectors) > 0 {
		t.Errorf("config.example.yaml omits registered collectors: %s", strings.Join(missingCollectors, ", "))
	}

	chartConfig := nestedYAMLMap(t, chart, "config")
	configType := reflect.TypeOf(Config{})
	var missingSections []string
	for i := 0; i < configType.NumField(); i++ {
		section := strings.Split(configType.Field(i).Tag.Get("yaml"), ",")[0]
		if section == "" || section == "-" {
			continue
		}
		if _, ok := chartConfig[section]; !ok {
			missingSections = append(missingSections, section)
		}
	}
	sort.Strings(missingSections)
	if len(missingSections) > 0 {
		t.Errorf("Helm values config omits internal/config sections: %s", strings.Join(missingSections, ", "))
	}
}

func readYAMLMap(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	parsed, err := yaml.Parser().Unmarshal(data)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return parsed
}

func nestedYAMLMap(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := parent[key]
	if !ok {
		t.Fatalf("YAML is missing %q section", key)
	}
	child, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("YAML section %q has type %T, want a mapping", key, value)
	}
	return child
}
