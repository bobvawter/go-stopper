// Copyright 2025 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"runtime/trace"
	"strings"
	"sync/atomic"
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
func process[T any](ctxCtx context.Context, work T) error {
	stopperCtx := stopper.From(ctxCtx)
	stopperCtx.Go(func(ctx *stopper.Context) error {
		return nil
	})
	return nil
}

// A pattern for using a stopper with an accept/poll type of network listening
// API.
func Example_netServer() {
	ctx := stopper.WithContext(context.Background())

	// Respond to interrupts with a 30-second grace period.
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	stopper.StopOnReceive(ctx, 30*time.Second, ch)

	// Open a network listener.
	const addr = "127.0.0.1:13013"
	l, err := net.Listen("tcp", addr)
	if err != nil {
		panic(err)
	}
	// Close the listener when the context stops.
	ctx.Go(func(ctx *stopper.Context) error {
		<-ctx.Stopping()
		return l.Close()
	})
	// Accept connections.
	ctx.Go(func(ctx *stopper.Context) error {
		for {
			conn, err := l.Accept()
			// This returns an error when the listener has been closed.
			if err != nil {
				return nil
			}
			// Handle the connection in its own goroutine.
			ctx.Go(func(ctx *stopper.Context) error {
				defer func() { _ = conn.Close() }()

				s := bufio.NewScanner(conn)
				s.Scan()
				fmt.Println(s.Text())

				// Simulate an OS signal to stop the app.
				ch <- os.Interrupt

				return nil
			})
		}
	})
	// A task to send a message.
	ctx.Go(func(ctx *stopper.Context) error {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			panic(err)
		}
		_, _ = conn.Write([]byte("Hello World!\n"))
		if err := conn.Close(); err != nil {
			slog.ErrorContext(ctx, "could not send data", "error", err)
		}
		return nil
	})
	// Block until tasks are complete.
	if err := ctx.Wait(); err != nil {
		slog.ErrorContext(ctx, "internal task error", "error", err)
	}
	// Output:
	// Hello World!
}

// An example showing the use of [stopper.Context.Call] when a goroutine is
// already allocated by some other API. In this case, the HTTP server creates a
// goroutine per request, but we still want to be able to interact with a
// stopper hierarchy.
func ExampleContext_Call_httpServer() {
	ctx := stopper.WithContext(context.Background())

	// Respond to interrupts with a 30-second grace period.
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	stopper.StopOnReceive(ctx, 30*time.Second, ch)

	// The HTTP server creates goroutines for us and has its own expectations
	// around goroutine/request lifecycles.
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// We use With() here so that a caller hangup can immediately stop any
		// work. This is optional, and might not be desirable in all
		// circumstances.
		err := ctx.With(r.Context()).Call(func(ctx *stopper.Context) error {
			data, err := io.ReadAll(r.Body)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			w.WriteHeader(http.StatusAccepted)
			return nil
		})

		if err == nil {
			return
		}

		// If Stop() has been called, Call() will immediately return ErrStopped.
		// We can respond to the client (or load-balancer) saying that this
		// instance of the server cannot fulfill the request.
		if errors.Is(err, stopper.ErrStopped) {
			w.Header().Add("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		slog.ErrorContext(ctx, "handler error", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
	}))

	// A task to make an HTTP request.
	ctx.Go(func(ctx *stopper.Context) error {
		_, err := svr.Client().Post(svr.URL, "text/plain", strings.NewReader("Hello World!"))
		if err != nil {
			// Returning an error here will make the call to Wait() below fail
			// out. This makes sense for an example, but production code should
			// handle the error in a reasonable way and return nil.
			return err
		}

		// Simulate an OS shutdown.
		ch <- os.Interrupt
		return nil
	})

	if err := ctx.Wait(); err != nil {
		slog.ErrorContext(ctx, "internal task error", "error", err)
	}
	// Output:
	// Hello World!
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

func ExampleContext_StopOnIdle() {
	ctx := stopper.WithContext(context.Background())
	var nestedDidAccept atomic.Bool
	ctx.Go(func(ctx *stopper.Context) error {
		// It's still valid to create additional tasks or additional
		// nested stoppers.
		ok := ctx.Go(func(ctx *stopper.Context) error {
			// Do nested tasks...
			return nil
		})
		nestedDidAccept.Store(ok)
		return nil
	})
	// StopOnIdle shouldn't be called until all parent tasks have been
	// started.
	ctx.StopOnIdle()
	// Wait doesn't return until all tasks are complete.
	err := ctx.Wait()
	fmt.Printf("OK: %t %t\n", err == nil, nestedDidAccept.Load())

	// Output:
	// OK: true true
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

// This shows the sequence of callbacks when nested contexts have Invokers
// defined. Note that the setup phase is bottom-up, while execution is top-down.
func ExampleWithInvoker_observeLifecycle() {
	outer := stopper.WithInvoker(context.Background(),
		func(fn stopper.Func) stopper.Func {
			fmt.Println("outer setting up")
			return func(ctx *stopper.Context) error {
				fmt.Println("outer start")
				defer fmt.Println("outer end")
				return fn(ctx)
			}
		})
	middle := stopper.WithInvoker(outer,
		func(fn stopper.Func) stopper.Func {
			fmt.Println("middle setting up")
			return func(ctx *stopper.Context) error {
				fmt.Println("middle start")
				defer fmt.Println("middle end")
				return fn(ctx)
			}
		})
	inner := stopper.WithContext(middle)
	inner.Go(func(ctx *stopper.Context) error {
		fmt.Println("here")
		return nil
	})
	outer.Stop(time.Second)
	if err := outer.Wait(); err != nil {
		panic(err)
	}
	// Output:
	// middle setting up
	// outer setting up
	// outer start
	// middle start
	// here
	// middle end
	// outer end
}
