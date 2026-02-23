# Graceful Task Lifecycle Management for Go

[![Go Reference](https://pkg.go.dev/badge/vawter.tech/stopper/v2.svg)](https://pkg.go.dev/vawter.tech/stopper/v2)
[![codecov](https://codecov.io/gh/bobvawter/go-stopper/graph/badge.svg?token=7XT22QWN4R)](https://codecov.io/gh/bobvawter/go-stopper)

```shell
go get vawter.tech/stopper/v2
```

Package `stopper` provides graceful lifecycle management for long-running
tasks in a Go program. A stopper `Context` extends the standard library
`context.Context` with a two-phase shutdown model — a soft-stop signal
for graceful draining followed by a hard cancel after a configurable
grace period — and includes task-launching APIs similar to
[`WaitGroup`](https://pkg.go.dev/sync#WaitGroup) or
[`ErrGroup`](https://pkg.go.dev/golang.org/x/sync/errgroup).

## Features

* **Two-phase shutdown** – calling `Stop()` closes the `Stopping()`
  channel, giving tasks a window to drain gracefully before the context
  is canceled (`Done()`). Use `Stopping()` in a `select` or call
  `IsStopping()` in a loop to respond to the signal.
* **Generic task adaptors** – the package-level `Go`, `GoN`, `Call`, and
  `Defer` functions accept any signature from `func()` to
  `func(stopper.Context) error`.
* **Nested contexts** – stop signals propagate from parent to child;
  `Len()` and `Wait()` account for the entire tree.
* **Middleware** – composable `Middleware` functions wrap task execution
  for cross-cutting concerns such as concurrency or rate limiting (see
  the [`limit`](https://pkg.go.dev/vawter.tech/stopper/v2/limit)
  sub-package).
* **Panic recovery** – every task launched via `Call` or `Go` is
  wrapped in a default panic handler. If a task panics, the value is
  recovered and wrapped in a `RecoveredError` that captures the
  goroutine stack at the point of the panic. The error is then handled
  exactly like a returned error (passed to the task's `ErrorHandler` for
  `Go`, or returned to the caller for `Call`). Use
  `errors.As(err, &re)` to extract the `RecoveredError` and inspect
  its `Stack` field or call its `String()` method for a human-readable
  trace.
* **Always-on `runtime/trace`** – every stopper `Context` and every
  task automatically creates a `runtime/trace.Task`, so the full
  hierarchy appears in Go execution traces with zero extra code.
  Built-in middleware in `limit` and `retry` annotate blocking waits
  with `trace.StartRegion` for concurrency, rate-limit, and retry
  visibility.
* **Context interop** – works with any library that produces custom
  `context.Context` values via `WithDelegate()` and
  `StoppingContext()`.
* **OS signal integration** – `StopOnReceive` triggers a graceful
  shutdown when a value arrives on any channel, making it easy to wire
  up `os/signal.Notify`.
* **Retry** – the
  [`retry`](https://pkg.go.dev/vawter.tech/stopper/v2/retry)
  sub-package provides `Middleware` for retrying failed tasks. `Backoff`
  offers exponential backoff with jitter, and `Loop` implements a
  simple synchronous retry.
* **Concurrent sequence processing** – the
  [`seq`](https://pkg.go.dev/vawter.tech/stopper/v2/seq)
  sub-package provides bounded-concurrency helpers for `iter.Seq` and
  `iter.Seq2` sequences. `ForEach` / `ForEach2` apply a callback to
  every element; `Map` / `Map2` and `MapUnordered` / `MapUnordered2`
  transform elements into a new `iter.Seq2[R, error]`, preserving or
  relaxing input order respectively.
* **Test helpers** – the
  [`linger`](https://pkg.go.dev/vawter.tech/stopper/v2/linger)
  sub-package detects tasks that fail to exit promptly.

## Quick Start

```go
ctx := stopper.New()

// Launch a background task that responds to the soft-stop signal.
_ = stopper.Go(ctx, func(ctx stopper.Context) {
    for {
        select {
        case <-ctx.Stopping():
            return // begin graceful draining
        case item := <-work:
            process(item)
        }
    }
})

// Graceful shutdown with a one-second grace period.
ctx.Stop(stopper.StopGracePeriod(time.Second))
if err := ctx.Wait(); err != nil {
    log.Fatal(err)
}
```

For simple loops that don't need a `select`, use the boolean shorthand:

```go
_ = stopper.Go(ctx, func(ctx stopper.Context) {
    for !ctx.IsStopping() {
        // Do work.
    }
})
```

## Examples

The package
[examples](https://pkg.go.dev/vawter.tech/stopper/v2#pkg-examples)
demonstrate a variety of patterns:

| Pattern | Example |
|---|---|
| Simplest usage | [`Example`](https://pkg.go.dev/vawter.tech/stopper/v2#example-package) |
| Feature overview | [`Example_features`](https://pkg.go.dev/vawter.tech/stopper/v2#example-package-Features) |
| Accept / poll network server | [`Example_netServer`](https://pkg.go.dev/vawter.tech/stopper/v2#example-package-NetServer) |
| HTTP server with `Call` | [`Example_httpServer`](https://pkg.go.dev/vawter.tech/stopper/v2#example-package-HttpServer) |
| Bounded work pool | [`Example_workPool`](https://pkg.go.dev/vawter.tech/stopper/v2#example-package-WorkPool) |
| Channel-based ticker | [`Example_ticker`](https://pkg.go.dev/vawter.tech/stopper/v2#example-package-Ticker) |
| Deferred cleanups | [`ExampleContext_defer`](https://pkg.go.dev/vawter.tech/stopper/v2#example-Context-Defer) |
| Nested contexts | [`ExampleContext_nesting`](https://pkg.go.dev/vawter.tech/stopper/v2#example-Context-Nesting) |
| Stop on idle | [`ExampleContext_stopOnIdle`](https://pkg.go.dev/vawter.tech/stopper/v2#example-Context-StopOnIdle) |
| Middleware | [`ExampleMiddleware`](https://pkg.go.dev/vawter.tech/stopper/v2#example-Middleware) |
| `runtime/trace` integration | [`ExampleContext_tracing`](https://pkg.go.dev/vawter.tech/stopper/v2#example-Context-Tracing) |
| Retry with exponential backoff | [`ExampleBackoff`](https://pkg.go.dev/vawter.tech/stopper/v2/retry#example-Backoff) |
| Concurrency / rate limiting | [`Example`](https://pkg.go.dev/vawter.tech/stopper/v2/limit#example-package) |
| Detecting lingering tasks | [`ExampleRecorder`](https://pkg.go.dev/vawter.tech/stopper/v2/linger#example-Recorder) |
| Concurrent ForEach | [`ExampleForEach`](https://pkg.go.dev/vawter.tech/stopper/v2/seq#example-ForEach) |
| Concurrent ForEach (key-value) | [`ExampleForEach2`](https://pkg.go.dev/vawter.tech/stopper/v2/seq#example-ForEach2) |
| Ordered concurrent Map | [`ExampleMap`](https://pkg.go.dev/vawter.tech/stopper/v2/seq#example-Map) |
| Ordered concurrent Map (key-value) | [`ExampleMap2`](https://pkg.go.dev/vawter.tech/stopper/v2/seq#example-Map2) |
| Unordered concurrent Map | [`ExampleMapUnordered`](https://pkg.go.dev/vawter.tech/stopper/v2/seq#example-MapUnordered) |
| Unordered concurrent Map (key-value) | [`ExampleMapUnordered2`](https://pkg.go.dev/vawter.tech/stopper/v2/seq#example-MapUnordered2) |
| Short-circuit processing | [`ExampleForEach_shortCircuit`](https://pkg.go.dev/vawter.tech/stopper/v2/seq#example-ForEach-ShortCircuit) |
| Map with parent context | [`ExampleMap_withContext`](https://pkg.go.dev/vawter.tech/stopper/v2/seq#example-Map-WithContext) |

## Project History

Version 1 of this repository was extracted from
`github.com/cockroachdb/field-eng-powertools` using
`git filter-repo --subdirectory-filter stopper --path LICENSE`
by the code's original author.

Version 2 is a complete overhaul of the library's API with a focus on
better composition and separation of concerns within the implementation.