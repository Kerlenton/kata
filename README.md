# kata

[![Go Reference](https://pkg.go.dev/badge/github.com/kerlenton/kata.svg)](https://pkg.go.dev/github.com/kerlenton/kata)
[![CI](https://github.com/kerlenton/kata/actions/workflows/ci.yml/badge.svg)](https://github.com/kerlenton/kata/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/kerlenton/kata/branch/main/graph/badge.svg)](https://codecov.io/gh/kerlenton/kata)

> In martial arts, a kata is a precise sequence of movements: executed fully, or not at all.

**kata** is an in-process orchestration library for Go for multi-step operations with automatic compensation on failure.

It helps you model workflows like:

- charge a card
- reserve stock
- create a shipment
- send notifications
- write audit records

If a later step fails, kata compensates completed steps in reverse order.

No external services. No brokers. No workflow cluster. Just import and run.

```go
runner := kata.New(
    kata.Step("charge-card", chargeCard).
        Compensate(refundCard).
        Retry(3, kata.Jitter(kata.Exponential(100*time.Millisecond))),

    kata.Step("reserve-stock", reserveStock).
        Compensate(releaseStock),

    kata.Step("create-shipment", createShipment),
)

if err := runner.Run(ctx, &OrderState{
    CardToken: "tok_123",
    Amount:    9900,
}); err != nil {
    // completed steps were already compensated automatically
}
````

If `create-shipment` fails, kata compensates completed steps in reverse order:

1. `reserve-stock` → `release-stock`
2. `charge-card` → `refund-card`

---

## Why kata?

A lot of backend workflows are really just:

1. do step A
2. do step B
3. do step C
4. if C fails, undo B and A

Most teams implement this by hand with scattered rollback code, nested `if err != nil`, ad-hoc retries, and unclear failure handling.

kata gives you one place to define that flow explicitly.

It is inspired by compensation-based workflow patterns, but it is **not** a durable distributed workflow engine.

kata sits in the middle between:

* hand-written rollback logic that is easy to get wrong
* heavyweight orchestration systems that need their own infrastructure

---

## Use kata when

Use kata when you need:

* a small to medium in-process workflow
* shared typed state across steps
* automatic compensation on failure
* per-step retry and timeout
* optional parallel execution
* hooks for logs, metrics, or tracing

Typical use cases:

* order placement
* payment + inventory reservation
* user onboarding flows
* local service orchestration inside one Go process
* workflows where rollback is application-level, not database-transaction-level

---

## Don't use kata when

kata is **not** the right tool when you need:

* persisted workflow state
* recovery after process crash
* long-running workflows across hours or days
* cross-process durability guarantees
* a distributed workflow engine

If you need durable execution and recovery, use a different class of tool.

---

## Installation

```bash
go get github.com/kerlenton/kata
```

Requires Go 1.22+.

---

## Quick start

```go
type OrderState struct {
    CardToken     string
    Amount        int64
    ItemID        string
    ChargeID      string
    ReservationID string
}

runner := kata.New(
    kata.Step("charge-card", func(ctx context.Context, s *OrderState) error {
        id, err := payments.Charge(ctx, s.CardToken, s.Amount)
        if err != nil {
            return err
        }
        s.ChargeID = id
        return nil
    }).Compensate(func(ctx context.Context, s *OrderState) error {
        return payments.Refund(ctx, s.ChargeID)
    }),

    kata.Step("reserve-stock", func(ctx context.Context, s *OrderState) error {
        id, err := warehouse.Reserve(ctx, s.ItemID)
        if err != nil {
            return err
        }
        s.ReservationID = id
        return nil
    }).Compensate(func(ctx context.Context, s *OrderState) error {
        return warehouse.Release(ctx, s.ReservationID)
    }),

    kata.Step("create-shipment", func(ctx context.Context, s *OrderState) error {
        return shipping.Create(ctx, s.ReservationID)
    }),
)

state := &OrderState{
    CardToken: "tok_123",
    Amount:    9900,
    ItemID:    "sku_42",
}

if err := runner.Run(ctx, state); err != nil {
    return err
}
```

---

## Core concepts

### Step

A `Step` is a named operation that reads from and writes to shared state.

Each step can optionally define a compensation function.

```go
kata.Step("charge-card", func(ctx context.Context, s *OrderState) error {
    id, err := stripe.Charge(s.CardToken, s.Amount)
    if err != nil {
        return err
    }
    s.ChargeID = id
    return nil
}).Compensate(func(ctx context.Context, s *OrderState) error {
    return stripe.Refund(s.ChargeID)
})
```

A compensation should undo the side effects of the step as much as possible.

### Shared typed state

kata uses a shared state value of type `T` across all steps.

That means each step can:

* read inputs from previous steps
* write outputs for later steps
* use the same state during compensation

This keeps the workflow explicit and type-safe.

---

## Retry

Retries are configured per step.

```go
kata.Step("call-flaky-api", callAPI).
    Retry(3, kata.Exponential(100*time.Millisecond))
// attempts: immediate -> 100ms -> 200ms -> 400ms

kata.Step("call-another", callOther).
    Retry(5, kata.Fixed(1*time.Second))

kata.Step("call-fast", callFast).
    Retry(2, kata.NoDelay)
```

Retry policies are composable:

```go
// Add jitter
kata.Jitter(kata.Exponential(100*time.Millisecond))

// Cap maximum delay
kata.Cap(kata.Exponential(100*time.Millisecond), 30*time.Second)

// Combine both
kata.Cap(kata.Jitter(kata.Exponential(100*time.Millisecond)), 30*time.Second)
```

`Exponential` has a built-in ceiling of 5 minutes to avoid overflow at high retry counts.

---

## Timeout

```go
kata.Step("slow-step", doWork).
    Timeout(5 * time.Second)
```

If a step exceeds its timeout, the step fails with `context.DeadlineExceeded` and compensation runs normally.

---

## Parallel steps

Run multiple steps concurrently inside a group.

```go
kata.Parallel("notify-customer",
    kata.Step("send-email", sendEmail),
    kata.Step("send-sms", sendSMS).Compensate(cancelSMS),
    kata.Step("send-push", sendPush),
)
```

If any step in the group fails:

* the remaining steps are cancelled
* successful steps in the group are compensated

If a later sequential step fails after the parallel group succeeded, the successful steps in the group are also compensated in reverse order.

Parallel groups can be nested:

```go
kata.Parallel("all-notifications",
    kata.Parallel("customer",
        kata.Step("email", sendEmail),
        kata.Step("sms", sendSMS),
    ),
    kata.Parallel("internal",
        kata.Step("slack", notifySlack),
        kata.Step("analytics", trackEvent),
    ),
)
```

**Thread safety:** all steps in a parallel group share state `T` concurrently. Use disjoint fields or protect shared fields with a `sync.Mutex`.

---

## Runner

Create a runner once and execute it per request.

```go
var orderRunner = kata.New(
    kata.Step("charge", chargeCard).Compensate(refundCard),
    kata.Step("reserve", reserveStock).Compensate(releaseStock),
    kata.Parallel("notify",
        kata.Step("email", sendEmail),
        kata.Step("sms", sendSMS),
    ),
)

func (s *OrderService) PlaceOrder(ctx context.Context, req *PlaceOrderRequest) error {
    state := &OrderState{
        CardToken: req.CardToken,
        ItemID:    req.ItemID,
    }
    return orderRunner.Run(ctx, state)
}
```

The runner checks context cancellation between steps. If the context is cancelled, kata stops and compensates completed steps.

Compensation always runs with `context.Background()` so rollback is not interrupted by the caller's cancelled context.

---

## Error handling

kata distinguishes between two failure modes:

1. a step failed, but compensation completed successfully
2. a step failed, and one or more compensations also failed

```go
err := runner.Run(ctx, state)

var stepErr *kata.StepError
var compErr *kata.CompensationError

switch {
case err == nil:
    // workflow completed successfully

case errors.As(err, &stepErr):
    // a step failed, compensation completed successfully
    log.Printf("rolled back after %q: %v", stepErr.StepName, stepErr.Cause)

case errors.As(err, &compErr):
    // a step failed and one or more compensations failed
    log.Printf("ALERT: step %q failed, compensation also failed", compErr.StepName)
    for _, f := range compErr.Failed {
        log.Printf("  - %q: %v", f.StepName, f.Err)
    }
}
```

Both error types implement `Unwrap()`, so `errors.Is` works with the original cause.

---

## Observability

Attach hooks for logging, metrics, or tracing without changing step code.

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

| Hook                   | When                                    |
| ---------------------- | --------------------------------------- |
| `OnStepStart`          | Before a step begins                    |
| `OnStepDone`           | After a step succeeds                   |
| `OnStepFailed`         | After a step exhausts retries and fails |
| `OnRetry`              | Before each retry attempt               |
| `OnCompensationStart`  | Before a compensation begins            |
| `OnCompensationDone`   | After a compensation succeeds           |
| `OnCompensationFailed` | After a compensation fails              |

---

## Full example

```go
type OrderState struct {
    // inputs
    CardToken string
    ItemID    string
    UserEmail string
    Amount    int64

    // filled by steps
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
            pagerduty.Fire(compErr)
        }
        return err
    }
    return nil
}
```

---

## Why not just write this by hand?

You can.

But hand-rolled compensation logic usually turns into:

* duplicated retry behavior
* duplicated timeout behavior
* rollback code spread across multiple branches
* inconsistent compensation order
* unclear observability
* fragile maintenance as workflows evolve

kata keeps that logic in one place.

---

## Comparison

|                           | kata          | Temporal/Cadence   | floxy      | go-saga |
| ------------------------- | ------------- | ------------------ | ---------- | ------- |
| External service required | ✗             | ✓ (server cluster) | ✗          | ✗       |
| Persistent state          | ✗ (in-memory) | ✓                  | PostgreSQL | ✗       |
| Typed shared state        | ✓             | ✗                  | ✗          | ✗       |
| Parallel steps            | ✓             | ✓                  | ✓          | ✗       |
| Nested parallel groups    | ✓             | ✓                  | ✗          | ✗       |
| Per-step retry + backoff  | ✓             | ✓                  | ✓          | ✗       |
| Per-step timeout          | ✓             | ✓                  | ✗          | ✗       |
| Composable retry policies | ✓             | ✗                  | ✗          | ✗       |
| Observability hooks       | ✓             | ✓                  | ✗          | ✗       |
| Zero dependencies         | ✓             | ✗                  | ✗          | ✓       |

---

## Feedback wanted

`kata` is now stable enough to use, and I want real-world feedback.

Please open an issue or discussion if:

* you tried kata on a real workflow
* something in the API felt awkward
* docs were unclear
* you considered using it but decided not to

The most useful feedback is:

* what workflow you wanted to build
* what blocked adoption
* what felt missing or confusing

---

## License

MIT
