package aigateway

import (
	"encoding/json"
	"reflect"
	"time"
)

const (
	detailCacheEntries = 6
	detailCacheBytes   = 16 << 20
	detailCacheTTL     = 5 * time.Minute
)

type detailCacheEntry struct {
	row    namedRow
	result fetchedLog
	bytes  int
}

// Only collection's context-aware guard accesses this cache. Completed data is
// immutable; entries expire from the original fetch, never from a cache hit.
type detailCache struct {
	entries []detailCacheEntry
	bytes   int
}

func sameDetail(a, b namedRow) bool {
	return a.gateway == b.gateway && a.row.ID == b.row.ID && a.row.CreatedAt.Equal(b.row.CreatedAt)
}

func (c *detailCache) remove(i int) detailCacheEntry {
	e := c.entries[i]
	c.bytes -= e.bytes
	copy(c.entries[i:], c.entries[i+1:])
	c.entries[len(c.entries)-1] = detailCacheEntry{}
	c.entries = c.entries[:len(c.entries)-1]
	return e
}

func (c *detailCache) take(row namedRow) (fetchedLog, bool) {
	for i := 0; i < len(c.entries); {
		e := c.entries[i]
		if time.Since(e.result.fetchedAt) >= detailCacheTTL {
			c.remove(i)
			continue
		}
		if sameDetail(e.row, row) {
			return c.remove(i).result, true
		}
		i++
	}
	return fetchedLog{}, false
}

func (c *detailCache) put(row namedRow, result fetchedLog) {
	if result.err != nil || time.Since(result.fetchedAt) >= detailCacheTTL {
		return
	}
	disabled := row.row.CollectLogPayload
	if result.log.detail.CollectLogPayload != nil {
		disabled = result.log.detail.CollectLogPayload
	}
	if (result.log.unavailable[0] || result.log.unavailable[1]) && (disabled == nil || *disabled) {
		// A transient missing body may become available on a later attempt.
		return
	}
	// JSON size conservatively covers retained string/raw-message storage; the
	// fixed allowance covers structs, pointers and slice bookkeeping. Count the
	// minimal identity key too; never retain re-listed metadata with old sizing.
	// A re-listed row can contain different/larger metadata. Retain only the
	// stable identity needed for matching, with no aliases to those payloads.
	row = namedRow{gateway: row.gateway, row: logRow{ID: row.row.ID, CreatedAt: row.row.CreatedAt.UTC()}}
	size := result.cacheBytes
	if size == 0 {
		detail, err := json.Marshal(result.log.detail)
		if err != nil {
			return
		}
		listed, err := json.Marshal(row.row)
		if err != nil {
			return
		}
		size = len(detail) + len(listed) + len(row.gateway) + cap(result.log.bodies[0]) + cap(result.log.bodies[1]) + rawCapacitySlack(result.log.detail) + 2048
		result.cacheBytes = size
	}
	if size > detailCacheBytes {
		return
	}
	for i := 0; i < len(c.entries); {
		if sameDetail(c.entries[i].row, row) || time.Since(c.entries[i].result.fetchedAt) >= detailCacheTTL {
			c.remove(i)
		} else {
			i++
		}
	}
	for len(c.entries) >= detailCacheEntries || c.bytes+size > detailCacheBytes {
		c.remove(0)
	}
	c.entries = append(c.entries, detailCacheEntry{row: row, result: result, bytes: size})
	c.bytes += size
}

func (c *detailCache) pruneBefore(mark time.Time) {
	for i := 0; i < len(c.entries); {
		if c.entries[i].row.row.CreatedAt.Before(mark) {
			c.remove(i)
		} else {
			i++
		}
	}
}

// Raw JSON slices may have unused capacity after decoding. Serialization counts
// their length only, so account for the remaining backing allocation as well.
func rawCapacitySlack(row logRow) int {
	value := reflect.ValueOf(row)
	slack := 0
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		if field.Kind() == reflect.Slice {
			slack += (field.Cap() - field.Len()) * int(field.Type().Elem().Size())
		}
	}
	return slack
}
