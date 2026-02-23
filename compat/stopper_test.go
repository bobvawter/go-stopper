// Copyright 2023 The Cockroach Authors
// Copyright 2025 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAmbient(t *testing.T) {
	r := require.New(t)

	s := From(context.Background())
	r.Same(s, Background())

	r.False(IsStopping(context.Background()))
	s.Go(func(*Context) error { return nil })

	// Should be a no-op.
	s.Stop(0)
	r.Nil(s.Err())
	r.Nil(s.Wait())
}

func TestCancelOuter(t *testing.T) {
	r := require.New(t)

	top, cancelTop := context.WithCancel(context.Background())

	s := WithContext(top)

	s.Go(func(*Context) error { <-s.Done(); return nil })

	cancelTop()
	select {
	case <-s.Stopping():
	// Verify that canceling the top-level also closes the Stopping channel.
	case <-time.After(time.Second):
		r.Fail("timed out waiting for Stopping to close")
	}
	r.True(IsStopping(s))
	r.ErrorIs(s.Err(), context.Canceled)
	r.ErrorIs(context.Cause(s), context.Canceled)
	r.Nil(s.Wait())
}

func TestCall(t *testing.T) {
	r := require.New(t)

	s := WithContext(context.Background())

	// Verify that returning an error from the callback does not stop
	// the Context.
	err := errors.New("BOOM")
	r.ErrorIs(s.Call(func(ctx *Context) error {
		// The call should increment the wait value.
		r.Equal(1, s.Len())
		return err
	}), err)

	r.False(s.IsStopping())

	s.Stop(0)
	r.ErrorIs(
		s.Call(func(ctx *Context) error { return nil }),
		ErrStopped)
}

func TestCallbackErrorStops(t *testing.T) {
	r := require.New(t)

	s := WithContext(context.Background())
	err := errors.New("BOOM")
	s.Go(func(*Context) error { return err })
	r.ErrorIs(s.Wait(), err)
	r.Error(context.Cause(s), ErrStopped)
}

func TestChainStopper(t *testing.T) {
	r := require.New(t)

	parent := WithContext(context.Background())
	mid := context.WithValue(parent, parent, parent) // Demonstrate unwrapping.
	child := WithContext(mid)
	r.Zero(parent.Len())
	r.Zero(child.Len())

	waitFor := make(chan struct{})
	child.Go(func(*Context) error { <-waitFor; return nil })

	// Task tracking chains.
	r.Equal(1, parent.Len())
	r.Equal(1, child.Len())

	// Verify that stopping the parent propagates to the child.
	parent.Stop(0)
	select {
	case <-child.Stopping():
	// OK
	case <-time.After(time.Second):
		r.Fail("call to stop did not propagate")
	}

	// However, the contexts should not cancel until the work is done.
	r.Nil(parent.Err())
	r.Nil(child.Err())

	// There are still pending tasks.
	r.Equal(1, parent.Len())
	r.Equal(1, child.Len())

	// Allow the work to finish, and verify cancellation.
	close(waitFor)

	select {
	case <-child.Done():
	// OK
	case <-time.After(time.Second):
		r.Fail("timeout waiting for child to finish")
	}

	select {
	case <-mid.Done():
	// OK
	case <-time.After(time.Second):
		r.Fail("timeout waiting for mid to finish")
	}

	select {
	case <-parent.Done():
	// OK
	case <-time.After(time.Second):
		r.Fail("timeout waiting for parent to finish")
	}

	r.ErrorIs(child.Err(), context.Canceled)
	r.ErrorIs(context.Cause(child), ErrStopped)
	r.Nil(child.Wait())

	r.ErrorIs(mid.Err(), context.Canceled)
	r.ErrorIs(context.Cause(mid), ErrStopped)

	r.ErrorIs(parent.Err(), context.Canceled)
	r.ErrorIs(context.Cause(parent), ErrStopped)
	r.Nil(child.Wait())

	r.Zero(parent.Len())
	r.Zero(child.Len())
}

func TestDeadline(t *testing.T) {
	r := require.New(t)

	ctxCtx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer cancel()

	s := WithContext(ctxCtx)

	_, hasDeadline := s.Deadline()
	r.True(hasDeadline)
}

func TestDefer(t *testing.T) {
	r := require.New(t)

	var mu sync.Mutex
	var calls []string
	recordCall := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, s)
	}

	s := WithContext(context.Background())
	s.Defer(func() { recordCall("fifo_a") })
	s.Defer(func() { recordCall("fifo_b") })
	s.Go(func(s *Context) error {
		recordCall("fifo_c")
		s.Stop(time.Second)
		return nil
	})
	r.Nil(s.Wait())
	s.Defer(func() { recordCall("immediate_a") })
	s.Defer(func() { recordCall("immediate_b") })

	mu.Lock()
	defer mu.Unlock()
	r.Equal([]string{"fifo_c", "fifo_b", "fifo_a", "immediate_a", "immediate_b"}, calls)

	r.PanicsWithError("cannot call Context.Defer() on a background context", func() {
		Background().Defer(func() {})
	})
}

func TestGracePeriod(t *testing.T) {
	r := require.New(t)

	s := WithContext(context.Background())

	// This goroutine waits on Done, which is not correct.
	s.Go(func(s *Context) error { <-s.Done(); return nil })

	s.Stop(time.Nanosecond)

	<-s.Done()
	r.ErrorIs(s.Err(), context.Canceled)
	r.ErrorIs(context.Cause(s), ErrGracePeriodExpired)
}

