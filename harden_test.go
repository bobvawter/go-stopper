// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHarden(t *testing.T) {
	stdCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	ctx := WithContext(stdCtx)
	// Make sure the stopper lingers beyond the call to Stop().
	ctx.Go(func(ctx *Context) error {
		<-stdCtx.Done()
		return nil
	})

	h := Harden(ctx)

	t.Run("hardenFrom", func(t *testing.T) {
		r := require.New(t)
		r.Same(h, HardenFrom(h))
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

	// Ensure original context can be recovered.
	t.Run("value", func(t *testing.T) {
		r := require.New(t)
		r.Same(ctx, From(h))
	})

	t.Run("done_is_stop", func(t *testing.T) {
		r := require.New(t)
		ctx.Stop(0)
		r.ErrorIs(h.Err(), ErrStopped)
		select {
		case <-h.Done():
		case <-ctx.Done():
			r.Fail("Stopping channel not closed")
		}
	})
}
