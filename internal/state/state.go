// Copyright 2023 The Cockroach Authors
// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

// Package state defines the core state-management types.
package state

import (
	"errors"
	"iter"
	"slices"
	"sync"
	"time"

	"vawter.tech/stopper/v2/internal/safe"
)

var (
	ErrStopped            = errors.New("stopped")
	ErrGracePeriodExpired = errors.New("grace period expired")
)

// A State may be shared between multiple Context instances.
type State struct {
	config   any    // General-purpose storage.
	parent   *State // Nil for top-level stoppers.
	stopping chan struct{}

	mu struct {
		sync.RWMutex                // Invariant: parents never lock children.
		cancel       func(error)    // Invoked via hardStopLocked.
		count        int            // Includes nested state counts.
		deferred     []func() error // Invoked via hardStopLocked.
		errs         []error
		graceTimer   *time.Timer // Created in Stop(), cleared in hardStopLocked.
		stopHooks    map[uint64]func()
		stopHooksID  uint64
		stopOnIdle   *time.Duration // The value is a grace period.
		stopping     bool
	}
}

func New(cancel func(error), config any, parent *State) *State {
	ret := &State{
		config:   config,
		parent:   parent,
		stopping: make(chan struct{}),
	}
	ret.mu.cancel = cancel
	return ret
}

// AddDeferred will execute the function if the State is already
// stopped. Otherwise, it will append it to the list of deferred
// callbacks.
func (s *State) AddDeferred(fn func() error) (deferred bool) {
	if s.addDeferred(fn) {
		return true
	}
	// We don't execute user code while holding a mutex.
	if err := safe.CallE(fn); err != nil {
		s.AddErrors(err)
	}
	return false
}

// addDeferred is a minimal critical section.
func (s *State) addDeferred(fn func() error) (deferred bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	invokeNow := s.mu.stopping && s.mu.count == 0
	if invokeNow {
		return false
	}
	s.mu.deferred = append(s.mu.deferred, fn)
	return true
}

// AddStopHook registers an internal callback for propagating stop
// calls. These hooks will be called synchronously from within
// softStopLocked immediately after the stopping channel has been
// closed. They must therefore not cause any reentrant behavior on the
// State context to which they're registered. The callback will be
// executed immediately if the state is already stopping.
func (s *State) AddStopHook(fn func()) (cancel func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mu.stopping {
		fn()
		return func() {}
	}
	if s.mu.stopHooks == nil {
		s.mu.stopHooks = make(map[uint64]func())
	}
	id := s.mu.stopHooksID
	s.mu.stopHooksID++
	s.mu.stopHooks[id] = fn
	return func() {
		// Disarm the cancel function if the state is already stopping.
		select {
		case <-s.stopping:
			return
		default:
		}

		s.mu.Lock()
		defer s.mu.Unlock()
		if s.mu.stopHooks != nil {
			delete(s.mu.stopHooks, id)
		}
	}
}

// Apply is used to maintain the count of started goroutines. It returns
// true if the delta was applied.
func (s *State) Apply(delta int) bool {
	// Execute any deferred callbacks outside the mutex.
	var deferred []func() error
	defer func() { s.callDeferred(deferred) }()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Don't allow new goroutines to be added when stopping.
	if s.mu.stopping && delta >= 0 {
		return false
	}

	// Ensure that nested jobs prolong the lifetime of the parent
	// context to prevent premature cancellation. Verify that the parent
	// accepted the delta in case it was just stopped, but our
	// stop-propagation callback hasn't fired yet.
	//
	// Note that the call to parent.Apply() is safe only because there
	// are currently no cases where a parent locks a child.
	if s.parent != nil && !s.parent.Apply(delta) {
		return false
	}

	s.mu.count += delta
	if s.mu.count < 0 {
		// Implementation error, not user problem.
		panic("over-released")
	}
	if s.mu.count == 0 {
		if s.mu.stopOnIdle != nil {
			deferred = s.softStopLocked(*s.mu.stopOnIdle)
		} else if s.mu.stopping {
			deferred = s.hardStopLocked(ErrStopped)
		}
	}
	return true
}

