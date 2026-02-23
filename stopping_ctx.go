// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	"context"
	"errors"
	"time"
)

var errCanceledStopped = errors.Join(context.Canceled, ErrStopped)

// stoppingCtx just swizzles the method set.
type stoppingCtx impl

var _ context.Context = (*stoppingCtx)(nil)

func (c *stoppingCtx) Deadline() (deadline time.Time, ok bool) {
	return (*impl)(c).Deadline()
}

func (c *stoppingCtx) Done() <-chan struct{} {
	return (*impl)(c).Stopping()
}

func (c *stoppingCtx) Err() error {
	i := (*impl)(c)
	if i.IsStopping() {
		return errCanceledStopped
	}
	return i.Err()
}

func (c *stoppingCtx) Value(key any) any {
	return (*impl)(c).Value(key)
}
