package kata

import (
	"context"
	"time"
)

// withRetry executes fn, retrying up to `count` additional times on failure.
// onRetry is called before each retry attempt (may be nil).
func withRetry(
	ctx context.Context,
	count int,
	policy RetryPolicy,
	onRetry func(attempt int, err error),
	fn func(context.Context) error,
) error {
	err := fn(ctx)
	if err == nil || count == 0 {
		return err
	}

	for attempt := 1; attempt <= count; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if onRetry != nil {
			onRetry(attempt, err)
		}
		if policy != nil {
			if wait := policy(attempt - 1); wait > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(wait):
				}
			}
		}
		err = fn(ctx)
		if err == nil {
			return nil
		}
	}
	return err
}

// withTimeout wraps fn in a context deadline if timeout > 0.
func withTimeout(ctx context.Context, timeout time.Duration, fn func(context.Context) error) error {
	if timeout == 0 {
		return fn(ctx)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return fn(ctx)
}
