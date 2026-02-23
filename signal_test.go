// Copyright 2025 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper_test

import (
	"os"
	"os/signal"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"vawter.tech/stopper/v2"
)

func ExampleStopOnReceive_interrupt() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	ctx := stopper.New()
	stopper.StopOnReceive(ctx, signals)

	_ = ctx.Go(func(ctx stopper.Context) error {
		// Do other work
		return nil
	})

	if err := ctx.Wait(); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestStopOnReceiveClosed(t *testing.T) {
	r := require.New(t)

	ctx := stopper.New()

	ch := make(chan struct{})
	close(ch)
	stopper.StopOnReceive(ctx, ch)

	select {
	case <-ctx.Stopping():
	case <-time.After(time.Second):
		r.Fail("did not stop")
	}
}

func TestStopOnReceiveStoppedElsewhere(t *testing.T) {
	r := require.New(t)

	ctx := stopper.New()

	ch := make(chan struct{})
	stopper.StopOnReceive(ctx, ch)

	r.NoError(ctx.Go(func(ctx stopper.Context) error {
		ctx.Stop(stopper.StopGracePeriod(time.Second))
		return nil
	}))

	select {
	case <-ctx.Stopping():
	case <-time.After(time.Second):
		r.Fail("did not stop")
	}
}

func TestStopOnReceiveValue(t *testing.T) {
	r := require.New(t)

	ctx := stopper.New()

	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	stopper.StopOnReceive(ctx, ch)

	select {
	case <-ctx.Stopping():
	case <-time.After(time.Second):
		r.Fail("did not stop")
	}
}
