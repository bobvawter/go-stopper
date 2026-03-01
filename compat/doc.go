// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

// Package stopper provides a compatibility layer that re-implements
// the original vawter.tech/stopper (v1) API on top of v2.
//
// Existing v1 consumers can switch to this module with no source-code
// changes by adding a replace directive to their go.mod:
//
//	require vawter.tech/stopper v1.2.0
//
//	replace vawter.tech/stopper v1.2.0 => vawter.tech/stopper/v2/compat v0.1.0
//
// For local development against an unpublished checkout, use a
// filesystem path instead:
//
//	replace vawter.tech/stopper v1.2.0 => /path/to/go-stopper/compat
package stopper
