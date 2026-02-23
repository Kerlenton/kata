package kata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ParallelDef runs a group of steps concurrently.
//
// Behaviour:
//   - All steps start at the same time and share the same state.
//   - If any step fails, the group cancels all remaining steps and
//     compensates the ones that already succeeded (in reverse order).
//   - If the whole group succeeds and a later sequential step fails,
//     all steps in the group are compensated in reverse order.
//
// Use kata.Parallel() to create one.
type ParallelDef[T any] struct {
	name  string
	steps []*StepDef[T]
}

// Parallel creates a group of steps that execute concurrently.
//
//	kata.Parallel("notifications",
//	    kata.Step("email", sendEmail),
//	    kata.Step("sms",   sendSMS).Compensate(cancelSMS),
//	    kata.Step("push",  sendPush),
//	)
func Parallel[T any](name string, steps ...*StepDef[T]) *ParallelDef[T] {
	return &ParallelDef[T]{name: name, steps: steps}
}

func (p *ParallelDef[T]) stepName() string { return p.name }

// execute runs all steps concurrently. If any fail:
//  1. Cancels remaining steps via context.
//  2. Compensates the ones that completed successfully.
//  3. Returns an error describing all failures.
func (p *ParallelDef[T]) execute(ctx context.Context, state T, h Hooks) error {
	if h.OnStepStart != nil {
		h.OnStepStart(ctx, p.name)
	}

	start := time.Now()

	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		idx int
		err error
	}

	results := make([]result, len(p.steps))
	var mu sync.Mutex
	completed := make([]bool, len(p.steps))

	var wg sync.WaitGroup
	wg.Add(len(p.steps))

	for i, step := range p.steps {
		i, step := i, step
		go func() {
			defer wg.Done()
			err := step.execute(execCtx, state, h)
			mu.Lock()
			results[i] = result{idx: i, err: err}
			if err == nil {
				completed[i] = true
			} else {
				cancel()
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	// Collect step failures.
	// context.Canceled is filtered out when it is sibling-induced: one step fails,
	// the group calls cancel(), and surviving siblings return context.Canceled as a
	// side-effect — that is noise, not an additional failure.
	//
	// However, if the *outer* ctx was cancelled by an external caller (SIGTERM,
	// request timeout, etc.) before any step had a real domain error, every step
	// will return a wrapped context.Canceled. filteredCanceled tracks whether any
	// such error was silenced so we can detect this case below.
	var errs []string
	var filteredCanceled bool
	for _, r := range results {
		if r.err == nil {
			continue
		}
		if errors.Is(r.err, context.Canceled) {
			filteredCanceled = true
		} else {
			errs = append(errs, fmt.Sprintf("%s: %v", p.steps[r.idx].name, r.err))
		}
	}

	if len(errs) == 0 {
		// All errors (if any) were context.Canceled. Two sub-cases:
		//   a) No real failure — every step succeeded. Happy path.
		//   b) External ctx was cancelled — every step was interrupted from outside.
		//      Distinguish via ctx.Err(): if the *outer* context is done and we did
		//      filter at least one cancellation, the group was externally aborted.
		if filteredCanceled && ctx.Err() != nil {
			extErr := fmt.Errorf("parallel group %q aborted: %w", p.name, ctx.Err())
			if h.OnStepFailed != nil {
				h.OnStepFailed(ctx, p.name, extErr)
			}
			return extErr
		}
		if h.OnStepDone != nil {
			h.OnStepDone(ctx, p.name, time.Since(start))
		}
		return nil
	}

	// Some steps failed - compensate those that succeeded, in reverse order.
	// Use context.Background() so compensation is not affected by cancellation.
	var compFailures []CompensationFailure
	for i := len(p.steps) - 1; i >= 0; i-- {
		if !completed[i] {
			continue
		}
		compFailures = append(compFailures, p.steps[i].rollback(context.Background(), state, h)...)
	}

	stepCause := fmt.Errorf("parallel group %q failed: %s", p.name, strings.Join(errs, "; "))

	if h.OnStepFailed != nil {
		h.OnStepFailed(ctx, p.name, stepCause)
	}

	if len(compFailures) > 0 {
		return &CompensationError{
			StepName:  p.name,
			StepCause: stepCause,
			Failed:    compFailures,
		}
	}

	return stepCause
}

// rollback is called by the Runner when a later sequential step fails.
// Since the group already succeeded, all steps ran - compensate in reverse.
func (p *ParallelDef[T]) rollback(ctx context.Context, state T, h Hooks) []CompensationFailure {
	var failures []CompensationFailure
	for i := len(p.steps) - 1; i >= 0; i-- {
		failures = append(failures, p.steps[i].rollback(ctx, state, h)...)
	}
	return failures
}
