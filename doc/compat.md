# Feasibility: `vawter.tech/stopper/v2/compat` module

This document evaluates whether a compatibility module can re-implement
the original `vawter.tech/stopper` (v1) API on top of the v2 code, and
whether consumers can adopt it via a `replace` directive in `go.mod`.

## Goal

The compat module must be a **drop-in replacement** for the original
`vawter.tech/stopper` (v1) module. Existing v1 consumers should be able
to switch to the compat module with no source-code changes — only a
`replace` directive in `go.mod`:

```go
// consumer go.mod
require vawter.tech/stopper v1.2.0

replace vawter.tech/stopper v1.2.0 => vawter.tech/stopper/v2/compat v2.x.x
```

The compat module would live inside this repository (e.g.
`compat/` directory) with its own `go.mod` declaring
`module vawter.tech/stopper` and importing `vawter.tech/stopper/v2`.

The original v1 test cases and examples should, at a minimum, be
included in the compat module's test suite. Because the compat layer
wraps v2 internals, some observable behaviors will differ in minor ways
(e.g. pointer comparisons against `*Context` values that are now wrapper
structs rather than the original concrete type). In such cases the
original tests may be amended, but every amendment **must** include an
explicit comment explaining the v1-to-v2 behavior change and why the
original assertion no longer holds.

Any v1 test code that accesses non-exported symbols (unexported fields,
functions, or methods) may be removed entirely. Only behavior reachable
through the package's exported API needs to be preserved in the compat
module.

## v1 → v2 API mapping

Every v1 export maps to a v2 equivalent. The table below lists each v1
symbol alongside its compat implementation strategy.

| v1 symbol | Compat implementation |
|---|---|
| `*stopper.Context` (concrete) | Wrapper struct holding a `v2.Context` |
| `type Func = func(*Context) error` | `type Func = func(*Context) error`; adapt at the `Go`/`Call`/`Defer` boundary |
| `type Invoker func(fn Func) Func` | Translate to `v2.Middleware`; see Approach section |
| `type Adaptable interface` | Re-declare with `func(*Context)` / `func(*Context) error` variants instead of `func(v2.Context)` |
| `stopper.Background()` | Return a compat `*Context` wrapping a long-lived `v2.New()` that is never stopped (see §Background below) |
| `stopper.Fn[A Adaptable](fn A) Func` | Re-implement over compat `Func` / `Adaptable` |
| `stopper.From(ctx) *Context` | Call `v2.From(ctx)`; on miss return the compat `Background()` singleton (v1 never returns nil) |
| `stopper.Harden(ctx)` | Unwrap inner v2, call `.StoppingContext()` |
| `stopper.HardenFrom(plainCtx)` | `v2.From(plainCtx)` → `.StoppingContext()`; on miss return the plain ctx unchanged (mirrors v1 returning hardened Background) |
| `stopper.IsStopping(ctx) bool` | `v2.IsStopping(ctx)` |
| `stopper.StopOnReceive(ctx, dur, ch)` | `v2.StopOnReceive(ctx.inner, ch, v2.StopGracePeriod(dur))` (note: arg order differs) |
| `stopper.WithContext(ctx)` | `v2.WithContext(ctx)` wrapped in compat `*Context` |
| `stopper.WithInvoker(ctx, inv)` | Build a `v2.Middleware` from the v1 `Invoker`, attach via `v2.WithTaskOptions` |
| `stopper.ErrStopped` | Re-export `v2.ErrStopped` |
| `stopper.ErrGracePeriodExpired` | Re-export `v2.ErrGracePeriodExpired` |
| `ctx.Call(fn) error` | Wrap `fn` and delegate to `ctx.inner.Call(...)` |
| `ctx.Defer(func())` | Wrap the `func()` as a `v2.Func` and delegate to `ctx.inner.Defer(...)` (v1 has no return value; v2 returns `bool` — compat signature must match v1) |
| `ctx.Done() <-chan struct{}` | `ctx.inner.Done()` |
| `ctx.Err() error` | `ctx.inner.Err()` |
| `ctx.Go(fn) bool` | Wrap `fn` and delegate; return `ctx.inner.Go(...) == nil` |
| `ctx.IsStopping() bool` | `ctx.inner.IsStopping()` |
| `ctx.Len() int` | `ctx.inner.Len()` |
| `ctx.Stop(dur)` | `ctx.inner.Stop(v2.StopGracePeriod(dur))` |
| `ctx.StopOnIdle()` | `ctx.inner.Stop(v2.StopOnIdle())` |
| `ctx.Stopping() <-chan struct{}` | `ctx.inner.Stopping()` |
| `ctx.Wait() error` | `ctx.inner.Wait()` |
| `ctx.With(other)` | `&Context{inner: ctx.inner.WithDelegate(other)}` |

