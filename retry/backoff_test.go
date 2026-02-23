// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package retry

import (
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"vawter.tech/stopper/v2"
)

// TestBackoffDefaults verifies that the sanitize method applies
// reasonable defaults when fields are left at their zero values.
func TestBackoffDefaults(t *testing.T) {
	r := require.New(t)

	b := &Backoff{}
	s := b.sanitize()

	r.Equal(4, s.MaxAttempts)
	r.Equal(time.Second, s.MaxDelay)
	r.Equal(10*time.Millisecond, s.MinDelay)
	r.Equal(float32(10), s.Multiplier)
	r.NotNil(s.Retryable)
	r.True(s.Retryable(errors.New("any")))
}

// TestBackoffExponentialDelay verifies that delays grow exponentially
// on successive retries, using synctest to advance fake time.
func TestBackoffExponentialDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := require.New(t)

		b := &Backoff{
			MaxAttempts: 10,
			MinDelay:    100 * time.Millisecond,
			MaxDelay:    10 * time.Second,
			Multiplier:  2,
		}

		ctx := stopper.WithContext(t.Context(),
			stopper.WithTaskOptions(stopper.TaskMiddleware(b.Middleware())),
		)
		defer ctx.Stop()

		var attempts atomic.Int32
		done := make(chan error, 1)

		_ = ctx.Go(func(_ stopper.Context) error {
			err := ctx.Call(func(_ stopper.Context) error {
				attempts.Add(1)
				if attempts.Load() < 4 {
					return errors.New("not yet")
				}
				return nil
			})
			done <- err
			return nil
		})

		// Attempt 1 fails immediately; delay = max(100ms, 0*2) = 100ms.
		synctest.Wait()
		r.Equal(int32(1), attempts.Load())

		// Advance past first delay (100ms).
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		r.Equal(int32(2), attempts.Load())

		// Second retry delay = max(100ms, 100ms*2) = 200ms.
		time.Sleep(200 * time.Millisecond)
		synctest.Wait()
		r.Equal(int32(3), attempts.Load())

		// Third retry delay = max(100ms, 200ms*2) = 400ms.
		time.Sleep(400 * time.Millisecond)
		synctest.Wait()
		r.Equal(int32(4), attempts.Load())

		err := <-done
		r.NoError(err)
	})
}

// TestBackoffMaxAttempts verifies that the middleware returns an error
// wrapping the original after exceeding the configured number of retries.
func TestBackoffMaxAttempts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := require.New(t)

		b := &Backoff{
			MaxAttempts: 3,
			MinDelay:    10 * time.Millisecond,
			MaxDelay:    10 * time.Millisecond,
			Multiplier:  1,
		}

		ctx := stopper.WithContext(t.Context(),
			stopper.WithTaskOptions(stopper.TaskMiddleware(b.Middleware())),
		)
		defer ctx.Stop()

		taskErr := errors.New("always fails")
		var attempts atomic.Int32
		done := make(chan error, 1)

		_ = ctx.Go(func(_ stopper.Context) error {
			err := ctx.Call(func(_ stopper.Context) error {
				attempts.Add(1)
				return taskErr
			})
			done <- err
			return nil
		})

		// Attempt 1 fails; wait for delay.
		synctest.Wait()
		r.Equal(int32(1), attempts.Load())

		time.Sleep(10 * time.Millisecond)
		synctest.Wait()
		r.Equal(int32(2), attempts.Load())

		// Attempt 2 fails; wait for delay.
		time.Sleep(10 * time.Millisecond)
		synctest.Wait()
		r.Equal(int32(3), attempts.Load())

		// Attempt 3 exceeds MaxAttempts=2, error returned.
		err := <-done
		r.Error(err)
		r.ErrorIs(err, taskErr)
		var att *MaxAttemptsError
		r.ErrorAs(err, &att)
	})
}

