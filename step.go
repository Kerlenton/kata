package kata

import (
	"context"
	"time"
)

// StepFunc is the function signature for a step.
type StepFunc[T any] func(ctx context.Context, state T) error

// StepDef defines a single sequential step with its configuration.
// Create one with kata.Step(), then chain builder methods to configure it.
type StepDef[T any] struct {
	name        string
	fn          StepFunc[T]
	compensate  StepFunc[T]
	retryCount  int
	retryPolicy RetryPolicy
	timeout     time.Duration
}

// Step creates a new step definition with the given name and function.
//
//	kata.Step("charge-card", chargeCard).
//	    Compensate(refundCard).
//	    Retry(3, kata.Exponential(100*time.Millisecond)).
//	    Timeout(10*time.Second)
func Step[T any](name string, fn StepFunc[T]) *StepDef[T] {
	return &StepDef[T]{
		name: name,
		fn:   fn,
	}
}

// Compensate sets the rollback function for this step.
// It is called automatically (in reverse order) if a later step fails.
func (s *StepDef[T]) Compensate(fn StepFunc[T]) *StepDef[T] {
	s.compensate = fn
	return s
}

// Retry sets the number of retry attempts and backoff policy.
// The step will be attempted up to 1+attempts times total.
func (s *StepDef[T]) Retry(attempts int, policy RetryPolicy) *StepDef[T] {
	s.retryCount = attempts
	s.retryPolicy = policy
	return s
}

// Timeout sets the maximum duration allowed for this step.
func (s *StepDef[T]) Timeout(d time.Duration) *StepDef[T] {
	s.timeout = d
	return s
}

// --- stepper interface ---

func (s *StepDef[T]) stepName() string { return s.name }

func (s *StepDef[T]) execute(ctx context.Context, state T, h Hooks) error {
	if h.OnStepStart != nil {
		h.OnStepStart(ctx, s.name)
	}

	start := time.Now()

	var onRetry func(attempt int, err error)
	if h.OnRetry != nil {
		onRetry = func(attempt int, err error) {
			h.OnRetry(ctx, s.name, attempt, err)
		}
	}

	err := withRetry(ctx, s.retryCount, s.retryPolicy, onRetry, func(ctx context.Context) error {
		return withTimeout(ctx, s.timeout, func(ctx context.Context) error {
			return s.fn(ctx, state)
		})
	})

	if err != nil {
		if h.OnStepFailed != nil {
			h.OnStepFailed(ctx, s.name, err)
		}
		return err
	}

	if h.OnStepDone != nil {
		h.OnStepDone(ctx, s.name, time.Since(start))
	}
	return nil
}

func (s *StepDef[T]) rollback(ctx context.Context, state T, h Hooks) []CompensationFailure {
	if s.compensate == nil {
		return nil
	}
	if h.OnCompensationStart != nil {
		h.OnCompensationStart(ctx, s.name)
	}
	if err := s.compensate(ctx, state); err != nil {
		if h.OnCompensationFailed != nil {
			h.OnCompensationFailed(ctx, s.name, err)
		}
		return []CompensationFailure{{StepName: s.name, Err: err}}
	}
	if h.OnCompensationDone != nil {
		h.OnCompensationDone(ctx, s.name)
	}
	return nil
}
