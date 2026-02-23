// Copyright 2025 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

// Package linger contains a utility for reporting on where lingering tasks were
// originally started.
package linger

import (
	"runtime"
	"sync"
	"sync/atomic"

	"vawter.tech/stopper/v2"
)

// This value is sensitive to the code structure. It is verified in the
// test by looking for expected function names in the stack frame.
const callersOffset = 4

// NewRecorder constructs a [Recorder] that samples the call stack at the
// requested depth. A depth of 1 will record the location at which
// [stopper.Context.Call] or [stopper.Context.Go] was executed.
func NewRecorder(depth int) *Recorder {
	return &Recorder{depth: depth}
}

// A Recorder can be attached to a [stopper.Context] to record the call
// stack where [stopper.Context.Call] or [stopper.Context.Go] has been
// called. See [CheckClean] for ways to use a Recorder in testing
// scenarios.
type Recorder struct {
	counter atomic.Uint64
	data    sync.Map
	depth   int
}

// Callers returns a snapshot of the caller stacks associated with any
// managed tasks that are currently running.
func (r *Recorder) Callers() [][]uintptr {
	var ret [][]uintptr
	r.data.Range(func(_, value any) bool {
		ret = append(ret, value.([]uintptr))
		return true
	})
	return ret
}

// Middleware is a [stopper.Middleware] that samples the caller to
// [stopper.Context.Call] or to [stopper.Context.Go].
func (r *Recorder) Middleware(outer stopper.Context) (stopper.Context, stopper.Invoker) {
	pc := make([]uintptr, r.depth)
	pc = pc[:runtime.Callers(callersOffset, pc)]

	id := r.counter.Add(1)
	r.data.Store(id, pc)

	return outer, func(ctx stopper.Context, fn stopper.Func) error {
		defer r.data.Delete(id)
		return fn(ctx)
	}
}
