// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

// Package limit provides [stopper.Middleware] to impose execution
// limits on tasks.
//
// Attach the Middlewares using [stopper.TaskMiddleware] during stopper
// construction.
package limit

import (
	"errors"
	"runtime/trace"

	"golang.org/x/time/rate"
	"vawter.tech/stopper/v2"
)

// WithMaxConcurrency limits the total number of goroutines executing a
// task by blocking calls to [stopper.Context.Call] or
// [stopper.Context.Go]. Blocked tasks will be discarded if the context
// is stopped.
func WithMaxConcurrency(limit int) stopper.Middleware {
	if limit <= 0 {
		panic(errors.New("limit must be greater than zero"))
	}
	ch := make(chan struct{}, limit)
	return func(outer stopper.Context) (stopper.Context, stopper.Invoker) {
		// Fast-path: A concurrency slot is available.
		select {
		case ch <- struct{}{}:
			return outer, func(ctx stopper.Context, task stopper.Func) error {
				defer func() { <-ch }()
				return task(ctx)
			}
		default:
		}

		defer trace.StartRegion(outer, "concurrency wait").End()

		select {
		case ch <- struct{}{}:
			return outer, func(ctx stopper.Context, task stopper.Func) error {
				defer func() { <-ch }()
				return task(ctx)
			}

		case <-outer.Stopping():
			// Return a no-op invoker during a soft stop.
			return outer, stopper.InvokerDrop

		case <-outer.Done():
			// Return an erroring invoker during a hard stop.
			return outer, stopper.InvokerErr(outer.Err())
		}
	}
}

// WithMaxRate is a wrapper around a [rate.Limiter] that enforces a rate
// by blocking calls to [stopper.Context.Call] or [stopper.Context.Go].
// Blocked tasks will be discarded if the context is stopped.
func WithMaxRate(r float64, b int) stopper.Middleware {
	l := rate.NewLimiter(rate.Limit(r), b)
	return func(outer stopper.Context) (stopper.Context, stopper.Invoker) {
		// Fast-path: there's capacity.
		if l.Allow() {
			return outer, stopper.InvokerCall
		}

		defer trace.StartRegion(outer, "rate limit wait").End()

		// Make wait conditional upon a soft-stop condition.
		if err := l.Wait(outer.StoppingContext()); err != nil {
			// Silent exit during soft-stop.
			if errors.Is(err, stopper.ErrStopped) {
				return outer, stopper.InvokerDrop
			}
			// Report any other error.
			return outer, stopper.InvokerErr(err)
		}
		return outer, stopper.InvokerCall
	}
}
