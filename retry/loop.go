// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package retry

import "vawter.tech/stopper/v2"

// Loop implements a trivial, synchronous looping behavior.
type Loop struct {
	MaxAttempts int              // Defaults to 2 if unset.
	Retryable   func(error) bool // Defaults to retrying all errors.
}

// Middleware returns a [stopper.Middleware] that implements a trivial
// looping behavior.
func (l *Loop) Middleware() stopper.Middleware {
	attempts := l.MaxAttempts
	if attempts == 0 {
		attempts = 2
	}
	fn := l.Retryable
	if fn == nil {
		fn = func(_ error) bool { return true }
	}
	return Middleware(func(_ stopper.Context, state *int, err error) (<-chan struct{}, error) {
		if !fn(err) {
			return nil, err
		}
		*state++
		if *state >= attempts {
			return nil, &MaxAttemptsError{err}
		}
		ch := make(chan struct{}, 1)
		ch <- struct{}{}
		return ch, nil
	})
}
