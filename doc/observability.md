# Design: Observability Middleware via `TaskInfo`

This document describes how the `Middleware` and `TaskInfo` types in
`vawter.tech/stopper/v2` can be used to build integrations with the four
most popular Go observability SDKs:

1. **OpenTelemetry** (`go.opentelemetry.io/otel`)
2. **Prometheus** (`github.com/prometheus/client_golang`)
3. **Datadog** (`gopkg.in/DataDog/dd-trace-go.v1`)
4. **Sentry** (`github.com/getsentry/sentry-go`)

Each integration is a standalone `Middleware` factory that can be
attached at the stopper level (via `WithTaskOptions`) or per-task (via
`TaskMiddleware`). No changes to the core `stopper` package are
required.

## Background

### `Middleware`

```go
type Middleware func(outer Context) (Context, Invoker)
type Invoker   func(ctx Context, task Func) error
```

A `Middleware` has two phases:

1. **Setup (synchronous)** — runs inside `Go`/`Call` on the caller's
   goroutine. It receives the outer `Context` and may capture state,
   amend the context (typically via `WithDelegate`), and return an
   `Invoker`.
2. **Invocation (asynchronous)** — the `Invoker` runs in the task's
   goroutine. It wraps the actual task execution and can perform
   before/after logic.

### `TaskInfo`

```go
type TaskInfo struct {
    Context     Context
    ContextName string
    Done        <-chan struct{}
    Error       atomic.Pointer[error]
    Started     time.Time
    Task        Func
    TaskName    string
}
```

A `TaskInfo` is injected into the task's context before the middleware
chain executes. It is retrievable via `TaskInfoFrom(ctx)`. The `Error`
field is a tri-state atomic pointer:

| `Error.Load()` | Meaning |
|---|---|
| `nil` | Task is still running |
| `*error(nil)` | Task completed successfully |
| `*error(err)` | Task failed with `err` |

The `Done` channel is closed when the task finishes, enabling
event-driven lifecycle observation.

## Design Principles

All four integrations follow the same structural pattern:

1. The **setup phase** captures the task name and any caller-scoped
   state (e.g. a parent span from the outer context).
2. The **invoker** starts the SDK-specific resource (span, timer,
   transaction) *before* calling the task and finalizes it *after* the
   task returns, using `defer` to guarantee cleanup even on panics.
3. Errors returned by the task are recorded on the SDK resource before
   it is closed.
4. Task metadata from `TaskInfo` (names, duration) supplies the
   attributes/tags/labels without requiring the user to pass them
   manually.

Because `Middleware` composes, users can stack multiple observability
integrations on the same stopper or task without interference:

```go
ctx := stopper.New(
    stopper.WithTaskOptions(
        stopper.TaskMiddleware(
            otelMiddleware(tracer),
            promMiddleware(histogram, counter),
            ddMiddleware(),
            sentryMiddleware(),
        ),
    ),
)
```

## 1. OpenTelemetry