## Approach

### Wrapper struct

The central challenge is that v1 exposes a concrete `*stopper.Context`
while v2 uses a `stopper.Context` interface. The compat module would
define:

```go
package stopper

import v2 "vawter.tech/stopper/v2"

type Context struct {
    inner        v2.Context
    isBackground bool
}
```

Every v1 method is a thin forwarder. Because `*Context` must also
satisfy `context.Context`, the wrapper delegates `Deadline`, `Done`,
`Err`, and `Value` to `inner`.

### Function-signature adaptor

v1's `Func` is `func(*Context) error`. The compat layer must convert
between the v1 and v2 function types at the `Go`/`Defer` boundary:

```go
type Func = func(*Context) error

func (c *Context) Go(fn Func) bool {
    err := c.inner.Go(func(ctx v2.Context) error {
        return fn(&Context{inner: ctx})
    })
    return err == nil
}
```

This wrapping is cheap (one closure allocation per call) and preserves
the stopper lifecycle semantics.

### Invoker translation

v1's `Invoker` type is `func(fn Func) Func`. This can be mechanically
translated to a v2 `Middleware`:

```go
func WithInvoker(ctx *Context, inv func(Func) Func) *Context {
    mw := func(outer v2.Context) v2.Invoker {
        return func(ctx v2.Context, task v2.Func) error {
            wrapped := inv(func(c *Context) error {
                return task(c.inner)
            })
            return wrapped(&Context{inner: ctx})
        }
    }
    inner := v2.WithContext(ctx.inner, v2.WithTaskOptions(v2.TaskMiddleware(mw)))
    return &Context{inner: inner}
}
```

### `Background()` semantics

v1's `Background()` returns a special singleton `*Context` that cannot
be stopped. Its behavior differs from a normal context in several ways:

- `Stop()` and `StopOnIdle()` are no-ops.
- `Go()` always accepts work (returns `true`) but does not track
  goroutine lifecycle.
- `Defer()` panics.
- `Wait()` returns `nil` immediately.

v2 has no direct equivalent — `v2.New()` creates a fully stoppable
context. The compat module must implement `Background()` as a special
case. The simplest approach is to create a single `v2.New()` context
that is never stopped and wrap it in a compat `*Context` with the
`isBackground` flag set:

```go
var bg = &Context{inner: v2.New(), isBackground: true}

func Background() *Context { return bg }
```

The `isBackground` field allows methods like `Stop`, `StopOnIdle`, and
`Wait` to check the flag and implement the v1 no-op / immediate-return
semantics without a separate type or interface. It also enables the
compat test suite to assert that `From` returns the background
singleton when no stopper is in the context chain.

Behavioral differences:

- `Defer()` panics, matching the v1 behavior, because the callbacks
  would linger indefinitely (the background context is never stopped).
- `Go()` will track goroutines. This is transparent to callers.
- `Wait()` will block until the context is stopped (which never
  happens). v1 code that calls `Background().Wait()` is unlikely, but
  if encountered, the compat module should document this as a known
  divergence.

### `From()` return convention

v1's `From(ctx)` returns `*Context` (never `nil`). When the context
chain contains no stopper, v1 returns the `Background()` singleton.
v2's `From(ctx)` returns `(Context, bool)` and yields `(nil, false)` on
miss.

The compat `From` must match the v1 signature:

```go
func From(ctx context.Context) *Context {
    inner, ok := v2.From(ctx)
    if !ok {
        return Background()
    }
    return &Context{inner: inner}
}
```

