// Copyright 2023 The Cockroach Authors
// Copyright 2025 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

// Package stopper contains a utility for gracefully terminating
// long-running tasks within a Go program.
package stopper

import (
	"context"
	"errors"
	"sync"
	"time"
)

// contextKey is a [context.Context.Value] key.
type contextKey struct{}

// background is a Context that never stops.
var background = &Context{
	delegate: context.Background(),
	stopping: make(chan struct{}),
}

// ErrStopped will be returned from [context.Cause] when the Context has
// been stopped.
var ErrStopped = errors.New("stopped")

// ErrGracePeriodExpired will be returned from [context.Cause] when the
// Context has been stopped, but the goroutines have not exited.
var ErrGracePeriodExpired = errors.New("grace period expired")

// A Context is conceptually similar to an [errgroup.Group] in that it
// manages a [context.Context] whose lifecycle is associated with some
// number of goroutines. Rather than canceling the associated context
// when a goroutine returns an error, it cancels the context after the
// Stop method is called and all associated goroutines have all exited.
//
// As an API convenience, the Context type implements [context.Context]
// so that it fits into idiomatic context-plumbing.  The [From]
// function can be used to retrieve a Context from any
// [context.Context].
type Context struct {
	cancel   func(error) // Invoked via cancelLocked.
	delegate context.Context
	stopping chan struct{}
	parent   *Context

	mu struct {
		sync.RWMutex
		count    int
		deferred []func()
		err      error
		stopping bool
	}
}

var _ context.Context = (*Context)(nil)

// Background is analogous to [context.Background]. It returns a Context
// which cannot be stopped or canceled, but which is otherwise
// functional.
func Background() *Context { return background }

// From returns a pre-existing Context from the Context chain. Use
// [WithContext] to construct a new Context.
//
// If the chain is not associated with a Context, the [Background]
// instance will be returned.
func From(ctx context.Context) *Context {
	if s, ok := ctx.(*Context); ok {
		return s
	}
	if s := ctx.Value(contextKey{}); s != nil {
		return s.(*Context)
	}
	return Background()
}

// IsStopping is a convenience method to determine if a stopper is
// associated with a Context and if work should be stopped.
func IsStopping(ctx context.Context) bool {
	return From(ctx).IsStopping()
}

// WithContext creates a new Context whose work will be immediately
// canceled when the parent context is canceled. If the provided context
// is already managed by a Context, a call to the enclosing
// [Context.Stop] method will also trigger a call to Stop in the
// newly-constructed Context.
func WithContext(ctx context.Context) *Context {
	// Might be background, which never stops.
	parent := From(ctx)

	ctx, cancel := context.WithCancelCause(ctx)
	s := &Context{
		cancel:   cancel,
		delegate: ctx,
		parent:   parent,
		stopping: make(chan struct{}),
	}

	// Propagate a parent stop or context cancellation into a Stop call
	// to ensure that all notification channels are closed.
	go func() {
		select {
		case <-parent.Stopping():
		case <-s.Done():
		}
		s.Stop(0)
	}()
	return s
}

// Call executes the given function within the current goroutine and
// monitors its lifecycle. That is, both Call and Wait will block until
// the function has returned.
//
// Call returns any error from the function with no other side effects.
// Unlike the Go method, Call does not stop the Context if the function
// returns an error. If the Context has already been stopped,
// [ErrStopped] will be returned.
//
// The function passed to Call should prefer the [Context.Stopping]
// channel to return instead of depending on [Context.Done]. This allows
// a soft-stop, rather than waiting for the grace period to expire when
// [Context.Stop] is called.
func (c *Context) Call(fn func(ctx *Context) error) error {
	if !c.apply(1) {
		return ErrStopped
	}
	defer c.apply(-1)
	return fn(c)
}

// Deadline implements [context.Context].
func (c *Context) Deadline() (deadline time.Time, ok bool) { return c.delegate.Deadline() }

// Defer registers a callback that will be executed after the
// [Context.Done] channel is closed. This method can be used to clean up
// resources that are used by goroutines associated with the Context
// (e.g. closing database connections). The Context will already have
// been canceled by the time the callback is run, so its behaviors
// should be associated with [context.Background] or similar. Callbacks
// will be executed in a LIFO manner. If the Context has already
// stopped, the callback will be executed immediately.
//
// Calling this method on the Background context will panic, since that
// context can never be cancelled.
func (c *Context) Defer(fn func()) {
	if c == background {
		panic(errors.New("cannot call Context.Defer() on a background context"))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Err() != nil {
		fn()
		return
	}
	c.mu.deferred = append(c.mu.deferred, fn)
}

// Done implements [context.Context]. The channel that is returned will
// be closed when Stop has been called and all associated goroutines
// have exited. The returned channel will be closed immediately if the
// parent context (passed to [WithContext]) is canceled. Functions
// passed to [Context.Go] should prefer [Context.Stopping] instead.
func (c *Context) Done() <-chan struct{} { return c.delegate.Done() }

// Err implements context.Context. When the return value for this is
// [context.ErrCanceled], [context.Cause] will return [ErrStopped] if
// the context cancellation resulted from a call to Stop.
func (c *Context) Err() error { return c.delegate.Err() }

// Len returns the number of tasks being tracked by the Context. This
// includes tasks started by derived Contexts.
func (c *Context) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mu.count
}

