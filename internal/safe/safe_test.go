// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package safe

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireStack asserts that the RecoveredError has a non-empty Stack
// whose frames include the named function.
func requireStack(r *require.Assertions, err error, funcName string) {
	var recovered *RecoveredError
	r.ErrorAs(err, &recovered)
	r.NotEmpty(recovered.Stack)

	frames := runtime.CallersFrames(recovered.Stack)
	var found bool
	for {
		frame, more := frames.Next()
		if strings.Contains(frame.Function, funcName) {
			found = true
			break
		}
		if !more {
			break
		}
	}
	r.True(found, "expected stack to contain %q, got:\n%s",
		funcName, recovered.String())
}

func TestCall(t *testing.T) {
	r := require.New(t)

	// Normal call.
	r.NoError(Call(func() {}))

	// Panic with error.
	boom := errors.New("boom")
	err := Call(func() { panic(boom) })
	r.ErrorIs(err, boom)
	requireStack(r, err, "TestCall")

	// Panic with non-error.
	err = Call(func() { panic("yikes") })
	r.ErrorContains(err, "yikes")
	requireStack(r, err, "TestCall")
}

func TestCallE(t *testing.T) {
	r := require.New(t)

	// Normal call returning nil.
	r.NoError(CallE(func() error { return nil }))

	// Normal call returning error.
	boom := errors.New("boom")
	r.ErrorIs(CallE(func() error { return boom }), boom)

	// Panic with error.
	kaboom := errors.New("kaboom")
	err := CallE(func() error { panic(kaboom) })
	r.ErrorIs(err, kaboom)
	requireStack(r, err, "TestCallE")

	// Panic with non-error.
	err = CallE(func() error { panic("oops") })
	r.ErrorContains(err, "oops")
	requireStack(r, err, "TestCallE")

	// Panic with error after returning an error: the deferred panic
	// joins with the returned error via errors.Join.
	panicOnly := CallE(func() error {
		defer func() { panic(kaboom) }()
		return boom
	})
	r.ErrorIs(panicOnly, kaboom)
	r.NotErrorIs(panicOnly, boom) // Panic masks setting return values.
	requireStack(r, panicOnly, "TestCallE")
}

func TestCallRE(t *testing.T) {
	r := require.New(t)

	// Normal call returning value and nil error.
	val, err := CallRE(func() (int, error) { return 42, nil })
	r.NoError(err)
	r.Equal(42, val)

	// Normal call returning value and error.
	boom := errors.New("boom")
	val, err = CallRE(func() (int, error) { return 99, boom })
	r.ErrorIs(err, boom)
	r.Equal(99, val)

	// Panic with error.
	kaboom := errors.New("kaboom")
	val, err = CallRE(func() (int, error) { panic(kaboom) })
	r.ErrorIs(err, kaboom)
	r.Zero(val)
	requireStack(r, err, "TestCallRE")

	// Panic with non-error.
	val, err = CallRE(func() (int, error) { panic("oops") })
	r.ErrorContains(err, "oops")
	r.Zero(val)
	requireStack(r, err, "TestCallRE")

	// Panic with error after returning an error: the deferred panic
	// joins with the returned error via errors.Join.
	val, err = CallRE(func() (int, error) {
		defer func() { panic(kaboom) }()
		return 123, boom
	})
	r.ErrorIs(err, kaboom)
	r.NotErrorIs(err, boom) // Panic masks setting return values.
	r.Zero(val)
	requireStack(r, err, "TestCallRE")
}
