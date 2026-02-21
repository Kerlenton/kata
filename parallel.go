package kata

import (
	"context"
	"fmt"
	"strings"
	"sync"
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
// Use flow.Parallel() to create one.
type ParallelDef[T any] struct {
	name  string
	steps []*StepDef[T]
}

// Parallel creates a group of steps that execute concurrently.
//
//	flow.Parallel("notifications",
//	    flow.Step("email", sendEmail),
//	    flow.Step("sms",   sendSMS).Compensate(cancelSMS),
//	    flow.Step("push",  sendPush),
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

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		idx int
		err error
	}

	results := make([]result, len(p.steps))
	var mu sync.Mutex
	completed := make([]bool, len(p.steps)) // tracks which steps succeeded

	var wg sync.WaitGroup
	wg.Add(len(p.steps))

	for i, step := range p.steps {
		i, step := i, step
		go func() {
			defer wg.Done()
			err := step.execute(ctx, state, h)
			mu.Lock()
			results[i] = result{idx: i, err: err}
			if err == nil {
				completed[i] = true
			} else {
				cancel() // signal other steps to stop
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	// Collect failures
	var errs []string
	for _, r := range results {
		if r.err != nil && r.err != context.Canceled {
			errs = append(errs, fmt.Sprintf("%s: %v", p.steps[r.idx].name, r.err))
		}
	}

	if len(errs) == 0 {
		if h.OnStepDone != nil {
			h.OnStepDone(ctx, p.name, 0)
		}
		return nil
	}

	// Some steps failed - compensate those that succeeded, in reverse order
	for i := len(p.steps) - 1; i >= 0; i-- {
		if !completed[i] {
			continue
		}
		// Use background context - outer ctx may be cancelled
		p.steps[i].rollback(context.Background(), state, h)
	}

	err := fmt.Errorf("parallel group %q failed: %s", p.name, strings.Join(errs, "; "))
	if h.OnStepFailed != nil {
		h.OnStepFailed(ctx, p.name, err)
	}
	return err
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
