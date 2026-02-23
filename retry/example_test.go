// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package retry_test

import (
	"errors"
	"fmt"
	"time"

	"vawter.tech/stopper/v2"
	"vawter.tech/stopper/v2/retry"
)

func ExampleBackoff() {
	myError := errors.New("boom")

	mw := (&retry.Backoff{
		Jitter:      time.Millisecond,
		MaxAttempts: 2,
		MaxDelay:    10 * time.Millisecond,
		MinDelay:    time.Millisecond,
		Multiplier:  10,
		Retryable: func(err error) bool {
			// This might look for a SQL database error code indicating
			// that a database transaction needs to be retried.
			return errors.Is(err, myError)
		},
	}).Middleware()

	ctx := stopper.New()
	_ = stopper.Go(ctx,
		func() error {
			fmt.Println("attempt")
			return myError
		},
		// If the stopper only executes retryable tasks, the middleware
		// could be attached to the stopper instead of the task
		// invocation.
		stopper.TaskMiddleware(mw),
	)

	ctx.Stop(stopper.StopOnIdle(), stopper.StopGracePeriod(time.Hour))

	err := ctx.Wait()
	fmt.Println(err.Error())
	// Output:
	// attempt
	// attempt
	// task: max attempts reached: boom
}

func ExampleLoop() {
	myError := errors.New("boom")

	mw := (&retry.Loop{
		MaxAttempts: 2,
		Retryable: func(err error) bool {
			// This might look for a SQL database error code indicating
			// that a database transaction needs to be retried.
			return errors.Is(err, myError)
		},
	}).Middleware()

	ctx := stopper.New()
	_ = stopper.Go(ctx,
		func() error {
			fmt.Println("attempt")
			return myError
		},
		// If the stopper only executes retryable tasks, the middleware
		// could be attached to the stopper instead of the task
		// invocation.
		stopper.TaskMiddleware(mw),
	)

	ctx.Stop(stopper.StopOnIdle(), stopper.StopGracePeriod(time.Hour))

	err := ctx.Wait()
	fmt.Println(err.Error())
	// Output:
	// attempt
	// attempt
	// task: max attempts reached: boom
}
