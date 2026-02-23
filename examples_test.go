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
	"runtime"
	"runtime/trace"
	"strings"
	"sync/atomic"
	"time"

	"vawter.tech/stopper/v2"
	"vawter.tech/stopper/v2/limit"
)

// The simplest possible use of the API.
func Example() {
	ctx := stopper.New()         // Create a stopper.
	_ = stopper.Go(ctx, func() { // Run some tasks.
		fmt.Println("Hello World!")
	})
	ctx.Stop()     // Put it in a soft-shutdown state.
	_ = ctx.Wait() // Wait for any errors to be returned.
}

func Example_features() {
	// Create a root stopper context.
	ctx := stopper.New()

	// Respond to signals.
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	stopper.StopOnReceive(ctx, ch)

	// Do work, often in a loop. Error-handling omitted for brevity.
	_ = stopper.Go(ctx, func(ctx stopper.Context) {
		for !ctx.IsStopping() {
			// Do stuff.
		}
	})

	// Plays nicely with channels.
	_ = stopper.Go(ctx, func(ctx stopper.Context) {
		for {
			select {
			case <-ctx.Stopping():
				return
			case work := <-sourceOfWork:
				// Launches additional workers.
				_ = stopper.Go(ctx, func(ctx stopper.Context) error {
					return process(ctx, work)
				})
			}
		}
	})

	// Soft-stop can be exported to other libraries.
	_ = stopper.Go(ctx, func(ctx stopper.Context) {
		otherLibrary(ctx.StoppingContext())
	})

	subCtx := stopper.WithContext(ctx) // Nested contexts can be created.
	_ = stopper.Call(subCtx, func() { fmt.Println("Hello World!") })

	ctx.Stop(stopper.StopGracePeriod(time.Second)) // Implicitly calls subCtx.Stop().
	// Blocks until all managed tasks are done.
	if err := ctx.Wait(); err != nil {
		panic(err)
	}
	// Output:
	// Hello World!
}

var sourceOfWork = make(chan struct{})

func otherLibrary(ctx context.Context) {
	<-ctx.Done()
}

// The stopper.Context type fits into existing context plumbing and can
// be retrieved later on.
func process[T any](ctx context.Context, work T) error {
	return stopper.Go(ctx, func(ctx context.Context) {
		// Do work...
		runtime.KeepAlive(work)
	})
}

// A pattern for creating a child stopper that processes a finite number
// of tasks.
func Example_workPool() {
	var count atomic.Int32

	// Assume the existence of some server-wide root stopper.
	rootContext := stopper.New()

	// Create a child stopper to handle a batch of tasks.
	childCtx := stopper.WithContext(rootContext,
		stopper.WithTaskOptions(
			// Record all errors instead of stopping on the first one.
			stopper.TaskErrHandler(stopper.ErrorHandlerRecord),
			// Push back on calls to Go() to limit total number of workers.
			stopper.TaskMiddleware(limit.WithMaxConcurrency(10)),
		))
	for range 20 {
		_ = stopper.Go(childCtx, func() error {
			count.Add(1)
			return nil
		})
	}
	// Make childCtx stop automatically. If the worker tasks above were
	// to create additional tasks or additional nested stoppers,
	// childCtx would wait for them to finish, too.
	childCtx.Stop(stopper.StopOnIdle())
	if err := childCtx.Wait(); err != nil {
		slog.ErrorContext(childCtx, "task error", "error", err)
	} else {
		fmt.Println(count.Load())
	}
	// Output:
	// 20
}