// TestBackoffMaxDelayCap verifies that the delay never exceeds MaxDelay
// even when the exponential calculation would produce a larger value.
func TestBackoffMaxDelayCap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := require.New(t)

		b := &Backoff{
			MaxAttempts: 10,
			MinDelay:    100 * time.Millisecond,
			MaxDelay:    500 * time.Millisecond,
			Multiplier:  100, // Aggressive multiplier to hit cap fast.
		}

		ctx := stopper.WithContext(t.Context(),
			stopper.WithTaskOptions(stopper.TaskMiddleware(b.Middleware())),
		)
		defer ctx.Stop()

		var attempts atomic.Int32
		done := make(chan error, 1)

		_ = ctx.Go(func(_ stopper.Context) error {
			err := ctx.Call(func(_ stopper.Context) error {
				attempts.Add(1)
				if attempts.Load() < 4 {
					return errors.New("not yet")
				}
				return nil
			})
			done <- err
			return nil
		})

		// Attempt 1 fails; delay = 100ms.
		synctest.Wait()
		r.Equal(int32(1), attempts.Load())

		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		r.Equal(int32(2), attempts.Load())

		// Second retry delay = min(100ms*100, 500ms) = 500ms (capped).
		time.Sleep(500 * time.Millisecond)
		synctest.Wait()
		r.Equal(int32(3), attempts.Load())

		// Third retry delay still capped at 500ms.
		time.Sleep(500 * time.Millisecond)
		synctest.Wait()
		r.Equal(int32(4), attempts.Load())

		err := <-done
		r.NoError(err)
	})
}

// TestBackoffNonRetryable verifies that errors rejected by the
// Retryable predicate are returned immediately without retry.
func TestBackoffNonRetryable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := require.New(t)

		permanent := errors.New("permanent")
		b := &Backoff{
			Retryable: func(err error) bool {
				return !errors.Is(err, permanent)
			},
		}

		ctx := stopper.WithContext(t.Context(),
			stopper.WithTaskOptions(stopper.TaskMiddleware(b.Middleware())),
		)
		defer ctx.Stop()

		done := make(chan error, 1)

		_ = ctx.Go(func(_ stopper.Context) error {
			err := ctx.Call(func(_ stopper.Context) error {
				return permanent
			})
			done <- err
			return nil
		})

		synctest.Wait()
		err := <-done
		r.ErrorIs(err, permanent)
	})
}

// TestBackoffSanitizePreservesExplicit verifies that explicitly set
// fields are not overridden by sanitize.
func TestBackoffSanitizePreservesExplicit(t *testing.T) {
	r := require.New(t)

	retryable := func(_ error) bool { return false }
	b := &Backoff{
		Jitter:      5 * time.Second,
		MaxAttempts: 7,
		MaxDelay:    3 * time.Second,
		MinDelay:    50 * time.Millisecond,
		Multiplier:  3.5,
		Retryable:   retryable,
	}
	s := b.sanitize()

	r.Equal(5*time.Second, s.Jitter)
	r.Equal(7, s.MaxAttempts)
	r.Equal(3*time.Second, s.MaxDelay)
	r.Equal(50*time.Millisecond, s.MinDelay)
	r.Equal(float32(3.5), s.Multiplier)
	r.False(s.Retryable(errors.New("any")))
}

// TestBackoffStoppingDuringDelay verifies that a stop signal received
// while waiting for a backoff delay results in ErrStopped.
func TestBackoffStoppingDuringDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := require.New(t)

		b := &Backoff{
			MinDelay: time.Hour, // Very long delay; stop should preempt.
			MaxDelay: time.Hour,
		}

		ctx := stopper.WithContext(t.Context(),
			stopper.WithTaskOptions(stopper.TaskMiddleware(b.Middleware())),
		)

		taskErr := errors.New("task error")
		done := make(chan error, 1)

		_ = ctx.Go(func(_ stopper.Context) error {
			err := ctx.Call(func(_ stopper.Context) error {
				return taskErr
			})
			done <- err
			return nil
		})

		// The task fails and the backoff starts a long delay.
		synctest.Wait()

		// Trigger a soft-stop. The retry wait should be interrupted.
		ctx.Stop()
		synctest.Wait()

		err := <-done
		r.ErrorIs(err, taskErr)
		r.ErrorIs(err, stopper.ErrStopped)
	})
}

// TestBackoffSuccessOnFirstAttempt verifies that a task succeeding on
// the first try is not retried and returns no error.
func TestBackoffSuccessOnFirstAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := require.New(t)

		b := &Backoff{}

		ctx := stopper.WithContext(t.Context(),
			stopper.WithTaskOptions(stopper.TaskMiddleware(b.Middleware())),
		)
		defer ctx.Stop()

		var attempts atomic.Int32
		done := make(chan error, 1)

		_ = ctx.Go(func(_ stopper.Context) error {
			err := ctx.Call(func(_ stopper.Context) error {
				attempts.Add(1)
				return nil
			})
			done <- err
			return nil
		})

		synctest.Wait()

		err := <-done
		r.NoError(err)
		r.Equal(int32(1), attempts.Load())
	})
}
