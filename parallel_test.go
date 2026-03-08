package kata_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kerlenton/kata"
)

// ── All succeed ───────────────────────────────────────────────────────────────

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

// ── Concurrency ───────────────────────────────────────────────────────────────

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
	elapsed := time.Since(start)
	// If sequential, ~150ms. Parallel should be ~50ms.
	if elapsed > 120*time.Millisecond {
		t.Errorf("took %v, expected <120ms for concurrent execution", elapsed)
	}
}

// ── Partial failure ───────────────────────────────────────────────────────────

func TestParallelPartialFailure(t *testing.T) {
	var compensated atomic.Bool

	runner := kata.New(
		kata.Parallel("group",
			kata.Step("ok", func(_ context.Context, _ *testState) error {
				return nil
			}).Compensate(func(_ context.Context, _ *testState) error {
				compensated.Store(true)
				return nil
			}),
			kata.Step("fail", func(_ context.Context, _ *testState) error {
				return errors.New("boom")
			}),
		),
	)
	err := runner.Run(context.Background(), &testState{})
	if err == nil {
		t.Fatal("expected error")
	}
	// The successful step should be compensated.
	if !compensated.Load() {
		t.Error("successful step in parallel group was not compensated")
	}
}

func TestParallelCompensationFailureReturnsCompensationError(t *testing.T) {
	runner := kata.New(
		kata.Parallel("group",
			kata.Step("ok", func(_ context.Context, _ *testState) error {
				return nil
			}).Compensate(func(_ context.Context, _ *testState) error {
				return errors.New("comp failed")
			}),
			kata.Step("fail", func(_ context.Context, _ *testState) error {
				return errors.New("step failed")
			}),
		),
	)
	err := runner.Run(context.Background(), &testState{})

	var compErr *kata.CompensationError
	if !errors.As(err, &compErr) {
		t.Fatalf("expected *CompensationError, got %T: %v", err, err)
	}
	if len(compErr.Failed) != 1 || compErr.Failed[0].StepName != "ok" {
		t.Errorf("unexpected compensation failures: %+v", compErr.Failed)
	}
}

// ── Rollback from later sequential step ───────────────────────────────────────

func TestParallelRollbackFromLaterStep(t *testing.T) {
	var compA, compB atomic.Bool

	runner := kata.New(
		kata.Parallel("group",
			kata.Step("a", func(_ context.Context, _ *testState) error {
				return nil
			}).Compensate(func(_ context.Context, _ *testState) error {
				compA.Store(true)
				return nil
			}),
			kata.Step("b", func(_ context.Context, _ *testState) error {
				return nil
			}).Compensate(func(_ context.Context, _ *testState) error {
				compB.Store(true)
				return nil
			}),
		),
		kata.Step("after", func(_ context.Context, _ *testState) error {
			return errors.New("after failed")
		}),
	)
	err := runner.Run(context.Background(), &testState{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !compA.Load() || !compB.Load() {
		t.Error("parallel group steps not compensated after later step failure")
	}
}

// ── External context cancellation ─────────────────────────────────────────────

func TestParallelExternalCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	runner := kata.New(
		kata.Parallel("group",
			kata.Step("a", func(ctx context.Context, _ *testState) error {
				cancel()
				<-ctx.Done()
				return ctx.Err()
			}),
			kata.Step("b", func(ctx context.Context, _ *testState) error {
				<-ctx.Done()
				return ctx.Err()
			}),
		),
	)
	err := runner.Run(ctx, &testState{})
	if err == nil {
		t.Fatal("expected error from externally cancelled context")
	}
}

// ── Single step in parallel group ─────────────────────────────────────────────

func TestParallelSingleStep(t *testing.T) {
	runner := kata.New(
		kata.Parallel("solo",
			kata.Step("a", func(_ context.Context, s *testState) error {
				s.append("do:a")
				return nil
			}),
		),
	)
	s := &testState{}
	if err := runner.Run(context.Background(), s); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertLog(t, s.log, []string{"do:a"})
}

// ── All steps in parallel group fail ──────────────────────────────────────────

func TestParallelAllFail(t *testing.T) {
	runner := kata.New(
		kata.Parallel("group",
			kata.Step("a", func(_ context.Context, _ *testState) error {
				return errors.New("a failed")
			}),
			kata.Step("b", func(_ context.Context, _ *testState) error {
				return errors.New("b failed")
			}),
		),
	)
	err := runner.Run(context.Background(), &testState{})
	if err == nil {
		t.Fatal("expected error when all parallel steps fail")
	}
}
