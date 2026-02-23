// Copyright 2025 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package linger

import (
	"runtime"
)

// CheckClean will record a test error if there are any active tasks
// being tracked by the Recorder. A snapshot of the stack where the
// tasks were launched will be written into the test log.
func CheckClean(t TestingT, r *Recorder) {
	callers := r.Callers()
	if len(callers) == 0 {
		return
	}

	// Improve error messages if we're being called from a real test.
	if x, ok := t.(interface{ Helper() }); ok {
		x.Helper()
	}

	t.Errorf("lingering tasks detected")
	for _, stack := range callers {
		t.Errorf("  stuck task started at:")
		frames := runtime.CallersFrames(stack)
		for {
			frame, more := frames.Next()
			t.Errorf("    %s ( %s:%d )", frame.Function, frame.File, frame.Line)
			if !more {
				break
			}
		}
	}
}

// TestingT is the subset of [testing.TB] needed by [CheckClean].
type TestingT interface {
	Errorf(string, ...any)
}
