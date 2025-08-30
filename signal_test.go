// Copyright 2025 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper_test

import (
	"context"
	"os"
	"os/signal"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"vawter.tech/stopper"
)

func ExampleStopOnReceive_interrupt() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	ctx := stopper.WithContext(context.Background())
	stopper.StopOnReceive(ctx, time.Second, signals)

	ctx.Go(func(ctx *stopper.Context) error {
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

	ctx := stopper.WithContext(context.Background())

	ch := make(chan struct{})
	close(ch)
	stopper.StopOnReceive(ctx, time.Second, ch)

	select {
	case <-ctx.Stopping():
	case <-time.After(time.Second):
		r.Fail("did not stop")
	}
}

func TestStopOnReceiveStoppedElsewhere(t *testing.T) {
	r := require.New(t)

	ctx := stopper.WithContext(context.Background())

	ch := make(chan struct{})
	stopper.StopOnReceive(ctx, time.Second, ch)

	ctx.Go(func(ctx *stopper.Context) error {
		ctx.Stop(time.Second)
		return nil
	})

	select {
	case <-ctx.Stopping():
	case <-time.After(time.Second):
		r.Fail("did not stop")
	}
}

func TestStopOnReceiveValue(t *testing.T) {
	r := require.New(t)

	ctx := stopper.WithContext(context.Background())

	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	stopper.StopOnReceive(ctx, time.Second, ch)

	select {
	case <-ctx.Stopping():
	case <-time.After(time.Second):
		r.Fail("did not stop")
	}
}
