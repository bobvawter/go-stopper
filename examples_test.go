// Copyright 2025 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper_test

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/trace"
	"time"

	"vawter.tech/stopper"
)

func Example_features() {
	// Create a stopper context from an existing context.
	ctx := stopper.WithContext(context.Background())

	// Respond to signals.
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	stopper.StopOnReceive(ctx, time.Second, ch)

	// Do work, often in a loop.
	ctx.Go(func(ctx *stopper.Context) error {
		for !ctx.IsStopping() {
		}
		return nil
	})

	// Plays nicely with channels.
	ctx.Go(func(ctx *stopper.Context) error {
		for {
			select {
			case <-ctx.Stopping():
				return nil
			case work := <-sourceOfWork:
				// Launches additional workers.
				ctx.Go(func(ctx *stopper.Context) error {
					return process(ctx, work)
				})
			}
		}
	})

	subCtx := stopper.WithContext(ctx) // Nested contexts can be created.
	subCtx.Stop(time.Second)           // Won't affect the outer context.

	// Blocks until all managed goroutines are done.
	if err := ctx.Wait(); err != nil {
		panic(err)
	}
}

var sourceOfWork chan struct{}

// The stopper.Context type fits into existing context plumbing and can be
// retrieved later on.
func process(ctxCtx context.Context, work struct{}) error {
	stopperCtx := stopper.From(ctxCtx)
	stopperCtx.Go(func(ctx *stopper.Context) error {
		return nil
	})
	return nil
}

func ExampleContext_Defer() {
	ctx := stopper.WithContext(context.Background())
	// Deferred functions are executed in reverse order.
	ctx.Defer(func() {
		fmt.Println("defer 0")
	})
	ctx.Defer(func() {
		fmt.Println("defer 1")
	})
	// This will run in a separate goroutine and then stop the context.
	ctx.Go(func(ctx *stopper.Context) error {
		fmt.Println("task")
		ctx.Stop(time.Second)
		return nil
	})
	// Wait for all tasks, including deferred callbacks to be complete.
	if err := ctx.Wait(); err != nil {
		fmt.Println(err)
	}
	fmt.Println("finished")
	// Output:
	// task
	// defer 1
	// defer 0
	// finished
}

// This example shows how a background task that should execute on a
// regular basis may be implemented.
func ExampleContext_ticker() {
	ctx := stopper.WithContext(context.Background())

	ctx.Go(func(ctx *stopper.Context) error {
		for {
			// Do some background task.
			select {
			case <-time.After(time.Second):
				// Loop around.
			case <-ctx.Stopping():
				// This channel closes when Stop() is called. The
				// context is not yet cancelled at this point.
				return nil
			case <-ctx.Done():
				// This is a hard-stop condition because either the
				// underlying context.Context was canceled or the task
				// has outlived its graceful shutdown time.
				return ctx.Err()
			}
		}
	})

	// Do other things.
	fmt.Println("task count:", ctx.Len())

	// Calling Stop() makes the Stopping channel close, allowing
	// processes one second before the context is hard-cancelled.
	ctx.Stop(time.Second)

	// Callers can wait for all tasks to finish, similar to an ErrGroup.
	if err := ctx.Wait(); err != nil {
		panic(err)
	}
	fmt.Println("task count:", ctx.Len())

	// Output:
	// task count: 1
	// task count: 0
}

// This example shows that contexts may be nested. Stop signals will
// propagate from enclosing to inner contexts, while the Len() and
// Wait() methods are aware of child contexts.
func ExampleContext_nested() {
	outer := stopper.WithContext(context.Background())
	middle := stopper.WithContext(outer)
	inner := stopper.WithContext(middle)

	middle.Go(func(ctx *stopper.Context) error {
		<-ctx.Stopping()
		return nil
	})

	inner.Go(func(ctx *stopper.Context) error {
		<-ctx.Stopping()
		return nil
	})

	fmt.Println("outer", outer.Len())
	fmt.Println("middle", middle.Len())
	fmt.Println("inner", inner.Len())

	// Stopping a parent context stops the child contexts.
	outer.Stop(time.Second)

	// Wait for all nested tasks.
	if err := outer.Wait(); err != nil {
		panic(err)
	}

	fmt.Println("outer", outer.Len())

	// Output:
	// outer 2
	// middle 2
	// inner 1
	// outer 0
}

// This shows how the [runtime/trace] package, or any other package that creates
// custom [context.Context] instances, can be interoperated with.
func ExampleContext_With_tracing() {
	f, err := os.OpenFile("trace.out", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			panic(err)
		}
		fmt.Println("trace written to", f.Name())
	}()

	if err := trace.Start(f); err != nil {
		panic(err)
	}
	defer trace.Stop()

	rootCtx, rootTask := trace.NewTask(context.Background(), "root task")
	defer rootTask.End()

	ctx := stopper.WithContext(rootCtx)
	defer trace.StartRegion(ctx, "root region").End()

	midCtx, midTask := trace.NewTask(ctx, "mid task")
	ctx.With(midCtx).Go(func(ctx *stopper.Context) error {
		defer midTask.End()
		defer trace.StartRegion(ctx, "mid region").End()
		trace.Log(ctx, "message", "middle task is here")

		innerCtx, innerTask := trace.NewTask(ctx, "inner task")
		ctx.With(innerCtx).Go(func(ctx *stopper.Context) error {
			defer innerTask.End()
			defer trace.StartRegion(ctx, "inner region").End()
			trace.Log(ctx, "message", "inner task is here")
			ctx.Stop(time.Second)
			return nil
		})

		return nil
	})

	if err := ctx.Wait(); err != nil {
		panic(err)
	}
	// Output:
	// trace written to trace.out
}
