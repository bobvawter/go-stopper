// Copyright 2025 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	"time"
)

// StopOnReceive will gracefully stop the Context when a value is received from
// the channel or if the channel is closed.
//
// This can be used, for example, with [os/signal.Notify].
func StopOnReceive[T any](ctx *Context, gracePeriod time.Duration, ch <-chan T) {
	ctx.Go(func(ctx *Context) error {
		select {
		case <-ch:
			ctx.Stop(gracePeriod)
		case <-ctx.Stopping():
		}
		return nil
	})
}
