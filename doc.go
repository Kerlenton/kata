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
//	        Retry(3, kata.Exponential(100*time.Millisecond)),
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
// # Parallel steps
//
// Use [Parallel] to run independent steps concurrently:
//
//	kata.Parallel("notify",
//	    kata.Step("email", sendEmail),
//	    kata.Step("sms",   sendSMS).Compensate(cancelSMS),
//	)
//
// # Observability
//
// Attach hooks for logging and metrics without changing step code:
//
//	runner.WithOptions(kata.WithHooks(kata.Hooks{
//	    OnStepStart:  func(ctx context.Context, name string) { ... },
//	    OnStepFailed: func(ctx context.Context, name string, err error) { ... },
//	}))
package kata
