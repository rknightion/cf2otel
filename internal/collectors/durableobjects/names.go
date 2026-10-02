package durableobjects

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rknightion/cf2otel/internal/cfapi"
)

const resourceDimension = "namespaceId"
const resourceListPath = "/workers/durable_objects/namespaces"

type resourceRow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// This cache is shared only by collectors registered in this domain. A failed
// refresh replaces the old mapping, so deleted or inaccessible names never stay
// alive indefinitely. The clock is injectable for deterministic TTL evidence.
type nameCache struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]nameEntry
}
type nameEntry struct {
	until time.Time
	names map[string]string
}

func newNameCache() *nameCache { return &nameCache{now: time.Now, entries: map[string]nameEntry{}} }

func (c *nameCache) lookup(ctx context.Context, api cfapi.Client, account string) map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := account + resourceListPath
	now := c.now()
	if entry, ok := c.entries[key]; ok && now.Before(entry.until) {
		return entry.names
	}
	names := loadNames(ctx, api, account)
	c.entries[key] = nameEntry{until: c.now().Add(time.Hour), names: names}
	return names
}

func loadNames(ctx context.Context, api cfapi.Client, account string) map[string]string {
	getter, ok := api.(cfapi.PageGetter)
	if !ok {
		return nil
	}
	names := map[string]string{}
	ambiguous := map[string]bool{}
	owners := map[string]string{}
	seen := 0
	total := -1
	size := 50
	for page := 1; page <= 100; page++ {
		var envelope struct {
			Result []resourceRow `json:"result"`
			Info   struct {
				Page    int `json:"page"`
				PerPage int `json:"per_page"`
				Count   int `json:"count"`
				Total   int `json:"total_count"`
			} `json:"result_info"`
		}
		query := url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(size)}}
		if err := getter.GetPage(ctx, "/accounts/"+url.PathEscape(account)+"/workers/durable_objects/namespaces", query, &envelope); err != nil {
			return nil
		}
		info := envelope.Info
		if info.Page != page || info.PerPage <= 0 || info.Count != len(envelope.Result) || len(envelope.Result) > info.PerPage || info.Total < 0 || info.Total > 5000 {
			return nil
		}
		if total < 0 {
			total = info.Total
		}
		if total != info.Total {
			return nil
		}
		size = info.PerPage
		seen += len(envelope.Result)
		if seen > 5000 || seen > total {
			return nil
		}
		for _, row := range envelope.Result {
			id, name := row.ID, row.Name
			if id == "" {
				continue
			}
			if strings.TrimSpace(name) == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 128 || name == "other" {
				ambiguous[id] = true
				continue
			}
			if previous, ok := names[id]; ok && previous != name {
				ambiguous[id] = true
			}
			if owner, ok := owners[name]; ok && owner != id {
				ambiguous[id] = true
				ambiguous[owner] = true
			}
			owners[name] = id
			names[id] = name
		}
		if seen == total {
			for id := range ambiguous {
				delete(names, id)
			}
			return names
		}
		if len(envelope.Result) != info.PerPage {
			return nil
		}
	}
	return nil
}

// Sticky admission is per metric and full attribute set for the collector's
// lifetime: 49 normal names plus the reserved other remainder. Missing/invalid
// names and later unadmitted names contribute to the remainder, never disappear.
type nameAdmission struct {
	mu      sync.Mutex
	metrics map[string]map[string]bool
}

func (a *nameAdmission) admit(metric, name string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if name == "" || name == "other" {
		return "other"
	}
	if a.metrics == nil {
		a.metrics = map[string]map[string]bool{}
	}
	if a.metrics[metric] == nil {
		a.metrics[metric] = map[string]bool{}
	}
	admitted := a.metrics[metric]
	if admitted[name] {
		return name
	}
	if len(admitted) >= 49 {
		return "other"
	}
	admitted[name] = true
	return name
}

func namedRows(rows []map[string]any, names map[string]string, from, to time.Time, gauge bool) map[string][]map[string]any {
	latest := time.Time{}
	if gauge {
		for _, row := range rows {
			bucket, err := rowBucket(row)
			if err == nil && !bucket.Before(from) && bucket.Before(to) && !bucket.Add(5*time.Minute).After(to) && bucket.After(latest) {
				latest = bucket
			}
		}
	}
	groups := map[string][]map[string]any{}
	for _, row := range rows {
		if gauge {
			bucket, err := rowBucket(row)
			if err == nil && !bucket.Equal(latest) {
				continue
			}
		}
		dims, _ := row["dimensions"].(map[string]any)
		id, _ := dims[resourceDimension].(string)
		name := names[id]
		if name == "" {
			name = "other"
		}
		groups[name] = append(groups[name], row)
	}
	return groups
}

func sortedNames(groups map[string][]map[string]any) []string {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
