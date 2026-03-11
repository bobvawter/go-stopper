// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

//go:build go1.24

// Package tctx contains a helper to return a context for use in
// testing.
package tctx

import (
	"context"
	"testing"
)

// Context returns the test's context.
func Context(t *testing.T) context.Context {
	return t.Context()
}
