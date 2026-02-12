// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	"context"
	"time"
)

// Harden adapts the soft-stop behaviors of a stopper into a
// [context.Context]. This can be used whenever it is necessary to call
// other APIs that should be made aware of the soft-stop condition.
//
// The returned context has the following behaviors:
//   - The [context.Context.Done] method returns [Context.Stopping].
//   - The [context.Context.Err] method returns [ErrStopped] if the
//     context has been stopped. Otherwise, it returns [Context.Err].
//   - All other interface methods delegate to the provided stopper.
func Harden(ctx *Context) context.Context {
	return (*hardened)(ctx)
}

// HardenFrom returns a hardened context for any input context.
func HardenFrom(ctx context.Context) context.Context {
	return Harden(From(ctx))
}

// hardened just swizzles the method set.
type hardened Context

var _ context.Context = (*hardened)(nil)

func (h *hardened) Deadline() (deadline time.Time, ok bool) {
	return (*Context)(h).Deadline()
}

func (h *hardened) Done() <-chan struct{} {
	return (*Context)(h).Stopping()
}

func (h *hardened) Err() error {
	ctx := (*Context)(h)
	if ctx.IsStopping() {
		return ErrStopped
	}
	return ctx.Err()
}

func (h *hardened) Value(key any) any {
	return (*Context)(h).Value(key)
}
