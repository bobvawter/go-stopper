// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package seq

import (
	"context"
	"iter"
	"sync"

	"vawter.tech/stopper/v2"
	"vawter.tech/stopper/v2/internal/safe"
)

type result[R any] struct {
	Err    error
	Result R
}

// Map will create a (nested) stopper Context and return a repeatable
// iterator that concurrently applies the given function to all elements
// in the input sequence.
//
// The returned sequence maintains the input order. That is, the Nth
// output is the result of applying the function to the Nth input value.
// The sequence will not end until the enclosed stopper has stopped.
//
// Any error returned by the callback will be emitted via the sequence
// without preemptively stopping. Callers are free to call
// [stopper.Context.Stop] from within the callback if short-circuiting
// is desired. Any non-nil error returned by [stopper.Context.Wait] will
// be returned as a final, extra, element in the sequence.
func Map[T, R any](
	ctx context.Context,
	items iter.Seq[T],
	numWorkers int,
	fn func(stopper.Context, int, T) (R, error),
	opts ...stopper.ConfigOption,
) iter.Seq2[R, error] {
	return func(yield func(R, error) bool) {
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

		earlyBreak := make(chan struct{})

		results := make(chan (<-chan result[R]), numWorkers)
		s.Defer(func(_ stopper.Context) error {
			close(results)
			return nil
		})

		for range numWorkers {
			// Report errors if the context is stopped during startup.
			s.AddError(s.Go(func(ctx stopper.Context) error {
				for {
					// Just respond to hard stop.
					if err := ctx.Err(); err != nil {
						return err
					}
					retChan := make(chan result[R], 1)

					// Collect the next value to process. Only enqueue a
					// result channel if there's an item to process. We
					// need to enqueue the return channel while holding
					// the mutex to guarantee ordering. If the yield
					// function returns false, we might block
					// indefinitely while writing to the results
					// channel. As such, it's necessary to have an extra
					// channel to distinguish an early break from a slow
					// consumer.
					nextMu.Lock()
					count := idx
					idx++
					item, ok := next()
					if ok {
						select {
						case results <- retChan:
						case <-earlyBreak:
							ok = false
						case <-ctx.Done():
							ok = false
						}
					}
					nextMu.Unlock()

					// Clean exit.
					if !ok {
						return nil
					}

					ret, err := safe.CallRE(func() (R, error) {
						return fn(ctx, count, item)
					})
					retChan <- result[R]{
						Err:    err,
						Result: ret,
					}
				}
			}))
		}
		s.Stop(stopper.StopOnIdle())

		if !yieldOrdered(s, results, yield) {
			close(earlyBreak)
		}
	}
}

// Map2 is a pairwise version of [Map].
func Map2[K, V, R any](
	ctx context.Context,
	items iter.Seq2[K, V],
	numWorkers int,
	fn func(stopper.Context, int, K, V) (R, error),
	opts ...stopper.ConfigOption,
) iter.Seq2[R, error] {
	return func(yield func(R, error) bool) {
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

		earlyBreak := make(chan struct{})

		results := make(chan (<-chan result[R]), numWorkers)
		s.Defer(func(_ stopper.Context) error {
			close(results)
			return nil
		})

		for range numWorkers {
			// Report errors if the context is stopped during startup.
			s.AddError(s.Go(func(ctx stopper.Context) error {
				for {
					// Just respond to hard stop.
					if err := ctx.Err(); err != nil {
						return err
					}
					retChan := make(chan result[R], 1)

					// Collect the next value to process. Only enqueue a
					// result channel if there's an item to process. We
					// need to enqueue the return channel while holding
					// the mutex to guarantee ordering. If the yield
					// function returns false, we might block
					// indefinitely while writing to the results
					// channel. As such, it's necessary to have an extra
					// channel to distinguish an early break from a slow
					// consumer.
					nextMu.Lock()
					count := idx
					idx++
					k, v, ok := next()
					if ok {
						select {
						case results <- retChan:
						case <-earlyBreak:
							ok = false
						case <-ctx.Done():
							ok = false
						}
					}
					nextMu.Unlock()

					// Clean exit.
					if !ok {
						return nil
					}

					ret, err := safe.CallRE(func() (R, error) {
						return fn(ctx, count, k, v)
					})
					retChan <- result[R]{
						Err:    err,
						Result: ret,
					}
				}
			}))
		}
		s.Stop(stopper.StopOnIdle())

		if !yieldOrdered(s, results, yield) {
			close(earlyBreak)
		}
	}
}

