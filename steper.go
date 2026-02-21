package kata

import "context"

// steper is the internal interface implemented by both StepDef and ParallelDef.
// Users of the library never interact with this interface directly - they use
// flow.Step() and flow.Parallel() constructors instead.
type steper[T any] interface {
	// stepName returns the name used for logging and error reporting.
	stepName() string

	// execute runs the step (or group) against the given state.
	// If execution partially succeeds and then fails, execute is responsible
	// for compensating the partial work internally before returning an error.
	execute(ctx context.Context, state T, h Hooks) error

	// rollback is called by the Runner when a later step fails.
	// It should undo all work done by this step/group.
	// Returns a slice of failures if any compensations fail.
	rollback(ctx context.Context, state T, h Hooks) []CompensationFailure
}
