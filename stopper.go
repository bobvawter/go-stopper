// Copyright 2023 The Cockroach Authors
// Copyright 2025 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"runtime/trace"
	"time"

	"vawter.tech/stopper/v2/internal/safe"
	"vawter.tech/stopper/v2/internal/state"
)

// ErrStopped will be returned from [context.Cause] when the Context has
// been stopped.
var ErrStopped = state.ErrStopped

// ErrGracePeriodExpired will be returned from [context.Cause] when the
// Context has been stopped, but the goroutines have not exited.
var ErrGracePeriodExpired = state.ErrGracePeriodExpired

// Key is a [context.Context.Value] key for a [Context] used by [From].
type Key struct{}

// implKey is a [context.Context] key for a *impl.
type implKey struct{}

// A Context provides task lifecycle services.
//
// A Context implements a two-phase cancellation model consisting of a
// soft-stop signal ([Context.Stopping]) that allows graceful draining
// of long-running tasks before a hard-stop where the context is
// canceled ([Context.Done]).
//
// Context type embeds the stdlib [context.Context] type so that it may
// be freely combined with other golang libraries.  The package-level
// [From] function can be used to retrieve a stopper Context from any
// [context.Context]. The package-level [Call], [Defer], and [Go]
// functions provide convenient access to the task-launching services
// provided by a Context.
//
// Contexts may be created hierarchically, allowing stop signals to
// propagate from parents to children (but not the other way around). A
// variety of task Middleware may be attached to a stopper hierarchy or
// to individual task executions.
//
// All methods on a Context are safe for concurrent use.
//
// Users who intend to mock the [Context] interface should make their
// implementation of the Value method respond to [Key] with an instance
// of [Context].
type Context interface {
	context.Context

	// AddError appends additional errors to the value returned by
	// [Context.Wait]. This is useful if the Context is being stopped in
	// response to some external error. Note that this method does not
	// interact with the installed [ErrorHandler].
	AddError(err ...error)

	// Call executes the given function within the current goroutine and
	// monitors its lifecycle. That is, both Call and Wait will block
	// until the function has returned.
	//
	// Call returns any error from the function with no other side
	// effects. That is, it will not invoke any installed [ErrorHandler].
	//
	// If the Context has already been stopped, [ErrStopped] will be
	// returned.
	//
	// See [Call] or [Fn] to adapt various function signatures.
	Call(fn Func, opts ...TaskOption) error

	// Defer registers a callback that will be executed after
	// [Context.Stop] has been called and all tasks managed by the
	// Context have completed.
	//
	// This method can be used to clean up resources that are used by
	// goroutines associated with the Context (e.g. closing database
	// connections). The Context will already have been canceled by the
	// time the Func is run, so the deferred behaviors should be
	// associated with [context.Background] or similar. Callbacks will
	// be executed in a LIFO manner. Any error returned by the deferred
	// function will be available from [Context.Wait].
	//
	// If the Context has already stopped, the callback will be executed
	// immediately and this method will return false. Otherwise, this
	// method will return true to indicate that the callback was retained
	// for later execution.
	//
	// See [Defer] or [Fn] to adapt various function signatures.
	Defer(fn Func) (deferred bool)

	// Done implements [context.Context] and represents reaching the
	// hard-stop phase. The channel that is returned will be closed when
	// Stop has been called and all tasks and deferred functions have
	// completed. For soft-stop notifications, use [Context.Stopping].
	Done() <-chan struct{}

	// Err implements [context.Context]. It will be non-nil when the
	// Context has entered a hard-stop condition. The returned value
	// may be a wrapper over multiple errors.
	Err() error

	// Go spawns a new goroutine to execute the given Func and monitors
	// its lifecycle.
	//
	// If the Func returns an error, the task's [ErrorHandler] will be
	// invoked. The default handler is [ErrorHandlerStop], which will stop
	// the context on the first error.
	//
	// If the Context has already been stopped, [ErrStopped] will be
	// returned.
	//
	// See [Go] or [Fn] to adapt various function signatures.
	Go(fn Func, opts ...TaskOption) error

	// IsStopping returns true once [Stop] has been called.  See also
	// [Stopping] for a notification-based API.
	IsStopping() bool

	// Len returns the number of tasks being tracked by the Context.
	// This includes tasks managed by child stoppers.
	Len() int

	// Stop begins a graceful shutdown of the Context.
	//
	// When this method is called, the stopper will move into a
	// soft-stop condition by closing the [Context.Stopping] channel. It
	// will reject any new task creation. The stopper will move to a
	// hard-stop condition after a grace period has expired.
	//
	// Once all tasks managed by the Context have completed, the
	// associated Context will be canceled, thus closing the Done
	// channel.
	Stop(opts ...StopOption)

	// Stopping returns a channel that is closed when a graceful
	// shutdown has been requested or when a parent context has been
	// stopped.
	Stopping() <-chan struct{}

	// StoppingContext adapts the soft-stop behaviors of a stopper into
	// a [context.Context]. This can be used whenever it is necessary to
	// call other APIs that should be made aware of the soft-stop
	// condition.
	//
	// The returned context has the following behaviors:
	//   - The [context.Context.Done] method returns [Context.Stopping].
	//   - The [context.Context.Err] method returns an error that is
	//     both [context.Canceled] and [ErrStopped] if the context has
	//     been stopped. Otherwise, it returns [Context.Err].
	//   - All other interface methods delegate to the receiver.
	StoppingContext() context.Context

	// Wait will block until Stop has been called and all associated
	// tasks have exited or the parent context has been canceled. This
	// method will return errors from any of the tasks passed to Go or
	// via [StopError].
	Wait() error

	// WaitCtx is an interruptable version of [Context.Wait]. If the
	// argument's Done() channel is closed, the argument's Err()
	// value will be returned.
	WaitCtx(ctx context.Context) error

	// WithDelegate returns a Context that is otherwise equivalent to
	// the receiver, save that all [context.Context] behavior is
	// delegated to the new context. This enables interaction, generally
	// via [Middleware], with the [runtime/trace] package or other
	// libraries that generate custom [context.Context] instances.
	//
	// The WithDelegate method does not create a new, nested stopper
	// hierarchy, so it is less expensive than calling [WithContext] in
	// tracing scenarios.
	WithDelegate(ctx context.Context) Context
}

