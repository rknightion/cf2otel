package aigateway

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rknightion/cf2otel/internal/config"
)

func TestDetailRetentionBoundsAndIdentity(t *testing.T) {
	now := time.Now()
	row := namedRow{gateway: "one", row: logRow{ID: "same", CreatedAt: now}}
	result := fetchedLog{fetchedAt: now}
	var cache detailCache
	for i := 0; i < 10; i++ {
		r := row
		r.row.ID = fmt.Sprint(i)
		cache.put(r, result)
	}
	if len(cache.entries) != 6 {
		t.Fatalf("retained %d details, want newest six", len(cache.entries))
	}
	if _, ok := cache.take(namedRow{gateway: "one", row: logRow{ID: "0", CreatedAt: now}}); ok {
		t.Fatal("oldest detail survived count eviction")
	}
	cache = detailCache{}
	result.log.bodies[0] = make([]byte, 9<<20)
	cache.put(row, result)
	another := row
	another.row.ID = "another"
	cache.put(another, result)
	if len(cache.entries) != 1 || cache.bytes > 16<<20 {
		t.Fatalf("retained %d details / %d bytes", len(cache.entries), cache.bytes)
	}
	if _, ok := cache.take(row); ok {
		t.Fatal("oldest detail survived byte eviction")
	}
	cache = detailCache{}
	result = fetchedLog{fetchedAt: now}
	result.log.detail.RequestHead = make([]byte, 4, 17<<20)
	copy(result.log.detail.RequestHead, "null")
	cache.put(row, result)
	if len(cache.entries) != 0 {
		t.Fatal("retained raw backing allocation over byte limit")
	}
	cache = detailCache{}
	result = fetchedLog{fetchedAt: now}
	cache.put(row, result)
	foreign := row
	foreign.gateway = "two"
	if _, ok := cache.take(foreign); ok {
		t.Fatal("reused another gateway's detail")
	}
	changed := row
	changed.row.CreatedAt = now.Add(time.Second)
	if _, ok := cache.take(changed); ok {
		t.Fatal("reused another source timestamp's detail")
	}
	if _, ok := cache.take(row); !ok {
		t.Fatal("matching detail missing")
	}
	result.err = errors.New("fetch failed")
	cache.put(row, result)
	if len(cache.entries) != 0 {
		t.Fatal("retained a failed fetch")
	}
	result.err = nil
	result.log.unavailable[0] = true
	cache.put(row, result)
	if len(cache.entries) != 0 {
		t.Fatal("retained a transient missing body")
	}
}

func TestDetailRetentionExpiresFromFetch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		row := namedRow{gateway: "one", row: logRow{ID: "row", CreatedAt: time.Now()}}
		var cache detailCache
		cache.put(row, fetchedLog{fetchedAt: time.Now()})
		time.Sleep(4 * time.Minute)
		result, ok := cache.take(row)
		if !ok {
			t.Fatal("entry expired early")
		}
		cache.put(row, result)
		time.Sleep(time.Minute)
		if _, ok := cache.take(row); ok {
			t.Fatal("cache hit renewed expired detail")
		}
	})
}

func TestWaitingCollectionCanCancel(t *testing.T) {
	c := NewLogs(nil, nil)
	// Use valid dependencies so cancellation is tested at the collection guard.
	cfg := config.Default()
	c.cfg = &cfg
	c.api = &fakeAPI{}
	c.collectGuard <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.CollectWindowBudget(ctx, time.Now(), time.Now().Add(time.Minute), 1, &fakeEmitter{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting collector returned %v", err)
	}
	<-c.collectGuard
}

func TestDetailRetentionDoesNotKeepRelistedMetadata(t *testing.T) {
	row := namedRow{gateway: "gateway", row: logRow{ID: "row", CreatedAt: time.Now()}}
	var cache detailCache
	cache.put(row, fetchedLog{fetchedAt: time.Now()})
	result, ok := cache.take(row)
	if !ok {
		t.Fatal("detail not retained")
	}
	row.row.Metadata = make([]byte, 4, 17<<20)
	copy(row.row.Metadata, "null")
	cache.put(row, result)
	if len(cache.entries) != 1 {
		t.Fatal("detail key unexpectedly evicted")
	}
	if len(cache.entries[0].row.row.Metadata) != 0 || cap(cache.entries[0].row.row.Metadata) != 0 {
		t.Fatal("cache retained unaccounted relisted metadata")
	}
}
