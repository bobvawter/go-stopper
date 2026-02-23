// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	"context"
	"errors"
)

// Adaptable is the set of function signatures accepted by [Fn].
type Adaptable interface {
	func() | func() error |
		func(context.Context) | func(context.Context) error |
		func(Context) | func(Context) error
}

// Fn adapts various function signatures to be compatible with [Context].
func Fn[A Adaptable](fn A) Func {
	// This would be more optimal if:
	// https://github.com/golang/go/issues/59591
	a := any(fn)
	switch t := a.(type) {
	case func():
		return func(_ Context) error {
			t()
			return nil
		}
	case func(context.Context):
		return func(ctx Context) error {
			t(ctx)
			return nil
		}
	case func() error:
		return func(_ Context) error {
			return t()
		}
	case func(context.Context) error:
		return func(ctx Context) error {
			return t(ctx)
		}
	case func(Context):
		return func(ctx Context) error {
			t(ctx)
			return nil
		}
	}
	// Exhaustive case.
	return a.(func(Context) error)
}

// ErrNoStopper is a sentinel error returned by the package-level
// functions to indicate that the provided context was not derived from
// a [Context].
var ErrNoStopper = errors.New("context is not a stopper")

// Call is a convenience adaptor to invoke [Context.Call] on any context
// using any [Adaptable] function signature. This function will return
// [ErrNoStopper] if the provided stdlib context is not derived from a
// stopper [Context].
func Call[A Adaptable](ctx context.Context, fn A, opts ...TaskOption) error {
	s, ok := From(ctx)
	if !ok {
		return ErrNoStopper
	}
	return s.Call(Fn(fn), opts...)
}

// Defer is a convenience adaptor to invoke [Context.Defer] on any
// context using any [Adaptable] function signature. This function will
// return [ErrNoStopper] if the provided stdlib context is not derived
// from a stopper [Context].
func Defer[A Adaptable](ctx context.Context, fn A) (deferred bool, err error) {
	s, ok := From(ctx)
	if !ok {
		return false, ErrNoStopper
	}
	return s.Defer(Fn(fn)), nil
}

// Go is a convenience adaptor to invoke [Context.Go] on any context
// using any [Adaptable] function signature. This function will return
// [ErrNoStopper] if the provided stdlib context is not derived from a
// stopper [Context].
func Go[A Adaptable](ctx context.Context, fn A, opts ...TaskOption) error {
	s, ok := From(ctx)
	if !ok {
		return ErrNoStopper
	}
	return s.Go(Fn(fn), opts...)
}

// GoN is a convenience adaptor to repeatedly invoke [Context.Go] on any
// context using any [Adaptable] function signature. This function will
// return [ErrNoStopper] if the provided stdlib context is not derived
// from a stopper [Context].
func GoN[A Adaptable](ctx context.Context, count int, fn A, opts ...TaskOption) error {
	s, ok := From(ctx)
	if !ok {
		return ErrNoStopper
	}
	f := Fn(fn) // Create wrapper once.
	errs := make([]error, count)
	for i := range count {
		errs[i] = s.Go(f, opts...)
	}
	return errors.Join(errs...)
}