// AddErrors will add any non-nil errors to the State's error slice.
// Calling this method will not cause the State to stop.
func (s *State) AddErrors(errs ...error) {
	// Common cases.
	if len(errs) == 0 || len(errs) == 1 && errs[0] == nil {
		return
	}
	isErr := slices.ContainsFunc(errs, func(err error) bool { return err != nil })
	if !isErr {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, err := range errs {
		if err != nil {
			s.mu.errs = append(s.mu.errs, err)
		}
	}
}

// Config returns the configuration object passed to [New]. The returned
// value may be nil. To inspect all configuration objects in the State
// chain, use [State.Configs].
func (s *State) Config() any {
	return s.config
}

// Configs yields any non-nil configuration in the State chain.
func (s *State) Configs() iter.Seq[any] {
	return func(yield func(any) bool) {
		stack := make([]*State, 0, 8)
		for ptr := s; ptr != nil; ptr = ptr.parent {
			stack = append(stack, ptr)
		}
		for i := len(stack) - 1; i >= 0; i-- {
			if cfg := stack[i].config; cfg != nil {
				if !yield(cfg) {
					return
				}
			}
		}
	}
}

// Errors will return a clone of the internal slice.
func (s *State) Errors() []error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.mu.errs)
}

func (s *State) IsStopping() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mu.stopping
}

func (s *State) IsStopOnIdle() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mu.stopOnIdle != nil
}

func (s *State) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mu.count
}

func (s *State) Parent() *State { return s.parent }

func (s *State) Stop(gracePeriod time.Duration) {
	var deferred []func() error
	defer func() { s.callDeferred(deferred) }()

	s.mu.Lock()
	defer s.mu.Unlock()
	deferred = s.softStopLocked(gracePeriod)
}

func (s *State) StopOnIdle(gracePeriod time.Duration) {
	var deferred []func() error
	defer func() { s.callDeferred(deferred) }()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.mu.stopOnIdle = &gracePeriod
	if s.mu.count == 0 {
		deferred = s.softStopLocked(gracePeriod)
	}
}

func (s *State) Stopping() <-chan struct{} { return s.stopping }

// callDeferred executes the functions in reverse order, appending any
// errors. This method should not be called with the mutex held, since
// it's calling user-provided code.
func (s *State) callDeferred(toCall []func() error) {
	for i := len(toCall) - 1; i >= 0; i-- {
		if err := safe.CallE(toCall[i]); err != nil {
			s.AddErrors(err)
		}
	}
}

// hardStopLocked is a one-shot method to capture the final value to
// pass to the context-cancellation function. It returns any pending
// deferred callbacks to be executed outside the mutex.
func (s *State) hardStopLocked(err error) (deferred []func() error) {
	cancelFn := s.mu.cancel
	if cancelFn == nil {
		return
	}
	s.mu.cancel = nil

	// Cancel any pending grace period timer when we hit a clean exit.
	if timer := s.mu.graceTimer; timer != nil {
		timer.Stop()
		s.mu.graceTimer = nil
	}

	// Capture deferred callbacks to execute. We treat the final
	// hard-stop cancellation as though it were the first deferred
	// function registered.
	deferred = make([]func() error, len(s.mu.deferred)+1)
	deferred[0] = func() error {
		// We don't need to lock here since this closed-over variable is
		// the only remaining reference to the cancel function.
		cancelFn(err)
		return nil
	}
	copy(deferred[1:], s.mu.deferred)
	s.mu.deferred = nil

	return
}

// softStopLocked is a one-shot method to place the context into the
// stopping state. It will hard-stop the State if no tasks remain or
// when the grace period has expired. This method returns deferred
// callbacks to execute outside the mutex.
func (s *State) softStopLocked(gracePeriod time.Duration) (deferred []func() error) {
	if s.mu.stopping {
		// We want to re-check the number of tasks here if Stop() were
		// called with a grace period while tasks are pending. Once the
		// number of active tasks reaches zero, this method will be
		// called again.
		if s.mu.count == 0 {
			deferred = s.hardStopLocked(ErrStopped)
		}
		return
	}
	s.mu.stopping = true
	close(s.stopping)

	// These hooks do not call user-provided code.
	if hooks := s.mu.stopHooks; hooks != nil {
		for _, fn := range hooks {
			fn()
		}
		s.mu.stopHooks = nil
	}

	// We may still want to call into hardStopLocked(), which is a one-shot,
	if s.mu.count == 0 {
		// Cancel the context if nothing's currently running.
		deferred = s.hardStopLocked(ErrStopped)
	} else if gracePeriod <= 0 {
		// A non-positive grace period is effectively a hard-stop.
		deferred = s.hardStopLocked(ErrGracePeriodExpired)
	} else {
		// Cancel after the grace period has expired. This is built
		// using a timer so that hardStopLocked can stop the timer early
		// on a clean exit. A race between the timer callback and a
		// clean exit is safe since the timer callback reacquires the
		// mutex and hardStopLocked() is a one-shot.
		s.mu.graceTimer = time.AfterFunc(gracePeriod, func() {
			var toCall []func() error
			defer func() { s.callDeferred(toCall) }()

			s.mu.Lock()
			defer s.mu.Unlock()
			toCall = s.hardStopLocked(ErrGracePeriodExpired)
		})
	}
	return
}
