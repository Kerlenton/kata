package kata_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kerlenton/kata"
)

// ── Happy path ────────────────────────────────────────────────────────────────

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

func TestEmptyRunner(t *testing.T) {
	runner := kata.New[*testState]()
	if err := runner.Run(context.Background(), &testState{}); err != nil {
		t.Fatalf("empty runner should succeed: %v", err)
	}
}

func TestSingleStep(t *testing.T) {
	runner := kata.New(
		kata.Step("only", mkStep("only")),
	)
	s := &testState{}
	if err := runner.Run(context.Background(), s); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertLog(t, s.log, []string{"do:only"})
}

// ── Compensation order ────────────────────────────────────────────────────────

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
	// Only "a" completed, so only "a" is compensated.
	assertLog(t, s.log, []string{"do:a", "undo:a"})
}

func TestCompensationReverseOrder(t *testing.T) {
	runner := kata.New(
		kata.Step("a", mkStep("a")).Compensate(mkComp("a")),
		kata.Step("b", mkStep("b")).Compensate(mkComp("b")),
		kata.Step("c", mkStep("c")).Compensate(mkComp("c")),
		kata.Step("d", mkStep("d")),
	)
	s := &testState{fail: "d"}
	err := runner.Run(context.Background(), s)

	var stepErr *kata.StepError
	if !errors.As(err, &stepErr) {
		t.Fatalf("expected *StepError, got %T: %v", err, err)
	}
	// a, b, c completed. Compensated in reverse: c, b, a.
	assertLog(t, s.log, []string{
		"do:a", "do:b", "do:c",
		"undo:c", "undo:b", "undo:a",
	})
}

func TestNoCompensation(t *testing.T) {
	runner := kata.New(
		kata.Step("a", mkStep("a")), // no compensate
		kata.Step("b", mkStep("b")).Compensate(mkComp("b")),
		kata.Step("c", mkStep("c")).Compensate(mkComp("c")),
	)
	s := &testState{fail: "c"}
	if err := runner.Run(context.Background(), s); err == nil {
		t.Fatal("expected error")
	}
	// "a" has no compensation, "b" does.
	assertLog(t, s.log, []string{"do:a", "do:b", "undo:b"})
}

// ── Compensation failure ──────────────────────────────────────────────────────

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

func TestMultipleCompensationFailures(t *testing.T) {
	runner := kata.New(
		kata.Step("a", mkStep("a")).Compensate(func(_ context.Context, _ *testState) error {
			return errors.New("comp a failed")
		}),
		kata.Step("b", mkStep("b")).Compensate(func(_ context.Context, _ *testState) error {
			return errors.New("comp b failed")
		}),
		kata.Step("c", mkStep("c")),
	)
	s := &testState{fail: "c"}
	err := runner.Run(context.Background(), s)

	var compErr *kata.CompensationError
	if !errors.As(err, &compErr) {
		t.Fatalf("expected *CompensationError, got %T: %v", err, err)
	}
	if len(compErr.Failed) != 2 {
		t.Errorf("expected 2 failures, got %d: %+v", len(compErr.Failed), compErr.Failed)
	}
}

// ── First step failure (nothing to compensate) ───────────────────────────────

func TestFirstStepFails(t *testing.T) {
	runner := kata.New(
		kata.Step("a", mkStep("a")).Compensate(mkComp("a")),
		kata.Step("b", mkStep("b")).Compensate(mkComp("b")),
	)
	s := &testState{fail: "a"}
	err := runner.Run(context.Background(), s)

	var stepErr *kata.StepError
	if !errors.As(err, &stepErr) {
		t.Fatalf("expected *StepError, got %T: %v", err, err)
	}
	if stepErr.StepName != "a" {
		t.Errorf("wrong StepName: %q", stepErr.StepName)
	}
	// Nothing completed, no compensations.
	assertLog(t, s.log, nil)
}

// ── StepError unwrap ─────────────────────────────────────────────────────────

func TestStepErrorUnwrap(t *testing.T) {
	sentinel := errors.New("sentinel")
	runner := kata.New(
		kata.Step[*testState]("a", func(_ context.Context, _ *testState) error {
			return sentinel
		}),
	)
	err := runner.Run(context.Background(), &testState{})
	if !errors.Is(err, sentinel) {
		t.Error("StepError should unwrap to the original cause")
	}

	var stepErr *kata.StepError
	if errors.As(err, &stepErr) {
		msg := stepErr.Error()
		if msg == "" {
			t.Error("StepError.Error() should return non-empty string")
		}
	}
}

func TestCompensationErrorUnwrap(t *testing.T) {
	sentinel := errors.New("sentinel")
	runner := kata.New(
		kata.Step("a", mkStep("a")).Compensate(func(_ context.Context, _ *testState) error {
			return errors.New("comp failed")
		}),
		kata.Step[*testState]("b", func(_ context.Context, _ *testState) error {
			return sentinel
		}),
	)
	err := runner.Run(context.Background(), &testState{})
	if !errors.Is(err, sentinel) {
		t.Error("CompensationError should unwrap to the original step cause")
	}

	var compErr *kata.CompensationError
	if errors.As(err, &compErr) {
		msg := compErr.Error()
		if msg == "" {
			t.Error("CompensationError.Error() should return non-empty string")
		}
	}
}

