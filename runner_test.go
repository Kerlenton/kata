package kata_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kerlenton/kata"
)

type testState struct {
	log  []string
	mu   sync.Mutex
	fail string
}

func (s *testState) append(v string) {
	s.mu.Lock()
	s.log = append(s.log, v)
	s.mu.Unlock()
}

func mkStep(name string) kata.StepFunc[*testState] {
	return func(_ context.Context, s *testState) error {
		if s.fail == name {
			return errors.New("injected failure in " + name)
		}
		s.append("do:" + name)
		return nil
	}
}

func mkComp(name string) kata.StepFunc[*testState] {
	return func(_ context.Context, s *testState) error {
		s.append("undo:" + name)
		return nil
	}
}

// ── Sequential ────────────────────────────────────────────────────────────────

func TestHappyPath(t *testing.T) {
	runner := kata.New(
		kata.Step("a", mkStep("a")).Compensate(mkComp("a")),
		kata.Step("b", mkStep("b")).Compensate(mkComp("b")),
		kata.Step("c", mkStep("c")),
	)
	s := &testState{}
	if err := runner.Run(context.Background(), s); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertLog(t, s.log, []string{"do:a", "do:b", "do:c"})
}

func TestCompensationOrder(t *testing.T) {
	runner := kata.New(
		kata.Step("a", mkStep("a")).Compensate(mkComp("a")),
		kata.Step("b", mkStep("b")).Compensate(mkComp("b")),
		kata.Step("c", mkStep("c")).Compensate(mkComp("c")),
	)
	s := &testState{fail: "b"}
	err := runner.Run(context.Background(), s)

	var stepErr *kata.StepError
	if !errors.As(err, &stepErr) {
		t.Fatalf("expected *StepError, got %T: %v", err, err)
	}
	if stepErr.StepName != "b" {
		t.Errorf("wrong StepName: %q", stepErr.StepName)
	}
	assertLog(t, s.log, []string{"do:a", "undo:a"})
}

func TestNoCompensation(t *testing.T) {
	runner := kata.New(
		kata.Step("a", mkStep("a")),
		kata.Step("b", mkStep("b")).Compensate(mkComp("b")),
		kata.Step("c", mkStep("c")).Compensate(mkComp("c")),
	)
	s := &testState{fail: "c"}
	if err := runner.Run(context.Background(), s); err == nil {
		t.Fatal("expected error")
	}
	assertLog(t, s.log, []string{"do:a", "do:b", "undo:b"})
}

func TestCompensationFailure(t *testing.T) {
	runner := kata.New(
		kata.Step("a", mkStep("a")).Compensate(func(_ context.Context, _ *testState) error {
			return errors.New("comp of a failed")
		}),
		kata.Step("b", mkStep("b")),
	)
	s := &testState{fail: "b"}
	err := runner.Run(context.Background(), s)

	var compErr *kata.CompensationError
	if !errors.As(err, &compErr) {
		t.Fatalf("expected *CompensationError, got %T: %v", err, err)
	}
	if len(compErr.Failed) != 1 || compErr.Failed[0].StepName != "a" {
		t.Errorf("unexpected failures: %+v", compErr.Failed)
	}
}

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

func TestTimeout(t *testing.T) {
	runner := kata.New(
		kata.Step("slow", func(ctx context.Context, _ *testState) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return nil
			}
		}).Timeout(20 * time.Millisecond),
	)
	s := &testState{}
	err := runner.Run(context.Background(), s)

	var stepErr *kata.StepError
	if !errors.As(err, &stepErr) {
		t.Fatalf("expected *StepError, got %T", err)
	}
	if !errors.Is(stepErr.Cause, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", stepErr.Cause)
	}
}

// ── Parallel ──────────────────────────────────────────────────────────────────

