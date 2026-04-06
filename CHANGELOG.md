# Changelog
All notable changes to this project will be documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.0] - 2026-04-06
### Added
- `kata.Jitter(policy)` - composable wrapper adding ±25% random jitter to any retry policy
- `kata.Cap(policy, max)` - composable wrapper capping delay at a maximum duration
- `OnRetry` hook - called before each retry attempt with step name, attempt number, and previous error
- Runner now checks `ctx.Err()` between steps, stopping early on cancellation (e.g. SIGTERM)
- Overflow protection in `Exponential` - capped at 5 minutes to prevent int64 overflow
- Thread safety documentation for `ParallelDef` / `Parallel` groups
- Benchmarks for sequential, parallel, compensation, and retry policy paths
- Split tests into per-module files: `runner_test.go`, `parallel_test.go`, `retry_test.go`, `hooks_test.go`, `bench_test.go`, `common_test.go`

### Changed
- `kata.Parallel()` now accepts both `Step` and nested `Parallel` groups (previously only `*StepDef`)
- Renamed internal interface `steper` → `stepper` (unexported, no breaking change)
- `withRetry` now accepts an `onRetry` callback parameter (internal, no breaking change)

## [0.1.1] - 2026-02-23
### Fixed
- Compensations now run with `context.Background()` instead of the caller's context,
  ensuring rollback completes even when the outer context is cancelled (e.g. SIGTERM)
- `Parallel` group now correctly reports `OnStepDone` duration instead of always passing `0`
- `Parallel` group now returns `*CompensationError` when internal compensations fail instead of silently ignoring them
- `Parallel` group now uses `errors.Is` instead of `==` when filtering `context.Canceled` from sibling steps,
  preventing wrapped cancellations from leaking into the error message and triggering spurious compensations
- `Parallel` group now returns an error when all steps are interrupted by an externally cancelled context
  instead of silently returning `nil`

## [0.1.0] - 2026-02-21
### Added
- `kata.New(steps...)` - creates a reusable `Runner[T]` with variadic steps
- `kata.Step(name, fn)` - defines a sequential step with builder API
  - `.Compensate(fn)` - rollback function, called automatically on failure
  - `.Retry(n, policy)` - retry with `Exponential`, `Fixed`, or `NoDelay` backoff
  - `.Timeout(d)` - per-step context deadline
- `kata.Parallel(name, steps...)` - runs steps concurrently
  - On partial failure: cancels remaining steps, compensates succeeded ones
  - On outer failure: entire group compensated in reverse order
- `*StepError` - returned when a step fails and all compensations ran successfully
- `*CompensationError` - returned when a step fails and some compensations also fail
- `kata.WithHooks(Hooks)` - observability hooks for metrics, logging, tracing
  - `OnStepStart`, `OnStepDone`, `OnStepFailed`
  - `OnCompensationStart`, `OnCompensationDone`, `OnCompensationFailed`
- `Runner.WithOptions(opts...)` - attach options to an existing runner non-destructively
- Zero external dependencies
- Requires Go 1.22+

[Unreleased]: https://github.com/kerlenton/kata/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/kerlenton/kata/compare/v0.1.1...v1.0.0
[0.1.1]: https://github.com/kerlenton/kata/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/kerlenton/kata/releases/tag/v0.1.0
