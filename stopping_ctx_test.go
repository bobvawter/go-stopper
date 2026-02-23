// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStoppingContext(t *testing.T) {
	stdCtx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)

	type k struct{}
	stdCtx = context.WithValue(stdCtx, k{}, "value")

	ctx := WithContext(stdCtx)
	// Make sure the stopper lingers beyond the call to Stop().
	_ = ctx.Go(func(ctx Context) error {
		<-stdCtx.Done()
		return nil
	})

	h := ctx.StoppingContext()

	// Ensure the original context can be recovered.
	t.Run("harden_from", func(t *testing.T) {
		r := require.New(t)
		found, ok := From(h)
		r.True(ok)
		r.Same(ctx, found)
	})

	t.Run("initial_state", func(t *testing.T) {
		r := require.New(t)
		r.Nil(h.Err())
		select {
		case <-h.Done():
			r.Fail("Should not be done")
		default:
		}
	})

	// Verify delegation.
	t.Run("deadline", func(t *testing.T) {
		r := require.New(t)
		d1, ok1 := stdCtx.Deadline()
		d2, ok2 := h.Deadline()
		r.Equal(d1, d2)
		r.Equal(ok1, ok2)
	})

	t.Run("done_is", func(t *testing.T) {
		r := require.New(t)
		ctx.Stop()
		r.ErrorIs(h.Err(), context.Canceled)
		r.ErrorIs(h.Err(), ErrStopped)
		select {
		case <-h.Done():
		case <-ctx.Done():
			r.Fail("Stopping channel not closed")
		}
	})

	t.Run("value", func(t *testing.T) {
		r := require.New(t)
		r.Equal("value", h.Value(k{}))
	})
}
