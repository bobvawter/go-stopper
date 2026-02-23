// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	a := assert.New(t)

	cfg := "my-config"
	canceled := false
	s := New(func(error) { canceled = true }, cfg, nil)

	a.NotNil(s)
	a.Equal(cfg, s.Config())
	a.Nil(s.Parent())
	a.False(s.IsStopping())
	a.False(s.IsStopOnIdle())
	a.Zero(s.Len())
	a.Nil(s.Errors())
	a.False(canceled)
}

func TestApplyBasic(t *testing.T) {
	a := assert.New(t)

	s := New(func(error) {}, nil, nil)

	a.True(s.Apply(1))
	a.Equal(1, s.Len())

	a.True(s.Apply(1))
	a.Equal(2, s.Len())

	a.True(s.Apply(-1))
	a.Equal(1, s.Len())

	a.True(s.Apply(-1))
	a.Equal(0, s.Len())
}

func TestApplyRejectsWhenStopping(t *testing.T) {
	a := assert.New(t)

	s := New(func(error) {}, nil, nil)
	s.Stop(0)
	a.True(s.IsStopping())

	// Positive delta should be rejected when stopping.
	a.False(s.Apply(1))
	// Zero delta should also be rejected.
	a.False(s.Apply(0))
}

func TestApplyRejectsWhenParentStopping(t *testing.T) {
	a := assert.New(t)

	parent := New(func(error) {}, nil, nil)
	child := New(func(error) {}, nil, parent)

	parent.Stop(0)

	// Child should reject because parent is stopping.
	a.False(child.Apply(1))
}

func TestApplyOverReleasePanics(t *testing.T) {
	a := assert.New(t)

	s := New(func(error) {}, nil, nil)
	a.True(s.Apply(1))

	a.Panics(func() {
		s.Apply(-2)
	})
}

func TestApplyTriggersStopOnIdle(t *testing.T) {
	a := assert.New(t)

	cancelErr := make(chan error, 1)
	s := New(func(err error) { cancelErr <- err }, nil, nil)

	a.True(s.Apply(1))
	s.StopOnIdle(0)
	// Not yet idle, so not canceled.
	a.True(s.IsStopOnIdle())

	a.True(s.Apply(-1))

	select {
	case err := <-cancelErr:
		a.ErrorIs(err, ErrStopped)
	case <-time.After(time.Second):
		a.Fail("timed out waiting for cancel")
	}
}

func TestApplyCancelsWhenStoppingAndCountReachesZero(t *testing.T) {
	a := assert.New(t)

	cancelErr := make(chan error, 1)
	s := New(func(err error) { cancelErr <- err }, nil, nil)

	a.True(s.Apply(1))
	s.Stop(time.Hour)

	// Decrement to zero should trigger cancel.
	a.True(s.Apply(-1))

	select {
	case err := <-cancelErr:
		a.ErrorIs(err, ErrStopped)
	case <-time.After(time.Second):
		a.Fail("timed out waiting for cancel")
	}
}

func TestAddErrors(t *testing.T) {
	a := assert.New(t)

	s := New(func(error) {}, nil, nil)

	// Adding nil errors should be a no-op.
	s.AddErrors(nil, nil)
	a.Nil(s.Errors())

	err1 := errors.New("err1")
	err2 := errors.New("err2")
	s.AddErrors(err1, nil, err2)
	a.Equal([]error{err1, err2}, s.Errors())

	// Adding more errors appends.
	err3 := errors.New("err3")
	s.AddErrors(err3)
	a.Len(s.Errors(), 3)
}

func TestAddDeferredExecutesOnAlreadyStopped(t *testing.T) {
	a := assert.New(t)

	s := New(func(error) {}, nil, nil)
	// Stop with zero count so it immediately cancels.
	s.Stop(0)

	executed := false
	a.False(s.AddDeferred(func() error { executed = true; return nil }))
	a.True(executed)
}

func TestAddDeferredAppendsWhenNotStopped(t *testing.T) {
	a := assert.New(t)

	executed := false
	cancelErr := make(chan error, 1)
	s := New(func(err error) { cancelErr <- err }, nil, nil)

	a.True(s.AddDeferred(func() error { executed = true; return nil }))
	a.False(executed, "should not execute immediately when not stopped")

	// Now stop to trigger deferred callbacks.
	s.Stop(0)
	a.True(executed, "deferred callback should have executed on stop")
}

