// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package retry

import (
	"math/rand/v2"
	"time"

	"vawter.tech/stopper/v2"
)

// Backoff implements an exponential backoff with jitter.
type Backoff struct {
	Jitter      time.Duration    // Delays are adjusted ±50% of this value. Default is 0.
	MaxAttempts int              // Defaults to 4 if unset.
	MaxDelay    time.Duration    // Defaults to 1s if unset.
	MinDelay    time.Duration    // Defaults to 10ms if unset.
	Multiplier  float32          // Defaults to 10.0 if unset.
	Retryable   func(error) bool // Defaults to retrying all errors.
}

// Middleware returns a [stopper.Middleware] that applies exponential
// backoff with jitter to task retries.
func (b *Backoff) Middleware() stopper.Middleware {
	b = b.sanitize() // Shadowing receiver.
	type state struct {
		count int
		delay time.Duration
	}
	return Middleware(func(ctx stopper.Context, st *state, err error) (<-chan time.Time, error) {
		if !b.Retryable(err) {
			return nil, err
		}

		st.count++
		if st.count >= b.MaxAttempts {
			return nil, &MaxAttemptsError{Err: err}
		}

		next := time.Duration(float32(st.delay) * b.Multiplier)
		st.delay = min(max(b.MinDelay, next), b.MaxDelay)
		jitter := time.Duration((rand.Float32() - 0.5) * float32(b.Jitter))

		return time.After(st.delay + jitter), nil
	})
}

// sanitize returns a copy with all fields initialized to a reasonable default.
func (b *Backoff) sanitize() *Backoff {
	ret := *b
	// Jitter defaults to 0.
	if ret.MaxAttempts == 0 {
		ret.MaxAttempts = 4
	}
	if ret.MaxDelay == 0 {
		ret.MaxDelay = 1 * time.Second
	}
	if ret.MinDelay == 0 {
		ret.MinDelay = 10 * time.Millisecond
	}
	if ret.Multiplier == 0 {
		ret.Multiplier = 10
	}
	if ret.Retryable == nil {
		ret.Retryable = func(_ error) bool { return true }
	}
	return &ret
}
