package kata_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kerlenton/kata"
)

var errBench = errors.New("bench error")

func BenchmarkRunnerSequential3Steps(b *testing.B) {
	noop := func(_ context.Context, _ *testState) error { return nil }
	runner := kata.New(
		kata.Step("a", noop).Compensate(noop),
		kata.Step("b", noop).Compensate(noop),
		kata.Step("c", noop),
	)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = runner.Run(context.Background(), &testState{})
	}
}

func BenchmarkRunnerSequential10Steps(b *testing.B) {
	noop := func(_ context.Context, _ *testState) error { return nil }
	s := kata.Step("s", noop).Compensate(noop)
	runner := kata.New(s, s, s, s, s, s, s, s, s, s)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = runner.Run(context.Background(), &testState{})
	}
}

func BenchmarkRunnerParallel3Steps(b *testing.B) {
	noop := func(_ context.Context, _ *testState) error { return nil }
	runner := kata.New(
		kata.Parallel("group",
			kata.Step("a", noop),
			kata.Step("b", noop),
			kata.Step("c", noop),
		),
	)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = runner.Run(context.Background(), &testState{})
	}
}

func BenchmarkRunnerWithHooks(b *testing.B) {
	noop := func(_ context.Context, _ *testState) error { return nil }
	runner := kata.New(
		kata.Step("a", noop),
		kata.Step("b", noop),
		kata.Step("c", noop),
	).WithOptions(kata.WithHooks(kata.Hooks{
		OnStepStart: func(_ context.Context, _ string) {},
		OnStepDone:  func(_ context.Context, _ string, _ time.Duration) {},
	}))

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = runner.Run(context.Background(), &testState{})
	}
}

func BenchmarkRunnerCompensation(b *testing.B) {
	noop := func(_ context.Context, _ *testState) error { return nil }
	fail := func(_ context.Context, _ *testState) error { return errBench }
	runner := kata.New(
		kata.Step("a", noop).Compensate(noop),
		kata.Step("b", noop).Compensate(noop),
		kata.Step("c", noop).Compensate(noop),
		kata.Step("d", fail),
	)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = runner.Run(context.Background(), &testState{})
	}
}

func BenchmarkExponentialPolicy(b *testing.B) {
	policy := kata.Exponential(100 * time.Millisecond)
	b.ResetTimer()
	for range b.N {
		_ = policy(10)
	}
}

func BenchmarkJitterPolicy(b *testing.B) {
	policy := kata.Jitter(kata.Exponential(100 * time.Millisecond))
	b.ResetTimer()
	for range b.N {
		_ = policy(5)
	}
}
