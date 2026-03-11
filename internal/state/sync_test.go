// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

//go:build go1.25

package state

import (
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

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
