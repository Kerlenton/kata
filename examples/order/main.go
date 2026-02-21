package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/kerlenton/kata"
)

// OrderState holds the shared state passed between all steps.
type OrderState struct {
	CardToken   string
	ItemID      string
	UserEmail   string
	UserPhone   string
	Amount      int64
	ChargeID    string
	Reservation string
}

func chargeCard(ctx context.Context, s *OrderState) error {
	slog.Info("charging card", "token", s.CardToken, "amount", s.Amount)
	s.ChargeID = "ch_abc123"
	return nil
}
func refundCard(ctx context.Context, s *OrderState) error {
	slog.Info("refunding charge", "charge_id", s.ChargeID)
	return nil
}
func reserveStock(ctx context.Context, s *OrderState) error {
	slog.Info("reserving stock", "item", s.ItemID)
	s.Reservation = "rsv_xyz789"
	return nil
}
func releaseStock(ctx context.Context, s *OrderState) error {
	slog.Info("releasing reservation", "reservation", s.Reservation)
	return nil
}
func createShipment(ctx context.Context, s *OrderState) error {
	slog.Info("creating shipment")
	return errors.New("shipment provider unavailable") // simulate failure
}
func sendEmail(ctx context.Context, s *OrderState) error {
	slog.Info("sending email", "to", s.UserEmail)
	return nil
}
func sendSMS(ctx context.Context, s *OrderState) error {
	slog.Info("sending SMS", "to", s.UserPhone)
	time.Sleep(10 * time.Millisecond) // simulate slightly slower
	return nil
}
func sendPush(ctx context.Context, s *OrderState) error {
	slog.Info("sending push notification")
	return nil
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))

	hooks := kata.WithHooks(kata.Hooks{
		OnStepStart: func(_ context.Context, name string) {
			slog.Debug("step started", "step", name)
		},
		OnStepDone: func(_ context.Context, name string, d time.Duration) {
			slog.Debug("step done", "step", name, "duration", d)
		},
		OnStepFailed: func(_ context.Context, name string, err error) {
			slog.Error("step failed", "step", name, "err", err)
		},
		OnCompensationStart: func(_ context.Context, name string) {
			slog.Warn("compensating", "step", name)
		},
		OnCompensationDone: func(_ context.Context, name string) {
			slog.Info("compensation done", "step", name)
		},
	})

	runner := kata.New(
		kata.Step("charge-card", chargeCard).
			Compensate(refundCard).
			Retry(3, kata.Exponential(50*time.Millisecond)).
			Timeout(10*time.Second),

		kata.Step("reserve-stock", reserveStock).
			Compensate(releaseStock),

		// This step will fail - triggering rollback of charge + reserve
		kata.Step("create-shipment", createShipment),

		// This group would run concurrently - but won't be reached
		kata.Parallel("notify",
			kata.Step("email", sendEmail),
			kata.Step("sms", sendSMS),
			kata.Step("push", sendPush),
		),
	).WithOptions(hooks)

	state := &OrderState{
		CardToken: "tok_test_123",
		ItemID:    "item_456",
		UserEmail: "user@example.com",
		UserPhone: "+1234567890",
		Amount:    9900,
	}

	fmt.Print("\n=== Scenario 1: shipment fails, rollback triggered ===\n\n")
	runAndReport(runner, state)

	successRunner := kata.New(
		kata.Step("charge-card", chargeCard).Compensate(refundCard),
		kata.Step("reserve-stock", reserveStock).Compensate(releaseStock),
		kata.Parallel("notify",
			kata.Step("email", sendEmail),
			kata.Step("sms", sendSMS),
			kata.Step("push", sendPush),
		),
	).WithOptions(hooks)

	fmt.Print("\n=== Scenario 2: full success with parallel notifications ===\n\n")
	runAndReport(successRunner, &OrderState{
		CardToken: "tok_ok", ItemID: "item_789",
		UserEmail: "user@example.com", UserPhone: "+1234567890",
		Amount: 4900,
	})
}

func runAndReport(runner *kata.Runner[*OrderState], state *OrderState) {
	err := runner.Run(context.Background(), state)
	fmt.Println()
	if err == nil {
		fmt.Println("Order completed successfully")
		return
	}

	var stepErr *kata.StepError
	var compErr *kata.CompensationError

	switch {
	case errors.As(err, &compErr):
		fmt.Printf("ERROR: manual intervention required\n")
		fmt.Printf("  Step %q failed: %v\n", compErr.StepName, compErr.StepCause)
		for _, f := range compErr.Failed {
			fmt.Printf("  Compensation %q also failed: %v\n", f.StepName, f.Err)
		}
	case errors.As(err, &stepErr):
		fmt.Printf("WARN: rolled back cleanly - step %q failed: %v\n", stepErr.StepName, stepErr.Cause)
	}
}