func TestAddDeferredPanicsCaptured(t *testing.T) {
	a := assert.New(t)

	s := New(func(error) {}, nil, nil)
	s.Stop(0)

	// AddDeferred on stopped state should capture panic as error.
	a.False(s.AddDeferred(func() error { panic(errors.New("boom")) }))
	a.Len(s.Errors(), 1)
	a.ErrorContains(s.Errors()[0], "boom")
}

func TestAddDeferredRunsInReverseOrder(t *testing.T) {
	a := assert.New(t)

	var order []int
	cancelErr := make(chan error, 1)
	s := New(func(err error) { cancelErr <- err }, nil, nil)

	a.True(s.AddDeferred(func() error { order = append(order, 1); return nil }))
	a.True(s.AddDeferred(func() error { order = append(order, 2); return nil }))
	a.True(s.AddDeferred(func() error { return errors.New("boom") }))
	a.True(s.AddDeferred(func() error { order = append(order, 3); return nil }))

	s.Stop(0)
	a.Equal([]int{3, 2, 1}, order)
	a.ErrorContains(s.Errors()[0], "boom")
}

func TestConfig(t *testing.T) {
	a := assert.New(t)

	s := New(func(error) {}, nil, nil)
	a.Nil(s.Config())

	cfg := struct{ Name string }{"test"}
	s2 := New(func(error) {}, cfg, nil)
	a.Equal(cfg, s2.Config())
}

func TestConfigs(t *testing.T) {
	a := assert.New(t)

	cfgA := "configA"
	cfgB := "configB"
	cfgC := "configC"

	grandparent := New(func(error) {}, cfgA, nil)
	parent := New(func(error) {}, nil, grandparent) // nil config, should be skipped
	child := New(func(error) {}, cfgB, parent)
	grandchild := New(func(error) {}, cfgC, child)

	configs := slices.Collect(grandchild.Configs())
	// Should yield from root to leaf, skipping nils.
	a.Equal([]any{cfgA, cfgB, cfgC}, configs)
}

func TestConfigsEmpty(t *testing.T) {
	a := assert.New(t)

	s := New(func(error) {}, nil, nil)
	configs := slices.Collect(s.Configs())
	a.Empty(configs)
}

func TestConfigsEarlyBreak(t *testing.T) {
	a := assert.New(t)

	s1 := New(func(error) {}, "a", nil)
	s2 := New(func(error) {}, "b", s1)

	var first any
	for cfg := range s2.Configs() {
		first = cfg
		break
	}
	a.Equal("a", first)
}

func TestStopWithZeroCount(t *testing.T) {
	a := assert.New(t)

	cancelErr := make(chan error, 1)
	s := New(func(err error) { cancelErr <- err }, nil, nil)

	s.Stop(0)
	a.True(s.IsStopping())

	select {
	case err := <-cancelErr:
		a.ErrorIs(err, ErrStopped)
	case <-time.After(time.Second):
		a.Fail("timed out waiting for cancel")
	}
}

func TestStopWithGracePeriod(t *testing.T) {
	a := assert.New(t)

	cancelErr := make(chan error, 1)
	s := New(func(err error) { cancelErr <- err }, nil, nil)

	a.True(s.Apply(1))
	s.Stop(50 * time.Millisecond)
	a.True(s.IsStopping())

	select {
	case err := <-cancelErr:
		a.ErrorIs(err, ErrGracePeriodExpired)
	case <-time.After(time.Second):
		a.Fail("timed out waiting for grace period expiry")
	}
}

func TestStopWithGraceNegative(t *testing.T) {
	a := assert.New(t)

	cancelErr := make(chan error, 1)
	s := New(func(err error) { cancelErr <- err }, nil, nil)

	a.True(s.Apply(1))
	s.Stop(-1)
	a.True(s.IsStopping())

	select {
	case err := <-cancelErr:
		a.ErrorIs(err, ErrGracePeriodExpired)
	case <-time.After(time.Second):
		a.Fail("timed out waiting for grace period expiry")
	}
}

func TestStopWithGracePeriodCleanExit(t *testing.T) {
	a := assert.New(t)

	cancelErr := make(chan error, 1)
	s := New(func(err error) { cancelErr <- err }, nil, nil)

	a.True(s.Apply(1))
	s.Stop(10 * time.Second)

	// Simulate clean exit before grace period expires.
	a.True(s.Apply(-1))

	select {
	case err := <-cancelErr:
		a.ErrorIs(err, ErrStopped)
	case <-time.After(time.Second):
		a.Fail("timed out waiting for cancel")
	}
}

