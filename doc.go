// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

// Package stopper provides graceful lifecycle management for
// long-running tasks in a Go program.
//
// A stopper [Context] extends the standard library [context.Context]
// with a two-phase shutdown model:
//
//  1. Soft stop – the [Context.Stopping] channel closes, signaling
//     tasks to begin draining. New tasks are rejected with [ErrStopped].
//  2. Hard stop – after a configurable grace period the underlying
//     context is canceled, closing [Context.Done].
//
// # Creating a stopper
//
// Use [New] to create a standalone stopper, or [WithContext] to derive
// one from an existing [context.Context]. When the parent is itself a
// stopper [Context], the child inherits stop signals and task counts.
//
//	ctx := stopper.New()
//	ctx := stopper.WithContext(parentCtx)
//
// # Running tasks
//
// Tasks are ordinary Go functions. The generic [Call], [Go], [GoN],
// and [Defer] package-level functions accept any [Adaptable] function
// signature — from a bare func() to func([Context]) error — so
// callers rarely need to construct a [Func] value directly.
//
//	// Fire-and-forget goroutine.
//	_ = stopper.Go(ctx, func(ctx stopper.Context) {
//	    for !ctx.IsStopping() { /* work */ }
//	})
//
//	// Synchronous call in the current goroutine.
//	err := stopper.Call(ctx, func() error { return doWork() })
//
// # Responding to a stop signal
//
// The [Context.Stopping] channel is the primary way for a task to
// learn that a graceful shutdown has been requested. It closes as
// soon as [Context.Stop] is called (or a parent context is stopped),
// while the underlying [Context.Done] channel remains open until the
// grace period expires. This separation gives tasks time to finish
// in-flight work, flush buffers, or deregister from external
// services before the hard cancel.
//
// Use [Context.Stopping] in a select statement together with other
// channels:
//
//	_ = stopper.Go(ctx, func(ctx stopper.Context) {
//	    for {
//	        select {
//	        case <-ctx.Stopping():
//	            return // begin draining
//	        case item := <-work:
//	            process(item)
//	        }
//	    }
//	})
//
// For simple loops that do not need a select, the boolean helper
// [Context.IsStopping] is a convenient shorthand.
//
// [Context.StoppingContext] wraps the soft-stop signal as a plain
// [context.Context], making it easy to pass the stop condition to
// libraries that are not stopper-aware (e.g. database drivers or
// gRPC clients).
//
// # Stopping and waiting
//
// Call [Context.Stop] to initiate a graceful shutdown. The optional
// [StopGracePeriod] controls how long tasks have before a hard cancel,
// and [StopOnIdle] triggers a stop once all running tasks complete.
//
//	ctx.Stop(stopper.StopGracePeriod(5 * time.Second))
//	if err := ctx.Wait(); err != nil { log.Fatal(err) }
//
// # Middleware
//
// [Middleware] functions wrap task execution and can be attached
// globally via [WithTaskOptions] or per-task via [TaskMiddleware]. The
// [limit] sub-package ships ready-made middlewares for concurrency and
// rate limiting. The [retry] sub-package provides [retry.Backoff] for
// configurable exponential backoff with jitter and [retry.Loop] for
// simple synchronous retries.
//
// # Nested contexts
//
// Contexts created with [WithContext] form a hierarchy: stopping a
// parent automatically stops its children, while [Context.Len] and
// [Context.Wait] account for tasks across the entire tree.
//
// # OS signal integration
//
// [StopOnReceive] triggers a graceful shutdown when a value arrives on
// any channel. It is commonly used with [os/signal.Notify] to wire up
// SIGINT or SIGTERM handling.
//
// # Panic recovery
//
// Every task launched via [Call] or [Go] is wrapped in a default panic
// handler. If a task panics, the panic value is recovered and wrapped
// in a [RecoveredError]. If the panic value already implements the
// [error] interface it is used directly; otherwise it is formatted with
// [fmt.Errorf]. In both cases the resulting error and the goroutine
// stack at the point of the panic are captured in the [RecoveredError].
// The error is then handled exactly like a returned error: for [Call]
// it is returned to the caller, and for [Go] it is passed to the
// task's [ErrorHandler] (default [ErrorHandlerStop]).
//
// Use [errors.As] to extract the [RecoveredError] from the returned
// error and inspect its Stack field or call its [RecoveredError.String]
// method for a human-readable stack trace.
//
// # Tracing
//
// Every [Context] and every task launched via [Call] or [Go]
// automatically creates a [runtime/trace.Task], so the stopper
// hierarchy is always visible in Go execution traces with no extra
// code. Built-in middleware in the [limit] and [retry] sub-packages
// annotate blocking waits with [runtime/trace.StartRegion], making it
// easy to spot concurrency bottlenecks, rate-limit pauses, and retry
// delays in the trace viewer. Use [WithName] and [TaskName] to give
// stoppers and tasks descriptive names in the trace output.
//
// # Concurrent sequence processing
//
// The [seq] sub-package provides bounded-concurrency helpers for
// [iter.Seq] and [iter.Seq2] sequences. [seq.ForEach] and
// [seq.ForEach2] apply a callback to every element, while [seq.Map] /
// [seq.Map2] and [seq.MapUnordered] / [seq.MapUnordered2] transform
// elements into a new iter.Seq2[R, error], preserving or relaxing
// input order respectively.
//
// # Integration with other libraries
//
// Because [Context] embeds [context.Context], it works with any
// library that accepts a context. [Context.WithDelegate] allows
// interoperation with packages that produce custom context values.
// [Context.StoppingContext] exports the soft-stop signal as a plain
// [context.Context] for libraries that are not stopper-aware. The
// [From] function recovers a stopper [Context] from any
// [context.Context] in the chain.
//
// # Testing
//
// The [linger] sub-package provides helpers to detect tasks that fail
// to exit promptly during tests.
package stopper
