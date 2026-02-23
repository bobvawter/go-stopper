// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import "context"

// Adaptable is the set of function signatures accepted by [Fn].
//
// Deprecated: Use [v2.Adaptable] instead.
type Adaptable interface {
	func() | func() error |
		func(context.Context) | func(context.Context) error |
		func(*Context) | func(*Context) error
}

// Fn adapts various function signatures to be compatible with [Context].
//
// Deprecated: Use [v2.Fn] instead.
func Fn[A Adaptable](fn A) Func {
	a := any(fn)
	switch t := a.(type) {
	case func():
		return func(_ *Context) error {
			t()
			return nil
		}
	case func(context.Context):
		return func(ctx *Context) error {
			t(ctx)
			return nil
		}
	case func() error:
		return func(_ *Context) error {
			return t()
		}
	case func(context.Context) error:
		return func(ctx *Context) error {
			return t(ctx)
		}
	case func(*Context):
		return func(ctx *Context) error {
			t(ctx)
			return nil
		}
	}
	return a.(func(*Context) error)
}