// MapUnordered will create a (nested) stopper Context and return a
// repeatable iterator that concurrently applies the given function to
// all elements in the input sequence.
//
// The returned sequence is not guaranteed to maintain the input order,
// but it does allow results that can be processed quickly to be emitted
// while slower results are still pending. The sequence will not end
// until the enclosed stopper has stopped.
//
// Any error returned by the callback will be emitted via the sequence
// without preemptively stopping. Callers are free to call
// [stopper.Context.Stop] from within the callback if short-circuiting
// is desired. Any non-nil error returned by [stopper.Context.Wait] will
// be returned as a final, extra, element in the sequence.
func MapUnordered[T, R any](
	ctx context.Context,
	items iter.Seq[T],
	numWorkers int,
	fn func(stopper.Context, int, T) (R, error),
	opts ...stopper.ConfigOption,
) iter.Seq2[R, error] {
	return func(yield func(R, error) bool) {
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

		earlyBreak := make(chan struct{})

		results := make(chan result[R], numWorkers)
		s.Defer(func(_ stopper.Context) error {
			close(results)
			return nil
		})

		for range numWorkers {
			// Report errors if the context is stopped during startup.
			s.AddError(s.Go(func(ctx stopper.Context) error {
				for {
					// Just respond to hard stop.
					if err := ctx.Err(); err != nil {
						return err
					}

					// Collect the next value to process.
					nextMu.Lock()
					count := idx
					idx++
					item, ok := next()
					nextMu.Unlock()

					// Clean exit.
					if !ok {
						return nil
					}

					ret, err := safe.CallRE(func() (R, error) {
						return fn(ctx, count, item)
					})
					select {
					case results <- result[R]{
						Err:    err,
						Result: ret,
					}:
					case <-earlyBreak:
						return nil
					case <-ctx.Done():
						// Just unblock, we'll return an error above.
					}
				}
			}))
		}
		s.Stop(stopper.StopOnIdle())

		if !yieldUnordered(s, results, yield) {
			close(earlyBreak)
		}
	}
}

// MapUnordered2 is a pairwise version of [MapUnordered].
func MapUnordered2[K, V, R any](
	ctx context.Context,
	items iter.Seq2[K, V],
	numWorkers int,
	fn func(stopper.Context, int, K, V) (R, error),
	opts ...stopper.ConfigOption,
) iter.Seq2[R, error] {
	return func(yield func(R, error) bool) {
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

		earlyBreak := make(chan struct{})

		results := make(chan result[R], numWorkers)
		s.Defer(func(_ stopper.Context) error {
			close(results)
			return nil
		})

		for range numWorkers {
			// Report errors if the context is stopped during startup.
			s.AddError(s.Go(func(ctx stopper.Context) error {
				for {
					// Just respond to hard stop.
					if err := ctx.Err(); err != nil {
						return err
					}

					// Collect the next value to process.
					nextMu.Lock()
					count := idx
					idx++
					k, v, ok := next()
					nextMu.Unlock()

					// Clean exit.
					if !ok {
						return nil
					}

					ret, err := safe.CallRE(func() (R, error) {
						return fn(ctx, count, k, v)
					})
					select {
					case results <- result[R]{
						Err:    err,
						Result: ret,
					}:
					case <-earlyBreak:
						return nil
					case <-ctx.Done():
						// Just unblock, we'll return an error above.
					}
				}
			}))
		}
		s.Stop(stopper.StopOnIdle())

		if !yieldUnordered(s, results, yield) {
			close(earlyBreak)
		}
	}
}

// yieldOrdered drains the channel-of-channels into the yield function.
// An extra value will be yielded if the context has a non-nil error
// status.
func yieldOrdered[R any](
	s stopper.Context,
	results <-chan (<-chan result[R]),
	yield func(R, error) bool,
) bool {
	// This channel is guaranteed to be closed by a Defer() function.
	for resultCh := range results {
		// The worker function wraps the user-provided code in a panic
		// handler, so we're guaranteed to see a value on the channel.
		result := <-resultCh
		if !yield(result.Result, result.Err) {
			return false
		}
	}

	// Wait is guaranteed to return immediately after deferred callbacks
	// are finished.
	if err := s.Wait(); err != nil {
		yield(*new(R), err)
	}
	return true
}

// yieldUnordered drains the results channel into the yield function. An
// extra value will be yielded if the context has a non-nil error
// status.
func yieldUnordered[R any](
	s stopper.Context,
	results <-chan result[R],
	yield func(R, error) bool,
) bool {
	// This channel is guaranteed to be closed by a Defer() function.
	for result := range results {
		if !yield(result.Result, result.Err) {
			return false
		}
	}

	// Wait is guaranteed to return immediately after deferred callbacks
	// are finished.
	if err := s.Wait(); err != nil {
		yield(*new(R), err)
	}
	return true
}