func TestCancelIdempotent(t *testing.T) {
	a := assert.New(t)
	cancelCount := 0
	s := New(func(error) { cancelCount++ }, nil, nil)

	s.callDeferred(s.hardStopLocked(ErrStopped))
	s.callDeferred(s.hardStopLocked(ErrStopped))

	a.Equal(1, cancelCount, "cancel should only be called once")
}

func TestStopIdempotent(t *testing.T) {
	a := assert.New(t)

	cancelCount := 0
	s := New(func(error) { cancelCount++ }, nil, nil)

	s.Stop(0)
	s.Stop(0)
	s.Stop(0)

	a.Equal(1, cancelCount, "cancel should only be called once")
}

func TestStoppingChannel(t *testing.T) {
	a := assert.New(t)

	s := New(func(error) {}, nil, nil)
	ch := s.Stopping()

	select {
	case <-ch:
		a.Fail("stopping channel should not be closed yet")
	default:
		// OK
	}

	s.Stop(0)

	select {
	case <-ch:
		// OK
	case <-time.After(time.Second):
		a.Fail("stopping channel should be closed after Stop")
	}
}

func TestStopOnIdleWhenAlreadyIdle(t *testing.T) {
	a := assert.New(t)

	cancelErr := make(chan error, 1)
	s := New(func(err error) { cancelErr <- err }, nil, nil)

	// Count is already 0, so StopOnIdle should immediately stop.
	s.StopOnIdle(0)
	a.True(s.IsStopOnIdle())
	a.True(s.IsStopping())

	select {
	case err := <-cancelErr:
		a.ErrorIs(err, ErrStopped)
	case <-time.After(time.Second):
		a.Fail("timed out waiting for cancel")
	}
}

func TestStopOnIdleWhenBusy(t *testing.T) {
	a := assert.New(t)

	cancelErr := make(chan error, 1)
	s := New(func(err error) { cancelErr <- err }, nil, nil)

	a.True(s.Apply(1))
	s.StopOnIdle(0)

	// Should not be stopping yet (count > 0).
	a.False(s.IsStopping())

	// Now become idle.
	a.True(s.Apply(-1))

	select {
	case err := <-cancelErr:
		a.ErrorIs(err, ErrStopped)
	case <-time.After(time.Second):
		a.Fail("timed out waiting for cancel")
	}
}

func TestParent(t *testing.T) {
	a := assert.New(t)

	parent := New(func(error) {}, nil, nil)
	child := New(func(error) {}, nil, parent)

	a.Same(parent, child.Parent())
	a.Nil(parent.Parent())
}

func TestApplyPropagatesToParent(t *testing.T) {
	a := assert.New(t)

	parent := New(func(error) {}, nil, nil)
	child := New(func(error) {}, nil, parent)

	a.True(child.Apply(1))
	a.Equal(1, child.Len())
	a.Equal(1, parent.Len())

	a.True(child.Apply(-1))
	a.Equal(0, child.Len())
	a.Equal(0, parent.Len())
}

func TestErrors(t *testing.T) {
	a := assert.New(t)

	s := New(func(error) {}, nil, nil)
	a.Nil(s.Errors())

	s.AddErrors()
	a.Nil(s.Errors())

	err := errors.New("test error")
	s.AddErrors(err)
	a.Equal([]error{err}, s.Errors())
}

func TestErrStopped(t *testing.T) {
	a := assert.New(t)
	a.EqualError(ErrStopped, "stopped")
}

func TestErrGracePeriodExpired(t *testing.T) {
	a := assert.New(t)
	a.EqualError(ErrGracePeriodExpired, "grace period expired")
}

func TestAddDeferredWhileStoppingWithActiveCount(t *testing.T) {
	a := assert.New(t)

	cancelErr := make(chan error, 1)
	s := New(func(err error) { cancelErr <- err }, nil, nil)

	a.True(s.Apply(1))
	s.Stop(time.Hour)

	// State is stopping but count > 0, so deferred should be appended (not executed).
	executed := false
	a.True(s.AddDeferred(func() error { executed = true; return nil }))
	a.False(executed, "deferred should not execute while tasks are still running")

	// Now decrement to zero, which should trigger cancel and deferred callbacks.
	a.True(s.Apply(-1))
	a.True(executed, "deferred should have been executed after count reaches zero")
}
