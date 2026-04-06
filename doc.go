// Package kata provides an embedded saga orchestrator for Go.
//
// kata executes a sequence of named steps against shared state, and
// automatically compensates (rolls back) completed steps in reverse order
// when any step fails. No external services or databases required.
//
// # Basic usage
//
//	type OrderState struct {
//	    CardToken string
//	    ChargeID  string
//	}
//
//	runner := kata.New(
//	    kata.Step("charge", chargeCard).
//	        Compensate(refundCard).
//	        Retry(3, kata.Jitter(kata.Exponential(100*time.Millisecond))),
//	    kata.Step("reserve", reserveStock).
//	        Compensate(releaseStock),
//	    kata.Step("ship", createShipment),
//	)
//
//	if err := runner.Run(ctx, &OrderState{CardToken: "tok_123"}); err != nil {
//	    var stepErr *kata.StepError
//	    var compErr *kata.CompensationError
//	    switch {
//	    case errors.As(err, &compErr):
//	        // step failed AND some compensations failed - needs manual fix
//	    case errors.As(err, &stepErr):
//	        // step failed, all compensations ran cleanly
//	    }
//	}
//
// # Retry policies
//
// Retry policies are composable via wrappers:
//
//	kata.Exponential(100*time.Millisecond)                              // 100ms, 200ms, 400ms, ...
//	kata.Jitter(kata.Exponential(100*time.Millisecond))                 // same with ±25% randomness
//	kata.Cap(kata.Exponential(100*time.Millisecond), 30*time.Second)    // capped at 30s
//	kata.Cap(kata.Jitter(kata.Exponential(100*time.Millisecond)), 30*time.Second)  // both
//
// # Parallel steps
//
// Use [Parallel] to run independent steps concurrently:
//
//	kata.Parallel("notify",
//	    kata.Step("email", sendEmail),
//	    kata.Step("sms",   sendSMS).Compensate(cancelSMS),
//	)
//
// All steps in a parallel group share state T concurrently.
// See [ParallelDef] for thread safety guidance.
//
// # Observability
//
// Attach hooks for logging and metrics without changing step code:
//
//	runner.WithOptions(kata.WithHooks(kata.Hooks{
//	    OnStepStart:  func(ctx context.Context, name string) { ... },
//	    OnStepFailed: func(ctx context.Context, name string, err error) { ... },
//	    OnRetry:      func(ctx context.Context, name string, attempt int, err error) { ... },
//	}))
package kata
