// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

//go:build !go1.24

package tctx

import (
	"context"
	"testing"
)

// Context returns a context that will be canceled when the test is
// complete.
func Context(t *testing.T) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}
