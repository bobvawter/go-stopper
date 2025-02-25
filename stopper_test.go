// Copyright 2023 The Cockroach Authors
// Copyright 2025 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAmbient(t *testing.T) {
	a := assert.New(t)

	s := From(context.Background())
	a.Same(s, background)

	a.False(IsStopping(context.Background()))
	s.Go(func(*Context) error { return nil })

	// Should be a no-op.
	s.Stop(0)
	a.False(s.mu.stopping)
	a.Nil(s.Err())
	a.Nil(s.Wait())
}

func TestCancelOuter(t *testing.T) {
	a := assert.New(t)

	top, cancelTop := context.WithCancel(context.Background())

	s := WithContext(top)

	s.Go(func(*Context) error { <-s.Done(); return nil })

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

	s := WithContext(context.Background())

	// Verify that returning an error from the callback does not stop
	// the Context.
	err := errors.New("BOOM")
	a.ErrorIs(s.Call(func(ctx *Context) error {
		// The call should increment the wait value.
		a.Equal(1, s.Len())
		return err
	}), err)

	a.False(s.IsStopping())

	s.Stop(0)
	a.ErrorIs(
		s.Call(func(ctx *Context) error { return nil }),
		ErrStopped)
}

func TestCallbackErrorStops(t *testing.T) {
	a := assert.New(t)

	s := WithContext(context.Background())
	err := errors.New("BOOM")
	s.Go(func(*Context) error { return err })
	a.ErrorIs(s.Wait(), err)
	a.Error(context.Cause(s), ErrStopped)
}

func TestChainStopper(t *testing.T) {
	a := assert.New(t)

	parent := WithContext(context.Background())
	mid := context.WithValue(parent, parent, parent) // Demonstrate unwrapping.
	child := WithContext(mid)
	a.Same(parent, child.parent)
	a.Zero(parent.Len())
	a.Zero(child.Len())

	waitFor := make(chan struct{})
	child.Go(func(*Context) error { <-waitFor; return nil })

	// Task tracking chains.
	a.Equal(1, parent.Len())
	a.Equal(1, child.Len())

	// Verify that stopping the parent propagates to the child.
	parent.Stop(0)
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

func TestDefer(t *testing.T) {
	a := assert.New(t)

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
	a.Nil(s.Wait())
	s.Defer(func() { recordCall("immediate_a") })
	s.Defer(func() { recordCall("immediate_b") })

	mu.Lock()
	defer mu.Unlock()
	a.Equal([]string{"fifo_c", "fifo_b", "fifo_a", "immediate_a", "immediate_b"}, calls)

	a.PanicsWithError("cannot call Context.Defer() on a background context", func() {
		Background().Defer(func() {})
	})
}

func TestGracePeriod(t *testing.T) {
	a := assert.New(t)

	s := WithContext(context.Background())

	// This goroutine waits on Done, which is not correct.
	s.Go(func(s *Context) error { <-s.Done(); return nil })

	s.Stop(time.Nanosecond)

	<-s.Done()
	a.ErrorIs(s.Err(), context.Canceled)
	a.ErrorIs(context.Cause(s), ErrGracePeriodExpired)
}

func TestStopper(t *testing.T) {
	a := assert.New(t)

	s := WithContext(context.Background())
	a.Same(s, From(s))                          // Direct cast
	a.Same(s, From(context.WithValue(s, s, s))) // Unwrapping
	select {
	case <-s.Stopping():
		a.Fail("should not be stopping yet")
	default:
		// OK
	}
	a.False(IsStopping(s))

	waitFor := make(chan struct{})
	a.True(s.Go(func(*Context) error { <-waitFor; return nil }))
	a.True(s.Go(func(*Context) error { return nil }))

	s.Stop(0)
	select {
	case <-s.Stopping():
	// OK
	case <-time.After(time.Second):
		a.Fail("timeout waiting for stopped")
	}

	// Verify that the context is stopping, but not cancelled.
	a.True(IsStopping(s))
	a.Nil(s.Err())

	// It's a no-op to run new routines after stopping.
	a.False(s.Go(func(*Context) error { return nil }))

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
}

// Verify that a never-used Stopper behaves correctly.
func TestUnused(t *testing.T) {
	a := assert.New(t)

	s := WithContext(context.Background())
	s.Stop(0)
	select {
	case <-s.Done():
	// OK
	case <-time.After(time.Second):
		a.Fail("timeout waiting for Context.Done()")
	}
	a.ErrorIs(context.Cause(s), ErrStopped)
	a.Nil(s.Wait())
}
