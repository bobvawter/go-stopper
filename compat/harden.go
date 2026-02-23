// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	"context"

	v2 "vawter.tech/stopper/v2"
)

// Harden adapts the soft-stop behavior of ctx into a [context.Context]
// whose Done channel closes when the stopper begins stopping.
//
// Deprecated: Use [v2.Context.StoppingContext] instead.
func Harden(ctx *Context) context.Context {
	return ctx.inner.StoppingContext()
}

// HardenFrom is like [Harden] but accepts any [context.Context]. If
// the context is not managed by a stopper, it is returned unchanged.
//
// Deprecated: Use [v2.Context.StoppingContext] instead.
func HardenFrom(ctx context.Context) context.Context {
	found, ok := v2.From(ctx)
	if !ok {
		return ctx
	}
	return found.StoppingContext()
}