// v1-to-v2: TestOverRelease removed — it tested the unexported apply() method.

func TestParentAlreadyStopping(t *testing.T) {
	r := require.New(t)

	parent := WithContext(context.Background())
	child := WithContext(parent)

	parent.Stop(0)

	r.False(child.Go(func(*Context) error { return nil }))
}

func TestStopper(t *testing.T) {
	r := require.New(t)

	s := WithContext(context.Background())
	// v1-to-v2: From() wraps the v2.Context in a new compat *Context,
	// so pointer identity (Same) no longer holds. Use NotNil instead.
	r.NotNil(From(s))
	r.NotNil(From(context.WithValue(s, s, s)))
	select {
	case <-s.Stopping():
		r.Fail("should not be stopping yet")
	default:
		// OK
	}
	r.False(IsStopping(s))

	waitFor := make(chan struct{})
	r.True(s.Go(func(*Context) error { <-waitFor; return nil }))
	r.True(s.Go(func(*Context) error { return nil }))

	s.Stop(0)
	select {
	case <-s.Stopping():
	// OK
	case <-time.After(time.Second):
		r.Fail("timeout waiting for stopped")
	}

	// Verify that the context is stopping, but not cancelled.
	r.True(IsStopping(s))
	r.Nil(s.Err())

	// It's a no-op to run new routines after stopping.
	r.False(s.Go(func(*Context) error { return nil }))

	// Stop the waiting goroutines.
	close(waitFor)

	// Once all workers have stopped, the context should cancel.
	select {
	case <-s.Done():
	// OK
	case <-time.After(time.Second):
		r.Fail("timeout waiting for Context.Done()")
	}
	r.True(IsStopping(s))
	r.NotNil(s.Err())
	r.ErrorIs(context.Cause(s), ErrStopped)
	r.Nil(s.Wait())
}

func TestStopWhenEmpty(t *testing.T) {
	r := require.New(t)

	s := WithContext(context.Background())
	s.Go(func(s *Context) error {
		s.StopOnIdle()
		return nil
	})

	select {
	case <-s.Done():
	//OK
	case <-time.After(time.Second):
		r.Fail("timeout waiting for Context.Done()")
	}
}

func TestStopWhenAlreadyEmpty(t *testing.T) {
	r := require.New(t)

	s := WithContext(context.Background())
	s.StopOnIdle()

	select {
	case <-s.Done():
	//OK
	case <-time.After(time.Second):
		r.Fail("timeout waiting for Context.Done()")
	}
}

func TestStopWhenEmptyBackground(t *testing.T) {
	r := require.New(t)
	s := Background()
	s.StopOnIdle()
	// v1-to-v2: replaced unexported mu.stopOnIdle check with IsStopping();
	// StopOnIdle is a no-op on background, just verify no panic.
	r.False(s.IsStopping())
}

// Verify that a never-used Stopper behaves correctly.
func TestUnused(t *testing.T) {
	r := require.New(t)

	s := WithContext(context.Background())
	s.Stop(0)
	select {
	case <-s.Done():
	// OK
	case <-time.After(time.Second):
		r.Fail("timeout waiting for Context.Done()")
	}
	r.ErrorIs(context.Cause(s), ErrStopped)
	r.Nil(s.Wait())
}

func TestWith(t *testing.T) {
	r := require.New(t)

	s := WithContext(context.Background())

	type k string

	c1 := context.WithValue(context.Background(), k("foo"), "bar")
	c2 := context.WithValue(context.Background(), k("baz"), "quux")

	s.With(c1).Go(func(ctx *Context) error {
		r.NotSame(s, ctx)
		r.Equal("bar", ctx.Value(k("foo")))
		return nil
	})
	s.With(c2).Go(func(ctx *Context) error {
		r.NotSame(s, ctx)
		r.Equal("quux", ctx.Value(k("baz")))
		return nil
	})

	s.Stop(time.Second)
	r.NoError(s.Wait())
}

func TestWithBackground(t *testing.T) {
	r := require.New(t)

	bg := Background()
	ch := make(chan *Context, 1)

	type k string
	w := bg.With(context.WithValue(bg, k("foo"), "bar"))
	r.NotSame(bg, w)
	r.Equal("bar", w.Value(k("foo")))
	r.PanicsWithError("cannot call Context.Defer() on a background context", func() {
		w.Defer(func() {})
	})
	w.Go(func(ctx *Context) error {
		ch <- ctx
		return errors.New("no effect")
	})

	select {
	case <-time.After(30 * time.Second):
		r.Fail("timeout")
	case found := <-ch:
		r.NotSame(Background(), found)
	}
}

func TestWithInvoker(t *testing.T) {
	r := require.New(t)

	var called atomic.Bool
	ctx := WithInvoker(context.Background(), func(fn Func) Func {
		return func(ctx *Context) error {
			called.Store(true)
			return fn(ctx)
		}
	})
	r.NoError(ctx.Call(func(ctx *Context) error { return nil }))
	r.True(called.Load())

	called.Store(false)
	ctx.Go(func(ctx *Context) error { return nil })
	ctx.Stop(30 * time.Second)
	r.NoError(ctx.Wait())
	r.True(called.Load())
}
