// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package limit

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"vawter.tech/stopper/v2"
)

func TestWithMaxConcurrency(t *testing.T) {
	r := require.New(t)

	const maxConcurrency = 3

	mw := WithMaxConcurrency(maxConcurrency)
	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(
			stopper.TaskMiddleware(mw),
		),
	)

	var running atomic.Int32
	var maxSeen atomic.Int32

	// Use a dispatcher that doesn't inherit the middleware to launch
	// tasks that do go through the concurrency middleware.
	r.NoError(ctx.Go(func(ctx stopper.Context) error {
		for range 20 {
			if err := ctx.Go(func(ctx stopper.Context) error {
				cur := running.Add(1)
				defer running.Add(-1)
				// Track the maximum observed concurrency.
				for {
					old := maxSeen.Load()
					if cur <= old || maxSeen.CompareAndSwap(old, cur) {
						break
					}
				}
				// Hold the slot briefly to allow other goroutines to contend.
				time.Sleep(10 * time.Millisecond)
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	}, stopper.TaskNoInherit()))

	ctx.Stop(stopper.StopOnIdle(), stopper.StopGracePeriod(10*time.Second))
	r.NoError(ctx.Wait())

	r.LessOrEqual(maxSeen.Load(), int32(maxConcurrency))
	r.Greater(maxSeen.Load(), int32(0))
}

func TestWithMaxConcurrencyPanicsOnZero(t *testing.T) {
	require.Panics(t, func() {
		WithMaxConcurrency(0)
	})
}

func TestWithMaxConcurrencyPanicsOnNegative(t *testing.T) {
	require.Panics(t, func() {
		WithMaxConcurrency(-1)
	})
}

// TestWithMaxConcurrencySoftStop verifies that a goroutine blocked
// waiting for a concurrency slot unblocks when a soft stop is triggered.
func TestWithMaxConcurrencySoftStop(t *testing.T) {
	r := require.New(t)

	const maxConcurrency = 1
	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(
			stopper.TaskMiddleware(WithMaxConcurrency(maxConcurrency)),
		),
	)

	hold := make(chan struct{})
	// Fill the single concurrency slot.
	r.NoError(ctx.Go(func(ctx stopper.Context) error {
		<-hold
		return nil
	}))

	// Launch a goroutine that will block in the middleware waiting
	// for a slot, and record whether the task body executes.
	var executed atomic.Bool
	blocked := make(chan struct{})
	go func() {
		close(blocked)
		// This Call will block in the middleware until a slot opens
		// or the context starts stopping.
		_ = ctx.Call(func(ctx stopper.Context) error {
			executed.Store(true)
			return nil
		})
	}()

	// Wait for the goroutine to enter the blocking Call.
	<-blocked
	time.Sleep(50 * time.Millisecond)

	// Trigger a soft stop. The blocked middleware should unblock
	// and return a no-op invoker.
	ctx.Stop(stopper.StopGracePeriod(10 * time.Second))

	// Release the held task so the stopper can finish.
	close(hold)
	r.NoError(ctx.Wait())

	// The blocked task's body should not have executed.
	r.False(executed.Load())
}

// TestWithMaxConcurrencyHardStop verifies that a goroutine blocked
// waiting for a concurrency slot unblocks with an error when a hard
// stop (context cancellation) is triggered.
func TestWithMaxConcurrencyHardStop(t *testing.T) {
	r := require.New(t)

	const maxConcurrency = 1
	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(
			stopper.TaskMiddleware(WithMaxConcurrency(maxConcurrency)),
		),
	)

	hold := make(chan struct{})
	// Fill the single concurrency slot.
	r.NoError(ctx.Go(func(ctx stopper.Context) error {
		<-hold
		return nil
	}))

	// Launch a goroutine that will block in the middleware.
	callDone := make(chan error, 1)
	go func() {
		callDone <- ctx.Call(func(ctx stopper.Context) error {
			return nil
		})
	}()

	// Trigger a hard stop with a very short grace period.
	ctx.Stop(stopper.StopGracePeriod(time.Millisecond))
	<-ctx.Done()

	close(hold)

	select {
	case err := <-callDone:
		// The middleware should have returned an error from the
		// done context.
		r.ErrorIs(err, stopper.ErrStopped)
	case <-time.After(5 * time.Second):
		r.Fail("timed out waiting for blocked call to return")
	}

	_ = ctx.Wait()
}

func TestWithMaxRate(t *testing.T) {
	r := require.New(t)

	// High rate and burst to avoid blocking in this test.
	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(
			stopper.TaskMiddleware(WithMaxRate(1000, 100)),
		),
	)

	var count atomic.Int32
	for range 10 {
		r.NoError(ctx.Call(func(ctx stopper.Context) error {
			count.Add(1)
			return nil
		}))
	}
	r.Equal(int32(10), count.Load())

	ctx.Stop(stopper.StopGracePeriod(10 * time.Second))
	r.NoError(ctx.Wait())
}

