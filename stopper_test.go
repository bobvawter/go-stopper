// Copyright 2023 The Cockroach Authors
// Copyright 2025 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCancelOuter(t *testing.T) {
	a := assert.New(t)

	top, cancelTop := context.WithCancel(t.Context())

	s := WithContext(top)

	a.NoError(s.Go(func(Context) error { <-s.Done(); return nil }))

	cancelTop()
	select {
	case <-s.Stopping():
	// Verify that canceling the top-level also closes the Stopping channel.
	case <-time.After(time.Second):
		a.Fail("timed out waiting for Stopping to close")
	}
	a.True(IsStopping(s))
	a.ErrorIs(s.Err(), context.Canceled)
	a.ErrorIs(context.Cause(s), context.Canceled)
	a.Nil(s.Wait())
}

func TestCall(t *testing.T) {
	a := assert.New(t)

	s := New()

	// Verify that returning an error from the callback does not stop
	// the Context.
	err := errors.New("BOOM")
	a.ErrorIs(s.Call(func(ctx Context) error {
		// The call should increment the wait value.
		a.Equal(1, s.Len())
		return err
	}), err)

	a.False(s.IsStopping())

	s.Stop()
	a.ErrorIs(
		s.Call(func(ctx Context) error { return nil }),
		ErrStopped)
}

func TestCallbackErrorStops(t *testing.T) {
	a := assert.New(t)

	s := New()
	err := errors.New("BOOM")
	a.NoError(s.Go(func(Context) error { return err }))
	a.ErrorIs(s.Wait(), err)
	a.Error(context.Cause(s), ErrStopped)
}

func TestChainStopper(t *testing.T) {
	a := assert.New(t)

	parent := New()
	mid := context.WithValue(parent, parent, parent) // Demonstrate unwrapping.
	child := WithContext(mid)
	a.Same(parent.(*impl).st, child.(*impl).st.Parent())
	a.Zero(parent.Len())
	a.Zero(child.Len())

	waitFor := make(chan struct{})
	a.NoError(child.Go(func(Context) error { <-waitFor; return nil }))

	// Task tracking chains.
	a.Equal(1, parent.Len())
	a.Equal(1, child.Len())

	// Verify that stopping the parent propagates to the child.
	parent.Stop()
	select {
	case <-child.Stopping():
	// OK
	case <-time.After(time.Second):
		a.Fail("call to stop did not propagate")
	}

	// However, the contexts should not cancel until the work is done.
	a.Nil(parent.Err())
	a.Nil(child.Err())

	// There are still pending tasks.
	a.Equal(1, parent.Len())
	a.Equal(1, child.Len())

	// Allow the work to finish, and verify cancellation.
	close(waitFor)

	select {
	case <-child.Done():
	// OK
	case <-time.After(time.Second):
		a.Fail("timeout waiting for child to finish")
	}

	select {
	case <-mid.Done():
	// OK
	case <-time.After(time.Second):
		a.Fail("timeout waiting for mid to finish")
	}

	select {
	case <-parent.Done():
	// OK
	case <-time.After(time.Second):
		a.Fail("timeout waiting for parent to finish")
	}

	a.ErrorIs(child.Err(), context.Canceled)
	a.ErrorIs(context.Cause(child), ErrStopped)
	a.Nil(child.Wait())

	a.ErrorIs(mid.Err(), context.Canceled)
	a.ErrorIs(context.Cause(mid), ErrStopped)

	a.ErrorIs(parent.Err(), context.Canceled)
	a.ErrorIs(context.Cause(parent), ErrStopped)
	a.Nil(child.Wait())

	a.Zero(parent.Len())
	a.Zero(child.Len())
}

func TestChildOfStoppedParent(t *testing.T) {
	r := require.New(t)
	parent := New()
	parent.Stop()
	r.NoError(parent.Wait())

	child := WithContext(parent)
	r.True(child.IsStopping())
	select {
	case <-child.Done():
	// OK
	default:
		r.Fail("child of stopped parent should already be done")
	}
	r.ErrorIs(child.Err(), context.Canceled)
	r.ErrorIs(context.Cause(child), ErrStopped)
}

