# Contributing to kata

Thank you for your interest in contributing!

## Getting started

```bash
git clone https://github.com/kerlenton/kata
cd kata
go test ./...
```

## Running tests

```bash
# all tests
go test ./...

# with race detector (always run this before submitting a PR)
go test -race ./...

# with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# benchmarks
go test -bench=. -benchmem ./...
```

## Test structure

Tests live alongside source files (standard Go convention). Each module has its own test file:

| File | What it covers |
|---|---|
| `common_test.go` | Shared helpers: `testState`, `mkStep`, `mkComp`, assertions |
| `runner_test.go` | Sequential execution, compensation order, context cancellation |
| `parallel_test.go` | Concurrent groups, partial failure, rollback from later steps |
| `retry_test.go` | Retry logic, `Exponential`, `Fixed`, `Jitter`, `Cap`, overflow |
| `hooks_test.go` | All lifecycle hooks including `OnRetry` |
| `bench_test.go` | Benchmarks for sequential, parallel, compensation, policies |

When adding a new feature, put tests in the file that matches the module. If you're adding a new module (e.g. `circuit.go`), create a corresponding `circuit_test.go`.

## Guidelines

- **Keep zero dependencies.** The core library (`kata` package) must have no external dependencies. Examples may use whatever they need.
- **All public API must be documented.** Every exported type, function, and method needs a godoc comment.
- **Tests for every feature.** New behaviour requires new tests. Bug fixes require a regression test.
- **Benchmarks for hot paths.** If your change affects step execution, retry, or compensation, run `go test -bench=. -benchmem` before and after to check for regressions.
- **Run `go vet` and `golangci-lint` before submitting.** The CI will catch it anyway, but saves time.

## Submitting a PR

1. Fork the repo and create a branch from `main`
2. Make your changes
3. Add or update tests
4. Run `go test -race ./...` - all tests must pass
5. Update `CHANGELOG.md` under `[Unreleased]`
6. Open a pull request with a clear description of what and why

## Reporting bugs

Open an issue with:
- Go version (`go version`)
- A minimal reproducer
- Expected vs actual behaviour
