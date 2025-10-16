// Copyright 2025 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package linger

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckEmpty(t *testing.T) {
	rec := NewRecorder(1)
	CheckClean(t, rec)
}

//line totally_fake.go:1232
func TestLingering(t *testing.T) {
	here := make([]uintptr, 1)
	runtime.Callers(1, here)

	r := require.New(t)

	rec := NewRecorder(1)
	rec.data.Store(1, here)

	fake := &fakeTB{t: t}
	CheckClean(fake, rec)

	r.True(fake.failed)
	r.Equal([]string{
		"lingering tasks detected",
		"  stuck task started at:",
		"    vawter.tech/stopper/linger.TestLingering (totally_fake.go:1234)",
	}, fake.msgs)
}

type fakeTB struct {
	failed bool
	msgs   []string
	t      *testing.T
}

func (f *fakeTB) Helper() {}
func (f *fakeTB) Errorf(s string, a ...any) {
	f.failed = true
	f.msgs = append(f.msgs, fmt.Sprintf(s, a...))
	f.t.Logf(s, a...)
}
