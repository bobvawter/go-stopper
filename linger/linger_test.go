// Copyright 2025 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package linger

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"vawter.tech/stopper"
)

func TestRecorder(t *testing.T) {
	r := require.New(t)

	rec := NewRecorder(1)
	ctx := stopper.WithInvoker(context.Background(), rec.Invoke)

	r.NoError(ctx.Call(func(ctx *stopper.Context) error {
		sample := rec.Callers()
		r.Len(sample, 1)    // One active task.
		r.Len(sample[0], 1) // Sample to depth of 1.
		frames := runtime.CallersFrames(sample[0])
		frame, _ := frames.Next()
		t.Logf("%s ( %s:%d )", frame.Function, frame.File, frame.Line)
		r.True(strings.HasSuffix(frame.Function, "linger.TestRecorder"))

		return nil
	}))

	ctx.Go(func(ctx *stopper.Context) error {
		sample := rec.Callers()
		r.Len(sample, 1)    // One active task.
		r.Len(sample[0], 1) // Sample to depth of 1.
		frames := runtime.CallersFrames(sample[0])
		frame, _ := frames.Next()
		t.Logf("%s ( %s:%d )", frame.Function, frame.File, frame.Line)
		r.True(strings.HasSuffix(frame.Function, "linger.TestRecorder"))

		return nil
	})

	ctx.Stop(10 * time.Second)
	select {
	case <-ctx.Done():
		r.NoError(ctx.Wait())
	case <-time.After(10 * time.Second):
		r.Fail("timed out")
	}
}
