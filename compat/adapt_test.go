// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdapt(t *testing.T) {
	var called bool
	err := errors.New("expected")

	tcs := []Func{
		Fn(func() { called = true }),
		Fn(func() error { called = true; return err }),
		Fn(func(context.Context) { called = true }),
		Fn(func(context.Context) error { called = true; return err }),
		Fn(func(*Context) { called = true }),
		Fn(func(*Context) error { called = true; return err }),
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
