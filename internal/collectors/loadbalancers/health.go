package loadbalancers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

const maxPools = 1000

type healthCollector struct {
	api      cfapi.Client
	account  string
	zones    []string
	interval time.Duration
	limit    int
	mu       sync.Mutex
	admitted map[string]bool
}

func (*healthCollector) Name() string                   { return semconv.CollectorNameLBHealth }
func (*healthCollector) DefaultInterval() time.Duration { return 5 * time.Minute }

type pool struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type health struct {
	PopHealth *struct {
		Healthy *bool `json:"healthy"`
	} `json:"pop_health"`
}

func validName(name string) bool {
	if strings.TrimSpace(name) == "" || len(name) > 128 || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (c *healthCollector) catalog(ctx context.Context) ([]pool, error) {
	var rows []pool
	getter, ok := c.api.(cfapi.PageGetter)
	if !ok {
		return nil, fmt.Errorf("load balancer catalog requires response metadata")
	}
	var page cfapi.Page
	// Preserve result_info to reject a disclosed incomplete catalog, without
	// inventing page/per_page query parameters absent from the documented method.
	if err := getter.GetPage(ctx, "/accounts/"+url.PathEscape(c.account)+"/load_balancers/pools", nil, &page); err != nil {
		return nil, fmt.Errorf("load balancer pool catalog read failed")
	}
	if err := json.Unmarshal(page.Result, &rows); err != nil {
		return nil, fmt.Errorf("load balancer pool catalog invalid array")
	}
	if page.ResultInfo.TotalCount > len(rows) || page.ResultInfo.Page > 1 || page.ResultInfo.Cursor != "" {
		return nil, fmt.Errorf("load balancer pool catalog incomplete")
	}
	if rows == nil {
		return nil, fmt.Errorf("load balancer pool catalog missing array")
	}
	// Reaching the catalog bound fails closed rather than assuming truncation is complete.
	if len(rows) >= maxPools {
		return nil, fmt.Errorf("load balancer pool catalog bound exhausted")
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, p := range rows {
		if p.ID == "" || !validName(p.Name) {
			return nil, fmt.Errorf("load balancer pool catalog invalid identity or configured name")
		}
		if ids[p.ID] || names[p.Name] {
			return nil, fmt.Errorf("load balancer pool catalog ambiguous identity or name")
		}
		ids[p.ID], names[p.Name] = true, true
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows, nil
}

func (c *healthCollector) Collect(ctx context.Context, e telemetry.Emitter) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	batch, ok := e.(telemetry.SnapshotBatchEmitter)
	if !ok {
		return fmt.Errorf("load balancer health requires expiring snapshot emitter")
	}
	if c.api == nil || c.account == "" || c.interval <= 0 || c.limit < 1 {
		return fmt.Errorf("invalid load balancer health configuration")
	}
	rows, err := c.catalog(ctx)
	if err != nil {
		return err
	}
	type measurement struct {
		name  string
		value float64
	}
	var values []measurement
	valid, failed := 0, 0
	for _, p := range rows {
		var result *health
		poolID := url.PathEscape(p.ID)
		if err := c.api.Get(ctx, "/accounts/"+url.PathEscape(c.account)+"/load_balancers/pools/"+poolID+"/health", nil, &result); err != nil || result == nil {
			failed++
			continue
		}
		valid++
		// Only the schema's direct property is read; dynamic regions/origin addresses
		// are neither guessed nor reinterpreted as a pool availability aggregate.
		if result.PopHealth == nil || result.PopHealth.Healthy == nil {
			continue
		}
		value := 0.0
		if *result.PopHealth.Healthy {
			value = 1
		}
		values = append(values, measurement{p.Name, value})
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(rows) > 0 && valid == 0 {
		return fmt.Errorf("load balancer health all %d pool reads failed", failed)
	}
	admitted := make(map[string]bool, len(c.admitted))
	for name := range c.admitted {
		admitted[name] = true
	}
	var points []telemetry.GaugePoint
	other, hasOther := 1.0, false
	for _, m := range values {
		if m.name != "other" && !admitted[m.name] && len(admitted) < c.limit-1 {
			admitted[m.name] = true
		}
		if m.name != "other" && admitted[m.name] {
			points = append(points, point(m.name, m.value))
			continue
		}
		hasOther = true
		other = min(other, m.value)
	}
	if hasOther {
		points = append(points, point("other", other))
	}
	// A valid empty catalog or all-unknown valid detail clears prior known flags.
	// Failed fetches alone never refresh prior expiry; a valid subset replaces it.
	requests, err := c.requests(ctx, rows, c.limit-len(points))
	if err != nil {
		return err
	}
	if err := batch.GaugeSnapshots(ctx, 3*c.interval, map[string][]telemetry.GaugePoint{
		semconv.MetricLBPoolHealth:   points,
		semconv.MetricLBPoolRequests: requests,
	}); err != nil {
		return err
	}
	c.admitted = admitted
	if len(rows) > 0 && len(values) == 0 {
		return fmt.Errorf("load balancer health has no known flags (%d failed pool reads)", failed)
	}
	if failed > 0 {
		return fmt.Errorf("load balancer health %d of %d pool reads failed", failed, len(rows))
	}
	return nil
}
func point(name string, value float64) telemetry.GaugePoint {
	return telemetry.GaugePoint{Value: value, Attrs: []telemetry.Attr{{Key: semconv.AttrLBPoolName, Value: name}}}
}
