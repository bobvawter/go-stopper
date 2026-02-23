// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	"time"

	v2 "vawter.tech/stopper/v2"
)

// StopOnReceive will gracefully stop the [Context] when a value is
// received from the channel or if the channel is closed.
//
// Deprecated: Use [v2.StopOnReceive] instead.
func StopOnReceive[T any](ctx *Context, gracePeriod time.Duration, ch <-chan T) {
	v2.StopOnReceive(ctx.inner, ch, v2.StopGracePeriod(gracePeriod))
}
