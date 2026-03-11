// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"vawter.tech/stopper/v2/internal/tctx"
)

func TestAdapt(t *testing.T) {
	var called bool
	err := errors.New("expected")

	tcs := []Func{
		Fn(func() { called = true }),
		Fn(func() error { called = true; return err }),
		Fn(func(context.Context) { called = true }),
		Fn(func(context.Context) error { called = true; return err }),
		Fn(func(Context) { called = true }),
		Fn(func(Context) error { called = true; return err }),
	}

	for i, tc := range tcs {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			r := require.New(t)
			called = false
			if i%2 == 1 {
				r.ErrorIs(tc(nil), err)
			} else {
				r.Nil(tc(nil))
			}
			r.True(called)
		})
	}
}

func TestNoStopper(t *testing.T) {
	r := require.New(t)
	ctx := tctx.Context(t)
	r.ErrorIs(Call(ctx, func() {}), ErrNoStopper)
	deferred, err := Defer(ctx, func() {})
	r.False(deferred)
	r.ErrorIs(err, ErrNoStopper)
	r.ErrorIs(Go(ctx, func() {}), ErrNoStopper)
	r.ErrorIs(GoN(ctx, 2, func() {}), ErrNoStopper)
}

func TestGoN(t *testing.T) {
	r := require.New(t)
	s := WithContext(tctx.Context(t))
	defer s.Stop()

	var count atomic.Int32
	hitCount := make(chan struct{})
	r.NoError(GoN(s, 4, func(ctx Context) {
		if count.Add(1) == 4 {
			close(hitCount)
		}
	}))
	select {
	case <-s.Done():
		r.NoError(s.Err())
	case <-hitCount:
		//OK
	}
}
