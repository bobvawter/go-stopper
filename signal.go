// Copyright 2025 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

// StopOnReceive will gracefully stop the Context when a value is
// received from the channel or if the channel is closed. StopOnReceive
// can be used, for example, with [os/signal.Notify]. If the context is
// already stopping or stopped, this function is a no-op.
func StopOnReceive[T any](ctx Context, ch <-chan T, opts ...StopOption) {
	// We can safely ignore this error since it would mean that the
	// context is already stopped, rendering this function moot.
	_ = ctx.Go(func(ctx Context) error {
		select {
		case <-ch:
			ctx.Stop(opts...)
		case <-ctx.Stopping():
		case <-ctx.Done():
		}
		return nil
	})
}
