// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

// Package safe contains utilities for executing user-provided
// functions.
package safe

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
)

const captureDepth = 32

// A RecoveredError associates an error with a stack trace.
type RecoveredError struct {
	Err   error
	Stack []uintptr
}

// Error implements error.
func (e *RecoveredError) Error() string {
	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "recovered: %v\n", e.Err)
	frames := runtime.CallersFrames(e.Stack)
	for {
		frame, more := frames.Next()
		_, _ = fmt.Fprintf(&sb, "%s ( %s:%d )\n", frame.Function, frame.File, frame.Line)

		if !more {
			return sb.String()
		}
	}
}

// String is for debugging use only.
func (e *RecoveredError) String() string {
	return e.Error()
}

// Unwrap return the enclosed error.
func (e *RecoveredError) Unwrap() error { return e.Err }

// Call executes the function. If the function panics, an error will be
// returned.
func Call(fn func()) (err error) {
	defer func() {
		switch r := recover().(type) {
		case nil:
			return
		case error:
			err = r
		default:
			err = fmt.Errorf("panic: %v", r)
		}
		stack := make([]uintptr, captureDepth)
		stack = stack[:runtime.Callers(2, stack)]
		err = &RecoveredError{
			Err:   err,
			Stack: stack,
		}
	}()
	fn()
	return
}

// CallE executes the function. If the function panics, the recovered
// value will be added to the returned error.
func CallE(fn func() error) (err error) {
	defer func() {
		switch r := recover().(type) {
		case nil:
			return
		case error:
			err = errors.Join(err, r)
		default:
			err = errors.Join(err, fmt.Errorf("panic: %v", r))
		}
		stack := make([]uintptr, captureDepth)
		stack = stack[:runtime.Callers(2, stack)]
		err = &RecoveredError{
			Err:   err,
			Stack: stack,
		}
	}()
	err = fn()
	return
}

// CallRE executes the function, returning some result value. If the
// function panics, the recovered value will be added to the returned
// error.
func CallRE[R any](fn func() (R, error)) (ret R, err error) {
	defer func() {
		switch r := recover().(type) {
		case nil:
			return
		case error:
			err = errors.Join(err, r)
		default:
			err = errors.Join(err, fmt.Errorf("panic: %v", r))
		}
		stack := make([]uintptr, captureDepth)
		stack = stack[:runtime.Callers(2, stack)]
		err = &RecoveredError{
			Err:   err,
			Stack: stack,
		}
	}()
	ret, err = fn()
	return
}
