# kata

> *In martial arts, a kata is a precise sequence of movements - executed with full commitment, or not at all. If you break the form, you return to the beginning.*

**kata** is an embedded Go library for orchestrating multi-step operations with automatic compensation on failure. No external services, no databases, no brokers - just import and use.

```go
runner := kata.New(
    kata.Step("charge-card",   chargeCard).Compensate(refundCard).Retry(3, kata.Jitter(kata.Exponential(100*time.Millisecond))),
    kata.Step("reserve-stock", reserveStock).Compensate(releaseStock),
    kata.Step("create-shipment", createShipment),
)

if err := runner.Run(ctx, &OrderState{CardToken: "tok_123", Amount: 9900}); err != nil {
    // all compensations already ran automatically
}
```

If `create-shipment` fails, kata automatically calls `releaseStock` then `refundCard` - in reverse order, with the full state available.

---

## Why kata?

Every non-trivial service has operations that span multiple steps: charge a card, reserve inventory, create a shipment. When step 3 fails, you need to undo steps 1 and 2. Most teams write this rollback logic by hand - scattered `defer` calls, nested `if err != nil` blocks, easy to get wrong.

The alternatives are either too heavy (Temporal, Cadence require a dedicated server cluster) or too primitive (existing Go saga libraries have no generics, no retry, no parallel execution).

kata sits in the middle: **zero dependencies**, idiomatic Go, production-ready features.

---

## Installation

```bash
go get github.com/kerlenton/kata
```

Requires Go 1.22+.

---

## Core concepts

### Steps

A `Step` is a named operation that reads from and writes to your shared state. Each step can optionally define a compensation (rollback) function.

```go
kata.Step("charge-card", func(ctx context.Context, s *OrderState) error {
    id, err := stripe.Charge(s.CardToken, s.Amount)
    if err != nil {
        return err
    }
    s.ChargeID = id // store result for later steps (and compensation)
    return nil
}).Compensate(func(ctx context.Context, s *OrderState) error {
    return stripe.Refund(s.ChargeID)
})
```

### Retry

Steps can be retried with configurable backoff:

```go
kata.Step("call-flaky-api", callAPI).
    Retry(3, kata.Exponential(100*time.Millisecond))
    // attempts: immediate -> 100ms -> 200ms -> 400ms

kata.Step("call-another", callOther).
    Retry(5, kata.Fixed(1*time.Second))

kata.Step("call-fast", callFast).
    Retry(2, kata.NoDelay)
```

Retry policies are composable - wrap them to add jitter or cap the delay:

```go
// Add ±25% random jitter to prevent thundering herd
kata.Jitter(kata.Exponential(100*time.Millisecond))

// Cap maximum delay at 30 seconds
kata.Cap(kata.Exponential(100*time.Millisecond), 30*time.Second)

// Combine both
kata.Cap(kata.Jitter(kata.Exponential(100*time.Millisecond)), 30*time.Second)
```

`Exponential` has a built-in ceiling of 5 minutes to prevent overflow at high retry counts.

### Timeout

```go
kata.Step("slow-step", doWork).
    Timeout(5 * time.Second)
```

If the step exceeds the timeout, the context is cancelled and the step fails with `context.DeadlineExceeded`. Compensations are triggered normally.

### Parallel steps

Run multiple steps concurrently within a group. If any step in the group fails, the others are cancelled and the successful ones are compensated.

```go
kata.Parallel("notify-customer",
    kata.Step("send-email", sendEmail),
    kata.Step("send-sms",   sendSMS).Compensate(cancelSMS),
    kata.Step("send-push",  sendPush),
)
```

If a later sequential step fails after the parallel group succeeds, all steps in the group are compensated in reverse order.

**Thread safety:** all steps in a parallel group share state `T` concurrently. Either assign disjoint fields to each step, or protect shared fields with a `sync.Mutex` in your state struct.

### Runner

`New` creates a reusable runner - define it once, call `Run` per request:

```go
// define once (e.g. at startup or in a constructor)
var orderRunner = kata.New(
    kata.Step("charge",  chargeCard).Compensate(refundCard),
    kata.Step("reserve", reserveStock).Compensate(releaseStock),
    kata.Parallel("notify",
        kata.Step("email", sendEmail),
        kata.Step("sms",   sendSMS),
    ),
)

// call per request
func (s *OrderService) PlaceOrder(ctx context.Context, req *PlaceOrderRequest) error {
    state := &OrderState{CardToken: req.CardToken, ItemID: req.ItemID}
    return orderRunner.Run(ctx, state)
}
```

The runner checks the context between steps - if the context is cancelled (e.g. SIGTERM, request timeout), it stops immediately and compensates all completed steps. Compensation always runs with `context.Background()` to guarantee rollback completes regardless of the caller's context.

---

## Error handling

kata distinguishes between two failure modes:

