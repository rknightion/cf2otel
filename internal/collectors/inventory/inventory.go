package inventory

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rknightion/cf2otel/internal/cfapi"
	"github.com/rknightion/cf2otel/internal/collector"
	"github.com/rknightion/cf2otel/internal/semconv"
	"github.com/rknightion/cf2otel/internal/telemetry"
)

type Catalog struct {
	mu     sync.RWMutex
	byHost map[string]collector.AppRecord
}

func NewCatalog() *Catalog { return &Catalog{byHost: map[string]collector.AppRecord{}} }
func normalizeHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
}
func (c *Catalog) PutApp(rec collector.AppRecord) {
	host := normalizeHost(rec.Host)
	if host == "" {
		return
	}
	rec.Host = host
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byHost[host] = rec
}
func (c *Catalog) ReplaceApps(records []collector.AppRecord) {
	next := make(map[string]collector.AppRecord, len(records))
	for _, rec := range records {
		host := normalizeHost(rec.Host)
		if host != "" {
			rec.Host = host
			next[host] = rec
		}
	}
	c.mu.Lock()
	c.byHost = next
	c.mu.Unlock()
}
func (c *Catalog) Hosts() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	hosts := make([]string, 0, len(c.byHost))
	for h := range c.byHost {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	return hosts
}
func (c *Catalog) Lookup(host string) (collector.AppRecord, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.byHost[normalizeHost(host)]
	return v, ok
}

type appRow struct {
	ID                string   `json:"id"`
	UID               string   `json:"uid"`
	Name              string   `json:"name"`
	Domain            string   `json:"domain"`
	Type              string   `json:"type"`
	SelfHostedDomains []string `json:"self_hosted_domains"`
}
type userRow struct {
	ID string `json:"id"`
}
type accessInventory struct {
	api       cfapi.Client
	accountID string
	apps      collector.AppCatalog
	pageSize  int
}

func (accessInventory) Name() string                   { return "inventory.access" }
func (accessInventory) DefaultInterval() time.Duration { return 15 * time.Minute }
func fetchPages[T any](ctx context.Context, api cfapi.Client, path string, size int) ([]T, error) {
	var all []T
	for page := 1; ; page++ {
		q := url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(size)}}
		var rows []T
		if err := api.Get(ctx, path, q, &rows); err != nil {
			return nil, err
		}
		all = append(all, rows...)
		if len(rows) < size {
			return all, nil
		}
		if page >= 10000 {
			return nil, fmt.Errorf("inventory pagination exceeded 10000 pages")
		}
	}
}
func (c accessInventory) Collect(ctx context.Context, e telemetry.Emitter) error {
	if c.api == nil || c.accountID == "" {
		return fmt.Errorf("inventory requires API and account ID")
	}
	size := c.pageSize
	if size <= 0 {
		size = 100
	}
	base := "/accounts/" + url.PathEscape(c.accountID) + "/access/"
	apps, err := fetchPages[appRow](ctx, c.api, base+"apps", size)
	if err != nil {
		return fmt.Errorf("access apps: %w", err)
	}
	users, err := fetchPages[userRow](ctx, c.api, base+"users", size)
	if err != nil {
		return fmt.Errorf("access users: %w", err)
	}
	records := make([]collector.AppRecord, 0, len(apps))
	for _, app := range apps {
		id := app.UID
		if id == "" {
			id = app.ID
		}
		for _, domain := range append([]string{app.Domain}, app.SelfHostedDomains...) {
			if normalizeHost(domain) != "" {
				records = append(records, collector.AppRecord{ID: id, Name: app.Name, Host: domain})
			}
		}
	}
	if c.apps != nil {
		if replacer, ok := c.apps.(interface{ ReplaceApps([]collector.AppRecord) }); ok {
			replacer.ReplaceApps(records)
		} else {
			for _, r := range records {
				c.apps.PutApp(r)
			}
		}
	}
	for _, app := range apps {
		if err := e.Gauge(ctx, semconv.MetricAccessApps, 1, telemetry.Attr{Key: semconv.AttrAccessApp, Value: app.Name}, telemetry.Attr{Key: semconv.AttrAccessAppType, Value: app.Type}); err != nil {
			return err
		}
	}
	return e.Gauge(ctx, semconv.MetricAccessUsers, float64(len(users)))
}
func registerInventory(deps collector.Deps) {
	cfg := deps.Config.Collector("inventory.access")
	if cfg.Enabled {
		deps.Registry.RegisterSnapshot(accessInventory{api: deps.API, accountID: deps.Config.Cloudflare.AccountID, apps: deps.Apps}, cfg.Interval)
	}
}
