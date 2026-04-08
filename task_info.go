// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type taskGroupKey struct{}

// A TaskGroup can be retrieved from any Context to be used as
// observability data.
type TaskGroup struct {
	Name   string
	Parent *TaskGroup

	// Keys are *TaskGroup.
	children sync.Map

	// Keys are *TaskInfo. A sync.Map is chosen for this use case
	// because most interactions are a task inserting its info and then
	// deleting it.
	tasks sync.Map
}

// TaskGroupFrom returns a [TaskGroup] for the given context, or false
// if the argument is not associated with a [Context].
func TaskGroupFrom(ctx context.Context) (*TaskGroup, bool) {
	found, ok := ctx.Value(taskGroupKey{}).(*TaskGroup)
	return found, ok
}

// Children appends the child groups of the receiver to buf and returns
// it.
func (g *TaskGroup) Children(buf []*TaskGroup) []*TaskGroup {
	g.children.Range(func(k, v interface{}) bool {
		buf = append(buf, k.(*TaskGroup))
		return true
	})
	return buf
}

// MarshalJSON summarizes the TaskGroup.
func (g *TaskGroup) MarshalJSON() ([]byte, error) {
	p := struct {
		Children []*TaskGroup `json:"children,omitempty"`
		Name     string       `json:"name,omitempty"`
		Tasks    []*TaskInfo  `json:"tasks,omitempty"`
	}{
		Children: g.Children(make([]*TaskGroup, 0, 8)),
		Name:     g.Name,
		Tasks:    g.Tasks(make([]*TaskInfo, 0, 8)),
	}
	return json.Marshal(p)
}

// String is for debugging use only.
func (g *TaskGroup) String() string {
	return g.Name
}

// Tasks appends the tasks contained within the group to buf and returns
// the buffer.
func (g *TaskGroup) Tasks(buf []*TaskInfo) []*TaskInfo {
	g.tasks.Range(func(k, v any) bool {
		buf = append(buf, k.(*TaskInfo))
		return true
	})
	return buf
}

type taskInfoKey struct{}

// A TaskInfo can be retrieved via [TaskInfoFrom] by [Middleware] or by
// tasks to be used as observability data. The enclosed channel allows
// event-driven lifecycle notifications.
type TaskInfo struct {
	Context  Context               // The undecorated Context executing the task.
	Done     <-chan struct{}       // Closed when the task has stopped executing.
	Error    atomic.Pointer[error] // Acts as a tri-state value.
	Group    *TaskGroup            // The group containing sibling tasks.
	Started  time.Time             // Set before [Middleware] starts.
	Task     Func                  // The task being executed.
	TaskName string                // The value passed to [TaskName].
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
		ContextName string    `json:"contextName,omitempty"`
		Error       string    `json:"error,omitempty"`
		Started     time.Time `json:"started,omitempty"`
		State       string    `json:"state,omitempty"`
		TaskName    string    `json:"taskName,omitempty"`
	}{
		ContextName: i.Group.Name,
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
		i.Group.Name, i.TaskName, i.Started, state)
}
