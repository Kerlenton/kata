package kata

import (
	"context"
	"time"
)

// Hooks provides lifecycle callbacks for observability (logging, metrics, tracing).
type Hooks struct {
	// OnStepStart is called before each step begins executing.
	OnStepStart func(ctx context.Context, name string)

	// OnStepDone is called after a step completes successfully.
	OnStepDone func(ctx context.Context, name string, duration time.Duration)

	// OnStepFailed is called when a step fails (after all retries are exhausted).
	OnStepFailed func(ctx context.Context, name string, err error)

	// OnCompensationStart is called before a compensation function begins.
	OnCompensationStart func(ctx context.Context, name string)

	// OnCompensationDone is called after a compensation completes successfully.
	OnCompensationDone func(ctx context.Context, name string)

	// OnCompensationFailed is called when a compensation function fails.
	OnCompensationFailed func(ctx context.Context, name string, err error)
}

// WithHooks is an option to set observability hooks on a Runner.
func WithHooks(h Hooks) RunnerOption {
	return func(cfg *runnerConfig) {
		cfg.hooks = h
	}
}

// RunnerOption is a functional option for configuring a Runner.
type RunnerOption func(*runnerConfig)

type runnerConfig struct {
	hooks Hooks
}