func TestParallelAllSucceed(t *testing.T) {
	var countA, countB, countC atomic.Int32

	runner := kata.New(
		kata.Parallel("group",
			kata.Step("a", func(_ context.Context, _ *testState) error { countA.Add(1); return nil }),
			kata.Step("b", func(_ context.Context, _ *testState) error { countB.Add(1); return nil }),
			kata.Step("c", func(_ context.Context, _ *testState) error { countC.Add(1); return nil }),
		),
	)
	if err := runner.Run(context.Background(), &testState{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if countA.Load() != 1 || countB.Load() != 1 || countC.Load() != 1 {
		t.Error("not all parallel steps ran exactly once")
	}
}

func TestParallelRunsConcurrently(t *testing.T) {
	start := time.Now()
	runner := kata.New(
		kata.Parallel("group",
			kata.Step("a", func(_ context.Context, _ *testState) error { time.Sleep(50 * time.Millisecond); return nil }),
			kata.Step("b", func(_ context.Context, _ *testState) error { time.Sleep(50 * time.Millisecond); return nil }),
			kata.Step("c", func(_ context.Context, _ *testState) error { time.Sleep(50 * time.Millisecond); return nil }),
		),
	)
	if err := runner.Run(context.Background(), &testState{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 120*time.Millisecond {
		t.Errorf("steps likely ran sequentially (took %v, expected <120ms)", elapsed)
	}
}

func TestParallelOneFailsCompensatesSucceeded(t *testing.T) {
	var log []string
	var mu sync.Mutex
	appendLog := func(s string) { mu.Lock(); log = append(log, s); mu.Unlock() }

	runner := kata.New(
		kata.Parallel("group",
			kata.Step("a",
				func(_ context.Context, _ *testState) error { appendLog("do:a"); return nil },
			).Compensate(func(_ context.Context, _ *testState) error { appendLog("undo:a"); return nil }),
			kata.Step("b",
				func(_ context.Context, _ *testState) error { return errors.New("b failed") },
			).Compensate(func(_ context.Context, _ *testState) error { appendLog("undo:b"); return nil }),
			kata.Step("c",
				func(_ context.Context, _ *testState) error { appendLog("do:c"); return nil },
			).Compensate(func(_ context.Context, _ *testState) error { appendLog("undo:c"); return nil }),
		),
	)
	err := runner.Run(context.Background(), &testState{})
	if err == nil {
		t.Fatal("expected error when parallel step fails")
	}
	// b failed -> should NOT be compensated
	if slices.Contains(log, "undo:b") {
		t.Error("b was compensated but it failed - must not be compensated")
	}
	// a and c succeeded -> must be compensated
	if !slices.Contains(log, "undo:a") || !slices.Contains(log, "undo:c") {
		t.Errorf("expected undo:a and undo:c, got: %v", log)
	}
}

func TestParallelGroupCompensatedByOuterRunner(t *testing.T) {
	var compA, compB bool

	runner := kata.New(
		kata.Step("seq1", mkStep("seq1")),
		kata.Parallel("group",
			kata.Step("a", func(_ context.Context, _ *testState) error { return nil }).
				Compensate(func(_ context.Context, _ *testState) error { compA = true; return nil }),
			kata.Step("b", func(_ context.Context, _ *testState) error { return nil }).
				Compensate(func(_ context.Context, _ *testState) error { compB = true; return nil }),
		),
		kata.Step("seq2", mkStep("seq2")), // will fail
	)

	s := &testState{fail: "seq2"}
	if err := runner.Run(context.Background(), s); err == nil {
		t.Fatal("expected error")
	}
	if !compA || !compB {
		t.Errorf("parallel steps not compensated: compA=%v compB=%v", compA, compB)
	}
}

// ── Hooks ─────────────────────────────────────────────────────────────────────

func TestHooks(t *testing.T) {
	var hookLog []string

	runner := kata.New(
		kata.Step("a", mkStep("a")).Compensate(mkComp("a")),
		kata.Step("b", mkStep("b")),
	).WithOptions(kata.WithHooks(kata.Hooks{
		OnStepStart:         func(_ context.Context, name string) { hookLog = append(hookLog, "start:"+name) },
		OnStepDone:          func(_ context.Context, name string, _ time.Duration) { hookLog = append(hookLog, "done:"+name) },
		OnStepFailed:        func(_ context.Context, name string, _ error) { hookLog = append(hookLog, "failed:"+name) },
		OnCompensationStart: func(_ context.Context, name string) { hookLog = append(hookLog, "comp-start:"+name) },
		OnCompensationDone:  func(_ context.Context, name string) { hookLog = append(hookLog, "comp-done:"+name) },
	}))

	s := &testState{fail: "b"}
	_ = runner.Run(context.Background(), s)

	assertLog(t, hookLog, []string{"start:a", "done:a", "start:b", "failed:b", "comp-start:a", "comp-done:a"})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func assertLog(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("log mismatch\n  got:  %v\n  want: %v", got, want)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
		}
	}
}
