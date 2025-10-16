# Graceful Golang Task Lifecycle Management

[![Go Reference](https://pkg.go.dev/badge/vawter.tech/stopper.svg)](https://pkg.go.dev/vawter.tech/stopper)
[![codecov](https://codecov.io/gh/bobvawter/go-stopper/graph/badge.svg?token=7XT22QWN4R)](https://codecov.io/gh/bobvawter/go-stopper)

```shell
go get vawter.tech/stopper
```

This package contains a utility for gracefully terminating long-running tasks
within a Go program. A `stopper.Context` extends the stdlib `context.Context`
API with a soft-stop signal and includes task-launching APIs similar to
[`WaitGroup`](https://pkg.go.dev/sync#WaitGroup) or
[`ErrGroup`](https://pkg.go.dev/golang.org/x/sync/errgroup).

Supported use-cases:
* [X] [`accept / poll` type APIs](https://pkg.go.dev/vawter.tech/stopper@main#example-package-NetServer)
* [X] [Adopting already-spawned goroutines](https://pkg.go.dev/vawter.tech/stopper@main#example-Context.Call-HttpServer)
* [X] [Channel-based notifications](https://pkg.go.dev/vawter.tech/stopper@main#example-Context-Ticker)
* [X] [Deferred cleanups](https://pkg.go.dev/vawter.tech/stopper@main#example-Context.Defer)
* [X] [Detect lingering tasks](https://pkg.go.dev/vawter.tech/stopper@main#example-package-Testing)
* [X] [Nested task groups](https://pkg.go.dev/vawter.tech/stopper@main#example-Context-Nested)
* [X] [`runtime/trace` or other Context-creating modules](https://pkg.go.dev/vawter.tech/stopper#example-Context.With-Tracing)

## API Use

There are a number of
[examples](https://pkg.go.dev/vawter.tech/stopper#pkg-examples) in the package
docs showing a variety of usecase patterns for polling and callback-style network servers.

```go
// Create a stopper context from an existing context.
ctx := stopper.WithContext(context.Background())

// Respond to signals.
ch := make(chan os.Signal, 1)
signal.Notify(ch, os.Interrupt)
stopper.StopOnReceive(ctx, time.Second, ch)

// Do work, often in a loop.
ctx.Go(func(ctx *stopper.Context) error {
  for !ctx.IsStopping() {
  }
  return nil
})

// Plays nicely with channels.
ctx.Go(func(ctx *stopper.Context) error {
  for {
    select {
    case <-ctx.Stopping():
      return nil
    case work := <-sourceOfWork:
      // Launches additional workers.
      ctx.Go(func(ctx *stopper.Context) error {
        return process(ctx, work)
      })
    }
  }
})

subCtx := stopper.WithContext(ctx) // Nested contexts can be created.
subCtx.Stop(time.Second)           // Won't affect the outer context.

// Blocks until all managed goroutines are done.
if err := ctx.Wait(); err != nil {
  panic(err)
}
```

## Tracing
Stopper also interoperates with `runtime/trace` or other modules that create
custom `context.Context` instances by way of the
[`stopper.Context.With()`](https://pkg.go.dev/vawter.tech/stopper#Context.With)
method.

![A view of nested golang trace regions](./docs/trace.png)

## Project History

This repository was extracted from `github.com/cockroachdb/field-eng-powertools` using the command
`git filter-repo --subdirectory-filter stopper --path LICENSE` by the code's original author.
