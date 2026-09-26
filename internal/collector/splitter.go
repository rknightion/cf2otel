package collector

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrWindowIrreducible marks a saturated window no longer than the splitter minimum.
	ErrWindowIrreducible = errors.New("saturated query window is irreducible")
	// ErrWindowUnsplittable marks a saturated window with no aligned split point strictly inside it.
	ErrWindowUnsplittable = errors.New("saturated query window has no aligned split point")
)

// SaturatedWindowError reports the half-open window [From, To) that stayed
// saturated after bisection stopped. Reason is ErrWindowIrreducible or
// ErrWindowUnsplittable; Cause is the leaf's saturation cause and may be nil.
type SaturatedWindowError struct {
	From, To time.Time
	Reason   error
	Cause    error
}

func (e *SaturatedWindowError) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("%v: %s..%s", e.Reason, e.From.Format(time.RFC3339), e.To.Format(time.RFC3339))
	}
	return fmt.Sprintf("%v: %s..%s: %v", e.Reason, e.From.Format(time.RFC3339), e.To.Format(time.RFC3339), e.Cause)
}

func (e *SaturatedWindowError) Unwrap() []error {
	if e.Cause == nil {
		return []error{e.Reason}
	}
	return []error{e.Reason, e.Cause}
}

// SplitWindow returns the bisection point of the saturated half-open window
// [from, to): the midpoint truncated to a multiple of step. A window no longer
// than minimum is irreducible; a midpoint that does not fall strictly inside
// the window is unsplittable.
func SplitWindow(from, to time.Time, step, minimum time.Duration) (time.Time, error) {
	if to.Sub(from) <= minimum {
		return time.Time{}, ErrWindowIrreducible
	}
	mid := from.Add(to.Sub(from) / 2).Truncate(step)
	if !mid.After(from) || !mid.Before(to) {
		return time.Time{}, ErrWindowUnsplittable
	}
	return mid, nil
}

// WindowQuery queries one half-open window. It returns the window's rows, or
// saturated=true with an optional cause when the window must be split. A
// non-nil error without saturation stops the bisection unchanged.
type WindowQuery[T any] func(from, to time.Time) (rows []T, saturated bool, err error)

// Bisect runs query over [from, to) and splits every saturated window at
// SplitWindow until each leaf returns unsaturated. Leaf rows are concatenated
// in time order. A window that cannot be split further fails the whole query
// with a *SaturatedWindowError, so a caller never commits a partial window.
func Bisect[T any](from, to time.Time, step, minimum time.Duration, query WindowQuery[T]) ([]T, error) {
	rows, saturated, err := query(from, to)
	if !saturated {
		if err != nil {
			return nil, err
		}
		return rows, nil
	}
	mid, splitErr := SplitWindow(from, to, step, minimum)
	if splitErr != nil {
		return nil, &SaturatedWindowError{From: from, To: to, Reason: splitErr, Cause: err}
	}
	left, err := Bisect(from, mid, step, minimum, query)
	if err != nil {
		return nil, err
	}
	right, err := Bisect(mid, to, step, minimum, query)
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}