### `context.Context` identity

Because v1 callers pass `*Context` to stdlib APIs that accept
`context.Context`, the wrapper must implement the full
`context.Context` interface. This is straightforward since `v2.Context`
already embeds `context.Context` and the wrapper simply delegates.

One subtlety: v2's `From` function looks for its own internal key in
the context value chain. The compat wrapper must ensure that `Value`
delegates to the inner v2 context so that `v2.From` (and by extension
functions like `StopOnReceive`) continue to work when handed a compat
`*Context`.

### `replace` directive mechanics

Go modules support replacing one module path with another:

```
replace vawter.tech/stopper v1.2.0 => vawter.tech/stopper/v2/compat v2.0.0
```

or, for local development:

```
replace vawter.tech/stopper v1.2.0 => ../go-stopper/compat
```

The compat module's `go.mod` would declare `module vawter.tech/stopper`
(the v1 module path) and `require vawter.tech/stopper/v2`. The
`replace` directive causes the Go toolchain to resolve all
`vawter.tech/stopper` imports to the compat package, which in turn
depends on v2. This is a well-supported Go modules feature.

**Important**: the compat module's `go.mod` must declare
`module vawter.tech/stopper` (not `vawter.tech/stopper/v2/compat`),
because the `replace` target is matched by module path. The directory
can live at `compat/` within this repository, but the module path must
match the v1 import path.

## Potential issues

### 1. Wrapping overhead (minor)

Each `Go` and `Defer` call allocates one extra closure to convert
between `func(*Context) error` and `func(v2.Context) error`. This is
negligible for the intended use case (lifecycle management, not
hot-path computation).

### 2. Type assertions across the boundary

Code that type-asserts `context.Context` to `*stopper.Context` will
work within the compat module's own `*Context` type. However, a v2
`Context` passed back through a `context.Context` cannot be
transparently unwrapped to a compat `*Context` without a helper.
The compat `From` function (see the Approach section) handles this by
calling `v2.From` and wrapping the result. Note that v1's `From`
never returns nil — it falls back to `Background()` — so the compat
`From` must preserve that convention.

### 3. `Defer` signature change

v1's `Defer` takes `func()` and has no return value. v2's `Defer`
takes `Func` (`func(Context) error`) and returns `bool`. The compat
module must match the v1 signature exactly:

```go
func (c *Context) Defer(fn func()) {
    _ = c.inner.Defer(func(_ v2.Context) error {
        fn()
        return nil
    })
}
```

The v2 return value (indicating whether the callback was deferred or
executed immediately) is intentionally discarded to match v1 behavior.

### 4. `Harden` Err semantics

v1's `Harden` returns a `context.Context` whose `Err()` returns
`ErrStopped` when the stopper is stopping. v2's `StoppingContext()`
returns a context whose `Err()` returns
`errors.Join(context.Canceled, ErrStopped)`. Code that checks
`err == stopper.ErrStopped` will break because the joined error is not
equal via `==`. However, `errors.Is(err, stopper.ErrStopped)` will
still match. If strict v1 compatibility is required, the compat
module could wrap the `StoppingContext()` result to return plain
`ErrStopped`, but this would break `errors.Is(err, context.Canceled)`
which v2 intentionally supports. This is a minor behavioral difference
worth documenting.

### 5. Sub-package compatibility

v1 did not have the `limit` or `linger` sub-packages in the same form.
If v1 consumers import sub-packages (e.g. `vawter.tech/stopper/linger`),
the compat module would need to provide matching sub-packages. This is
feasible since the sub-packages are small, but it does expand the
surface area.

### 6. Interop between v1 and v2 contexts in the same process

If a process uses both the compat v1 wrapper and native v2 code, the
two will share the same underlying `v2.Context` state. This is
actually desirable — parent/child relationships, task counts, and stop
signals propagate correctly because the compat layer is a thin wrapper
over real v2 objects.

### 7. `Func` type alias

v1 declared `type Func = func(*Context) error` (a type alias). This
means that v1 callers can pass bare `func(*Context) error` values
without explicit conversion. The compat module preserves this by using
the same alias declaration.

