// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package seq

import (
	"context"
	"fmt"
	"iter"
	"sync"

	"vawter.tech/stopper/v2"
	"vawter.tech/stopper/v2/internal/safe"
)

// ForEach will create a (nested) stopper Context and concurrently
// execute the callback for each item in the sequence. The maximum
// concurrency is bounded to the provided limit. The enclosed Context
// will be stopped once all (nested) tasks have completed.
//
// Any error returned by the callback will be added to the stopper and
// returned without preemptively stopping. Callers are free to call
// [stopper.Context.Stop] from within the callback if short-circuiting
// is desired.
func ForEach[T any](
	ctx context.Context,
	items iter.Seq[T],
	numWorkers int,
	fn func(stopper.Context, int, T) error,
	opts ...stopper.ConfigOption,
) error {
	var nextMu sync.Mutex
	idx := 0
	next, stop := iter.Pull(items)
	defer func() {
		nextMu.Lock()
		defer nextMu.Unlock()
		stop()
	}()

	s := stopper.WithContext(ctx, opts...)
	defer s.Stop()

	for range numWorkers {
		// Report errors if the context is stopped during startup.
		s.AddError(s.Go(func(ctx stopper.Context) error {
			for {
				if ctx.IsStopping() {
					return stopper.ErrStopped
				}

				// Collect the next value to process.
				nextMu.Lock()
				count := idx
				idx++
				item, ok := next()
				nextMu.Unlock()

				if !ok {
					// Clean exit.
					return nil
				}
				if err := safe.CallE(func() error {
					return fn(ctx, count, item)
				}); err != nil {
					ctx.AddError(fmt.Errorf("index %d: %w", count, err))
				}
			}
		}))
	}
	s.Stop(stopper.StopOnIdle())
	return s.WaitCtx(ctx)
}

// ForEach2 is a pairwise version of [ForEach].
func ForEach2[K, V any](
	ctx context.Context,
	items iter.Seq2[K, V],
	numWorkers int,
	fn func(stopper.Context, int, K, V) error,
	opts ...stopper.ConfigOption,
) error {
	var nextMu sync.Mutex
	idx := 0
	next, stop := iter.Pull2(items)
	defer func() {
		nextMu.Lock()
		defer nextMu.Unlock()
		stop()
	}()

	s := stopper.WithContext(ctx, opts...)
	defer s.Stop()

	for range numWorkers {
		// Report errors if the context is stopped during startup.
		s.AddError(s.Go(func(ctx stopper.Context) error {
			for {
				if ctx.IsStopping() {
					return stopper.ErrStopped
				}

				// Collect the next value to process.
				nextMu.Lock()
				count := idx
				idx++
				k, v, ok := next()
				nextMu.Unlock()

				if !ok {
					// Clean exit.
					return nil
				}
				if err := safe.CallE(func() error {
					return fn(ctx, count, k, v)
				}); err != nil {
					ctx.AddError(fmt.Errorf("index %d: %w", count, err))
				}
			}
		}))
	}
	s.Stop(stopper.StopOnIdle())
	return s.WaitCtx(ctx)
}
