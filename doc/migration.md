# Migrating from v1 to v2

This document covers the API differences between `v1.2.0` and `v2.0.0`
of the `stopper` package. The module path changed from
`vawter.tech/stopper` to `vawter.tech/stopper/v2`, so both versions
can coexist during a gradual migration.

## Import path

```diff
-import "vawter.tech/stopper"
+import "vawter.tech/stopper/v2"
```

## Context is now an interface

The concrete `*stopper.Context` type has been replaced by a
`stopper.Context` **interface**. All existing method calls continue to
work, but code that stores or passes `*stopper.Context` must drop the
pointer:

```diff
-func work(ctx *stopper.Context) error {
+func work(ctx stopper.Context) error {
```

## Func signature

The `Func` type alias now accepts the interface:

```diff
-type Func = func(*Context) error
+type Func func(Context) error
```

Because `Func` is no longer a type alias, callers that relied on the
interchangeability of `func(*Context) error` and `Func` should use an
explicit conversion or, better, the generic adaptor functions described
below.

## Creating a stopper

`Background()` has been removed. Use `New()` (or `WithContext()`) with
functional options:

```diff
-ctx := stopper.Background()
-ctx := stopper.WithContext(parent)
+ctx := stopper.New()                          // standalone
+ctx := stopper.WithContext(parent)             // from a context.Context
+ctx := stopper.New(stopper.WithName("main"))   // with options
```

`New` and `WithContext` accept `ConfigOption` values:

| Option              | Purpose                                      |
|---------------------|----------------------------------------------|
| `WithGracePeriod`   | Default grace period for `Stop()` calls.     |
| `WithName`          | Assign a debug name (visible in `String()`). |
| `WithNoInherit`     | Ignore parent task configuration.            |
| `WithTaskOptions`   | Attach default `TaskOption` values.          |

## Stopping

`Stop` now takes functional options instead of a bare `time.Duration`:

```diff
-ctx.Stop(5 * time.Second)
+ctx.Stop(stopper.StopGracePeriod(5 * time.Second))
```

`StopOnIdle()` moved from a method on `Context` to a `StopOption`:

```diff
-ctx.StopOnIdle()
+ctx.Stop(stopper.StopOnIdle())
```

Additional `StopOption` values:

| Option             | Purpose                                   |
|--------------------|-------------------------------------------|
| `StopGracePeriod`  | Override the default grace period.        |
| `StopOnIdle`       | Stop automatically when all tasks finish. |
| `StopError`        | Inject an error into the `Wait()` result. |

## Running tasks

### Package-level generic helpers (new)

The package-level `Call`, `Go`, and `Defer` functions accept any
`context.Context` and any `Adaptable` function signature, removing the
need to manually wrap functions with `Fn`:

```go
// v2 — any Adaptable signature, works with plain context.Context too.
_ = stopper.Go(ctx, func() { /* ... */ })
_ = stopper.Go(ctx, func(ctx stopper.Context) error { return nil })
err := stopper.Call(ctx, func(ctx context.Context) error { return nil })
```

These helpers return `ErrNoStopper` when the context is not managed by
a stopper.

### Context.Go

`Go` now returns an `error` instead of a `bool`:

```diff
-if ok := ctx.Go(fn); !ok {
-    // already stopped
-}
+if err := ctx.Go(fn); err != nil {
+    // ErrStopped, or an error from middleware
+}
```

### Context.Call (new)

`Call` executes a `Func` in the **current** goroutine while still
tracking its lifecycle. This is useful inside HTTP handlers or other
callbacks where a goroutine is already allocated.

### Context.Defer

`Defer` now accepts a `Func` (was `func()`) and returns a `bool`
indicating whether the callback was retained:

```diff
-ctx.Defer(func() { cleanup() })
+deferred := ctx.Defer(stopper.Fn(func() { cleanup() }))
```

The package-level `Defer` helper auto-adapts the signature:

```go
deferred, err := stopper.Defer(ctx, func() { cleanup() })
```

### Task options (new)

Both `Go` and `Call` accept per-task `TaskOption` values:

| Option            | Purpose                                          |
|-------------------|--------------------------------------------------|
| `TaskName`        | Debug name for the task.                         |
| `TaskErrHandler`  | Override the default error handler.              |
| `TaskMiddleware`  | Attach middleware to this task only.             |
| `TaskNoInherit`   | Ignore inherited task configuration.             |

## Middleware (replaces Invoker)

The v1 `Invoker` type (`func(fn Func) Func`) and `WithInvoker`
constructor have been replaced by a two-phase `Middleware` /
`Invoker` model:

```diff
-stopper.WithInvoker(ctx, func(fn stopper.Func) stopper.Func {
-    return func(ctx *stopper.Context) error {
-        // wrap
-        return fn(ctx)
-    }
-})
+ctx := stopper.New(
+    stopper.WithTaskOptions(
+        stopper.TaskMiddleware(func(outer stopper.Context) stopper.Invoker {
+            // setup phase (runs in the caller's goroutine)
+            return func(ctx stopper.Context, task stopper.Func) error {
+                // invocation phase
+                return task(ctx)
+            }
+        }),
+    ),
+)
```

Built-in `Invoker` helpers: `InvokerCall` (pass-through),
`InvokerDrop` (silently discard), `InvokerErr` (return an error).

## Error handling (new)

v1 had no configurable error handling — the first error from `Go`
would be returned by `Wait`. v2 introduces the `ErrorHandler` type:

| Handler            | Behavior                                     |
|--------------------|----------------------------------------------|
| `ErrHandlerStop`   | Stop the context on the first error (default). |
| `ErrHandlerRecord` | Record errors without stopping.              |

Attach via `TaskErrHandler` as a `TaskOption`.

## Harden → StoppingContext

`Harden` and `HardenFrom` have been replaced by
`Context.StoppingContext`, which returns a `context.Context` whose
`Done` channel mirrors `Stopping`:

```diff
-hardenedCtx := stopper.Harden(ctx)
-hardenedCtx := stopper.HardenFrom(plainCtx)
+stoppingCtx := ctx.StoppingContext()
```

## Context.With → Context.WithDelegate

The `With` method has been renamed to `WithDelegate` to better
describe its purpose — delegating `context.Context` behavior to
another context (e.g. for `runtime/trace` integration):

```diff
-ctx.With(traceCtx)
+ctx.WithDelegate(traceCtx)
```

## StopOnReceive

`StopOnReceive` now takes a `stopper.Context` (the interface) as its
first argument and accepts `StopOption` values instead of a bare
`time.Duration`:

```diff
-stopper.StopOnReceive(ctx, 5*time.Second, ch)
+stopper.StopOnReceive(ctx, ch, stopper.StopGracePeriod(5*time.Second))
```

## WaitCtx (new)

`WaitCtx` is an interruptible version of `Wait` that accepts a
`context.Context`. If the argument's `Done` channel closes before the
stopper finishes, the argument's `Err` is returned.

## linger package

`Recorder.Invoke` has been replaced by `Recorder.Middleware`, which
conforms to the new `stopper.Middleware` type:

```diff
-stopper.WithInvoker(ctx, recorder.Invoke)
+ctx := stopper.New(
+    stopper.WithTaskOptions(
+        stopper.TaskMiddleware(recorder.Middleware),
+    ),
+)
```

## limit package (new)

The new `limit` sub-package provides ready-made middleware:

| Middleware           | Purpose                                    |
|----------------------|--------------------------------------------|
| `WithMaxConcurrency` | Cap the number of concurrent tasks.        |
| `WithMaxRate`        | Token-bucket rate limiting via `x/time`.   |

## Compatibility module

If a full migration is not practical right away, the `compat` module
re-implements the entire v1 API on top of v2. Existing code continues
to compile and run without source changes — only a `replace` directive
in `go.mod` is needed.

### Setup

Add the `compat` module as a replacement for the v1 module path:

```
require vawter.tech/stopper v1.2.0

replace vawter.tech/stopper v1.2.0 => vawter.tech/stopper/v2/compat v2.x.x
```

For local development against an unpublished checkout, use a
filesystem path instead:

```
replace vawter.tech/stopper v1.2.0 => /path/to/go-stopper/compat
```

Once the directive is in place, all `import "vawter.tech/stopper"`
statements resolve to the compat wrapper, which delegates to v2
internally.