// ── Cancelled context ─────────────────────────────────────────────────────────

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

func TestCompensationContextIsNotCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	compCtxAlive := false

	runner := kata.New(
		kata.Step("a", func(_ context.Context, s *testState) error {
			s.append("do:a")
			return nil
		}).Compensate(func(ctx context.Context, s *testState) error {
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
			cancel()
			return errors.New("c failed")
		}),
	)

	err := runner.Run(ctx, &testState{})
	if err == nil {
		t.Fatal("expected error")
	}

	assertContains(t, compLog, "undo:a")
	assertContains(t, compLog, "undo:b")
}

// ── Context checked between steps ─────────────────────────────────────────────

func TestContextCheckedBetweenSteps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	runner := kata.New(
		kata.Step("a", func(_ context.Context, s *testState) error {
			s.append("do:a")
			cancel() // cancel after a succeeds
			return nil
		}).Compensate(mkComp("a")),
		kata.Step("b", func(_ context.Context, s *testState) error {
			s.append("do:b") // should never run
			return nil
		}).Compensate(mkComp("b")),
	)

	s := &testState{}
	err := runner.Run(ctx, s)

	var stepErr *kata.StepError
	if !errors.As(err, &stepErr) {
		t.Fatalf("expected *StepError, got %T: %v", err, err)
	}
	if stepErr.StepName != "b" {
		t.Errorf("expected step name 'b', got %q", stepErr.StepName)
	}
	if !errors.Is(stepErr.Cause, context.Canceled) {
		t.Errorf("expected context.Canceled cause, got %v", stepErr.Cause)
	}
	// "a" ran and was compensated; "b" never started.
	assertLog(t, s.log, []string{"do:a", "undo:a"})
}

func TestContextDeadlineBetweenSteps(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	runner := kata.New(
		kata.Step("a", func(_ context.Context, s *testState) error {
			s.append("do:a")
			time.Sleep(10 * time.Millisecond) // let deadline expire
			return nil
		}).Compensate(mkComp("a")),
		kata.Step("b", func(_ context.Context, s *testState) error {
			s.append("do:b")
			return nil
		}),
	)

	s := &testState{}
	err := runner.Run(ctx, s)
	if err == nil {
		t.Fatal("expected error from expired deadline")
	}

	var stepErr *kata.StepError
	if !errors.As(err, &stepErr) {
		t.Fatalf("expected *StepError, got %T: %v", err, err)
	}
	// "b" should not have run.
	assertContains(t, s.log, "do:a")
	assertContains(t, s.log, "undo:a")
	for _, entry := range s.log {
		if entry == "do:b" {
			t.Error("step 'b' should not have executed after deadline")
		}
	}
}

func TestContextCheckedBetweenStepsCompensationError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	runner := kata.New(
		kata.Step("a", func(_ context.Context, s *testState) error {
			s.append("do:a")
			cancel()
			return nil
		}).Compensate(func(_ context.Context, _ *testState) error {
			return errors.New("comp a failed")
		}),
		kata.Step("b", func(_ context.Context, s *testState) error {
			s.append("do:b")
			return nil
		}),
	)

	s := &testState{}
	err := runner.Run(ctx, s)

	var compErr *kata.CompensationError
	if !errors.As(err, &compErr) {
		t.Fatalf("expected *CompensationError, got %T: %v", err, err)
	}
	if !errors.Is(compErr.StepCause, context.Canceled) {
		t.Errorf("expected context.Canceled as step cause, got %v", compErr.StepCause)
	}
}

// ── Timeout ───────────────────────────────────────────────────────────────────

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

func TestTimeoutDoesNotAffectFastStep(t *testing.T) {
	runner := kata.New(
		kata.Step("fast", func(_ context.Context, s *testState) error {
			s.append("do:fast")
			return nil
		}).Timeout(5 * time.Second),
	)
	s := &testState{}
	if err := runner.Run(context.Background(), s); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertLog(t, s.log, []string{"do:fast"})
}

// ── Runner reuse ──────────────────────────────────────────────────────────────

func TestRunnerIsReusable(t *testing.T) {
	runner := kata.New(
		kata.Step("a", mkStep("a")).Compensate(mkComp("a")),
	)

	for i := range 5 {
		s := &testState{}
		if err := runner.Run(context.Background(), s); err != nil {
			t.Fatalf("run %d: unexpected error: %v", i, err)
		}
		assertLog(t, s.log, []string{"do:a"})
	}
}

// ── WithOptions ───────────────────────────────────────────────────────────────

func TestWithOptionsDoesNotMutateOriginal(t *testing.T) {
	var hookCalled bool
	original := kata.New(
		kata.Step("a", mkStep("a")),
	)
	_ = original.WithOptions(kata.WithHooks(kata.Hooks{
		OnStepStart: func(_ context.Context, _ string) { hookCalled = true },
	}))

	// Running the original should not trigger the hook.
	s := &testState{}
	_ = original.Run(context.Background(), s)
	if hookCalled {
		t.Error("WithOptions mutated the original runner")
	}
}