## Verdict

**Feasible.** The compat module is straightforward to implement, with
a few areas that require careful attention:

- Every v1 API element has a v2 equivalent, though several need thin
  adaptors (`Defer` signature, `From` return convention, `Background`
  semantics).
- The concrete-to-interface gap (`*Context` vs `Context`) is cleanly
  bridged by a single wrapper struct.
- The `replace` directive is a standard Go modules feature that
  requires no tooling changes.
- Wrapping overhead is negligible.
- Minor behavioral differences exist (`Harden`/`StoppingContext` Err
  value) but are unlikely to affect real-world consumers and can be
  documented.
- The main cost is maintaining the compat module as an additional
  package in the repository. Given the small API surface (~20 exported
  symbols), this is low effort.

A reasonable implementation would be a `compat/` directory at the
repository root containing:

```
compat/
  go.mod          # module vawter.tech/stopper
  go.sum
  stopper.go      # wrapper type + forwarding methods + Background/From
  adapt.go        # Fn, Adaptable re-declarations
  harden.go       # Harden, HardenFrom
  signal.go       # StopOnReceive with v1 arg order
```

Total implementation effort is estimated at 300–400 lines of code plus
tests.

## Populating the compat test suite from v1.2.0

The original v1 test files should be copied into the compat module so
that the wrapper is validated against the same expectations as v1. Use
the `v1.2.0` tag as the source of truth:

```bash
# From the repository root:
git show v1.2.0:stopper_test.go   > compat/stopper_test.go
git show v1.2.0:signal_test.go    > compat/signal_test.go
git show v1.2.0:adapt_test.go     > compat/adapt_test.go
git show v1.2.0:examples_test.go  > compat/examples_test.go
git show v1.2.0:harden_test.go    > compat/harden_test.go
git show v1.2.0:rig_test.go       > compat/rig_test.go
```

After copying, each file will need minor adjustments:

1. **Update the copyright header** if the project convention has changed.
2. **Run `go test`** inside `compat/` — any compilation errors indicate
   API surface gaps in the wrapper that must be filled.
3. **Amend assertions only when necessary.** Some tests may fail because
   the compat `*Context` is a wrapper struct rather than the original v1
   concrete type. Every such amendment **must** include a comment
   explaining the v1-to-v2 behavior change (see the goal statement
   above).

## Implementation task list

### 1. Create the `compat/` module directory ✅

Create `compat/` at the repository root with its own `go.mod`:

```
module vawter.tech/stopper

go 1.25.0
```

The `compat/go.mod` must **not** contain a `replace` directive — that
would infect the published module. Instead, create a `go.work` file
inside the `compat/` directory itself so the Go workspace resolves
`vawter.tech/stopper/v2` from the parent module during local
development and CI:

```
go 1.25.0

use (
    .
    ..
)
```

Run `go work sync` inside `compat/` to populate `compat/go.work.sum`.

### 2. Copy the original v1.2.0 tests into `compat/` ✅

```bash
git show v1.2.0:stopper_test.go   > compat/stopper_test.go
git show v1.2.0:signal_test.go    > compat/signal_test.go
git show v1.2.0:adapt_test.go     > compat/adapt_test.go
git show v1.2.0:examples_test.go  > compat/examples_test.go
git show v1.2.0:harden_test.go    > compat/harden_test.go
git show v1.2.0:rig_test.go       > compat/rig_test.go
```

Do **not** modify the test files yet — they will not compile until the
wrapper types and functions are in place. The tests serve as the
acceptance criteria: the compat module is complete when every original
test passes (with only the minimal amendments described in the Goal
section).

### 3. Declare the `Context` wrapper struct and core types ✅

Create `compat/stopper.go` with:

- `type Context struct` holding `inner v2.Context` and `isBackground bool`.
- `type Func = func(*Context) error` (type alias, matching v1).
- `type Invoker func(fn Func) Func`.
- Compile-time check: `var _ context.Context = (*Context)(nil)`.

### 4. Implement the `context.Context` interface on `*Context` ✅

Add forwarding methods on `*Context`:

