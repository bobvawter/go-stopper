// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package limit_test

import (
	"context"
	"runtime"

	"vawter.tech/stopper/v2"
	"vawter.tech/stopper/v2/limit"
)

var unboundedSourceOfWork <-chan any

func Example() {
	ctx := stopper.WithContext(
		context.Background(),

		// Set defaults to apply to all tasks. The Middleware instances
		// could instead be provided on every use of Call() or Go(). In
		// general, rate-limiting should be applied before concurrency
		// limits.
		stopper.WithTaskOptions(
			stopper.TaskMiddleware(
				limit.WithMaxRate(10, 5),
				limit.WithMaxConcurrency(5),
			),
		),
	)

	// We prevent this dispatcher task from consuming a concurrency slot
	// by passing the no-inherit option.
	if err := stopper.Go(ctx,
		func(ctx stopper.Context) {
			for {
				select {
				case <-ctx.Stopping():
					// Respond to soft stop.
					return

				case <-ctx.Done():
					// Respond to hard stop.
					return

				case work := <-unboundedSourceOfWork:
					// If a rate or concurrency limit is reached, the
					// pushback will occur within the call to Go(),
					// before any goroutine is spawned.
					_ = stopper.Go(ctx, func() {
						// Do something with the work object.
						runtime.KeepAlive(work)
					})
				}
			}
		},
		stopper.TaskNoInherit(),
	); err != nil {
		panic(err)
	}

	// Not shown: a call to ctx.Stop()
	if err := ctx.Wait(); err != nil {
		panic(err)
	}
}