func TestWithMaxRateEnforcesLimit(t *testing.T) {
	r := require.New(t)

	// Rate of 10/sec with burst of 2: after burst, calls must wait.
	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(
			stopper.TaskMiddleware(WithMaxRate(20, 2)),
		),
	)

	start := time.Now()
	for range 5 {
		r.NoError(ctx.Call(func(ctx stopper.Context) error {
			return nil
		}))
	}
	elapsed := time.Since(start)

	// With burst=2, first 2 calls are instant. Remaining 3 calls
	// need tokens at 20/sec = 50ms each = ~150ms total.
	r.GreaterOrEqual(elapsed, 100*time.Millisecond)
}

// TestWithMaxRateSoftStop verifies that a call blocked waiting for a
// rate token unblocks when a soft stop is triggered.
func TestWithMaxRateSoftStop(t *testing.T) {
	r := require.New(t)

	// Very low rate so waiting for a token takes a long time.
	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(
			stopper.TaskMiddleware(WithMaxRate(0.001, 1)),
		),
	)

	// Consume the burst token.
	r.NoError(ctx.Call(func(ctx stopper.Context) error {
		return nil
	}))

	// Launch a goroutine that will block waiting for a rate token.
	var executed atomic.Bool
	blocked := make(chan struct{})
	go func() {
		close(blocked)
		_ = ctx.Call(func(ctx stopper.Context) error {
			executed.Store(true)
			return nil
		})
	}()

	<-blocked
	time.Sleep(50 * time.Millisecond)

	// Trigger a soft stop; the blocked Wait should unblock.
	ctx.Stop(stopper.StopGracePeriod(10 * time.Second))
	r.NoError(ctx.Wait())

	// The task body should not have executed.
	r.False(executed.Load())
}

func TestWithMaxRateErrorPropagation(t *testing.T) {
	r := require.New(t)

	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(
			stopper.TaskMiddleware(WithMaxRate(1000, 100)),
		),
	)

	testErr := errors.New("test error")
	err := ctx.Call(func(ctx stopper.Context) error {
		return testErr
	})
	r.ErrorIs(err, testErr)

	ctx.Stop(stopper.StopGracePeriod(10 * time.Second))
	_ = ctx.Wait()
}

func TestWithMaxConcurrencyErrorPropagation(t *testing.T) {
	r := require.New(t)

	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(
			stopper.TaskMiddleware(WithMaxConcurrency(5)),
		),
	)

	testErr := errors.New("test error")
	err := ctx.Call(func(ctx stopper.Context) error {
		return testErr
	})
	r.ErrorIs(err, testErr)

	ctx.Stop(stopper.StopGracePeriod(10 * time.Second))
	_ = ctx.Wait()
}

func TestCombinedRateAndConcurrency(t *testing.T) {
	r := require.New(t)

	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(
			stopper.TaskMiddleware(
				WithMaxRate(1000, 100),
				WithMaxConcurrency(2),
			),
		),
	)

	var count atomic.Int32
	for range 10 {
		r.NoError(ctx.Call(func(ctx stopper.Context) error {
			count.Add(1)
			return nil
		}))
	}
	r.Equal(int32(10), count.Load())

	ctx.Stop(stopper.StopGracePeriod(10 * time.Second))
	r.NoError(ctx.Wait())
}

func TestWithMaxConcurrencyReleasesOnCompletion(t *testing.T) {
	r := require.New(t)

	const maxConcurrency = 1
	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(
			stopper.TaskMiddleware(WithMaxConcurrency(maxConcurrency)),
		),
	)

	// Run sequential tasks. If slots are not released, the second
	// call would block forever.
	for range 5 {
		r.NoError(ctx.Call(func(ctx stopper.Context) error {
			return nil
		}))
	}

	ctx.Stop(stopper.StopGracePeriod(10 * time.Second))
	r.NoError(ctx.Wait())
}

func TestWithMaxConcurrencyWithGo(t *testing.T) {
	r := require.New(t)

	const maxConcurrency = 2
	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(
			stopper.TaskMiddleware(WithMaxConcurrency(maxConcurrency)),
		),
	)

	var running atomic.Int32
	var maxSeen atomic.Int32

	// Use a dispatcher that doesn't inherit the middleware.
	r.NoError(ctx.Go(func(ctx stopper.Context) error {
		for range 10 {
			if err := ctx.Go(func(ctx stopper.Context) error {
				cur := running.Add(1)
				defer running.Add(-1)
				for {
					old := maxSeen.Load()
					if cur <= old || maxSeen.CompareAndSwap(old, cur) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	}, stopper.TaskNoInherit()))

	ctx.Stop(stopper.StopOnIdle(), stopper.StopGracePeriod(10*time.Second))
	r.NoError(ctx.Wait())

	r.LessOrEqual(maxSeen.Load(), int32(maxConcurrency))
	r.Greater(maxSeen.Load(), int32(0))
}
