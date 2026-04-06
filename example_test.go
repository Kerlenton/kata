package kata_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/kerlenton/kata"
)

func ExampleStep() {
	type state struct{ Charged bool }

	runner := kata.New(
		kata.Step("charge", func(_ context.Context, s *state) error {
			s.Charged = true
			return nil
		}).Compensate(func(_ context.Context, s *state) error {
			s.Charged = false
			return nil
		}),
	)

	s := &state{}
	err := runner.Run(context.Background(), s)
	fmt.Println("err:", err)
	fmt.Println("charged:", s.Charged)
	// Output:
	// err: <nil>
	// charged: true
}

func ExampleRunner_Run() {
	type state struct {
		Log []string
	}

	runner := kata.New(
		kata.Step("step-1", func(_ context.Context, s *state) error {
			s.Log = append(s.Log, "step-1: done")
			return nil
		}).Compensate(func(_ context.Context, s *state) error {
			s.Log = append(s.Log, "step-1: compensated")
			return nil
		}),
		kata.Step("step-2", func(_ context.Context, s *state) error {
			return fmt.Errorf("boom")
		}),
	)

	s := &state{}
	err := runner.Run(context.Background(), s)

	var stepErr *kata.StepError
	if errors.As(err, &stepErr) {
		fmt.Println("failed step:", stepErr.StepName)
	}
	for _, entry := range s.Log {
		fmt.Println(entry)
	}
	// Output:
	// failed step: step-2
	// step-1: done
	// step-1: compensated
}

func ExampleParallel() {
	type state struct {
		mu  sync.Mutex
		Log []string
	}

	runner := kata.New(
		kata.Parallel("notify",
			kata.Step("email", func(_ context.Context, s *state) error {
				s.mu.Lock()
				s.Log = append(s.Log, "email sent")
				s.mu.Unlock()
				return nil
			}),
			kata.Step("sms", func(_ context.Context, s *state) error {
				s.mu.Lock()
				s.Log = append(s.Log, "sms sent")
				s.mu.Unlock()
				return nil
			}),
		),
	)

	s := &state{}
	err := runner.Run(context.Background(), s)
	fmt.Println("err:", err)

	sort.Strings(s.Log)
	for _, entry := range s.Log {
		fmt.Println(entry)
	}
	// Output:
	// err: <nil>
	// email sent
	// sms sent
}

func ExampleStep_retry() {
	type state struct{}

	var attempts atomic.Int32

	runner := kata.New(
		kata.Step("flaky", func(_ context.Context, _ *state) error {
			n := attempts.Add(1)
			if n < 3 {
				return fmt.Errorf("transient error")
			}
			return nil
		}).Retry(3, kata.NoDelay),
	)

	err := runner.Run(context.Background(), &state{})
	fmt.Println("err:", err)
	fmt.Println("attempts:", attempts.Load())
	// Output:
	// err: <nil>
	// attempts: 3
}

func ExampleCompensationError() {
	type state struct{}

	runner := kata.New(
		kata.Step("step-1", func(_ context.Context, _ *state) error {
			return nil
		}).Compensate(func(_ context.Context, _ *state) error {
			return fmt.Errorf("compensate failed")
		}),
		kata.Step("step-2", func(_ context.Context, _ *state) error {
			return fmt.Errorf("boom")
		}),
	)

	err := runner.Run(context.Background(), &state{})

	var compErr *kata.CompensationError
	if errors.As(err, &compErr) {
		fmt.Println("trigger:", compErr.StepName)
		for _, f := range compErr.Failed {
			fmt.Printf("compensation %q failed: %v\n", f.StepName, f.Err)
		}
	}
	// Output:
	// trigger: step-2
	// compensation "step-1" failed: compensate failed
}
