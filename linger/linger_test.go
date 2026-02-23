// Copyright 2025 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package linger

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"vawter.tech/stopper/v2"
	"vawter.tech/stopper/v2/limit"
)

const sampleDepth = 2

func TestRecorderNested(t *testing.T) {
	r := require.New(t)

	// The checker only wants to see one stack at a time.
	mux := limit.WithMaxConcurrency(1)

	rec1 := NewRecorder(sampleDepth)
	ctx := stopper.WithContext(t.Context(),
		stopper.WithTaskOptions(
			stopper.TaskMiddleware(mux, rec1.Middleware),
		),
	)
	rec2 := NewRecorder(sampleDepth)
	ctx = stopper.WithContext(ctx,
		stopper.WithTaskOptions(
			stopper.TaskMiddleware(rec2.Middleware),
		),
	)

	// Call via interface.
	r.NoError(ctx.Call(func(ctx stopper.Context) error {
		checkRecorder(r, rec1, "linger.TestRecorderNested")
		checkRecorder(r, rec2, "linger.TestRecorderNested")

		return nil
	}))

	// Call via helper.
	r.NoError(stopper.Call(ctx, func() {
		checkRecorder(r, rec1, "linger.TestRecorderNested")
		checkRecorder(r, rec2, "linger.TestRecorderNested")
	}))

	// Call via interface.
	r.NoError(ctx.Go(func(ctx stopper.Context) error {
		checkRecorder(r, rec1, "linger.TestRecorderNested")
		checkRecorder(r, rec2, "linger.TestRecorderNested")

		return nil
	}))

	// Call via helper.
	r.NoError(stopper.Go(ctx, func() {
		checkRecorder(r, rec1, "linger.TestRecorderNested")
		checkRecorder(r, rec2, "linger.TestRecorderNested")
	}))

	ctx.Stop(stopper.StopGracePeriod(time.Second))
	select {
	case <-ctx.Done():
		r.NoError(ctx.Wait())
	case <-time.After(10 * time.Second):
		r.Fail("timed out")
	}
}

func checkRecorder(r *require.Assertions, rec *Recorder, where string) {
	sample := rec.Callers()
	r.Len(sample, 1) // One active task, enforced by mux.
	r.Len(sample[0], sampleDepth)
	frames := runtime.CallersFrames(sample[0])
	for {
		frame, more := frames.Next()
		if strings.HasSuffix(frame.Function, where) {
			break
		}
		if !more {
			r.Failf("did not find expected frame %s: check callersOffset constant", where)
		}
	}
}
