# Changelog
All notable changes to this project will be documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.1] - 2026-02-23
### Fixed
- Compensations now run with `context.Background()` instead of the caller's context,
  ensuring rollback completes even when the outer context is cancelled (e.g. SIGTERM)
- `Parallel` group now correctly reports `OnStepDone` duration instead of always passing `0`
- `Parallel` group now returns `*CompensationError` when internal compensations fail instead of silently ignoring them

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

[Unreleased]: https://github.com/kerlenton/kata/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/kerlenton/kata/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/kerlenton/kata/releases/tag/v0.1.0
