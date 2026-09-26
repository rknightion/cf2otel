package collector

import (
	"errors"
	"testing"
	"time"
)

var splitBase = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)

type leafCall struct{ from, to time.Time }

func TestBisectFailsOnIrreducibleBucket(t *testing.T) {
	cause := errors.New("limit reached")
	var calls []leafCall
	_, err := Bisect(splitBase, splitBase.Add(5*time.Minute), 5*time.Minute, 5*time.Minute, func(from, to time.Time) ([]int, bool, error) {
		calls = append(calls, leafCall{from, to})
		return nil, true, cause
	})
	var sat *SaturatedWindowError
	if !errors.As(err, &sat) || !errors.Is(err, ErrWindowIrreducible) || !errors.Is(err, cause) {
		t.Fatalf("error = %v, want an irreducible saturated window wrapping the leaf cause", err)
	}
	if !sat.From.Equal(splitBase) || !sat.To.Equal(splitBase.Add(5*time.Minute)) || len(calls) != 1 {
		t.Fatalf("irreducible window %s..%s after %d queries, want the single five-minute bucket queried once", sat.From, sat.To, len(calls))
	}
}

func TestBisectSplitsSaturatedWindowOnBucketBoundaries(t *testing.T) {
	var calls []leafCall
	rows, err := Bisect(splitBase, splitBase.Add(15*time.Minute), 5*time.Minute, 5*time.Minute, func(from, to time.Time) ([]time.Time, bool, error) {
		calls = append(calls, leafCall{from, to})
		if to.Sub(from) > 5*time.Minute {
			return nil, true, nil
		}
		return []time.Time{from}, false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Time{splitBase, splitBase.Add(5 * time.Minute), splitBase.Add(10 * time.Minute)}
	if len(rows) != len(want) {
		t.Fatalf("leaf rows = %v, want %v", rows, want)
	}
	for i := range want {
		if !rows[i].Equal(want[i]) {
			t.Fatalf("leaf rows = %v, want contiguous buckets %v in time order", rows, want)
		}
	}
	for _, call := range calls {
		if !call.from.Equal(call.from.Truncate(5*time.Minute)) || !call.to.Equal(call.to.Truncate(5*time.Minute)) {
			t.Fatalf("query %s..%s cuts a five-minute bucket", call.from, call.to)
		}
	}
}

func TestSplitWindowRejectsWindowWithoutAlignedInteriorPoint(t *testing.T) {
	// A six-minute window starting on a boundary has its midpoint floor on its own start.
	_, err := SplitWindow(splitBase, splitBase.Add(6*time.Minute), 5*time.Minute, time.Minute)
	if !errors.Is(err, ErrWindowUnsplittable) {
		t.Fatalf("error = %v, want ErrWindowUnsplittable", err)
	}
	mid, err := SplitWindow(splitBase.Add(500*time.Millisecond), splitBase.Add(2*time.Minute+500*time.Millisecond), time.Second, time.Minute)
	if err != nil || !mid.Equal(splitBase.Add(time.Minute)) {
		t.Fatalf("one-second split = %s, %v; want %s", mid, err, splitBase.Add(time.Minute))
	}
}