```go
err := runner.Run(ctx, state)

var stepErr *kata.StepError
var compErr *kata.CompensationError

switch {
case err == nil:
    // all steps succeeded

case errors.As(err, &stepErr):
    // a step failed, all compensations ran successfully
    // stepErr.StepName - which step failed
    // stepErr.Cause   - the original error
    log.Printf("rolled back cleanly after %q: %v", stepErr.StepName, stepErr.Cause)

case errors.As(err, &compErr):
    // a step failed AND one or more compensations also failed
    // the system may be in a partially inconsistent state
    // manual intervention may be required
    log.Printf("ALERT: step %q failed, compensations also failed:", compErr.StepName)
    for _, f := range compErr.Failed {
        log.Printf("  - %q: %v", f.StepName, f.Err)
    }
}
```

Both error types implement `Unwrap()`, so `errors.Is` works against the original cause.

---

## Observability

Attach hooks for logging, metrics, or tracing - no changes to step code required:

```go
runner := kata.New(steps...).WithOptions(
    kata.WithHooks(kata.Hooks{
        OnStepStart: func(ctx context.Context, name string) {
            metrics.Inc("kata.step.started", name)
        },
        OnStepDone: func(ctx context.Context, name string, d time.Duration) {
            metrics.Histogram("kata.step.duration", d, name)
        },
        OnStepFailed: func(ctx context.Context, name string, err error) {
            log.Errorf("step %q failed: %v", name, err)
        },
        OnRetry: func(ctx context.Context, name string, attempt int, err error) {
            log.Warnf("retrying %q (attempt %d): %v", name, attempt, err)
        },
        OnCompensationStart: func(ctx context.Context, name string) {
            log.Warnf("compensating %q", name)
        },
        OnCompensationFailed: func(ctx context.Context, name string, err error) {
            alerts.Fire("compensation_failed", name, err)
        },
    }),
)
```

Available hooks:

| Hook | When |
|---|---|
| `OnStepStart` | Before a step begins |
| `OnStepDone` | After a step succeeds |
| `OnStepFailed` | After a step exhausts all retries and fails |
| `OnRetry` | Before each retry attempt (with attempt number and previous error) |
| `OnCompensationStart` | Before a compensation begins |
| `OnCompensationDone` | After a compensation succeeds |
| `OnCompensationFailed` | After a compensation fails |

---

## Full example

```go
type OrderState struct {
    // inputs
    CardToken string
    ItemID    string
    UserEmail string
    Amount    int64

    // filled in by steps
    ChargeID      string
    ReservationID string
}

var orderRunner = kata.New(
    kata.Step("charge-card", func(ctx context.Context, s *OrderState) error {
        id, err := payments.Charge(ctx, s.CardToken, s.Amount)
        s.ChargeID = id
        return err
    }).Compensate(func(ctx context.Context, s *OrderState) error {
        return payments.Refund(ctx, s.ChargeID)
    }).Retry(3, kata.Jitter(kata.Exponential(100*time.Millisecond))).Timeout(10*time.Second),

    kata.Step("reserve-stock", func(ctx context.Context, s *OrderState) error {
        id, err := warehouse.Reserve(ctx, s.ItemID)
        s.ReservationID = id
        return err
    }).Compensate(func(ctx context.Context, s *OrderState) error {
        return warehouse.Release(ctx, s.ReservationID)
    }),

    kata.Step("create-shipment", func(ctx context.Context, s *OrderState) error {
        return shipping.Create(ctx, s.ReservationID)
    }),

    kata.Parallel("notify",
        kata.Step("email", func(ctx context.Context, s *OrderState) error {
            return mailer.Send(ctx, s.UserEmail, "Your order is confirmed!")
        }),
        kata.Step("analytics", func(ctx context.Context, s *OrderState) error {
            return analytics.Track(ctx, "order_placed", s.ItemID)
        }),
    ),
)

func PlaceOrder(ctx context.Context, req *Request) error {
    state := &OrderState{
        CardToken: req.CardToken,
        ItemID:    req.ItemID,
        UserEmail: req.UserEmail,
        Amount:    req.Amount,
    }

    err := orderRunner.Run(ctx, state)
    if err != nil {
        var compErr *kata.CompensationError
        if errors.As(err, &compErr) {
            // compensation failed - alert on-call
            pagerduty.Fire(compErr)
        }
        return err
    }
    return nil
}
```

---

## Comparison

| | kata | Temporal/Cadence | floxy | go-saga |
|---|---|---|---|---|
| External service required | ✗ | ✓ (server cluster) | ✗ | ✗ |
| Persistent state | plug-in | ✓ | PostgreSQL | ✗ |
| Generics (typed state) | ✓ | ✗ | ✗ | ✗ |
| Parallel steps | ✓ | ✓ | ✓ | ✗ |
| Per-step retry + backoff | ✓ | ✓ | ✓ | ✗ |
| Per-step timeout | ✓ | ✓ | ✗ | ✗ |
| Composable retry policies | ✓ | ✗ | ✗ | ✗ |
| Observability hooks | ✓ | ✓ | ✗ | ✗ |
| Zero dependencies | ✓ | ✗ | ✗ | ✓ |

---

## License

MIT
