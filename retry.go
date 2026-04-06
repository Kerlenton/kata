package kata

import (
	"math/rand/v2"
	"time"
)

// maxBackoff is the absolute ceiling for any computed backoff duration.
const maxBackoff = 5 * time.Minute

// RetryPolicy determines the wait duration between retry attempts.
type RetryPolicy func(attempt int) time.Duration

// Fixed waits the same duration between every attempt.
func Fixed(d time.Duration) RetryPolicy {
	return func(_ int) time.Duration {
		return d
	}
}

// Exponential doubles the wait time on each attempt, starting from base.
// The result is capped at 5 minutes to prevent overflow.
//
//	Exponential(100*time.Millisecond)
//	// attempt 0: 100ms, 1: 200ms, 2: 400ms, 3: 800ms, ...
func Exponential(base time.Duration) RetryPolicy {
	return func(attempt int) time.Duration {
		d := base
		for range attempt {
			if d > maxBackoff/2 {
				return maxBackoff
			}
			d *= 2
		}
		return d
	}
}

// NoDelay retries immediately with no wait between attempts.
var NoDelay RetryPolicy = func(_ int) time.Duration {
	return 0
}

// Jitter wraps a retry policy adding ±25% random jitter to each delay.
// This prevents thundering-herd effects when many callers retry simultaneously.
//
//	kata.Jitter(kata.Exponential(100*time.Millisecond))
func Jitter(policy RetryPolicy) RetryPolicy {
	return func(attempt int) time.Duration {
		d := policy(attempt)
		if d <= 0 {
			return 0
		}
		// ±25%: multiply by [0.75, 1.25)
		jitter := 0.75 + rand.Float64()*0.5
		return time.Duration(float64(d) * jitter)
	}
}

// Cap wraps a retry policy capping the delay at max.
//
//	kata.Cap(kata.Exponential(100*time.Millisecond), 30*time.Second)
func Cap(policy RetryPolicy, max time.Duration) RetryPolicy {
	return func(attempt int) time.Duration {
		d := policy(attempt)
		if d > max {
			return max
		}
		return d
	}
}