// From returns an enclosing Context or returns false if the argument is
// not managed by a stopper. This function will unwrap a stdlib context
// returned from [Context.StoppingContext].
func From(ctx context.Context) (found Context, ok bool) {
	if found, ok := ctx.Value(Key{}).(Context); ok {
		return found, true
	}
	return nil, false
}

// IsStopping is a convenience method to determine the argument is both
// managed by a stopper [Context] and that [Context] is stopping. This
// function will always return false if the argument is not managed by a
// stopper.
func IsStopping(ctx context.Context) bool {
	s, ok := From(ctx)
	if !ok {
		return false
	}
	return s.IsStopping()
}

// New returns a ready-to-use Context.
func New(opts ...ConfigOption) Context {
	return WithContext(context.Background(), opts...)
}

// WithContext creates a new Context whose work will be immediately
// canceled when the parent context is canceled. If the provided context
// is a stopper [Context], the newly constructed stopper will be a child
// of the pre-existing stopper.
func WithContext(ctx context.Context, opts ...ConfigOption) Context {
	var parent *state.State
	if i, ok := ctx.Value(implKey{}).(*impl); ok {
		parent = i.st
	}

	// Flatten and merge configuration data.
	next := &config{}
	for _, opt := range opts {
		opt(next)
	}
	var cfg *config
	if parent == nil {
		cfg = &config{}
	} else {
		cfg = parent.Config().(*config).Clone()
	}
	cfg.Merge(next)
	cfg.Sanitize()

	ctx, traceTask := trace.NewTask(ctx, cfg.name)
	ctx, cancel := context.WithCancelCause(ctx)
	cleanup := func(err error) {
		cancel(err)
		traceTask.End()
	}

	s := &impl{
		delegate: ctx,
		st:       state.New(cleanup, cfg, parent),
	}

	// Propagate a parent stop or context cancellation into a Stop call
	// to ensure that all notification channels are closed. This
	// goroutine is left untracked since it doesn't represent
	// user-provided work.
	go func() {
		if parent == nil {
			<-s.Done()
		} else {
			select {
			case <-parent.Stopping():
			case <-s.Done():
			}
		}
		s.Stop()
	}()
	return s
}

// Func is the canonical task function signature accepted by a
// [Context]. See [Fn] to convert other function signatures to a Func. A
// Func value should never be nil.
type Func func(ctx Context) error

// A RecoveredError will be returned by a task that panics.
type RecoveredError = safe.RecoveredError

type impl struct {
	delegate context.Context
	st       *state.State
}

var _ Context = (*impl)(nil)

func (c *impl) AddError(err ...error) { c.st.AddErrors(err...) }

func (c *impl) Call(fn Func, opts ...TaskOption) error {
	if !c.st.Apply(1) {
		return ErrStopped
	}
	defer func() { c.st.Apply(-1) }()

	iCtx, inv := c.taskInvoker(true, opts)
	return inv(iCtx, fn)
}

func (c *impl) Deadline() (deadline time.Time, ok bool) { return c.delegate.Deadline() }

func (c *impl) Defer(fn Func) bool {
	return c.st.AddDeferred(func() error {
		return fn(c)
	})
}