### What stays the same

- All exported types, functions, variables, and type aliases
  (`*Context`, `Func`, `Invoker`, `Adaptable`, `Fn`, `Background`,
  `From`, `WithContext`, `WithInvoker`, `Harden`, `HardenFrom`,
  `IsStopping`, `StopOnReceive`, `ErrStopped`, `ErrGracePeriodExpired`)
  are present with their original signatures.
- `Stop(0)` means "wait indefinitely," matching v1 semantics.
- `Background()` returns a singleton that cannot be stopped.
- `Defer` on a background context panics with an `error` value,
  matching v1.
- The `linger` sub-package is included.

### Known behavioral differences

1. **`From()` pointer identity** — v1's `From` returns the same
   `*Context` pointer that was placed in the context chain. The compat
   wrapper allocates a new `*Context` struct each time, so pointer
   comparisons via `==` may differ. Use `From` only to obtain a
   usable `*Context`, not for identity checks.

2. **`Harden` / `StoppingContext` Err value** — v1's `Harden` returns
   a context whose `Err()` yields plain `ErrStopped`. The compat
   layer delegates to v2's `StoppingContext`, which returns
   `errors.Join(context.Canceled, ErrStopped)`. Code that checks
   `err == ErrStopped` will break; switch to `errors.Is(err, ErrStopped)`,
   which works with both v1 and compat.

3. **`linger` callers offset** — the compat wrapper adds one extra
   stack frame, so the internal `callersOffset` constant is `4`
   instead of `3`. This is transparent to callers of the `linger`
   API.

### When to prefer a full migration

The compat module is intended as a bridge, not a permanent solution.
Consider a full migration when:

- You want access to v2-only features (`Call`, `TaskMiddleware`,
  `StopError`, `limit` package, `WaitCtx`, etc.).
- You need `Context` as an interface (e.g. for mocking in tests).
- You want to eliminate the thin wrapper overhead (one extra closure
  per `Go`/`Call`/`Defer`).

Every symbol in the compat module carries a `Deprecated:` comment
pointing to its v2 replacement, so IDEs will guide the migration
incrementally.

See [compat.md](compat.md) for the full feasibility analysis and
validation report.

## Quick reference

| v1                                  | v2                                                       |
|-------------------------------------|----------------------------------------------------------|
| `*stopper.Context`                  | `stopper.Context` (interface)                            |
| `stopper.Background()`              | `stopper.New()`                                          |
| `stopper.WithContext(ctx)`          | `stopper.WithContext(ctx, ...ConfigOption)`               |
| `ctx.Stop(dur)`                     | `ctx.Stop(stopper.StopGracePeriod(dur))`                 |
| `ctx.StopOnIdle()`                  | `ctx.Stop(stopper.StopOnIdle())`                         |
| `ctx.Go(fn)` → `bool`              | `ctx.Go(fn)` → `error`                                  |
| `ctx.Defer(func())`                | `ctx.Defer(stopper.Fn(func() { ... }))`                  |
| n/a                                 | `ctx.Call(fn, ...TaskOption)`                            |
| `ctx.With(other)`                   | `ctx.WithDelegate(other)`                                |
| `stopper.Harden(ctx)`              | `ctx.StoppingContext()`                                  |
| `stopper.HardenFrom(ctx)`          | `ctx.StoppingContext()`                                  |
| `stopper.WithInvoker(ctx, inv)`    | `stopper.WithTaskOptions(stopper.TaskMiddleware(mw))`    |
| `stopper.StopOnReceive(ctx, d, c)` | `stopper.StopOnReceive(ctx, c, stopper.StopGracePeriod(d))` |
| `recorder.Invoke`                  | `recorder.Middleware`                                    |
| n/a                                 | `stopper.Call(ctx, fn)`                                  |
| n/a                                 | `stopper.Go(ctx, fn)`                                    |
| n/a                                 | `stopper.Defer(ctx, fn)`                                 |
| n/a                                 | `limit.WithMaxConcurrency(n)`                            |
| n/a                                 | `limit.WithMaxRate(r, b)`                                |
| n/a                                 | `ctx.WaitCtx(ctx)`                                      |