[go.opentelemetry.io/otel](https://pkg.go.dev/go.opentelemetry.io/otel)
is the vendor-neutral standard for distributed tracing and metrics.

### Integration Point

OpenTelemetry's tracing API operates on `context.Context`; a child span
is derived from a parent span stored in the context. This maps directly
to the `Middleware` pattern: the `Invoker` starts a span, injects it
into the context via `WithDelegate`, and ends it on return.

### Sketch

```go
package otelstop

import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/codes"
    "go.opentelemetry.io/otel/trace"
    "vawter.tech/stopper/v2"
)

// WithTracing returns a Middleware that creates an OpenTelemetry span
// for each task execution.
func WithTracing(tracer trace.Tracer) stopper.Middleware {
    return func(outer stopper.Context) (stopper.Context, stopper.Invoker) {
        // Setup phase: nothing to capture beyond the tracer.
        return outer, func(ctx stopper.Context, task stopper.Func) error {
            info, _ := stopper.TaskInfoFrom(ctx)

            spanCtx, span := tracer.Start(ctx,
                info.ContextName+"."+info.TaskName,
                trace.WithAttributes(
                    attribute.String("stopper.context", info.ContextName),
                    attribute.String("stopper.task", info.TaskName),
                ),
            )
            defer span.End()

            // Inject the span context so downstream code inherits it.
            ctx = ctx.WithDelegate(spanCtx)

            err := task(ctx)
            if err != nil {
                span.RecordError(err)
                span.SetStatus(codes.Error, err.Error())
            } else {
                span.SetStatus(codes.Ok, "")
            }
            return err
        }
    }
}
```

### Key Observations

- The parent span is automatically inherited from `outer` because
  OpenTelemetry stores spans in `context.Context` values.
- `WithDelegate` is used instead of `WithContext` to avoid creating a
  new stopper hierarchy — it only replaces the context delegate.
- Task duration is implicitly captured by the span's start/end
  timestamps.
- If the user also enables `runtime/trace` (the stdlib tracing already
  built into stopper), both trace systems coexist because they use
  independent context keys.

### Metrics Extension

OpenTelemetry metrics can be layered in the same middleware or as a
separate `Middleware`:

```go
func WithMetrics(meter metric.Meter) stopper.Middleware {
    taskDuration, _ := meter.Float64Histogram("stopper.task.duration",
        metric.WithUnit("s"),
    )
    taskCount, _ := meter.Int64Counter("stopper.task.count")

    return func(outer stopper.Context) (stopper.Context, stopper.Invoker) {
        return outer, func(ctx stopper.Context, task stopper.Func) error {
            info, _ := stopper.TaskInfoFrom(ctx)
            attrs := attribute.NewSet(
                attribute.String("stopper.context", info.ContextName),
                attribute.String("stopper.task", info.TaskName),
            )

            start := time.Now()
            err := task(ctx)

            status := "ok"
            if err != nil {
                status = "error"
            }
            taskDuration.Record(ctx,
                time.Since(start).Seconds(),
                metric.WithAttributeSet(attrs),
            )
            taskCount.Add(ctx, 1,
                metric.WithAttributeSet(attrs),
                metric.WithAttributes(attribute.String("status", status)),
            )
            return err
        }
    }
}
```

## 2. Prometheus

[github.com/prometheus/client_golang](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus)
is the de-facto standard for pull-based metrics in Go services.

### Integration Point

Prometheus is context-unaware — it uses global or injected registries.
The middleware captures pre-allocated metric collectors and records
observations keyed by task/context name labels extracted from `TaskInfo`.

### Sketch

```go
package promstop

import (
    "time"

    "github.com/prometheus/client_golang/prometheus"
    "vawter.tech/stopper/v2"
)

// Metrics holds pre-registered Prometheus collectors.
type Metrics struct {
    TaskDuration *prometheus.HistogramVec // labels: context, task, status
    TasksActive  *prometheus.GaugeVec    // labels: context, task
}

// NewMetrics creates and registers the default metric set.
func NewMetrics(reg prometheus.Registerer) *Metrics {
    m := &Metrics{
        TaskDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
            Name:    "stopper_task_duration_seconds",
            Help:    "Duration of stopper task executions.",
            Buckets: prometheus.DefBuckets,
        }, []string{"context", "task", "status"}),
        TasksActive: prometheus.NewGaugeVec(prometheus.GaugeOpts{
            Name: "stopper_tasks_active",
            Help: "Number of currently running stopper tasks.",
        }, []string{"context", "task"}),
    }
    reg.MustRegister(m.TaskDuration, m.TasksActive)
    return m
}

// WithMetrics returns a Middleware that records Prometheus metrics.
func WithMetrics(m *Metrics) stopper.Middleware {
    return func(outer stopper.Context) (stopper.Context, stopper.Invoker) {
        return outer, func(ctx stopper.Context, task stopper.Func) error {
            info, _ := stopper.TaskInfoFrom(ctx)
            labels := prometheus.Labels{
                "context": info.ContextName,
                "task":    info.TaskName,
            }

            m.TasksActive.With(labels).Inc()
            start := time.Now()

            err := task(ctx)

            m.TasksActive.With(labels).Dec()
            status := "ok"
            if err != nil {
                status = "error"
            }
            labels["status"] = status
            m.TaskDuration.With(labels).Observe(time.Since(start).Seconds())

            return err
        }
    }
}
```

### Key Observations

- No context manipulation is needed — `outer` is returned unchanged
  from the setup phase.
- The `GaugeVec` for active tasks provides real-time visibility into
  in-flight work, complementing `Context.Len()`.
- Label cardinality is bounded by the number of distinct context/task
  name pairs, which are developer-controlled strings.
- Alternatively, an integration could spawn a background goroutine that
  listens on `TaskInfo.Done` to record completion asynchronously, but
  the synchronous `defer` approach in the `Invoker` is simpler and
  sufficient.

## 3. Datadog (`dd-trace-go`)

[gopkg.in/DataDog/dd-trace-go.v1](https://pkg.go.dev/gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer)
provides tracing and APM integration for Datadog.

### Integration Point

Datadog's tracer stores spans in `context.Context`, similar to
OpenTelemetry. The `tracer.StartSpanFromContext` function extracts a
parent span and returns a child. This maps directly to the `Invoker`
pattern.

### Sketch

```go
package ddstop

import (
    "gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
    "vawter.tech/stopper/v2"
)

// WithTracing returns a Middleware that creates a Datadog span for each
// task execution.
func WithTracing() stopper.Middleware {
    return func(outer stopper.Context) (stopper.Context, stopper.Invoker) {
        return outer, func(ctx stopper.Context, task stopper.Func) error {
            info, _ := stopper.TaskInfoFrom(ctx)

            span, spanCtx := tracer.StartSpanFromContext(ctx,
                "stopper.task",
                tracer.ResourceName(info.ContextName+"."+info.TaskName),
                tracer.Tag("stopper.context", info.ContextName),
                tracer.Tag("stopper.task", info.TaskName),
            )
            defer span.Finish()

            ctx = ctx.WithDelegate(spanCtx)

            err := task(ctx)
            if err != nil {
                span.Finish(tracer.WithError(err))
            }
            return err
        }
    }
}
```

### Key Observations

- The structure is nearly identical to the OpenTelemetry integration
  because both SDKs use context-propagated spans.
- `WithDelegate` injects the Datadog span context without creating a
  new stopper hierarchy, preserving the task's stop/done lifecycle.
- Datadog's `tracer.Tag` is used to attach stopper-specific metadata
  that appears in the Datadog APM UI.
- For services that use both Datadog and OpenTelemetry (e.g. during a
  migration), both middleware can be stacked and will each produce
  independent spans.

## 4. Sentry

[github.com/getsentry/sentry-go](https://pkg.go.dev/github.com/getsentry/sentry-go)
is an error-tracking and performance-monitoring platform.

### Integration Point

Sentry's Go SDK uses `*sentry.Hub` instances that are typically cloned
per-goroutine. The setup phase of a `Middleware` clones the hub from the
outer context (capturing the caller's scope), and the `Invoker` attaches
it to the task context and wraps execution with error capture.

### Sketch

```go
package sentrystop

import (
    "github.com/getsentry/sentry-go"
    "vawter.tech/stopper/v2"
)

// WithErrorCapture returns a Middleware that reports task errors and
// panics to Sentry and creates performance transactions.
func WithErrorCapture() stopper.Middleware {
    return func(outer stopper.Context) (stopper.Context, stopper.Invoker) {
        // Setup phase: clone the hub so the task gets its own scope.
        parentHub := sentry.GetHubFromContext(outer)
        if parentHub == nil {
            parentHub = sentry.CurrentHub()
        }
        hub := parentHub.Clone()

        return outer, func(ctx stopper.Context, task stopper.Func) error {
            info, _ := stopper.TaskInfoFrom(ctx)
            opName := info.ContextName + "." + info.TaskName

            // Attach the cloned hub to the task context.
            sentryCtx := sentry.SetHubOnContext(ctx, hub)
            ctx = ctx.WithDelegate(sentryCtx)

            hub.ConfigureScope(func(scope *sentry.Scope) {
                scope.SetTag("stopper.context", info.ContextName)
                scope.SetTag("stopper.task", info.TaskName)
            })

            // Start a Sentry transaction for performance monitoring.
            tx := sentry.StartTransaction(sentryCtx, opName,
                sentry.WithOpName("stopper.task"),
            )
            defer tx.Finish()

            err := task(ctx)
            if err != nil {
                hub.CaptureException(err)
                tx.Status = sentry.SpanStatusInternalError
            } else {
                tx.Status = sentry.SpanStatusOK
            }
            return err
        }
    }
}
```

### Key Observations

- Hub cloning in the **setup phase** is critical: Sentry hubs are not
  goroutine-safe, so each task must have its own hub. The `Middleware`
  two-phase design aligns perfectly — setup clones on the caller's
  goroutine; the invoker uses the clone on the task's goroutine.
- Panics recovered by stopper's built-in panic handler produce a
  `*RecoveredError` that flows through the error path, so Sentry
  receives both the error and the original panic value.
- The Sentry transaction provides performance monitoring (duration,
  status) without additional timing code.
- For long-running tasks (e.g. server loops), the `TaskInfo.Done`
  channel could be used to flush the hub when the task completes:

  ```go
  go func() {
      <-info.Done
      hub.Flush(2 * time.Second)
  }()
  ```

## Cross-Cutting Concerns

### Composition

All four integrations are independent `Middleware` values that compose
naturally:

```go
stopper.TaskMiddleware(
    otelstop.WithTracing(tracer),
    promstop.WithMetrics(metrics),
    ddstop.WithTracing(),
    sentrystop.WithErrorCapture(),
)
```

The middleware chain executes in declaration order for setup, and wraps
in the same order for invocation — the first middleware is the outermost
wrapper.

### Naming Convention

All integrations derive operation/span/metric names from
`TaskInfo.ContextName` and `TaskInfo.TaskName`. Users control these via
`WithName` and `TaskName`:

```go
ctx := stopper.New(stopper.WithName("api-server"))
_ = stopper.Go(ctx, handler, stopper.TaskName("handle-request"))
// → span name: "api-server.handle-request"
```

### Error Propagation

The `Invoker` receives the task's return value. All four integrations
record errors on their respective resources (span status, counter label,
Sentry event) and then **return the error unchanged** so the stopper's
own `ErrorHandler` logic is unaffected.

### Minimal Overhead When Disabled

Each factory accepts its SDK client as a parameter. If no client is
provided (or the SDK is not initialized), the middleware can short-
circuit to `InvokerCall`:

```go
func WithTracing(tracer trace.Tracer) stopper.Middleware {
    if tracer == nil {
        return func(outer stopper.Context) (stopper.Context, stopper.Invoker) {
            return outer, stopper.InvokerCall
        }
    }
    // ...
}
```

### `TaskInfo.Done` for Async Reporting

Some observability backends benefit from asynchronous lifecycle events.
The `TaskInfo.Done` channel enables this without blocking the task
itself. For example, a metrics middleware could launch a goroutine to
observe task completion and update a long-lived gauge:

```go
return outer, func(ctx stopper.Context, task stopper.Func) error {
    info, _ := stopper.TaskInfoFrom(ctx)
    gauge.Inc()
    go func() {
        <-info.Done
        gauge.Dec()
    }()
    return task(ctx)
}
```

This is useful in `Call` scenarios where the `Invoker` runs on the
caller's goroutine and blocking after the task would delay the caller.

## Summary

| SDK | Setup Phase | Invoker Phase | Context Amended? |
|---|---|---|---|
| OpenTelemetry | (none) | Start/end span, record error, emit metrics | Yes (`WithDelegate`) |
| Prometheus | (none) | Inc/dec gauge, observe histogram | No |
| Datadog | (none) | Start/finish span, set tags | Yes (`WithDelegate`) |
| Sentry | Clone hub | Attach hub, start/finish tx, capture error | Yes (`WithDelegate`) |

The `Middleware` + `TaskInfo` design provides a uniform integration
surface that maps cleanly onto all four observability SDKs with no
changes to the core stopper package. Each integration is a single
function returning a `Middleware` value, composable via
`TaskMiddleware`, and driven entirely by the metadata already present
in `TaskInfo`.