// Middleware are helper functions executed around tasks.
func ExampleMiddleware() {
	// Middleware can be attached to all tasks managed by the context or
	// its children.
	ctx := stopper.New(
		stopper.WithTaskOptions(
			stopper.TaskMiddleware(func(outer stopper.Context) (stopper.Context, stopper.Invoker) {
				// The setup phase of a Middleware is executed within
				// Call() or Go(), providing access to the invoking
				// context.
				fmt.Println("setup outer")
				return outer, func(ctx stopper.Context, task stopper.Func) error {
					// The inner context will be a separate goroutine if
					// Go() is called.
					fmt.Println("before outer")
					defer fmt.Println("after outer")
					return task(ctx)
				}
			}),
		),
	)

	// Middleware can also be attached to individual task executions.
	// These will be composed with the middleware attached to the
	// stopper.
	_ = stopper.Call(ctx,
		func() { fmt.Println("task") },
		stopper.TaskMiddleware(func(outer stopper.Context) (stopper.Context, stopper.Invoker) {
			fmt.Println("setup inner")
			return outer, func(ctx stopper.Context, task stopper.Func) error {
				fmt.Println("before inner")
				defer fmt.Println("after inner")
				return task(ctx)
			}
		}),
	)

	// Individual tasks can be isolated from inherited configuration.
	_ = stopper.Call(ctx,
		func() { fmt.Println("the end") },
		stopper.TaskNoInherit(),
	)
	// Output:
	// setup outer
	// setup inner
	// before outer
	// before inner
	// task
	// after inner
	// after outer
	// the end
}

