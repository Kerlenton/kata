package kata

import "time"

// RetryPolicy determines the wait duration between retry attempts.
type RetryPolicy func(attempt int) time.Duration

// Fixed waits the same duration between every attempt.
func Fixed(d time.Duration) RetryPolicy {
	return func(_ int) time.Duration {
		return d
	}
}

// Exponential doubles the wait time on each attempt, starting from base.
// e.g. base=100ms → 100ms, 200ms, 400ms, 800ms...
func Exponential(base time.Duration) RetryPolicy {
	return func(attempt int) time.Duration {
		d := base
		for i := 0; i < attempt; i++ {
			d *= 2
		}
		return d
	}
}

// NoDelay retries immediately with no wait between attempts.
var NoDelay RetryPolicy = func(_ int) time.Duration {
	return 0
}
