package identity

import (
	"container/heap"
	"errors"
	"strings"
	"sync"
	"time"
)

// Stats is a snapshot of lookup outcomes. It contains no identity or address labels.
type Stats struct {
	Matched, Unmatched, Ambiguous uint64
}

type key struct{ ip, host string }
type candidate struct {
	login    Login
	key      key
	bucket   int64
	sequence uint64
}
type candidates []*candidate

func (h candidates) Len() int { return len(h) }
func (h candidates) Less(i, j int) bool {
	if h[i].login.At.Equal(h[j].login.At) {
		return h[i].sequence < h[j].sequence
	}
	return h[i].login.At.Before(h[j].login.At)
}
func (h candidates) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *candidates) Push(x any)   { *h = append(*h, x.(*candidate)) }
func (h *candidates) Pop() any {
	last := len(*h) - 1
	x := (*h)[last]
	(*h)[last] = nil
	*h = (*h)[:last]
	return x
}

// MemoryIndex retains at most maxCandidates logins and only the configured
// interval behind the latest observed login or lookup time. Older out-of-order
// requests cannot be matched after that interval has been evicted.
type MemoryIndex struct {
	mu            sync.Mutex
	window        time.Duration
	retention     time.Duration
	maxCandidates int
	latest        time.Time
	sequence      uint64
	byMinute      map[int64]map[key]map[*candidate]struct{}
	oldest        candidates
	stats         Stats
}

var _ Index = (*MemoryIndex)(nil)

func New(window time.Duration, maxCandidates int) (*MemoryIndex, error) {
	return NewWithRetention(window, window, maxCandidates)
}

// NewWithRetention keeps replayed login candidates across a longer backlog
// while matching each request only within the narrower inference window.
func NewWithRetention(window, retention time.Duration, maxCandidates int) (*MemoryIndex, error) {
	if window <= 0 || retention < window || maxCandidates <= 0 {
		return nil, errors.New("identity window and max candidates must be positive")
	}
	return &MemoryIndex{window: window, retention: retention, maxCandidates: maxCandidates, byMinute: make(map[int64]map[key]map[*candidate]struct{})}, nil
}

func normalizedKey(ip, host string) key {
	return key{strings.TrimSpace(ip), strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")}
}

func (idx *MemoryIndex) Observe(login Login) {
	k := normalizedKey(login.ClientIP, login.Host)
	if k.ip == "" || k.host == "" || strings.TrimSpace(login.UserEmail) == "" || login.At.IsZero() {
		return
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.advance(login.At)
	if login.At.Before(idx.latest.Add(-idx.retention)) {
		return
	}
	idx.sequence++
	c := &candidate{login: login, key: k, bucket: login.At.Unix() / 60, sequence: idx.sequence}
	if idx.byMinute[c.bucket] == nil {
		idx.byMinute[c.bucket] = make(map[key]map[*candidate]struct{})
	}
	if idx.byMinute[c.bucket][k] == nil {
		idx.byMinute[c.bucket][k] = make(map[*candidate]struct{})
	}
	idx.byMinute[c.bucket][k][c] = struct{}{}
	heap.Push(&idx.oldest, c)
	for idx.oldest.Len() > idx.maxCandidates {
		idx.removeOldest()
	}
}

func (idx *MemoryIndex) Lookup(ip, host string, at time.Time) Match {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.advance(at)
	k := normalizedKey(ip, host)
	var chosen *candidate
	var user string
	for _, keyed := range idx.byMinute {
		for c := range keyed[k] {
			if c.login.At.After(at) || at.Sub(c.login.At) > idx.window {
				continue
			}
			u := strings.ToLower(strings.TrimSpace(c.login.UserEmail))
			if user != "" && user != u {
				idx.stats.Ambiguous++
				return Match{Ambiguous: true}
			}
			user = u
			if chosen == nil || c.login.At.After(chosen.login.At) || (c.login.At.Equal(chosen.login.At) && c.sequence > chosen.sequence) {
				chosen = c
			}
		}
	}
	if chosen == nil {
		idx.stats.Unmatched++
		return Match{}
	}
	idx.stats.Matched++
	return Match{UserEmail: chosen.login.UserEmail, LoginRayID: chosen.login.RayID, Inferred: true}
}

func (idx *MemoryIndex) Stats() Stats {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.stats
}

func (idx *MemoryIndex) Len() int {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.oldest.Len()
}

// Prune expires candidates against wall time even when no logins or HTTP rows arrive.
func (idx *MemoryIndex) Prune(now time.Time) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.advance(now)
}

func (idx *MemoryIndex) advance(at time.Time) {
	if at.After(idx.latest) {
		idx.latest = at
	}
	cutoff := idx.latest.Add(-idx.retention)
	for idx.oldest.Len() > 0 && idx.oldest[0].login.At.Before(cutoff) {
		idx.removeOldest()
	}
}

func (idx *MemoryIndex) removeOldest() {
	c := heap.Pop(&idx.oldest).(*candidate)
	keyed := idx.byMinute[c.bucket]
	delete(keyed[c.key], c)
	if len(keyed[c.key]) == 0 {
		delete(keyed, c.key)
	}
	if len(keyed) == 0 {
		delete(idx.byMinute, c.bucket)
	}
}
