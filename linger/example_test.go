// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package linger_test

import (
	"fmt"
	"runtime"

	"vawter.tech/stopper/v2"
	"vawter.tech/stopper/v2/linger"
)

func ExampleRecorder() {
	// Construct the recorder and attach it as task middleware. The
	// stack depth counts away from Context.Call() or Context.Go(). If
	// the package-level Call() or Go() helpers are used, the smallest
	// useful depth is 2 frames.
	rec := linger.NewRecorder(2 /* stack depth */)
	ctx := stopper.New(
		stopper.WithTaskOptions(
			stopper.TaskMiddleware(rec.Middleware),
		))
	defer ctx.Stop()

	// Start some tasks.
	_ = stopper.Go(ctx, func(ctx stopper.Context) {
		<-ctx.Stopping()
	})

	// Sample the tasks asynchronously.
	stacks := rec.Callers()
	for _, stack := range stacks {
		frames := runtime.CallersFrames(stack)
		for {
			frame, more := frames.Next()
			fmt.Println(frame.Function)
			if !more {
				break
			}
		}
	}
	// Output:
	// vawter.tech/stopper/v2.Go[...]
	// vawter.tech/stopper/v2/linger_test.ExampleRecorder
}
