// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	"context"
	"errors"
	"math"
	"slices"
	"time"

	v2 "vawter.tech/stopper/v2"
)

var _ context.Context = (*Context)(nil)

var bg = &Context{inner: v2.New(), isBackground: true}

// Background returns a singleton [Context] that is never stopped.
// It is the compat equivalent of the v1 Background stopper.
//
// Deprecated: v2 has no Background equivalent. Use [v2.New] to create
// a new, stoppable context instead.
func Background() *Context { return bg }

// Call wraps [Func] and delegates to the inner v2 [v2.Context.Call].
//
// Deprecated: Use [v2.Context.Call] instead.
func (c *Context) Call(fn Func) error {
	task := c.applyInvokers(fn)
	return c.inner.Call(func(ctx v2.Context) error {
		return task(&Context{inner: ctx})
	})
}

// Context wraps a v2 [v2.Context] to provide backward compatibility
// with the original v1 API surface.
//
// Deprecated: Use [v2.Context] instead.
type Context struct {
	inner        v2.Context
	invokers     []Invoker
	isBackground bool
}

// Deprecated: Use [v2.Context.Deadline] instead.
func (c *Context) Deadline() (time.Time, bool) { return c.inner.Deadline() }

// Defer registers a callback to run after all tasks complete.
// The bool return from the v2 API is intentionally discarded.
// Calling Defer on the Background context panics because the
// callbacks would linger indefinitely.
//
// Deprecated: Use [v2.Context.Defer] instead.
func (c *Context) Defer(fn func()) {
	if c.isBackground {
		panic(errors.New("cannot call Context.Defer() on a background context"))
	}
	_ = c.inner.Defer(func(_ v2.Context) error {
		fn()
		return nil
	})
}

// Deprecated: Use [v2.Context.Done] instead.
func (c *Context) Done() <-chan struct{} { return c.inner.Done() }

// Deprecated: Use [v2.Context.Err] instead.
func (c *Context) Err() error { return c.inner.Err() }

// Deprecated: Use [v2.Context.Value] instead.
func (c *Context) Value(key any) any { return c.inner.Value(key) }

// ErrGracePeriodExpired is returned when the grace period expires
// before all tasks have exited.
//
// Deprecated: Use [v2.ErrGracePeriodExpired] instead.
var ErrGracePeriodExpired = v2.ErrGracePeriodExpired

// ErrStopped is returned when the context has been stopped.
//
// Deprecated: Use [v2.ErrStopped] instead.
var ErrStopped = v2.ErrStopped

// From returns the [Context] associated with ctx, or [Background] if
// no stopper is found. Unlike the v2 API, v1 never returns nil.
//
// Deprecated: Use [v2.From] instead. The v2 variant returns
// (Context, bool) rather than falling back to a background singleton.
func From(ctx context.Context) *Context {
	if c, ok := ctx.(*Context); ok {
		return c
	}
	found, ok := v2.From(ctx)
	if !ok {
		return Background()
	}
	return &Context{inner: found}
}

// Func is a task function that receives a compat [Context].
//
// Deprecated: Use [v2.Func] instead.
type Func = func(*Context) error

// Go spawns a new goroutine and monitors its lifecycle. It returns
// true if the task was accepted.
//
// Deprecated: Use [v2.Context.Go] instead.
func (c *Context) Go(fn Func) bool {
	task := c.applyInvokers(fn)
	err := c.inner.Go(func(ctx v2.Context) error {
		return task(&Context{inner: ctx})
	})
	return err == nil
}

// applyInvokers composes the accumulated [Invoker] chain around fn.
// Invokers are applied from innermost to outermost so that setup runs
// bottom-up and execution runs top-down.
func (c *Context) applyInvokers(fn Func) Func {
	task := fn
	for i := len(c.invokers) - 1; i >= 0; i-- {
		task = c.invokers[i](task)
	}
	return task
}

// Invoker wraps a [Func] to provide middleware-like behavior.
//
// Deprecated: Use [v2.Middleware] instead.
type Invoker func(fn Func) Func

// IsStopping reports whether ctx is managed by a stopper and that
// stopper is stopping.
//
// Deprecated: Use [v2.IsStopping] instead.
func IsStopping(ctx context.Context) bool { return v2.IsStopping(ctx) }

// Deprecated: Use [v2.Context.IsStopping] instead.
func (c *Context) IsStopping() bool { return c.inner.IsStopping() }

// Deprecated: Use [v2.Context.Len] instead.
func (c *Context) Len() int { return c.inner.Len() }

// Stop begins a graceful shutdown. On a background context this is a
// no-op. A zero gracePeriod is mapped to the maximum duration to match
// v1 semantics, where zero meant "wait indefinitely."
//
// Deprecated: Use [v2.Context.Stop] instead.
func (c *Context) Stop(gracePeriod time.Duration) {
	if c.isBackground {
		return
	}
	if gracePeriod == 0 {
		gracePeriod = time.Duration(math.MaxInt64)
	}
	c.inner.Stop(v2.StopGracePeriod(gracePeriod))
}

// StopOnIdle delays stop until the context has no active tasks. On a
// background context this is a no-op.
//
// Deprecated: Use [v2.Context.Stop] with [v2.StopOnIdle] instead.
func (c *Context) StopOnIdle() {
	if c.isBackground {
		return
	}
	c.inner.Stop(v2.StopOnIdle())
}

// Deprecated: Use [v2.Context.Stopping] instead.
func (c *Context) Stopping() <-chan struct{} { return c.inner.Stopping() }

// Wait blocks until the context has stopped and all tasks have exited.
// On a background context it returns nil immediately.
//
// Deprecated: Use [v2.Context.Wait] instead.
func (c *Context) Wait() error {
	if c.isBackground {
		return nil
	}
	return c.inner.Wait()
}

// With returns a [Context] that delegates [context.Context] behavior to
// ctx while sharing the same stopper lifecycle.
//
// Deprecated: Use [v2.Context.WithDelegate] instead.
func (c *Context) With(ctx context.Context) *Context {
	return &Context{inner: c.inner.WithDelegate(ctx), isBackground: c.isBackground}
}

// WithContext creates a new [Context] whose work will be canceled when
// the parent context is canceled.
//
// Deprecated: Use [v2.WithContext] instead.
func WithContext(ctx context.Context) *Context {
	parent := From(ctx)
	return &Context{inner: v2.WithContext(ctx), invokers: parent.invokers}
}

// WithInvoker creates a child [Context] that applies the given
// [Invoker] middleware to every task launched from it.
//
// Deprecated: Use [v2.WithContext] with [v2.TaskMiddleware] instead.
func WithInvoker(ctx context.Context, inv Invoker) *Context {
	parent := From(ctx)
	invs := append(slices.Clone(parent.invokers), inv)
	return &Context{inner: v2.WithContext(ctx), invokers: invs}
}
