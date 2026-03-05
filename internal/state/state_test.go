// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	synctest.Test(t, func(t *testing.T) {
		r := require.New(t)

		cancelErr := make(chan error, 1)
		s := New(func(err error) { cancelErr <- err }, nil, nil)

		r.True(s.Apply(1))
		s.StopOnIdle(time.Hour)
		// Not yet idle, so not canceled.
		r.True(s.IsStopOnIdle())

		r.True(s.Apply(-1))

		err := <-cancelErr
		r.ErrorIs(err, ErrStopped)
	})
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
	s := New(func(error) {}, nil, nil)

	a.True(s.AddDeferred(func() error { order = append(order, 1); return nil }))
	a.True(s.AddDeferred(func() error { order = append(order, 2); return nil }))
	a.True(s.AddDeferred(func() error { return errors.New("boom") }))
	a.True(s.AddDeferred(func() error { order = append(order, 3); return nil }))

	// No active tasks, so this will immediately invoke the deferred
	// functions.
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

// This test checks that the state will move into a hard stop condition
// with an expected error state.
func TestStopStateTransitions(t *testing.T) {
	tcs := []struct {
		Grace  time.Duration
		Apply  bool
		Drain  bool
		Expect error
	}{
		{0, false, false, ErrStopped},
		{0, true, false, ErrGracePeriodExpired},
		{0, true, true, ErrGracePeriodExpired},

		{time.Hour, false, false, ErrStopped},
		{time.Hour, true, false, ErrGracePeriodExpired},
		{time.Hour, true, true, ErrStopped},
	}

	for idx, tc := range tcs {
		t.Run(fmt.Sprintf("%d", idx), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := require.New(t)
				now := time.Now()

				// Set up a State that records its cancellation error.
				cancelErr := make(chan error, 1)
				s := New(func(err error) { cancelErr <- err }, nil, nil)

				// Optionally, mark a task as running.
				if tc.Apply {
					r.True(s.Apply(1))
				}

				// Place the State into the soft-stop condition.
				s.Stop(tc.Grace)
				<-s.Stopping()
				r.True(s.IsStopping())

				// Jump halfway into the grace period if there's an open
				// task. Verify that the cancel function hasn't been
				// invoked yet.
				if tc.Apply && tc.Grace > 0 {
					time.Sleep(tc.Grace / 2)
					select {
					case <-cancelErr:
						r.Fail("should not be canceled yet")
					default:
					}
				}

				if tc.Drain {
					// Ensure we wouldn't over-release the State.
					r.True(tc.Apply)
					r.True(s.Apply(-1))
				}

				// Wait for the state to hard-stop and cancel out.
				err := <-cancelErr
				r.ErrorIs(err, tc.Expect)

				// Determine how much fake time has elapsed.
				delta := time.Since(now)
				if errors.Is(tc.Expect, ErrStopped) {
					r.LessOrEqual(delta, tc.Grace)
				} else if errors.Is(tc.Expect, ErrGracePeriodExpired) {
					r.GreaterOrEqual(delta, tc.Grace)
				}
			})
		})
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

func TestStopOnIdleWhenAlreadyIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := require.New(t)

		cancelErr := make(chan error, 1)
		s := New(func(err error) { cancelErr <- err }, nil, nil)

		// Count is already 0, so StopOnIdle should immediately stop.
		s.StopOnIdle(time.Hour)
		r.True(s.IsStopOnIdle())
		r.True(s.IsStopping())

		err := <-cancelErr
		r.ErrorIs(err, ErrStopped)
	})
}

func TestStopOnIdleWhenBusy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := require.New(t)

		cancelErr := make(chan error, 1)
		s := New(func(err error) { cancelErr <- err }, nil, nil)

		r.True(s.Apply(1))
		s.StopOnIdle(time.Hour)
		r.True(s.IsStopOnIdle())

		// Should not be stopping yet (count > 0).
		r.False(s.IsStopping())

		// Now become idle.
		r.True(s.Apply(-1))

		err := <-cancelErr
		r.ErrorIs(err, ErrStopped)
	})
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

func TestAddStopHookCalledOnStop(t *testing.T) {
	r := require.New(t)
	s := New(func(error) {}, nil, nil)
	called := false
	s.AddStopHook(func() { called = true })
	r.False(called)
	s.softStopLocked(time.Duration(0))
	r.True(called)
}

func TestAddStopHookCalledImmediatelyWhenAlreadyStopping(t *testing.T) {
	r := require.New(t)
	s := New(func(error) {}, nil, nil)
	s.softStopLocked(time.Duration(0))
	called := false
	s.AddStopHook(func() { called = true })() // Also check no-op cancel
	r.True(called)
}

func TestAddStopHookCancelRemovesHook(t *testing.T) {
	r := require.New(t)
	s := New(func(error) {}, nil, nil)
	called := false
	cancel := s.AddStopHook(func() { called = true })
	cancel()
	s.softStopLocked(time.Duration(0))
	r.False(called)
}

func TestAddStopHookCancelSafeAfterStop(t *testing.T) {
	r := require.New(t)
	s := New(func(error) {}, nil, nil)
	cancel := s.AddStopHook(func() {})
	s.softStopLocked(time.Duration(0))
	// Should not panic or deadlock.
	r.NotPanics(cancel)
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
