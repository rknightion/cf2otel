package aigateway

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func BenchmarkDetailRetentionLargeHeads(b *testing.B) {
	head, _ := json.Marshal(strings.Repeat("x", 2<<20))
	row := namedRow{gateway: "gateway", row: logRow{ID: "row", CreatedAt: time.Now()}}
	result := fetchedLog{fetchedAt: time.Now(), log: capturedLog{detail: logRow{RequestHead: head, ResponseHead: head}, bodies: [2]json.RawMessage{make([]byte, 2<<20), make([]byte, 2<<20)}}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var cache detailCache
		cache.put(row, result)
	}
}