- `Deadline() (time.Time, bool)` → `c.inner.Deadline()`.
- `Done() <-chan struct{}` → `c.inner.Done()`.
- `Err() error` → `c.inner.Err()`.
- `Value(key any) any` → `c.inner.Value(key)`.

### 5. Implement `Background()` and `From()` ✅

- `var bg = &Context{inner: v2.New(), isBackground: true}`.
- `func Background() *Context` — returns the singleton.
- `func From(ctx context.Context) *Context` — calls `v2.From(ctx)`;
  on miss returns `Background()` (v1 never returns nil).

### 6. Implement `WithContext()` and `WithInvoker()` ✅

- `func WithContext(ctx context.Context) *Context` — wraps
  `v2.WithContext(ctx)` in a compat `*Context`.
- `func WithInvoker(ctx context.Context, inv Invoker) *Context` —
  translates the v1 `Invoker` to a `v2.Middleware` and attaches it
  via `v2.WithTaskOptions(v2.TaskMiddleware(...))`.

### 7. Implement `IsStopping()` (package-level) and `ErrStopped` / `ErrGracePeriodExpired` ✅

- `func IsStopping(ctx context.Context) bool` → `v2.IsStopping(ctx)`.
- `var ErrStopped = v2.ErrStopped`.
- `var ErrGracePeriodExpired = v2.ErrGracePeriodExpired`.

### 8. Implement `*Context` lifecycle methods ✅

- `Go(fn Func) bool` — wraps `fn`, delegates to `c.inner.Go(...)`;
  returns `err == nil`.
- `Call(fn Func) error` — wraps `fn`, delegates to `c.inner.Call(...)`.
- `Defer(fn func())` — wraps the `func()` as a `v2.Func`, delegates
  to `c.inner.Defer(...)`; discards the `bool` return (`_ = ...`).
- `Stop(gracePeriod time.Duration)` — if `isBackground`, no-op;
  otherwise `c.inner.Stop(v2.StopGracePeriod(gracePeriod))`.
- `StopOnIdle()` — if `isBackground`, no-op; otherwise
  `c.inner.Stop(v2.StopOnIdle())`.
- `Wait() error` — if `isBackground`, return `nil` immediately;
  otherwise `c.inner.Wait()`.
- `IsStopping() bool` → `c.inner.IsStopping()`.
- `Stopping() <-chan struct{}` → `c.inner.Stopping()`.
- `Len() int` → `c.inner.Len()`.
- `With(ctx context.Context) *Context` →
  `&Context{inner: c.inner.WithDelegate(ctx)}`.

### 9. Implement `Adaptable` and `Fn` ✅

Create `compat/adapt.go` with:

- `type Adaptable interface` — re-declared with `func(*Context)` and
  `func(*Context) error` variants (matching v1 signatures).
- `func Fn[A Adaptable](fn A) Func` — generic adaptor over compat
  `Func` / `Adaptable`.

### 10. Implement `Harden` and `HardenFrom` ✅

Create `compat/harden.go` with:

- `func Harden(ctx *Context) context.Context` — unwraps `inner`,
  calls `c.inner.StoppingContext()`.
- `func HardenFrom(ctx context.Context) context.Context` — calls
  `v2.From(ctx)` → `.StoppingContext()`; on miss returns `ctx`
  unchanged (mirrors v1 behavior).

### 11. Implement `StopOnReceive` ✅

Create `compat/signal.go` with:

- `func StopOnReceive[T any](ctx *Context, gracePeriod time.Duration, ch <-chan T)` —
  delegates to `v2.StopOnReceive(ctx.inner, ch, v2.StopGracePeriod(gracePeriod))`
  (note the v1-to-v2 argument reordering).

### 12. Fix test compilation errors ✅

Run `go test` inside `compat/`. Resolve every compilation error by
filling gaps in the wrapper implementation. Do **not** weaken or skip
tests — compilation failures indicate missing API surface.

### 13. Amend tests for v1-to-v2 behavioral differences ✅

Run `go test -race -timeout 10s` and fix failing assertions. Every
amendment **must** include an inline comment explaining the behavioral
change. Known areas:

- Pointer identity: compat `*Context` is a wrapper; two calls to
  `From` may return different `*Context` values wrapping the same
  `v2.Context`.
- `Harden`/`StoppingContext` `Err()` returns
  `errors.Join(context.Canceled, ErrStopped)` instead of plain
  `ErrStopped`. Tests using `==` must switch to `errors.Is`.
- `Background().Defer()` panics (matching v1).
- `Background().Wait()` blocks instead of returning immediately — the
  compat wrapper overrides this to return `nil` via `isBackground`.

### 14. Verify the full test suite passes ✅

```bash
cd compat && go test -race -timeout 10s ./...
```

All tests must pass with the race detector enabled.

## Compat validation report (2026-02-21)

### Methodology

The v1.2.0 test files were extracted via `git show v1.2.0:<file>` and
diffed against their `compat/` counterparts. Every difference was
categorized as one of:

- **Style-only** — no behavioral impact (e.g. `assert` → `require`).
- **Unexported-symbol removal** — permitted by the porting goal that
  "v1 test code that accesses non-exported symbols may be removed
  entirely."
- **Behavioral amendment with comment** — assertion changed to
  accommodate a v1-to-v2 behavioral difference, with an inline
  `// v1-to-v2:` comment explaining the change.
- **Behavioral amendment missing comment** — assertion changed or
  removed to accommodate a v1-to-v2 behavioral difference, but the
  required inline comment is absent.

The compat test suite was run with `go test -race -timeout 10s ./...`
and all tests passed.

### Files with no behavioral changes

| File | Notes |
|---|---|
| `adapt_test.go` | Identical to v1.2.0 |
| `examples_test.go` | Identical to v1.2.0 |
| `rig_test.go` | Identical to v1.2.0 |
| `signal_test.go` | Identical to v1.2.0 |
| `linger/check_test.go` | Whitespace-only differences |
| `linger/linger_test.go` | Whitespace-only differences |

### Files with amendments

#### `stopper_test.go`

1. **`assert.New(t)` → `require.New(t)` throughout** — style-only;
   all `a.` calls become `r.` calls. No behavioral impact. ✅

2. **`TestBackground`** — removed `a.Same(s, background)` (used the
   unexported `background` variable) and `a.False(s.canStop())` (used
   the unexported `canStop` method). Replaced with
   `r.Same(s, Background())`. The `canStop` line was dropped without a
   comment, which is permitted because it accesses an unexported method. ✅

3. **`TestBackground` / `TestStopper`** — removed
   `a.False(s.mu.stopping)`. Accesses unexported `mu` field; removal
   permitted. ✅

4. **`TestNested`** — `parent.Stop(0)` is unchanged from v1; the
   compat `Stop` method maps zero to `math.MaxInt64` internally. ✅

5. **`TestOverRelease`** — entire test removed with a `// v1-to-v2:`
   comment noting it tested the unexported `apply()` method. ✅

6. **`TestRejectedWhileStopping`** — replaced direct mutation of
   `parent.mu.stopping` with `parent.Stop(0)`. Accesses unexported
   field; the replacement is functionally equivalent. ✅

7. **`TestStopper`** — `a.Same(s, From(s))` and
   `a.Same(s, From(context.WithValue(s, s, s)))` replaced with
   `r.NotNil(...)`. Has a `// v1-to-v2:` comment. ✅

8. **`TestStopper`** — `s.Stop(0)` is unchanged from v1; the compat
   `Stop` method maps zero to `math.MaxInt64` internally. ✅

9. **`TestStopper`** — removed `a.True(s.canStop())`. Accesses
   unexported method; removal permitted. ✅

10. **`TestStopOnIdleBackground`** — `a.False(s.mu.stopOnIdle)` replaced
    with `r.False(s.IsStopping())` and a `// v1-to-v2:` comment. The
    original assertion tested unexported state; the replacement is
    reasonable. ✅

11. **`TestDefer`** — `a.PanicsWithError(...)` preserved as
    `r.PanicsWithError(...)`. The compat `Defer` panics with
    `errors.New(...)` (an `error` value), matching v1. ✅

