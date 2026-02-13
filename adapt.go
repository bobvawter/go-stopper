// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import "context"

// Adaptable is the set of function signatures accepted by [Fn].
type Adaptable interface {
	func() | func() error |
		func(context.Context) | func(context.Context) error |
		func(*Context) | Func
}

// Fn adapts various function signatures to be compatible with [Context].
func Fn[A Adaptable](fn A) Func {
	// This would be more optimal if:
	// https://github.com/golang/go/issues/59591
	a := any(fn)
	switch t := a.(type) {
	case func():
		return func(ctx *Context) error {
			t()
			return nil
		}
	case func(context.Context):
		return func(ctx *Context) error {
			t(ctx)
			return nil
		}
	case func() error:
		return func(ctx *Context) error {
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
	return a.(Func)
}
