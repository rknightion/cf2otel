package identity

import (
	"bytes"
	"container/heap"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash"
	"math"
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
	login       Login
	key         key
	bucket      int64
	fingerprint [sha256.Size]byte
}
type candidates []*candidate

func (h candidates) Len() int { return len(h) }
func (h candidates) Less(i, j int) bool {
	return rankLess(h[i].login.At, h[i].fingerprint, h[j].login.At, h[j].fingerprint)
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

type observedLogin struct {
	fingerprint [sha256.Size]byte
	at          time.Time
}
type observedLogins []*observedLogin

func (h observedLogins) Len() int { return len(h) }
func (h observedLogins) Less(i, j int) bool {
	return rankLess(h[i].at, h[i].fingerprint, h[j].at, h[j].fingerprint)
}
func (h observedLogins) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *observedLogins) Push(x any)   { *h = append(*h, x.(*observedLogin)) }
func (h *observedLogins) Pop() any {
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
	mu             sync.Mutex
	window         time.Duration
	retention      time.Duration
	maxCandidates  int
	maxSeen        int
	fingerprintKey [sha256.Size]byte
	latest         time.Time
	byMinute       map[int64]map[key]map[*candidate]struct{}
	oldest         candidates
	seenRows       map[[sha256.Size]byte]struct{}
	seenOldest     observedLogins // Bounded fingerprints outlive candidate eviction, but not retention.
	stats          Stats
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
	maxSeen := maxCandidates
	if maxCandidates <= math.MaxInt/4 {
		maxSeen *= 4
	}
	var fingerprintKey [sha256.Size]byte
	if _, err := rand.Read(fingerprintKey[:]); err != nil {
		return nil, err
	}
	return &MemoryIndex{
		window:         window,
		retention:      retention,
		maxCandidates:  maxCandidates,
		maxSeen:        maxSeen,
		fingerprintKey: fingerprintKey,
		byMinute:       make(map[int64]map[key]map[*candidate]struct{}),
		seenRows:       make(map[[sha256.Size]byte]struct{}),
	}, nil
}

func normalizedKey(ip, host string) key {
	return key{strings.TrimSpace(ip), strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")}
}

func writeFingerprintPart(h hash.Hash, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = h.Write(length[:])
	_, _ = h.Write([]byte(value))
}

func (idx *MemoryIndex) fingerprint(login Login) [sha256.Size]byte {
	h := hmac.New(sha256.New, idx.fingerprintKey[:])
	for _, part := range []string{login.RayID, login.ClientIP, login.Host, login.UserEmail, login.At.Round(0).UTC().Format(time.RFC3339Nano)} {
		writeFingerprintPart(h, part)
	}
	var result [sha256.Size]byte
	copy(result[:], h.Sum(nil))
	return result
}

// rankLess orders equal-time rows by a process-keyed digest, so a bounded
// index retains the same rows regardless of collection or replay order.
func rankLess(aTime time.Time, aFingerprint [sha256.Size]byte, bTime time.Time, bFingerprint [sha256.Size]byte) bool {
	if aTime.Equal(bTime) {
		return bytes.Compare(aFingerprint[:], bFingerprint[:]) < 0
	}
	return aTime.Before(bTime)
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
	fingerprint := idx.fingerprint(login)
	if _, seen := idx.seenRows[fingerprint]; seen {
		return
	}
	// A row below the bounded seen set is also below the candidate set.
	// Reject it before either heap changes, including on later replay.
	if idx.seenOldest.Len() == idx.maxSeen &&
		!rankLess(idx.seenOldest[0].at, idx.seenOldest[0].fingerprint, login.At, fingerprint) {
		return
	}
	idx.seenRows[fingerprint] = struct{}{}
	heap.Push(&idx.seenOldest, &observedLogin{fingerprint: fingerprint, at: login.At.Round(0).UTC()})
	for idx.seenOldest.Len() > idx.maxSeen {
		oldest := heap.Pop(&idx.seenOldest).(*observedLogin)
		delete(idx.seenRows, oldest.fingerprint)
	}
	if idx.oldest.Len() == idx.maxCandidates &&
		!rankLess(idx.oldest[0].login.At, idx.oldest[0].fingerprint, login.At, fingerprint) {
		return
	}
	c := &candidate{login: login, key: k, bucket: login.At.Unix() / 60, fingerprint: fingerprint}
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
			if chosen == nil || rankLess(chosen.login.At, chosen.fingerprint, c.login.At, c.fingerprint) {
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
	for idx.seenOldest.Len() > 0 && idx.seenOldest[0].at.Before(cutoff) {
		seen := heap.Pop(&idx.seenOldest).(*observedLogin)
		delete(idx.seenRows, seen.fingerprint)
	}
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
