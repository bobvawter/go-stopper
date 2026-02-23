// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package retry

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"vawter.tech/stopper/v2"
)

// TestChannelCloseAbandon verifies that closing the retry channel
// without sending a value causes the task to succeed (nil return).
func TestChannelCloseAbandon(t *testing.T) {
	r := require.New(t)

	taskErr := errors.New("task error")
	classifier := func(_ stopper.Context, _ *struct{}, _ error) (<-chan struct{}, error) {
		ch := make(chan struct{})
		close(ch)
		return ch, nil
	}

	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(stopper.TaskMiddleware(Middleware(classifier))),
	)
	defer ctx.Stop()

	err := ctx.Call(func(_ stopper.Context) error {
		return taskErr
	})
	r.ErrorIs(err, taskErr)
}

// TestClassifierEatsError verifies that when the Classifier returns
// (nil, nil) the error is considered handled and the task succeeds.
func TestClassifierEatsError(t *testing.T) {
	r := require.New(t)

	classifier := func(_ stopper.Context, _ *struct{}, _ error) (<-chan struct{}, error) {
		return nil, nil
	}

	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(stopper.TaskMiddleware(Middleware(classifier))),
	)
	defer ctx.Stop()

	err := ctx.Call(func(_ stopper.Context) error {
		return errors.New("some error")
	})
	r.NoError(err)
}

// TestClassifierRejects verifies that if the Classifier returns a
// non-nil error, that error is returned to the caller immediately.
func TestClassifierRejects(t *testing.T) {
	r := require.New(t)

	rejectErr := errors.New("rejected")
	classifier := func(_ stopper.Context, _ *struct{}, _ error) (<-chan struct{}, error) {
		return nil, rejectErr
	}

	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(stopper.TaskMiddleware(Middleware(classifier))),
	)
	defer ctx.Stop()

	err := ctx.Call(func(_ stopper.Context) error {
		return errors.New("task error")
	})
	r.ErrorIs(err, rejectErr)
}

// TestContextDone verifies that if the context is fully canceled while
// waiting for a retry signal, ctx.Err() is returned.
func TestContextDone(t *testing.T) {
	r := require.New(t)

	classifier := func(_ stopper.Context, _ *struct{}, _ error) (<-chan struct{}, error) {
		// Return a channel that will never fire.
		return make(chan struct{}), nil
	}

	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(stopper.TaskMiddleware(Middleware(classifier))),
		stopper.WithGracePeriod(time.Millisecond),
	)

	err := ctx.Call(func(ctx stopper.Context) error {
		ctx.Stop()
		return errors.New("task error")
	})
	r.Error(err)
}

// TestRetrySuccess verifies that the task is retried when the
// Classifier sends a value on the channel, and succeeds on a
// subsequent attempt.
func TestRetrySuccess(t *testing.T) {
	r := require.New(t)

	classifier := func(_ stopper.Context, _ *struct{}, _ error) (<-chan struct{}, error) {
		ch := make(chan struct{}, 1)
		ch <- struct{}{}
		return ch, nil
	}

	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(stopper.TaskMiddleware(Middleware(classifier))),
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

// TestStateAccumulates verifies that the state pointer passed to the
// Classifier accumulates across retries for a single task execution.
func TestStateAccumulates(t *testing.T) {
	r := require.New(t)

	classifier := func(_ stopper.Context, state *int, _ error) (<-chan struct{}, error) {
		*state++
		if *state >= 3 {
			return nil, errors.New("too many retries")
		}
		ch := make(chan struct{}, 1)
		ch <- struct{}{}
		return ch, nil
	}

	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(stopper.TaskMiddleware(Middleware(classifier))),
	)
	defer ctx.Stop()

	err := ctx.Call(func(_ stopper.Context) error {
		return errors.New("always fail")
	})
	r.Error(err)
	r.Equal("task: too many retries", err.Error())
}

// TestStoppingDuringWait verifies that if the stopper enters a
// soft-stop while waiting for a retry signal, the task error is
// joined with stopper.ErrStopped.
func TestStoppingDuringWait(t *testing.T) {
	r := require.New(t)

	taskErr := errors.New("task error")
	classifier := func(_ stopper.Context, _ *struct{}, _ error) (<-chan struct{}, error) {
		// Return a channel that will never fire.
		return make(chan struct{}), nil
	}

	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(stopper.TaskMiddleware(Middleware(classifier))),
	)

	err := ctx.Call(func(ctx stopper.Context) error {
		// Trigger soft-stop from within the task.
		ctx.Stop()
		return taskErr
	})
	r.ErrorIs(err, taskErr)
	r.ErrorIs(err, stopper.ErrStopped)
}

// TestTaskSucceeds verifies that a task that returns nil on the first
// attempt is not retried.
func TestTaskSucceeds(t *testing.T) {
	r := require.New(t)

	called := false
	classifier := func(_ stopper.Context, _ *struct{}, _ error) (<-chan struct{}, error) {
		called = true
		return nil, nil
	}

	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(stopper.TaskMiddleware(Middleware(classifier))),
	)
	defer ctx.Stop()

	attempts := 0
	err := ctx.Call(func(_ stopper.Context) error {
		attempts++
		return nil
	})
	r.NoError(err)
	r.Equal(1, attempts)
	r.False(called, "classifier should not be called on success")
}
