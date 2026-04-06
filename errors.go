package kata

import "fmt"

// StepError is returned when a step fails and all compensations ran successfully.
// This means the saga was rolled back cleanly.
type StepError struct {
	// StepName is the name of the step that failed.
	StepName string
	// Cause is the original error returned by the step function.
	Cause error
}

func (e *StepError) Error() string {
	return fmt.Sprintf("step %q failed (rolled back cleanly): %v", e.StepName, e.Cause)
}

func (e *StepError) Unwrap() error {
	return e.Cause
}

// CompensationError is returned when a step fails AND one or more compensations
// also fail. The saga is in a partially inconsistent state - requires manual intervention.
type CompensationError struct {
	// StepName is the step that originally failed and triggered compensation.
	StepName string
	// StepCause is the original error that triggered compensation.
	StepCause error
	// Failed contains the names and errors of compensations that failed.
	Failed []CompensationFailure
}

func (e *CompensationError) Error() string {
	return fmt.Sprintf(
		"step %q failed and %d compensation(s) also failed - manual intervention required: %v",
		e.StepName, len(e.Failed), e.StepCause,
	)
}

func (e *CompensationError) Unwrap() error {
	return e.StepCause
}

// CompensationFailure holds the name and error for a single failed compensation.
type CompensationFailure struct {
	// StepName is the name of the step whose compensation failed.
	StepName string
	// Err is the error returned by the compensation function.
	Err error
}
