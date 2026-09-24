package identity

import (
	"bytes"
	"fmt"
	"sort"
	"testing"
	"time"
)

func TestJoinWindowAndCounters(t *testing.T) {
	idx, err := New(10*time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	idx.Observe(Login{ClientIP: "192.0.2.1", Host: "Example.com", UserEmail: "a@example.com", RayID: "ray-a", At: at})
	for _, tc := range []struct {
		name, ip, host string
		at             time.Time
		want           Match
	}{
		{"before", "192.0.2.1", "example.com", at.Add(-time.Second), Match{}},
		{"wrong host", "192.0.2.1", "other.example.com", at.Add(time.Minute), Match{}},
		{"wrong ip", "192.0.2.2", "example.com", at.Add(time.Minute), Match{}},
		{"match", "192.0.2.1", "example.com", at.Add(10 * time.Minute), Match{UserEmail: "a@example.com", LoginRayID: "ray-a", Inferred: true}},
		{"outside", "192.0.2.1", "example.com", at.Add(10*time.Minute + time.Second), Match{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := idx.Lookup(tc.ip, tc.host, tc.at); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
	if got := idx.Stats(); got != (Stats{Matched: 1, Unmatched: 4}) {
		t.Fatalf("stats: %+v", got)
	}
}

func TestAmbiguityAndRepeatUser(t *testing.T) {
	idx, _ := New(time.Hour, 10)
	at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, login := range []Login{
		{ClientIP: "192.0.2.1", Host: "example.com", UserEmail: "a@example.com", RayID: "old", At: at},
		{ClientIP: "192.0.2.1", Host: "example.com", UserEmail: "A@example.com", RayID: "new", At: at.Add(time.Minute)},
	} {
		idx.Observe(login)
	}
	if got := idx.Lookup("192.0.2.1", "example.com", at.Add(2*time.Minute)); got != (Match{UserEmail: "A@example.com", LoginRayID: "new", Inferred: true}) {
		t.Fatalf("repeat user: %+v", got)
	}
	idx.Observe(Login{ClientIP: "192.0.2.1", Host: "example.com", UserEmail: "b@example.com", RayID: "other", At: at.Add(3 * time.Minute)})
	if got := idx.Lookup("192.0.2.1", "example.com", at.Add(4*time.Minute)); got != (Match{Ambiguous: true}) {
		t.Fatalf("ambiguity: %+v", got)
	}
	if got := idx.Stats(); got != (Stats{Matched: 1, Ambiguous: 1}) {
		t.Fatalf("stats: %+v", got)
	}
}

func TestEvictionAndCandidateCap(t *testing.T) {
	idx, _ := New(10*time.Minute, 2)
	at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i, ip := range []string{"192.0.2.1", "192.0.2.2", "192.0.2.3"} {
		idx.Observe(Login{ClientIP: ip, Host: "example.com", UserEmail: "a@example.com", At: at.Add(time.Duration(i) * time.Minute)})
	}
	if got := idx.Len(); got != 2 {
		t.Fatalf("cap retained %d", got)
	}
	if got := idx.Lookup("192.0.2.1", "example.com", at.Add(3*time.Minute)); got != (Match{}) {
		t.Fatalf("oldest survived cap: %+v", got)
	}
	idx.Lookup("192.0.2.3", "example.com", at.Add(20*time.Minute))
	if got := idx.Len(); got != 0 {
		t.Fatalf("expired retained %d", got)
	}
}

func TestRejectInvalidConfig(t *testing.T) {
	if _, err := New(0, 10); err == nil {
		t.Fatal("zero window accepted")
	}
	if _, err := New(time.Minute, 0); err == nil {
		t.Fatal("zero capacity accepted")
	}
}

func TestPruneExpiresQuietIndex(t *testing.T) {
	idx, err := NewWithRetention(10*time.Minute, time.Hour, 10)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	idx.Observe(Login{ClientIP: "192.0.2.1", Host: "example.com", UserEmail: "user@example.com", At: at})
	idx.Prune(at.Add(time.Hour + time.Second))
	if idx.Len() != 0 {
		t.Fatal("quiet index retained expired identity")
	}
}

func TestObserveDeduplicatesCompleteRowIdentity(t *testing.T) {
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		mutate func(*Login)
	}{
		{"different IP", func(login *Login) { login.ClientIP = "192.0.2.2" }},
		{"different host", func(login *Login) { login.Host = "other.example.com" }},
		{"different email", func(login *Login) { login.UserEmail = "b@example.com" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx, err := New(10*time.Minute, 10)
			if err != nil {
				t.Fatal(err)
			}
			first := Login{ClientIP: "192.0.2.1", Host: "example.com", UserEmail: "a@example.com", RayID: "same-ray", At: at}
			distinct := first
			tc.mutate(&distinct)
			idx.Observe(first)
			idx.Observe(first)
			idx.Observe(distinct)
			if got := idx.Len(); got != 2 {
				t.Fatalf("candidate count = %d, want duplicate collapsed and distinct row retained", got)
			}
			firstMatch := idx.Lookup(first.ClientIP, first.Host, at)
			distinctMatch := idx.Lookup(distinct.ClientIP, distinct.Host, at)
			if tc.name == "different email" {
				if firstMatch != (Match{Ambiguous: true}) || distinctMatch != (Match{Ambiguous: true}) {
					t.Fatalf("same-key distinct identities: first=%+v distinct=%+v, want both ambiguous", firstMatch, distinctMatch)
				}
				return
			}
			want := Match{UserEmail: "a@example.com", LoginRayID: "same-ray", Inferred: true}
			if firstMatch != want || distinctMatch != want {
				t.Fatalf("first=%+v distinct=%+v, want independently matchable rows %+v", firstMatch, distinctMatch, want)
			}
		})
	}
}

func TestCapacityEvictionBoundsReplayMetadata(t *testing.T) {
	idx, err := NewWithRetention(10*time.Minute, time.Hour, 1)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	evicted := Login{ClientIP: "192.0.2.1", Host: "example.com", UserEmail: "a@example.com", RayID: "ray-a", At: at}
	retained := Login{ClientIP: "192.0.2.2", Host: "example.com", UserEmail: "b@example.com", RayID: "ray-b", At: at.Add(time.Minute)}
	idx.Observe(evicted)
	idx.Observe(retained)
	if idx.Len() != 1 {
		t.Fatalf("candidate count after cap = %d, want 1", idx.Len())
	}
	idx.Observe(evicted)
	if idx.Len() != 1 {
		t.Fatalf("replaying an evicted row changed candidate count to %d, want 1", idx.Len())
	}
	if got := idx.Lookup(evicted.ClientIP, evicted.Host, retained.At); got != (Match{}) {
		t.Fatalf("capacity-evicted row was reinserted: %+v", got)
	}
	if got := idx.Lookup(retained.ClientIP, retained.Host, retained.At); got != (Match{UserEmail: retained.UserEmail, LoginRayID: retained.RayID, Inferred: true}) {
		t.Fatalf("retained row lookup = %+v", got)
	}
	for i := 0; i < 10; i++ {
		newer := retained
		newer.At = at.Add(time.Duration(i+2) * time.Minute)
		idx.Observe(newer)
	}
	if len(idx.seenRows) > idx.maxSeen || idx.seenOldest.Len() > idx.maxSeen {
		t.Fatalf("replay metadata grew beyond cap %d: map=%d heap=%d", idx.maxSeen, len(idx.seenRows), idx.seenOldest.Len())
	}
	if idx.Len() != 1 {
		t.Fatalf("candidate count after repeated capacity eviction = %d, want 1", idx.Len())
	}
	idx.Prune(at.Add(72 * time.Minute))
	if len(idx.seenRows) != 0 || idx.seenOldest.Len() != 0 {
		t.Fatalf("expired replay metadata retained %d row keys and %d expiry entries", len(idx.seenRows), idx.seenOldest.Len())
	}
}

func TestSameTimestampOverflowReplayKeepsDeterministicCandidates(t *testing.T) {
	idx, err := NewWithRetention(10*time.Minute, time.Hour, 2)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	rows := make([]Login, idx.maxSeen+1)
	for i := range rows {
		rows[i] = Login{
			ClientIP: fmt.Sprintf("192.0.2.%d", i+1),
			Host:     "example.com", UserEmail: "user@example.com",
			RayID: fmt.Sprintf("ray-%d", i), At: at,
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		left, right := idx.fingerprint(rows[i]), idx.fingerprint(rows[j])
		return bytes.Compare(left[:], right[:]) < 0
	})
	for _, row := range rows {
		idx.Observe(row)
	}
	want := rows[len(rows)-idx.maxCandidates:]
	for _, row := range want {
		if got := idx.Lookup(row.ClientIP, row.Host, at); got != (Match{UserEmail: row.UserEmail, LoginRayID: row.RayID, Inferred: true}) {
			t.Fatalf("retained distinct row %s: got %+v", row.RayID, got)
		}
	}
	idx.Observe(rows[0]) // The first fingerprint has fallen outside the bounded seen set.
	if got := idx.Lookup(rows[0].ClientIP, rows[0].Host, at); got != (Match{}) {
		t.Fatalf("overflow replay inserted an out-ranked row: %+v", got)
	}
	for _, row := range want {
		if got := idx.Lookup(row.ClientIP, row.Host, at); got.LoginRayID != row.RayID {
			t.Fatalf("overflow replay displaced %s: %+v", row.RayID, got)
		}
	}
	if idx.Len() != idx.maxCandidates || len(idx.seenRows) > idx.maxSeen || idx.seenOldest.Len() > idx.maxSeen {
		t.Fatalf("bounds: candidates=%d, fingerprints=%d, expiry entries=%d", idx.Len(), len(idx.seenRows), idx.seenOldest.Len())
	}
}
