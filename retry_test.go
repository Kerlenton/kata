package kata_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kerlenton/kata"
)

// ── Basic retry ───────────────────────────────────────────────────────────────

func TestRetry(t *testing.T) {
	calls := 0
	runner := kata.New(
		kata.Step("flaky", func(_ context.Context, s *testState) error {
			calls++
			if calls < 3 {
				return errors.New("not yet")
			}
			s.append("do:flaky")
			return nil
		}).Retry(3, kata.NoDelay),
	)
	s := &testState{}
	if err := runner.Run(context.Background(), s); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetryExhausted(t *testing.T) {
	calls := 0
	runner := kata.New(
		kata.Step("always-fail", func(_ context.Context, _ *testState) error {
			calls++
			return errors.New("always fails")
		}).Retry(2, kata.NoDelay),
	)
	err := runner.Run(context.Background(), &testState{})
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	// 1 initial + 2 retries = 3 total.
	if calls != 3 {
		t.Errorf("expected 3 calls (1 + 2 retries), got %d", calls)
	}
}

func TestRetryZero(t *testing.T) {
	calls := 0
	runner := kata.New(
		kata.Step("no-retry", func(_ context.Context, _ *testState) error {
			calls++
			return errors.New("fail")
		}).Retry(0, kata.NoDelay),
	)
	_ = runner.Run(context.Background(), &testState{})
	if calls != 1 {
		t.Errorf("expected 1 call with 0 retries, got %d", calls)
	}
}

func TestRetrySucceedsOnLastAttempt(t *testing.T) {
	calls := 0
	runner := kata.New(
		kata.Step("last-chance", func(_ context.Context, _ *testState) error {
			calls++
			if calls <= 3 { // fails 3 times, succeeds on 4th (initial + 3 retries)
				return errors.New("not yet")
			}
			return nil
		}).Retry(3, kata.NoDelay),
	)
	if err := runner.Run(context.Background(), &testState{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 4 {
		t.Errorf("expected 4 calls, got %d", calls)
	}
}

// ── Retry with context cancellation ───────────────────────────────────────────

func TestRetryRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0

	runner := kata.New(
		kata.Step("cancel-during-retry", func(_ context.Context, _ *testState) error {
			calls++
			if calls == 1 {
				cancel() // cancel before first retry
			}
			return errors.New("fail")
		}).Retry(5, kata.NoDelay),
	)
	err := runner.Run(ctx, &testState{})
	if err == nil {
		t.Fatal("expected error")
	}
	// Should stop retrying after context is cancelled.
	if calls > 2 {
		t.Errorf("expected at most 2 calls, got %d (retries should stop after cancel)", calls)
	}
}

// ── Fixed policy ──────────────────────────────────────────────────────────────

func TestFixed(t *testing.T) {
	policy := kata.Fixed(100 * time.Millisecond)
	for attempt := range 5 {
		d := policy(attempt)
		if d != 100*time.Millisecond {
			t.Errorf("attempt %d: expected 100ms, got %v", attempt, d)
		}
	}
}

// ── Exponential policy ────────────────────────────────────────────────────────

func TestExponential(t *testing.T) {
	policy := kata.Exponential(100 * time.Millisecond)
	expected := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		1600 * time.Millisecond,
	}
	for i, want := range expected {
		got := policy(i)
		if got != want {
			t.Errorf("attempt %d: expected %v, got %v", i, want, got)
		}
	}
}

func TestExponentialOverflowProtection(t *testing.T) {
	policy := kata.Exponential(1 * time.Second)
	// At attempt 60, naive doubling would overflow int64.
	d := policy(60)
	if d <= 0 || d > 5*time.Minute {
		t.Errorf("expected capped positive duration, got %v", d)
	}
}

func TestExponentialLargeBase(t *testing.T) {
	policy := kata.Exponential(3 * time.Minute)
	// Already close to cap at attempt 0.
	d := policy(1)
	if d > 5*time.Minute {
		t.Errorf("expected ≤5m, got %v", d)
	}
}

// ── NoDelay policy ────────────────────────────────────────────────────────────

func TestNoDelay(t *testing.T) {
	for attempt := range 10 {
		d := kata.NoDelay(attempt)
		if d != 0 {
			t.Errorf("attempt %d: expected 0, got %v", attempt, d)
		}
	}
}

// ── Jitter ────────────────────────────────────────────────────────────────────

func TestJitter(t *testing.T) {
	base := kata.Fixed(1 * time.Second)
	policy := kata.Jitter(base)

	// Run many times to check range and variance.
	var sawDifferent bool
	var first time.Duration
	for i := range 100 {
		d := policy(0)
		if d < 750*time.Millisecond || d >= 1250*time.Millisecond {
			t.Errorf("jitter out of ±25%% range: %v", d)
		}
		if i == 0 {
			first = d
		} else if d != first {
			sawDifferent = true
		}
	}
	if !sawDifferent {
		t.Error("jitter produced identical values across 100 calls - likely not random")
	}
}

func TestJitterZeroDelay(t *testing.T) {
	policy := kata.Jitter(kata.NoDelay)
	d := policy(0)
	if d != 0 {
		t.Errorf("expected 0 for jittered zero delay, got %v", d)
	}
}

// ── Cap ───────────────────────────────────────────────────────────────────────

func TestCap(t *testing.T) {
	policy := kata.Cap(kata.Exponential(100*time.Millisecond), 500*time.Millisecond)

	tests := []struct {
		attempt int
		max     time.Duration
	}{
		{0, 100 * time.Millisecond},  // 100ms
		{1, 200 * time.Millisecond},  // 200ms
		{2, 400 * time.Millisecond},  // 400ms
		{3, 500 * time.Millisecond},  // 800ms capped to 500ms
		{4, 500 * time.Millisecond},  // capped
		{10, 500 * time.Millisecond}, // capped
	}
	for _, tt := range tests {
		got := policy(tt.attempt)
		if got > tt.max {
			t.Errorf("attempt %d: expected ≤%v, got %v", tt.attempt, tt.max, got)
		}
	}
}

// ── Composability ─────────────────────────────────────────────────────────────

func TestCapJitterComposition(t *testing.T) {
	policy := kata.Cap(
		kata.Jitter(kata.Exponential(100*time.Millisecond)),
		300*time.Millisecond,
	)
	for i := range 50 {
		d := policy(i)
		if d > 300*time.Millisecond {
			t.Errorf("attempt %d: expected ≤300ms after cap, got %v", i, d)
		}
		if d < 0 {
			t.Errorf("attempt %d: negative duration: %v", i, d)
		}
	}
}

// ── Retry with actual backoff timing ──────────────────────────────────────────

func TestRetryWithFixedDelay(t *testing.T) {
	calls := 0
	start := time.Now()

	runner := kata.New(
		kata.Step("delayed", func(_ context.Context, _ *testState) error {
			calls++
			if calls < 3 {
				return errors.New("fail")
			}
			return nil
		}).Retry(2, kata.Fixed(30*time.Millisecond)),
	)

	if err := runner.Run(context.Background(), &testState{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	elapsed := time.Since(start)
	// 2 retries × 30ms = ~60ms minimum.
	if elapsed < 50*time.Millisecond {
		t.Errorf("retries too fast: %v, expected ≥50ms", elapsed)
	}
}
