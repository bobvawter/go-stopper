// Copyright 2025 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper_test

import (
	"context"
	"testing"
	"time"

	"vawter.tech/stopper"
	"vawter.tech/stopper/linger"
)

func NewStopperForTest(t *testing.T) *stopper.Context {
	const grace = 5 * time.Second

	// Impose a per-test timout.
	stdCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	// Add tracking for where goroutine tasks are started.
	rec := linger.NewRecorder(10 /* depth */)
	ctx := stopper.WithInvoker(stdCtx, rec.Invoke)

	// Register a cleanup, which could be a deferred function, that will stop
	// the context, wait for all tasks to exit, and then verify that there are
	// no lingering goroutines associated with the context.
	t.Cleanup(func() {
		ctx.Stop(grace)
		if err := ctx.Wait(); err != nil {
			t.Errorf("task returned an error: %v", err)
		}
		linger.CheckClean(t, rec)
	})

	return ctx
}

// This is a general pattern for constructing a [stopper.Context] for testing
// purposes. The specifics of error reporting, timeouts, and other administrivia
// will vary across projects, hence this not being part of the stopper module.
func Example_testing() {}