func TestConfigInherit(t *testing.T) {
	r := require.New(t)

	parent := New(
		WithName("parent"),
		WithGracePeriod(time.Second),
	)

	childA := WithContext(parent,
		WithName("childA"),
		WithNoInherit(),
	)

	childB := WithContext(parent,
		WithName("childB"),
	)

	pCfg := parent.(*impl).config()
	r.Equal(time.Second, *pCfg.gracePeriod)

	cfgA := childA.(*impl).config()
	r.Equal(DefaultGracePeriod, *cfgA.gracePeriod)
	r.Equal("childA", cfgA.name)

	cfgB := childB.(*impl).config()
	r.Equal(pCfg.gracePeriod, cfgB.gracePeriod)
	r.Equal("parent.childB", cfgB.name)
}

func TestDeadline(t *testing.T) {
	a := assert.New(t)

	ctxCtx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Hour))
	defer cancel()

	s := WithContext(ctxCtx)

	_, hasDeadline := s.Deadline()
	a.True(hasDeadline)
}

func TestErrorHandlerRecord(t *testing.T) {
	r := require.New(t)
	s := WithContext(t.Context(),
		WithTaskOptions(
			TaskErrHandler(ErrorHandlerRecord),
		))

	// The handler should not be used for Call.
	err := Call(s, func() error { return errors.New("boom") })
	r.ErrorContains(err, "boom")

	// Ensure the handler is used for Go.
	r.NoError(Go(s, func() error { return errors.New("boom") }))

	// Spin until idle.
	for s.Len() > 0 {
		time.Sleep(time.Millisecond)
	}
	r.False(s.IsStopping())
	s.Stop()
	r.ErrorContains(s.Wait(), "boom")
}

func TestFromBackground(t *testing.T) {
	r := require.New(t)
	found, ok := From(t.Context())
	r.False(ok)
	r.Nil(found)
}

func TestIsStoppingOther(t *testing.T) {
	r := require.New(t)
	r.False(IsStopping(t.Context()))
}

func TestNoGoroutinesOnIdle(t *testing.T) {
	r := require.New(t)

	// Allow any background goroutines to settle before we take a baseline.
	runtime.Gosched()
	before := runtime.NumGoroutine()

	s := New()

	// Make sure that creating a stopper doesn't increase the count.
	runtime.Gosched()
	r.Equal(runtime.NumGoroutine(), before)

	// We do expect to see another goroutine now.
	r.NoError(Go(s, func() {
		<-s.Stopping()
	}))
	r.Greater(runtime.NumGoroutine(), before)

	// The goroutine should terminate before Wait() returns.
	s.Stop()
	r.NoError(s.Wait())
	r.Equal(runtime.NumGoroutine(), before)
}

func TestStopper(t *testing.T) {
	a := assert.New(t)

	s := New(WithName("tester"))
	// Verify debugging output.
	a.Equal("tester: (0 tasks) (0 errors) (stopping=false)", fmt.Sprintf("%s", s))

	found, ok := From(s) // Direct cast
	a.True(ok)
	a.Same(s, found)
	found, ok = From(context.WithValue(s, s, s)) // Unwrapping
	a.True(ok)
	a.Same(s, found)
	select {
	case <-s.Stopping():
		a.Fail("should not be stopping yet")
	default:
		// OK
	}
	a.False(IsStopping(s))

	waitFor := make(chan struct{})
	a.NoError(s.Go(func(Context) error { <-waitFor; return nil }))
	a.Equal("tester: (1 tasks) (0 errors) (stopping=false)", fmt.Sprintf("%s", s))
	a.NoError(s.Go(func(Context) error { return nil }))

	s.Stop()
	select {
	case <-s.Stopping():
	// OK
	case <-time.After(time.Second):
		a.Fail("timeout waiting for stopped")
	}

	// Verify that the context is stopping but not canceled.
	a.True(IsStopping(s))
	a.Nil(s.Err())

	// It's a no-op to run new routines after stopping.
	a.ErrorIs(s.Go(func(Context) error { return nil }), ErrStopped)

	// Stop the waiting goroutines.
	close(waitFor)

	// Once all workers have stopped, the context should cancel.
	select {
	case <-s.Done():
	// OK
	case <-time.After(time.Second):
		a.Fail("timeout waiting for Context.Done()")
	}
	a.True(IsStopping(s))
	a.NotNil(s.Err())
	a.ErrorIs(context.Cause(s), ErrStopped)
	a.Nil(s.Wait())
	a.Equal("tester: (0 tasks) (0 errors) (stopping=true)", fmt.Sprintf("%s", s))
}

