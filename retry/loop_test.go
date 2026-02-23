// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package retry

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"vawter.tech/stopper/v2"
)

// TestLoopCustomRetryable verifies that the Retryable predicate
// controls which errors are retried and which are returned immediately.
func TestLoopCustomRetryable(t *testing.T) {
	r := require.New(t)

	permanent := errors.New("permanent")
	l := &Loop{
		MaxAttempts: 5,
		Retryable: func(err error) bool {
			return !errors.Is(err, permanent)
		},
	}

	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(stopper.TaskMiddleware(l.Middleware())),
	)
	defer ctx.Stop()

	attempts := 0
	err := ctx.Call(func(_ stopper.Context) error {
		attempts++
		return permanent
	})
	r.ErrorIs(err, permanent)
	r.Equal(1, attempts)

	// A non-permanent error should be retried.
	transient := errors.New("transient")
	attempts = 0
	err = ctx.Call(func(_ stopper.Context) error {
		attempts++
		if attempts < 3 {
			return transient
		}
		return nil
	})
	r.NoError(err)
	r.Equal(3, attempts)
}

// TestLoopDefaults verifies that a zero-value Loop uses MaxAttempts=2
// and retries all errors.
func TestLoopDefaults(t *testing.T) {
	r := require.New(t)

	l := &Loop{}

	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(stopper.TaskMiddleware(l.Middleware())),
	)
	defer ctx.Stop()

	taskErr := errors.New("always fails")
	attempts := 0
	err := ctx.Call(func(_ stopper.Context) error {
		attempts++
		return taskErr
	})
	r.Equal(2, attempts)
	r.ErrorIs(err, taskErr)
	var maxErr *MaxAttemptsError
	r.ErrorAs(err, &maxErr)
}

// TestLoopMaxAttempts verifies that the middleware returns a
// MaxAttemptsError wrapping the original after exceeding the configured
// number of retries.
func TestLoopMaxAttempts(t *testing.T) {
	r := require.New(t)

	l := &Loop{MaxAttempts: 3}

	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(stopper.TaskMiddleware(l.Middleware())),
	)
	defer ctx.Stop()

	taskErr := errors.New("always fails")
	attempts := 0
	err := ctx.Call(func(_ stopper.Context) error {
		attempts++
		return taskErr
	})
	r.Equal(3, attempts)
	r.ErrorIs(err, taskErr)
	var maxErr *MaxAttemptsError
	r.ErrorAs(err, &maxErr)
}

// TestLoopMaxAttemptsOne verifies that MaxAttempts=1 means the task is
// tried exactly once with no retries.
func TestLoopMaxAttemptsOne(t *testing.T) {
	r := require.New(t)

	l := &Loop{MaxAttempts: 1}

	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(stopper.TaskMiddleware(l.Middleware())),
	)
	defer ctx.Stop()

	taskErr := errors.New("fail")
	attempts := 0
	err := ctx.Call(func(_ stopper.Context) error {
		attempts++
		return taskErr
	})
	r.Equal(1, attempts)
	r.ErrorIs(err, taskErr)
	var maxErr *MaxAttemptsError
	r.ErrorAs(err, &maxErr)
}

// TestLoopMiddlewareIndependentState verifies that each Call through the
// same middleware instance gets independent retry state.
func TestLoopMiddlewareIndependentState(t *testing.T) {
	r := require.New(t)

	l := &Loop{MaxAttempts: 3}

	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(stopper.TaskMiddleware(l.Middleware())),
	)
	defer ctx.Stop()

	// First call exhausts retries.
	attempts1 := 0
	err := ctx.Call(func(_ stopper.Context) error {
		attempts1++
		return errors.New("fail")
	})
	r.Error(err)
	r.Equal(3, attempts1)

	// Second call should start fresh, not carry over state.
	attempts2 := 0
	err = ctx.Call(func(_ stopper.Context) error {
		attempts2++
		if attempts2 < 2 {
			return errors.New("fail once")
		}
		return nil
	})
	r.NoError(err)
	r.Equal(2, attempts2)
}

// TestLoopSuccessOnFirstAttempt verifies that a task succeeding on the
// first try is not retried.
func TestLoopSuccessOnFirstAttempt(t *testing.T) {
	r := require.New(t)

	l := &Loop{MaxAttempts: 5}

	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(stopper.TaskMiddleware(l.Middleware())),
	)
	defer ctx.Stop()

	attempts := 0
	err := ctx.Call(func(_ stopper.Context) error {
		attempts++
		return nil
	})
	r.NoError(err)
	r.Equal(1, attempts)
}

// TestLoopSuccessOnRetry verifies that a task that fails initially but
// succeeds on a subsequent attempt returns no error.
func TestLoopSuccessOnRetry(t *testing.T) {
	r := require.New(t)

	l := &Loop{MaxAttempts: 5}

	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(stopper.TaskMiddleware(l.Middleware())),
	)
	defer ctx.Stop()

	attempts := 0
	err := ctx.Call(func(_ stopper.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("not yet")
		}
		return nil
	})
	r.NoError(err)
	r.Equal(3, attempts)
}
