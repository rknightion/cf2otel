package loadbalancers_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestPoolRequestsRenderedPanel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "-c", `import importlib.util,json
s=importlib.util.spec_from_file_location("dashboard","grafana/build_dashboard.py")
m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
print(json.dumps(m.render()))`)
	cmd.Dir = "../../.."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("render dashboard: %v: %s", err, output)
	}
	var dashboard any
	if err := json.Unmarshal(output, &dashboard); err != nil {
		t.Fatal(err)
	}
	var panel map[string]any
	var find func(any)
	find = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if id, ok := v["id"].(float64); ok && id == 2691 {
				panel = v
			}
			for _, child := range v {
				find(child)
			}
		case []any:
			for _, child := range v {
				find(child)
			}
		}
	}
	find(dashboard)
	if panel == nil {
		t.Fatal("request-count signal lacks rendered panel")
	}
	raw, _ := json.Marshal(panel)
	s := string(raw)
	for _, required := range []string{"cloudflare_loadbalancers_pool_requests", "cloudflare_loadbalancers_pool_name", "instance", "uncached", "not all HTTP requests", "snapshot"} {
		if !strings.Contains(s, required) {
			t.Errorf("panel missing semantics/query %q", required)
		}
	}
	if strings.Contains(s, "rate(") || strings.Contains(s, "increase(") || strings.Contains(s, "$zone") || strings.Contains(s, "$host") {
		t.Fatal("snapshot panel uses counter or inapplicable filters")
	}
}