// A pattern for using a stopper with an accept/poll type of network listening
// API.
func Example_netServer() {
	ctx := stopper.New()

	// Respond to interrupts.
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	stopper.StopOnReceive(ctx, ch)

	// Open a network listener.
	const addr = "127.0.0.1:13013"
	l, err := net.Listen("tcp", addr)
	if err != nil {
		slog.ErrorContext(ctx, "could not open listener", "error", err)
		return
	}
	// Close the listener during the soft-stop phase.
	_ = stopper.Go(ctx, func(ctx stopper.Context) {
		<-ctx.Stopping()
		_ = l.Close()
	})
	// Accept connections.
	_ = stopper.Go(ctx, func(ctx stopper.Context) {
		for {
			conn, err := l.Accept()
			// This returns an error when the listener has been closed.
			if err != nil {
				return
			}
			// Handle the connection in its own goroutine.
			_ = stopper.Go(ctx, func(ctx stopper.Context) {
				defer func() { _ = conn.Close() }()

				s := bufio.NewScanner(conn)
				s.Scan()
				fmt.Println(s.Text())

				// Simulate an OS signal to stop the app.
				ch <- os.Interrupt
			})
		}
	})
	// A task to send a message.
	_ = stopper.Go(ctx, func(ctx stopper.Context) error {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			return err
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

// An example showing the use of [stopper.Context.Call] when a goroutine
// is already allocated by some other API. In this case, the stdlib HTTP
// server creates a goroutine per request, but we still want to be able
// to interact with a stopper hierarchy.
func Example_httpServer() {
	ctx := stopper.New()

	// Respond to interrupts.
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	stopper.StopOnReceive(ctx, ch)

	// The HTTP server creates goroutines for us and has its own expectations
	// around goroutine/request lifecycles.
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// We use WithDelegate() here so that a caller hangup can
		// immediately stop any work. This is optional and might not be
		// desirable in all circumstances.
		err := stopper.Call(ctx.WithDelegate(r.Context()), func() error {
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
	defer svr.Close()

	// A task to make an HTTP request.
	_ = stopper.Go(ctx, func() error {
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

func ExampleContext_defer() {
	ctx := stopper.New()
	// Deferred functions are executed in reverse order.
	_, _ = stopper.Defer(ctx, func() {
		fmt.Println("defer 0")
	})
	_, _ = stopper.Defer(ctx, func() {
		fmt.Println("defer 1")
	})
	// This will run in a separate goroutine and then stop the context.
	_ = stopper.Go(ctx, func(ctx stopper.Context) {
		fmt.Println("task")
		ctx.Stop(stopper.StopGracePeriod(time.Second))
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
func Example_ticker() {
	ctx := stopper.New()

	_ = stopper.Go(ctx, func(ctx stopper.Context) error {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			// Do some background task.
			select {
			case <-ticker.C:
				// Loop around.
			case <-ctx.Stopping():
				// This channel closes when Stop() is called. The
				// context is not yet canceled at this point.
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
	// processes one second before the context is hard-canceled.
	ctx.Stop(stopper.StopGracePeriod(time.Second))

	// Callers can wait for all tasks to finish, similar to an ErrGroup.
	if err := ctx.Wait(); err != nil {
		slog.ErrorContext(ctx, "task error", "error", err)
	} else {
		fmt.Println("task count:", ctx.Len())
	}
	// Output:
	// task count: 1
	// task count: 0
}

// This example shows that contexts may be nested. Stop signals will
// propagate from enclosing to inner contexts, while the Len() and
// Wait() methods are aware of child contexts.
func ExampleContext_nesting() {
	outer := stopper.New()
	middle := stopper.WithContext(outer)
	inner := stopper.WithContext(middle)

	_ = stopper.Go(middle, func(ctx stopper.Context) {
		<-ctx.Stopping()
	})

	_ = stopper.Go(inner, func(ctx stopper.Context) {
		<-ctx.Stopping()
	})

	fmt.Println("outer", outer.Len())
	fmt.Println("middle", middle.Len())
	fmt.Println("inner", inner.Len())

	// Stopping a parent context stops the child contexts.
	outer.Stop(stopper.StopGracePeriod(time.Second))

	// Wait for all nested tasks.
	if err := outer.Wait(); err != nil {
		slog.ErrorContext(outer, "task error", "error", err)
	} else {
		fmt.Println("outer", outer.Len())
	}
	// Output:
	// outer 2
	// middle 2
	// inner 1
	// outer 0
}

func ExampleContext_stopOnIdle() {
	ctx := stopper.New()
	var nestedDidAccept atomic.Bool
	_ = stopper.Go(ctx, func(ctx stopper.Context) {
		// It's still valid to create additional tasks or additional
		// nested stoppers.
		err := stopper.Go(ctx, func() {
			// Do nested tasks...
		})
		nestedDidAccept.Store(err == nil)
	})
	// StopOnIdle shouldn't be called until all parent tasks have been
	// started.
	ctx.Stop(stopper.StopOnIdle())
	// Wait doesn't return until all tasks are complete.
	err := ctx.Wait()
	fmt.Printf("OK: %t %t\n", err == nil, nestedDidAccept.Load())

	// Output:
	// OK: true true
}

// A [stopper.Context] creates a [trace.Task] for itself and then for
// each task passed to [stopper.Context.Call] or [stopper.Context.Go].
func ExampleContext_tracing() {
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

	// Each stopper.Context has an implicit runtime/trace task, the name
	// of which can be set.
	ctx := stopper.WithContext(rootCtx, stopper.WithName("my stopper"))

	// Similarly, each use of Call() or Go() creates a trace task.
	_ = stopper.Go(ctx,
		func(ctx stopper.Context) {
			trace.Log(ctx, "message", "middle task is here")

			// Nested tasks will result in nested trace tasks.
			_ = stopper.Go(ctx,
				func(ctx stopper.Context) {
					trace.Log(ctx, "message", "inner task is here")
					ctx.Stop(stopper.StopGracePeriod(time.Second))
				},
				stopper.TaskName("inner task"),
			)
		},
		stopper.TaskName("middle task"),
	)

	if err := ctx.Wait(); err != nil {
		panic(err)
	}
	// Output:
	// trace written to trace.out
}

func ExampleTaskInfo() {
	ctx := stopper.New(stopper.WithName("outer"))
	nested := stopper.WithContext(ctx, stopper.WithName("inner"))
	_ = stopper.Call(nested,
		func(ctx context.Context) {
			if info, ok := stopper.TaskInfoFrom(ctx); ok {
				fmt.Println(info.ContextName)
				fmt.Println(info.TaskName)
			}
		},
		stopper.TaskName("my-task"))
	// Output:
	// outer.inner
	// my-task
}
