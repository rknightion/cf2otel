package cfapi

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

type admissionLog struct {
	mu    sync.Mutex
	times map[*tokenBucket][]time.Time
	worst map[string]time.Duration
}

func (l *admissionLog) record(class string, wait time.Duration, now time.Time, buckets ...*tokenBucket) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, b := range buckets {
		if b != nil {
			l.times[b] = append(l.times[b], now)
		}
	}
	l.worst[class] = max(l.worst[class], wait)
}

// assertWithinBudget checks every window between two admissions of a bucket
// against its burst plus refill, the quota contract every class must keep.
func assertWithinBudget(t *testing.T, name string, b *tokenBucket, times []time.Time) {
	t.Helper()
	for i := range times {
		for j := i; j < len(times); j++ {
			allowed := float64(b.burst) + times[j].Sub(times[i]).Seconds()*b.rate
			if float64(j-i+1) > allowed+1e-9 {
				t.Fatalf("%s admitted %d requests in %s, budget allows %.2f", name, j-i+1, times[j].Sub(times[i]), allowed)
			}
		}
	}
}

// Saturating REST callers must not leave an older waiter of any class behind
// for many token intervals: admission follows arrival order.
func TestSaturatedAdmissionWaitIsBoundedByQueue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		total, rest, graphql := newTokenBucket(3.75, 5), newTokenBucket(3.75, 5), newTokenBucket(2.5, 5)
		scopes := []*tokenBucket{newTokenBucket(0.9, 2), newTokenBucket(0.9, 2), newTokenBucket(0.9, 2), newTokenBucket(0.9, 2)}
		log := &admissionLog{times: map[*tokenBucket][]time.Time{}, worst: map[string]time.Duration{}}
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancel()
		var wg sync.WaitGroup
		loop := func(class string, next func(int) []*tokenBucket) {
			defer wg.Done()
			for n := 0; ; n++ {
				buckets := next(n)
				waiting := time.Now()
				if err := acquireBudgets(ctx, buckets...); err != nil {
					return
				}
				log.record(class, time.Since(waiting), time.Now(), buckets...)
				select {
				case <-time.After(500 * time.Millisecond): // upstream latency
				case <-ctx.Done():
					return
				}
			}
		}
		for range 6 {
			wg.Add(1)
			go loop("rest", func(int) []*tokenBucket { return []*tokenBucket{nil, rest, total} })
		}
		wg.Add(1)
		go loop("graphql", func(n int) []*tokenBucket { return []*tokenBucket{scopes[n%len(scopes)], graphql, total} })
		wg.Wait()

		// Seven closed-loop waiters at 3.75 rps: each queues behind at most six
		// others, under 1.9s; 2.5s leaves room for burst and scope refill edges.
		for class, worst := range log.worst {
			if worst > 2500*time.Millisecond {
				t.Errorf("%s waited %s for admission behind seven waiters", class, worst)
			}
		}
		// Its per-waiter share is 3.75/7 rps, about 160 admissions in 300s.
		if n := len(log.times[graphql]); n < 144 {
			t.Errorf("graphql admitted only %d requests in 300s, under 90%% of its per-waiter share", n)
		}
		assertWithinBudget(t, "total", total, log.times[total])
		assertWithinBudget(t, "rest", rest, log.times[rest])
		assertWithinBudget(t, "graphql", graphql, log.times[graphql])
		for _, s := range scopes {
			assertWithinBudget(t, "scope", s, log.times[s])
		}
	})
}

// An older waiter held by its own class pause or resource bucket must not hold
// back a later waiter whose budgets have tokens.
func TestBlockedWaiterDoesNotHoldBackOthers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		block func(graphql, scope *tokenBucket)
	}{
		{"class pause", func(graphql, _ *tokenBucket) { graphql.pause(300 * time.Second) }},
		{"empty resource bucket", func(_, scope *tokenBucket) { scope.tokens, scope.started, scope.last = 0, true, time.Now() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				total, rest, graphql, scope := newTokenBucket(3.75, 5), newTokenBucket(3.75, 5), newTokenBucket(2.5, 5), newTokenBucket(0.01, 1)
				tc.block(graphql, scope)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				blocked := make(chan error, 1)
				go func() { blocked <- acquireBudgets(ctx, scope, graphql, total) }()
				synctest.Wait() // the blocked waiter is queued first
				start := time.Now()
				for range 3 {
					if err := acquireBudgets(ctx, nil, rest, total); err != nil {
						t.Fatal(err)
					}
				}
				if waited := time.Since(start); waited != 0 {
					t.Fatalf("REST waited %s behind a waiter blocked on its own budget", waited)
				}
				cancel()
				if err := <-blocked; !errors.Is(err, context.Canceled) {
					t.Fatalf("blocked waiter returned %v", err)
				}
			})
		})
	}
}

// A cancelled waiter consumes nothing, and the next waiter is admitted when
// the token it would have taken refills.
func TestCancelledWaiterLeavesQueueWithoutTokens(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		total, rest := newTokenBucket(1, 1), newTokenBucket(1, 1)
		if err := acquireBudgets(context.Background(), nil, rest, total); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancelled := make(chan error, 1)
		go func() { cancelled <- acquireBudgets(ctx, nil, rest, total) }()
		time.Sleep(500 * time.Millisecond)
		cancel()
		if err := <-cancelled; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled waiter returned %v", err)
		}
		start := time.Now()
		if err := acquireBudgets(context.Background(), nil, rest, total); err != nil {
			t.Fatal(err)
		}
		if waited := time.Since(start); waited != 500*time.Millisecond {
			t.Fatalf("next waiter admitted after %s, want the remaining 500ms refill", waited)
		}
	})
}