12. **`TestWith`** — removed `a.Same(s.state, ctx.state)` (two
    occurrences). Accesses unexported `state` field; removal permitted. ✅

13. **`TestWithBackground`** — removed `a.False(w.canStop())` and
    `a.False(found.canStop())`. Both access an unexported method;
    removal permitted. The `a.PanicsWithError("cannot call
    Context.Defer()...")` assertion is preserved as
    `r.PanicsWithError(...)`, matching v1. ✅

#### `examples_test.go`

(Identical to v1.2.0 — no amendments.)

#### `harden_test.go`

1. **`TestHarden`** — `r.Same(ctx, From(h))` replaced with
   `r.NotNil(From(h))`. Has a `// v1-to-v2:` comment. ✅

2. **`TestHarden`** — `ctx.Stop(0)` and `<-ctx.Done()` are unchanged
   from v1; the compat `Stop` method maps zero to `math.MaxInt64`
   internally. ✅

#### `linger/linger.go` (source, not test)

1. **`callersOffset`** — changed from `3` to `4`. Has a
   `// v1-to-v2:` comment. ✅

### Behavioral differences between v1 and compat

#### 1. `Background().Defer()` panic value ✅

v1 panics with `errors.New("cannot call Context.Defer() on a background
context")` — an `error` value. Compat also panics with
`errors.New(...)`, matching v1. Both `TestDefer` and
`TestWithBackground` use `PanicsWithError`, preserving the original v1
assertions.

#### 2. `From()` pointer identity — acceptable ✅

v1's `From(ctx)` returns the same `*Context` pointer that was placed in
the context chain. Compat's `From()` wraps the inner `v2.Context` in a
new `*Context` struct each time, so pointer identity (`Same`) does not
hold. Tests correctly replaced `Same` with `NotNil` and added
`// v1-to-v2:` comments. This is an inherent consequence of the wrapper
architecture and does not affect real-world consumers, which never rely
on pointer identity of `*Context` values.

#### 3. `Stop(0)` semantics ✅

v1's `Stop(0)` enters the stopping phase but does not immediately cancel
the context — the grace period of zero effectively means "wait
indefinitely for tasks to finish." v2's `Stop` with a zero-duration
grace period expires immediately, canceling the context before tasks
can complete. The compat `Stop` method now maps a zero duration to
`math.MaxInt64`, preserving v1 semantics. All compat tests use
`Stop(0)` unchanged from v1.

#### 4. `Harden` / `StoppingContext` Err value — acceptable ✅

v1's `Harden` returns a context whose `Err()` returns plain
`ErrStopped`. Compat delegates to v2's `StoppingContext()`, which
returns `errors.Join(context.Canceled, ErrStopped)`. Code using
`err == ErrStopped` will break; `errors.Is(err, ErrStopped)` continues
to work. The existing `harden_test.go` uses `errors.Is`, so this
difference does not surface as a test failure. The joined error is
strictly more informative than the v1 value, and idiomatic Go code
already uses `errors.Is` rather than `==` for sentinel checks.

#### 5. `linger` callers offset — acceptable ✅

The compat wrapper adds one extra stack frame in `applyInvokers`,
requiring `callersOffset` to increase from `3` to `4`. This is
transparent to callers of the `linger` API; the `Recorder` reports the
same caller-visible frame regardless of the offset adjustment.

### Missing `// v1-to-v2:` comments

All test amendments now use the `// v1-to-v2:` comment convention.
No missing comments remain.

### Summary

The compat module achieves its primary goal: all original v1 exported
API symbols are present and all ported tests pass with the race
detector enabled. Four test files are completely unchanged from v1.2.0
and two more have only whitespace differences. The remaining behavioral
differences — `From()` pointer identity, `Harden` error wrapping, and
linger stack offset — are all documented either in the tests or in
this feasibility document and have been marked acceptable (no
additional work required). All test amendments consistently use the
`// v1-to-v2:` comment convention. Two earlier discrepancies have been
resolved: `Defer` panics with `errors.New(...)` matching v1, and
`Stop(0)` maps to `math.MaxInt64` so that zero means "wait
indefinitely" as in v1.
