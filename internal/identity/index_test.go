package identity

import (
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
