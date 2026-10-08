// Package passwordwork bounds native password computation independently of its
// caller's transaction lifetime. Results are data, never identity authority.
package passwordwork

import (
	"context"

	"github.com/ajent-social/amos/identity/password"
)

// Slots count computations, including those whose canceled callers returned.
// The existing hasher's instance and process limits also remain in force.
var slots = make(chan struct{}, 2)

// Verification describes computation only. Replacement has not been persisted.
type Verification struct {
	Verified    bool
	Replacement string
}

type result[T any] struct {
	value T
	err   error
}

// Verify performs exactly one existing verifier call with the original native
// budget context. Optional rehash is captured in memory, never written to SQL.
func Verify(ctx context.Context, h *password.Hasher, budgetKey, secret, encoded string, rehash bool) (Verification, error) {
	return run(ctx, h, budgetKey, func() (Verification, error) {
		var replacement string
		var captureErr error
		var capture password.PersistRehash
		if rehash {
			called := false
			capture = func(_ context.Context, old, next string) error {
				if called || old != encoded || next == "" || len(next) > 256 {
					captureErr = password.ErrUnavailable
					return captureErr
				}
				called = true
				replacement = next
				return nil
			}
		}
		verified, err := h.Verify(ctx, budgetKey, secret, encoded, capture)
		if err != nil {
			return Verification{}, err
		}
		// Hasher intentionally tolerates optional persistence failure. A malformed
		// capture is nevertheless an invalid adapter result, not authentication.
		if captureErr != nil || (!verified.Verified && replacement != "") {
			return Verification{}, password.ErrUnavailable
		}
		return Verification{Verified: verified.Verified, Replacement: replacement}, nil
	})
}

// Hash uses the same global computation bound and existing password policy.
func Hash(ctx context.Context, h *password.Hasher, budgetKey, secret string) (string, error) {
	return run(ctx, h, budgetKey, func() (string, error) {
		return h.Hash(ctx, budgetKey, secret)
	})
}

// run has only the two fixed native callers above. Its function parameter is
// private plumbing, not a configurable host callback or credential producer.
func run[T any](ctx context.Context, h *password.Hasher, key string, compute func() (T, error)) (T, error) {
	var zero T
	if ctx == nil || h == nil || key == "" {
		return zero, password.ErrUnavailable
	}
	if _, ok := ctx.Deadline(); !ok {
		return zero, password.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	select {
	case slots <- struct{}{}:
	default:
		return zero, password.ErrBusy
	}
	if err := ctx.Err(); err != nil {
		<-slots
		return zero, err
	}
	completed := make(chan result[T], 1)
	go func() {
		defer func() { <-slots }()
		value, err := compute()
		completed <- result[T]{value: value, err: err}
	}()
	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case value := <-completed:
		return accept(ctx, value)
	}
}

func accept[T any](ctx context.Context, computed result[T]) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if computed.err != nil {
		return zero, computed.err
	}
	return computed.value, nil
}
