package kata_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
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

// ── Cancelled context (SIGTERM simulation) ────────────────────────────────────

// TestCompensationRunsAfterContextCancelled verifies that when the outer context
// is cancelled (e.g. SIGTERM) compensation still runs to completion.
// This is the key correctness guarantee: rollback must not be skipped just
// because the caller's context is gone.
func TestCompensationRunsAfterContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	compensated := false

	runner := kata.New(
		kata.Step("a", func(_ context.Context, s *testState) error {
			s.append("do:a")
			return nil
		}).Compensate(func(_ context.Context, s *testState) error {
			compensated = true
			s.append("undo:a")
			return nil
		}),
		kata.Step("b", func(_ context.Context, _ *testState) error {
			// Cancel the context mid-run to simulate SIGTERM, then fail.
			cancel()
			return errors.New("b failed")
		}),
	)

	s := &testState{}
	err := runner.Run(ctx, s)

	var stepErr *kata.StepError
	if !errors.As(err, &stepErr) {
		t.Fatalf("expected *StepError, got %T: %v", err, err)
	}
	if !compensated {
		t.Error("compensation did not run after context cancellation")
	}
	assertLog(t, s.log, []string{"do:a", "undo:a"})
}

// TestCompensationContextIsNotCancelled verifies that the context passed to
// compensation functions is not the cancelled outer context.
func TestCompensationContextIsNotCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	compCtxAlive := false

	runner := kata.New(
		kata.Step("a", func(_ context.Context, s *testState) error {
			s.append("do:a")
			return nil
		}).Compensate(func(ctx context.Context, s *testState) error {
			// Compensation should receive a fresh context, not the cancelled one.
			compCtxAlive = ctx.Err() == nil
			s.append("undo:a")
			return nil
		}),
		kata.Step("b", func(_ context.Context, _ *testState) error {
			cancel()
			return errors.New("b failed")
		}),
	)

	s := &testState{}
	_ = runner.Run(ctx, s)

	if !compCtxAlive {
		t.Error("compensation received a cancelled context, expected fresh context.Background()")
	}
}

