// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"
)

type taskInfoKey struct{}

// A TaskInfo can be retrieved via [TaskInfoFrom] by [Middleware] or by
// tasks to be used as observability data. The enclosed channel allows
// event-driven lifecycle notifications.
type TaskInfo struct {
	Context     Context               // The undecorated Context executing the task.
	ContextName string                // A dotted [WithName].
	Done        <-chan struct{}       // Closed when the task has stopped executing.
	Error       atomic.Pointer[error] // Acts as a tri-state value.
	Started     time.Time             // Set before Middleware starts.
	Task        Func                  // The task being executed.
	TaskName    string                // The value passed to [TaskName].
}

// TaskInfoFrom returns a [TaskInfo] for the given context, or false if
// the context is not associated with a task.
func TaskInfoFrom(ctx context.Context) (*TaskInfo, bool) {
	found, ok := ctx.Value(taskInfoKey{}).(*TaskInfo)
	return found, ok
}

// MarshalJSON summarizes the TaskInfo.
func (i *TaskInfo) MarshalJSON() (ret []byte, err error) {
	p := struct {
		ContextName string    `json:"contextName,omitzero"`
		Error       string    `json:"error,omitzero"`
		Started     time.Time `json:"started,omitzero"`
		State       string    `json:"state,omitzero"`
		TaskName    string    `json:"taskName,omitzero"`
	}{
		ContextName: i.ContextName,
		Started:     i.Started,
		TaskName:    i.TaskName,
	}

	if ptr := i.Error.Load(); ptr == nil {
		p.State = "running"
	} else if err := *ptr; err == nil {
		p.State = "success"
	} else {
		p.Error = err.Error()
		p.State = "failed"
	}

	return json.Marshal(p)
}

// String is for debugging use only.
func (i *TaskInfo) String() string {
	var state string
	if ptr := i.Error.Load(); ptr == nil {
		state = "(running)"
	} else if err := *ptr; err == nil {
		state = "(success)"
	} else {
		state = fmt.Sprintf("(failed %v)", err)
	}

	return fmt.Sprintf("%s.%s (started %s) %s",
		i.ContextName, i.TaskName, i.Started, state)
}
