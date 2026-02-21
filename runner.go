package kata

import (
	"context"
)

// Runner orchestrates a sequence of steps with automatic compensation on failure.
// It is safe to reuse a Runner across multiple Run calls (e.g. per-request).
type Runner[T any] struct {
	steps  []steper[T]
	config runnerConfig
}

// New creates a reusable Runner from a sequence of steps and parallel groups.
//
// Steps execute in order. On failure, completed steps are compensated in reverse.
//
//	runner := flow.New(
//	    flow.Step("charge", chargeCard).Compensate(refundCard).Retry(3, flow.Exponential(100*time.Millisecond)),
//	    flow.Step("reserve", reserveStock).Compensate(releaseStock),
//	    flow.Parallel("notify",
//	        flow.Step("email", sendEmail),
//	        flow.Step("sms",   sendSMS),
//	    ),
//	)
func New[T any](steps ...steper[T]) *Runner[T] {
	return &Runner[T]{steps: steps}
}

// WithOptions returns a new Runner with the given options applied.
// Useful when you want to add hooks without changing the step definitions.
//
//	runner := flow.New(step1, step2).WithOptions(flow.WithHooks(myHooks))
func (r *Runner[T]) WithOptions(opts ...RunnerOption) *Runner[T] {
	cfg := r.config
	for _, o := range opts {
		o(&cfg)
	}
	return &Runner[T]{steps: r.steps, config: cfg}
}

// Run executes all steps in order against the given state.
//
// Returns:
//   - nil on success
//   - *StepError if a step failed and all compensations ran successfully
//   - *CompensationError if a step failed AND some compensations also failed
func (r *Runner[T]) Run(ctx context.Context, state T) error {
	h := r.config.hooks
	completed := make([]steper[T], 0, len(r.steps))

	for _, step := range r.steps {
		if err := step.execute(ctx, state, h); err != nil {
			compFailures := r.compensate(ctx, completed, state, h)
			if len(compFailures) > 0 {
				return &CompensationError{
					StepName:  step.stepName(),
					StepCause: err,
					Failed:    compFailures,
				}
			}
			return &StepError{
				StepName: step.stepName(),
				Cause:    err,
			}
		}
		completed = append(completed, step)
	}
	return nil
}

// compensate runs rollback for all completed steps in reverse order.
func (r *Runner[T]) compensate(ctx context.Context, completed []steper[T], state T, h Hooks) []CompensationFailure {
	var failures []CompensationFailure
	for i := len(completed) - 1; i >= 0; i-- {
		failures = append(failures, completed[i].rollback(ctx, state, h)...)
	}
	return failures
}