// TestCancelledContextCompensationCompletes verifies all compensations run fully
// even when the context is cancelled before the failing step.
func TestCancelledContextCompensationCompletes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var compLog []string
	var mu sync.Mutex
	appendComp := func(name string) {
		mu.Lock()
		compLog = append(compLog, name)
		mu.Unlock()
	}

	runner := kata.New(
		kata.Step("a", func(_ context.Context, _ *testState) error {
			return nil
		}).Compensate(func(_ context.Context, _ *testState) error {
			appendComp("undo:a")
			return nil
		}),
		kata.Step("b", func(_ context.Context, _ *testState) error {
			return nil
		}).Compensate(func(_ context.Context, _ *testState) error {
			appendComp("undo:b")
			return nil
		}),
		kata.Step("c", func(_ context.Context, _ *testState) error {
			cancel() // cancel before c fails
			return errors.New("c failed")
		}),
	)

	err := runner.Run(ctx, &testState{})
	if err == nil {
		t.Fatal("expected error")
	}

	// Both a and b must be compensated despite cancelled ctx.
	if !slices.Contains(compLog, "undo:a") || !slices.Contains(compLog, "undo:b") {
		t.Errorf("not all compensations ran: %v", compLog)
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

func TestParallelCompensationFailureReturnsCompensationError(t *testing.T) {
	runner := kata.New(
		kata.Parallel("group",
			kata.Step("a",
				func(_ context.Context, _ *testState) error { return nil },
			).Compensate(func(_ context.Context, _ *testState) error {
				return errors.New("comp of a failed")
			}),
			kata.Step("b",
				func(_ context.Context, _ *testState) error { return errors.New("b failed") },
			),
		),
	)

	err := runner.Run(context.Background(), &testState{})

	var compErr *kata.CompensationError
	if !errors.As(err, &compErr) {
		t.Fatalf("expected *CompensationError, got %T: %v", err, err)
	}
	if compErr.StepName != "group" {
		t.Errorf("wrong StepName: %q", compErr.StepName)
	}
	if len(compErr.Failed) != 1 || compErr.Failed[0].StepName != "a" {
		t.Errorf("unexpected failures: %+v", compErr.Failed)
	}
}

// ── Parallel: wrapped context.Canceled ───────────────────────────────────────
//
// These tests cover the bug where sibling steps cancelled by the group's own
// cancel() returned fmt.Errorf("...: %w", ctx.Err()) — a wrapped
// context.Canceled. Before the fix, the pointer comparison
//
//	r.err != context.Canceled
//
// evaluated to true for wrapped errors, causing sibling cancellations to leak
// into the failure list and trigger spurious compensations.
// After the fix errors.Is(r.err, context.Canceled) is used instead.

// TestParallelWrappedCanceledNotCountedAsFailure is the direct regression test.
//
// Step "b" fails with a real error.
// Step "a" receives a cancelled context and returns fmt.Errorf("...: %w", ctx.Err()),
// i.e. a wrapped context.Canceled.
//
// Only "b"'s root cause must appear in the returned error.
func TestParallelWrappedCanceledNotCountedAsFailure(t *testing.T) {
	runner := kata.New(
		kata.Parallel("group",
			// "a" wraps the cancellation error instead of returning it raw.
			kata.Step("a", func(ctx context.Context, _ *testState) error {
				select {
				case <-ctx.Done():
					return fmt.Errorf("a interrupted: %w", ctx.Err())
				case <-time.After(5 * time.Second):
					return nil
				}
			}),
			// "b" is the root cause — it fails with a domain error.
			kata.Step("b", func(_ context.Context, _ *testState) error {
				return errors.New("b: payment gateway timeout")
			}),
		),
	)

	err := runner.Run(context.Background(), &testState{})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	// The runner-level error must mention only "b"'s cause, not "a interrupted".
	if strings.Contains(err.Error(), "a interrupted") {
		t.Errorf("wrapped context.Canceled from step \"a\" leaked into the error: %v", err)
	}
	if !strings.Contains(err.Error(), "payment gateway timeout") {
		t.Errorf("root cause from step \"b\" missing from error: %v", err)
	}
}

// TestParallelRawCanceledNotCountedAsFailure mirrors the above but the sibling
// returns the raw ctx.Err() — verifying the baseline still works after the fix.
func TestParallelRawCanceledNotCountedAsFailure(t *testing.T) {
	runner := kata.New(
		kata.Parallel("group",
			// "a" returns raw context.Canceled (no wrapping).
			kata.Step("a", func(ctx context.Context, _ *testState) error {
				select {
				case <-ctx.Done():
					return ctx.Err() // raw — context.Canceled
				case <-time.After(5 * time.Second):
					return nil
				}
			}),
			kata.Step("b", func(_ context.Context, _ *testState) error {
				return errors.New("b: real failure")
			}),
		),
	)

	err := runner.Run(context.Background(), &testState{})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if strings.Contains(err.Error(), "context canceled") {
		t.Errorf("raw context.Canceled from sibling step leaked into error: %v", err)
	}
	if !strings.Contains(err.Error(), "b: real failure") {
		t.Errorf("root cause missing from error: %v", err)
	}
}

// TestParallelOnlyRealFailuresCompensated verifies the compensation side of the
// same bug: only steps that succeeded should be compensated. A step that was
// cancelled mid-execution did not commit any work, so it must not be compensated.
func TestParallelOnlyRealFailuresCompensated(t *testing.T) {
	var mu sync.Mutex
	var compLog []string
	appendComp := func(name string) {
		mu.Lock()
		compLog = append(compLog, name)
		mu.Unlock()
	}

	runner := kata.New(
		kata.Parallel("group",
			// "a" succeeds before "b" fires — should be compensated.
			kata.Step("a", func(_ context.Context, _ *testState) error {
				return nil
			}).Compensate(func(_ context.Context, _ *testState) error {
				appendComp("undo:a")
				return nil
			}),
			// "b" fails — triggers cancellation of siblings.
			kata.Step("b", func(_ context.Context, _ *testState) error {
				return errors.New("b failed")
			}).Compensate(func(_ context.Context, _ *testState) error {
				// "b" never committed work — its compensation must not run.
				appendComp("undo:b")
				return nil
			}),
			// "c" is slow and gets cancelled; it wraps the ctx error.
			kata.Step("c", func(ctx context.Context, _ *testState) error {
				select {
				case <-ctx.Done():
					return fmt.Errorf("c aborted: %w", ctx.Err())
				case <-time.After(5 * time.Second):
					return nil
				}
			}).Compensate(func(_ context.Context, _ *testState) error {
				// "c" was cancelled before doing any work — must not be compensated.
				appendComp("undo:c")
				return nil
			}),
		),
	)

	err := runner.Run(context.Background(), &testState{})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	for _, entry := range compLog {
		if entry == "undo:b" {
			t.Error("step \"b\" was compensated but it never committed work (it failed)")
		}
		if entry == "undo:c" {
			t.Error("step \"c\" was compensated but it was only cancelled — wrapped context.Canceled treated as success")
		}
	}
	if !slices.Contains(compLog, "undo:a") {
		t.Errorf("step \"a\" was not compensated; comp log: %v", compLog)
	}
}

// TestParallelMultipleWrappedCancellationsOneRealFailure checks that when many
// siblings all return wrapped cancellations, only the one real domain error is
// surfaced — not an aggregate of N "context canceled" noise.
func TestParallelMultipleWrappedCancellationsOneRealFailure(t *testing.T) {
	makeSlow := func(name string) kata.StepFunc[*testState] {
		return func(ctx context.Context, _ *testState) error {
			select {
			case <-ctx.Done():
				return fmt.Errorf("%s: %w", name, ctx.Err())
			case <-time.After(5 * time.Second):
				return nil
			}
		}
	}

	runner := kata.New(
		kata.Parallel("group",
			kata.Step("slow-1", makeSlow("slow-1")),
			kata.Step("slow-2", makeSlow("slow-2")),
			kata.Step("slow-3", makeSlow("slow-3")),
			kata.Step("bomber", func(_ context.Context, _ *testState) error {
				return errors.New("only real error")
			}),
		),
	)

	err := runner.Run(context.Background(), &testState{})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	msg := err.Error()

	if !strings.Contains(msg, "only real error") {
		t.Errorf("root cause missing from error: %v", msg)
	}
	for _, sibling := range []string{"slow-1", "slow-2", "slow-3"} {
		if strings.Contains(msg, sibling) {
			t.Errorf("cancelled sibling %q leaked into error message: %v", sibling, msg)
		}
	}
}

// TestParallelAllFailWithWrappedCanceledFromExternalCtx ensures that when the
// caller passes an already-cancelled context, every step returns a wrapped
// context.Canceled, and the group still reports an error rather than returning
// nil (which would falsely signal success).
func TestParallelAllFailWithWrappedCanceledFromExternalCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	runner := kata.New(
		kata.Parallel("group",
			kata.Step("a", func(ctx context.Context, _ *testState) error {
				return fmt.Errorf("a: %w", ctx.Err())
			}),
			kata.Step("b", func(ctx context.Context, _ *testState) error {
				return fmt.Errorf("b: %w", ctx.Err())
			}),
		),
	)

	err := runner.Run(ctx, &testState{})
	if err == nil {
		t.Fatal("expected error when all steps received pre-cancelled context, got nil")
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
