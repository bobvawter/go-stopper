// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

// Package seq contains helpers for concurrent processing of sequences.
//
// The functions in this package create a nested [stopper.Context] when
// invoked. The context arguments to these functions do not need to be
// derived from the stopper package.
package seq