func TestStopWhenEmpty(t *testing.T) {
	a := assert.New(t)

	s := New()
	a.NoError(s.Go(func(s Context) error {
		s.Stop(StopOnIdle())
		return nil
	}))

	select {
	case <-s.Done():
	//OK
	case <-time.After(time.Second):
		a.Fail("timeout waiting for Context.Done()")
	}
}

func TestWith(t *testing.T) {
	a := assert.New(t)

	s := New()

	type k string

	c1 := context.WithValue(t.Context(), k("foo"), "bar")
	c2 := context.WithValue(t.Context(), k("baz"), "quux")

	a.NoError(Go(s.WithDelegate(c1), func(ctx Context) error {
		a.NotSame(s, ctx)
		a.Same(s.(*impl).st, ctx.(*impl).st)
		a.Equal("bar", ctx.Value(k("foo")))
		return nil
	}))
	a.NoError(Go(s.WithDelegate(c2), func(ctx Context) error {
		a.NotSame(s, ctx)
		a.Same(s.(*impl).st, ctx.(*impl).st)
		a.Equal("quux", ctx.Value(k("baz")))
		return nil
	}))

	s.Stop(StopGracePeriod(time.Second))
	a.NoError(s.Wait())
}

func TestPanicHandlerError(t *testing.T) {
	r := require.New(t)
	s := New()

	err := s.Call(
		func(ctx Context) error {
			panic(net.ErrClosed)
		},
		TaskName("tester"),
	)
	r.ErrorContains(err, "tester: recovered: "+net.ErrClosed.Error())
	r.ErrorIs(err, net.ErrClosed)

	var recovered *RecoveredError
	r.ErrorAs(err, &recovered)
	r.NotZero(len(recovered.Stack))
	t.Log(recovered.String())
}

func TestPanicHandlerString(t *testing.T) {
	r := require.New(t)
	s := New()

	err := s.Call(
		func(ctx Context) error {
			panic("boom!")
		},
		TaskName("tester"),
	)
	r.ErrorContains(err, "tester: recovered: boom!")

	var recovered *RecoveredError
	r.ErrorAs(err, &recovered)
	r.NotZero(len(recovered.Stack))
	t.Log(recovered.String())
}

func TestWaitInterrupt(t *testing.T) {
	r := require.New(t)
	ctx := New()

	block := make(chan struct{})
	defer close(block)
	r.NoError(Go(ctx, func() { <-block }))

	stdCtx, cancel := context.WithCancel(ctx)
	cancel()
	r.ErrorIs(ctx.WaitCtx(stdCtx), context.Canceled)
}

func TestWithMiddleware(t *testing.T) {
	r := require.New(t)

	var called atomic.Bool
	ctx := WithContext(t.Context(),
		WithTaskOptions(TaskMiddleware(
			func(outer Context) (Context, Invoker) {
				return outer, func(ctx Context, task Func) error {
					called.Store(true)
					return task(ctx)
				}
			},
		)),
	)
	r.NoError(ctx.Call(func(ctx Context) error { return nil }))
	r.True(called.Load())

	called.Store(false)
	r.NoError(ctx.Go(func(ctx Context) error { return nil }))
	ctx.Stop(StopGracePeriod(30 * time.Second))
	r.NoError(ctx.Wait())
	r.True(called.Load())
}