func (c *impl) Done() <-chan struct{} { return c.delegate.Done() }

func (c *impl) Err() error { return c.delegate.Err() }

func (c *impl) Len() int { return c.st.Len() }

func (c *impl) Go(fn Func, opts ...TaskOption) error {
	if !c.st.Apply(1) {
		return ErrStopped
	}
	iCtx, inv := c.taskInvoker(false, opts)
	go func() {
		defer c.st.Apply(-1)
		// The invoker will delegate to an ErrorHandler.
		_ = inv(iCtx, fn)
	}()
	return nil
}

func (c *impl) IsStopping() bool { return c.st.IsStopping() }

func (c *impl) Stop(opts ...StopOption) {
	// Initialize the stop configuration from the context config.
	stopCfg := &stop{
		gracePeriod: c.config().gracePeriod,
	}
	for _, opt := range opts {
		opt(stopCfg)
	}
	stopCfg.Sanitize()

	if stopCfg.onIdle {
		c.st.StopOnIdle(*stopCfg.gracePeriod)
	} else {
		c.st.Stop(*stopCfg.gracePeriod)
	}
}

func (c *impl) Stopping() <-chan struct{} { return c.st.Stopping() }

func (c *impl) StoppingContext() context.Context {
	return (*stoppingCtx)(c)
}

// String is for debugging use only.
func (c *impl) String() string {
	return fmt.Sprintf("%s: (%d tasks) (%d errors) (stopping=%t)",
		c.config().name, c.st.Len(), len(c.st.Errors()), c.st.IsStopping())
}

func (c *impl) Value(key any) any {
	switch key.(type) {
	case Key:
		return Context(c)
	case implKey:
		return c
	default:
		return c.delegate.Value(key)
	}
}

func (c *impl) Wait() error {
	return c.WaitCtx(context.Background())
}

func (c *impl) WaitCtx(ctx context.Context) error {
	select {
	case <-c.Done():
	case <-ctx.Done():
		return ctx.Err()
	}

	return errors.Join(c.st.Errors()...)
}

func (c *impl) WithDelegate(ctx context.Context) Context {
	return &impl{
		delegate: ctx,
		st:       c.st,
	}
}

func (c *impl) config() *config {
	return c.st.Config().(*config)
}

func (c *impl) taskInvoker(returnErr bool, opts []TaskOption) (Context, Invoker) {
	ctxCfg := c.config()
	taskCfg := applyTaskOpts(ctxCfg.taskOpts, opts)

	// Install runtime tracing.
	traceCtx, traceTask := trace.NewTask(c, taskCfg.name)
	iCtx := c.WithDelegate(traceCtx)

	// Call setup in declaration order.
	invokers := make([]Invoker, len(taskCfg.mw))
	for idx, mw := range taskCfg.mw {
		iCtx, invokers[idx] = mw(iCtx)
	}

	// Build the invocation chain from the bottom up.
	chain := InvokerCall
	for i := len(invokers) - 1; i >= 0; i-- {
		invoker := invokers[i] // Capture
		nextInChain := chain   // Capture
		chain = func(ctx Context, task Func) error {
			return invoker(ctx, func(ctx Context) error {
				return nextInChain(ctx, task)
			})
		}
	}

	// Install a panic handler at the root of the chain.
	return iCtx, func(ctx Context, task Func) (outErr error) {
		defer traceTask.End()

		// Optionally, inject TaskInfo at the root for Middleware.
		var taskDone chan struct{}
		var taskInfo *TaskInfo
		if !ctxCfg.noTaskInfo {
			taskDone = make(chan struct{})
			defer close(taskDone)

			taskInfo = &TaskInfo{
				Context:     c,
				ContextName: ctxCfg.name,
				Done:        taskDone,
				Started:     time.Now(),
				Task:        task,
				TaskName:    taskCfg.name,
			}
			ctx = ctx.WithDelegate(
				context.WithValue(ctx, taskInfoKey{}, taskInfo))
		}

		defer func() {
			if r := recover(); r != nil {
				rErr, ok := r.(error)
				if !ok {
					rErr = fmt.Errorf("%v", r)
				}
				stack := make([]uintptr, 32)
				stack = stack[:runtime.Callers(2, stack)]
				rErr = &RecoveredError{
					Err:   rErr,
					Stack: stack,
				}
				outErr = errors.Join(outErr, rErr)
			}
			if outErr == nil {
				return
			}
			outErr = fmt.Errorf("%s: %w", taskCfg.name, outErr)
			if taskInfo != nil {
				taskInfo.Error.Store(&outErr)
			}
			if returnErr {
				return
			}
			// Delegate to installed handler.
			if h := taskCfg.errHandler; h != nil {
				h(ctx, outErr)
				outErr = nil
			}
		}()

		return chain(ctx, task)
	}
}