// Go spawns a new goroutine to execute the given function and monitors
// its lifecycle.
//
// If the function returns an error, the Stop method will be called. The
// returned error will be available from Wait once the remaining
// goroutines have exited.
//
// This method will not execute the function and return false if Stop
// has already been called.
//
// The function passed to Go should prefer the [Context.Stopping]
// channel to return instead of depending on [Context.Done]. This allows
// a soft-stop, rather than waiting for the grace period to expire when
// [Context.Stop] is called.
func (c *Context) Go(fn func(ctx *Context) error) (accepted bool) {
	if !c.apply(1) {
		return false
	}

	go func() {
		defer c.apply(-1)
		if err := fn(c); err != nil {
			c.Stop(0)
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.mu.err == nil {
				c.mu.err = err
			}
		}
	}()
	return true
}

// IsStopping returns true once [Stop] has been called.  See also
// [Stopping] for a notification-based API.
func (c *Context) IsStopping() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mu.stopping
}

// Stop begins a graceful shutdown of the Context. When this method is
// called, the Stopping channel will be closed.  Once all goroutines
// started by Go have exited, the associated Context will be cancelled,
// thus closing the Done channel. If the gracePeriod is non-zero, the
// context will be forcefully cancelled if the goroutines have not
// exited within the given timeframe.
func (c *Context) Stop(gracePeriod time.Duration) {
	if c == background {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.mu.stopping {
		return
	}
	c.mu.stopping = true
	close(c.stopping)

	// Cancel the context if nothing's currently running.
	if c.mu.count == 0 {
		c.cancelLocked(ErrStopped)
	} else if gracePeriod > 0 {
		go func() {
			select {
			case <-time.After(gracePeriod):
				// Cancel after the grace period has expired. This
				// should immediately terminate any well-behaved
				// goroutines driven by Go().
				c.mu.Lock()
				defer c.mu.Unlock()
				c.cancelLocked(ErrGracePeriodExpired)
			case <-c.Done():
				// We'll hit this path in a clean-exit, where apply()
				// cancels the context after the last goroutine has
				// exited.
			}
		}()
	}
}

// Stopping returns a channel that is closed when a graceful shutdown
// has been requested or when a parent context has been stopped.
func (c *Context) Stopping() <-chan struct{} {
	return c.stopping
}

// Value implements context.Context.
func (c *Context) Value(key any) any {
	if _, ok := key.(contextKey); ok {
		return c
	}
	return c.delegate.Value(key)
}

// Wait will block until Stop has been called and all associated
// goroutines have exited or the parent context has been cancelled. This
// method will return the first, non-nil error from any of the callbacks
// passed to Go. If Wait is called on the [Background] instance, it will
// immediately return nil.
func (c *Context) Wait() error {
	if c == background {
		return nil
	}
	<-c.Done()

	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mu.err
}

// apply is used to maintain the count of started goroutines. It returns
// true if the delta was applied.
func (c *Context) apply(delta int) bool {
	if c == background {
		return true
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Don't allow new goroutines to be added when stopping.
	if c.mu.stopping && delta >= 0 {
		return false
	}

	// Ensure that nested jobs prolong the lifetime of the parent
	// context to prevent premature cancellation. Verify that the parent
	// accepted the delta in case it was just stopped, but our helper
	// goroutine hasn't yet called Stop on this instance.
	if !c.parent.apply(delta) {
		return false
	}

	c.mu.count += delta
	if c.mu.count < 0 {
		// Implementation error, not user problem.
		panic("over-released")
	}
	if c.mu.count == 0 && c.mu.stopping {
		c.cancelLocked(ErrStopped)
	}
	return true
}

// cancelLocked invokes the context-cancellation function and then
// executes any deferred callbacks.
func (c *Context) cancelLocked(err error) {
	c.cancel(err)
	for i := len(c.mu.deferred) - 1; i >= 0; i-- {
		c.mu.deferred[i]()
	}
	c.mu.deferred = nil
}
