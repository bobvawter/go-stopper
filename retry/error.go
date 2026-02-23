// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package retry

import "fmt"

// MaxAttemptsError indicates that a task reached the maximum number of
// retries.
type MaxAttemptsError struct {
	Err error
}

// Error implements error.
func (e *MaxAttemptsError) Error() string {
	return fmt.Sprintf("max attempts reached: %v", e.Err)
}

// Unwrap returns the enclosed error.
func (e *MaxAttemptsError) Unwrap() error {
	return e.Err
}
