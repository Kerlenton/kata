package kata_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kerlenton/kata"
)

// ── OnStepStart / OnStepDone ──────────────────────────────────────────────────

func TestHooksStartDone(t *testing.T) {
	var mu sync.Mutex
	var started, done []string

	hooks := kata.WithHooks(kata.Hooks{
		OnStepStart: func(_ context.Context, name string) {
			mu.Lock()
			started = append(started, name)
			mu.Unlock()
		},
		OnStepDone: func(_ context.Context, name string, d time.Duration) {
			mu.Lock()
			done = append(done, name)
			mu.Unlock()
			if d <= 0 {
				t.Errorf("step %q: expected positive duration, got %v", name, d)
			}
		},
	})

	runner := kata.New(
		kata.Step("a", mkStep("a")),
		kata.Step("b", mkStep("b")),
	).WithOptions(hooks)

	if err := runner.Run(context.Background(), &testState{}); err != nil {
		t.Fatal(err)
	}
	assertLog(t, started, []string{"a", "b"})
	assertLog(t, done, []string{"a", "b"})
}

// ── OnStepFailed ──────────────────────────────────────────────────────────────

func TestHooksStepFailed(t *testing.T) {
	var failedName string
	var failedErr error

	hooks := kata.WithHooks(kata.Hooks{
		OnStepFailed: func(_ context.Context, name string, err error) {
			failedName = name
			failedErr = err
		},
	})

	runner := kata.New(
		kata.Step("a", mkStep("a")),
		kata.Step("b", mkStep("b")),
	).WithOptions(hooks)

	s := &testState{fail: "b"}
	_ = runner.Run(context.Background(), s)

	if failedName != "b" {
		t.Errorf("expected failed step 'b', got %q", failedName)
	}
	if failedErr == nil {
		t.Error("expected non-nil error")
	}
}

// ── OnCompensationStart / OnCompensationDone ──────────────────────────────────

func TestHooksCompensation(t *testing.T) {
	var compStarted, compDone []string
	var mu sync.Mutex

	hooks := kata.WithHooks(kata.Hooks{
		OnCompensationStart: func(_ context.Context, name string) {
			mu.Lock()
			compStarted = append(compStarted, name)
			mu.Unlock()
		},
		OnCompensationDone: func(_ context.Context, name string) {
			mu.Lock()
			compDone = append(compDone, name)
			mu.Unlock()
		},
	})

	runner := kata.New(
		kata.Step("a", mkStep("a")).Compensate(mkComp("a")),
		kata.Step("b", mkStep("b")).Compensate(mkComp("b")),
		kata.Step("c", mkStep("c")),
	).WithOptions(hooks)

	s := &testState{fail: "c"}
	_ = runner.Run(context.Background(), s)

	// Compensated in reverse: b, a.
	assertLog(t, compStarted, []string{"b", "a"})
	assertLog(t, compDone, []string{"b", "a"})
}

// ── OnCompensationFailed ──────────────────────────────────────────────────────

func TestHooksCompensationFailed(t *testing.T) {
	var failedName string

	hooks := kata.WithHooks(kata.Hooks{
		OnCompensationFailed: func(_ context.Context, name string, _ error) {
			failedName = name
		},
	})

	runner := kata.New(
		kata.Step("a", mkStep("a")).Compensate(func(_ context.Context, _ *testState) error {
			return errors.New("comp failed")
		}),
		kata.Step("b", mkStep("b")),
	).WithOptions(hooks)

	s := &testState{fail: "b"}
	_ = runner.Run(context.Background(), s)

	if failedName != "a" {
		t.Errorf("expected compensation failure for 'a', got %q", failedName)
	}
}

// ── OnRetry ───────────────────────────────────────────────────────────────────

func TestHooksOnRetry(t *testing.T) {
	type retryRecord struct {
		name    string
		attempt int
	}

	var mu sync.Mutex
	var retries []retryRecord

	hooks := kata.WithHooks(kata.Hooks{
		OnRetry: func(_ context.Context, name string, attempt int, _ error) {
			mu.Lock()
			retries = append(retries, retryRecord{name, attempt})
			mu.Unlock()
		},
	})

	calls := 0
	runner := kata.New(
		kata.Step("flaky", func(_ context.Context, _ *testState) error {
			calls++
			if calls < 4 {
				return errors.New("fail")
			}
			return nil
		}).Retry(3, kata.NoDelay),
	).WithOptions(hooks)

	if err := runner.Run(context.Background(), &testState{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 3 retries: attempts 1, 2, 3.
	if len(retries) != 3 {
		t.Fatalf("expected 3 OnRetry calls, got %d", len(retries))
	}
	for i, r := range retries {
		if r.name != "flaky" {
			t.Errorf("retry[%d]: expected name 'flaky', got %q", i, r.name)
		}
		if r.attempt != i+1 {
			t.Errorf("retry[%d]: expected attempt %d, got %d", i, i+1, r.attempt)
		}
	}
}

func TestHooksOnRetryNotCalledOnSuccess(t *testing.T) {
	retryCalled := false

	hooks := kata.WithHooks(kata.Hooks{
		OnRetry: func(_ context.Context, _ string, _ int, _ error) {
			retryCalled = true
		},
	})

	runner := kata.New(
		kata.Step("ok", func(_ context.Context, _ *testState) error { return nil }).
			Retry(3, kata.NoDelay),
	).WithOptions(hooks)

	if err := runner.Run(context.Background(), &testState{}); err != nil {
		t.Fatal(err)
	}
	if retryCalled {
		t.Error("OnRetry should not be called when step succeeds on first attempt")
	}
}

func TestHooksOnRetryNotCalledWithoutRetryConfig(t *testing.T) {
	retryCalled := false

	hooks := kata.WithHooks(kata.Hooks{
		OnRetry: func(_ context.Context, _ string, _ int, _ error) {
			retryCalled = true
		},
	})

	runner := kata.New(
		kata.Step("no-retry", func(_ context.Context, _ *testState) error {
			return errors.New("fail")
		}),
	).WithOptions(hooks)

	_ = runner.Run(context.Background(), &testState{})
	if retryCalled {
		t.Error("OnRetry should not be called when step has no retry configured")
	}
}

// ── Nil hooks (no panic) ─────────────────────────────────────────────────────

func TestNilHooksDoNotPanic(t *testing.T) {
	// Default zero-value Hooks - all nil functions.
	runner := kata.New(
		kata.Step("a", mkStep("a")).Compensate(mkComp("a")),
		kata.Step("b", mkStep("b")),
	)

	s := &testState{fail: "b"}
	_ = runner.Run(context.Background(), s)
	// If we reach here without panic, the test passes.
}

// ── Hooks on parallel group ──────────────────────────────────────────────────

func TestHooksParallelGroup(t *testing.T) {
	var mu sync.Mutex
	var started []string

	hooks := kata.WithHooks(kata.Hooks{
		OnStepStart: func(_ context.Context, name string) {
			mu.Lock()
			started = append(started, name)
			mu.Unlock()
		},
	})

	runner := kata.New(
		kata.Parallel("notify",
			kata.Step("email", func(_ context.Context, _ *testState) error { return nil }),
			kata.Step("sms", func(_ context.Context, _ *testState) error { return nil }),
		),
	).WithOptions(hooks)

	if err := runner.Run(context.Background(), &testState{}); err != nil {
		t.Fatal(err)
	}

	// Should see the group name and both child step names.
	assertContains(t, started, "notify")
	assertContains(t, started, "email")
	assertContains(t, started, "sms")
}
