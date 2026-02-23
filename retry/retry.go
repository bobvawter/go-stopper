// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

// Package retry contains [stopper.Middleware] that allows tasks to be
// retried.
//
// The [Middleware] is a general-purpose building block for retryable
// behaviors using a [Classifier] function to drive the retry policy. An
// exponential [Backoff] and a trivial [Loop] implementation are
// provided.
package retry

import (
	"errors"
	"runtime/trace"

	"vawter.tech/stopper/v2"
)

// A Classifier is a function that determines if an error is retryable.
// Each task to be executed is associated with a state value, which is
// initially the zero value for the S type. If the task execution fails,
// the error and the current state are passed to the Classifier. The
// Classifier may return an error to fail the task if it should not be
// retried.
//
// If the task should be retried, the Classifier returns a channel that
// emits a value when the task should be retried (e.g.: [time.After]).
// Closing the channel without emitting a value will abandon the task
// retry, failing with the error most recently passed to the Classifier.
//
// If the [stopper.Context.Stopping] channel is closed while waiting for
// the retry signal, the task will be failed with the previously
// examined error joined with [stopper.ErrStopped].
//
// If the returned channel and error are both nil, the error will be
// considered to have been handled by the Classifier function and the
// task will be considered a success.
type Classifier[S, N any] func(ctx stopper.Context, state *S, err error) (<-chan N, error)

// Middleware constructs a [stopper.Middleware] around a [Classifier]
// function.
func Middleware[S, N any](fn Classifier[S, N]) stopper.Middleware {
	return func(outer stopper.Context) (stopper.Context, stopper.Invoker) {
		return outer, func(ctx stopper.Context, task stopper.Func) error {
			var state S
			for {
				// Make the attempt.
				err := task(ctx)
				if err == nil {
					return nil
				}
				// Classify the error.
				next, fail := fn(ctx, &state, err)
				// Classifier is rejecting the error.
				if fail != nil {
					return fail
				}
				// Classifier ate the error condition.
				if next == nil {
					return nil
				}
				// Wait for a decision.
				if err := waitOnChannel(ctx, next, err); err != nil {
					return err
				}
			}
		}
	}
}

func waitOnChannel[N any](ctx stopper.Context, next <-chan N, err error) error {
	defer trace.StartRegion(ctx, "retry wait").End()
	select {
	case _, ok := <-next:
		if ok {
			return nil
		}
		return err
	case <-ctx.Stopping():
		return errors.Join(err, stopper.ErrStopped)
	case <-ctx.Done():
		return ctx.Err()
	}
}
