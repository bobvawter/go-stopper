// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"vawter.tech/stopper/v2/internal/tctx"
)

func TestTaskGroupMarshalJSON(t *testing.T) {
	r := require.New(t)
	parent := &TaskGroup{Name: "parent"}
	child := &TaskGroup{Name: "child", Parent: parent}
	parent.children.Store(child, struct{}{})

	info := &TaskInfo{
		Group:    parent,
		TaskName: "task",
	}
	parent.tasks.Store(info, struct{}{})

	data, err := json.Marshal(parent)
	r.NoError(err)

	var m map[string]any
	r.NoError(json.Unmarshal(data, &m))
	r.Equal("parent", m["name"])

	children := m["children"].([]any)
	r.Len(children, 1)
	r.Equal("child", children[0].(map[string]any)["name"])

	tasks := m["tasks"].([]any)
	r.Len(tasks, 1)
	r.Equal("task", tasks[0].(map[string]any)["taskName"])
}

func TestTaskGroupString(t *testing.T) {
	r := require.New(t)
	g := &TaskGroup{Name: "foo"}
	r.Equal("foo", g.String())
}

func TestTaskInfoMarshalJSON(t *testing.T) {
	r := require.New(t)
	started := time.Date(2026, 2, 23, 12, 0, 0, 0, time.UTC)

	t.Run("running", func(t *testing.T) {
		r := require.New(t)
		info := &TaskInfo{
			Group: &TaskGroup{
				Name: "ctx",
			},
			Started:  started,
			TaskName: "task",
		}
		data, err := info.MarshalJSON()
		r.NoError(err)

		var m map[string]any
		r.NoError(json.Unmarshal(data, &m))
		r.Equal("ctx", m["contextName"])
		r.Equal("task", m["taskName"])
		r.Equal("running", m["state"])
		r.NotContains(m, "error")
	})

	t.Run("success", func(t *testing.T) {
		r := require.New(t)
		info := &TaskInfo{
			Group: &TaskGroup{
				Name: "ctx",
			},
			Started:  started,
			TaskName: "task",
		}
		var nilErr error
		info.Error.Store(&nilErr)

		data, err := info.MarshalJSON()
		r.NoError(err)

		var m map[string]any
		r.NoError(json.Unmarshal(data, &m))
		r.Equal("success", m["state"])
		r.NotContains(m, "error")
	})

	t.Run("failed", func(t *testing.T) {
		r := require.New(t)
		info := &TaskInfo{
			Group: &TaskGroup{
				Name: "ctx",
			},
			Started:  started,
			TaskName: "task",
		}
		taskErr := fmt.Errorf("boom")
		info.Error.Store(&taskErr)

		data, err := info.MarshalJSON()
		r.NoError(err)

		var m map[string]any
		r.NoError(json.Unmarshal(data, &m))
		r.Equal("failed", m["state"])
		r.Equal("boom", m["error"])
	})

	// Verify round-trip through json.Marshal uses MarshalJSON.
	info := &TaskInfo{
		Group: &TaskGroup{
			Name: "ctx",
		},
		Started:  started,
		TaskName: "rt-task",
	}
	data, err := json.Marshal(info)
	r.NoError(err)
	r.Contains(string(data), `"state":"running"`)
}

func TestTaskInfoString(t *testing.T) {
	started := time.Date(2026, 2, 23, 12, 0, 0, 0, time.UTC)

	t.Run("running", func(t *testing.T) {
		r := require.New(t)
		info := &TaskInfo{
			Group: &TaskGroup{
				Name: "ctx",
			},
			Started:  started,
			TaskName: "task",
		}
		s := info.String()
		r.Contains(s, "ctx.task")
		r.Contains(s, "(running)")
	})

	t.Run("success", func(t *testing.T) {
		r := require.New(t)
		info := &TaskInfo{
			Group: &TaskGroup{
				Name: "ctx",
			},
			Started:  started,
			TaskName: "task",
		}
		var nilErr error
		info.Error.Store(&nilErr)
		s := info.String()
		r.Contains(s, "(success)")
	})

	t.Run("failed", func(t *testing.T) {
		r := require.New(t)
		info := &TaskInfo{
			Group: &TaskGroup{
				Name: "ctx",
			},
			Started:  started,
			TaskName: "task",
		}
		taskErr := fmt.Errorf("boom")
		info.Error.Store(&taskErr)
		s := info.String()
		r.Contains(s, "(failed boom)")
	})
}

func TestTaskInfoFrom(t *testing.T) {
	r := require.New(t)

	s := New(WithName("my-ctx"))

	r.NoError(s.Call(func(ctx Context) error {
		info, ok := TaskInfoFrom(ctx)
		r.True(ok)
		r.Equal("my-ctx", info.Group.Name)
		r.Equal("my-task", info.TaskName)
		r.NotSame(info.Context, ctx)
		r.NotNil(info.Task)
		r.False(info.Started.IsZero())
		r.Contains(info.String(), "my-ctx.my-task")
		return nil
	}, TaskName("my-task")))

	s.Stop()
	_ = s.Wait()
}

func TestTaskInfoFromMissing(t *testing.T) {
	r := require.New(t)

	_, ok := TaskInfoFrom(tctx.Context(t))
	r.False(ok)
}

func TestTaskInfoFromMiddleware(t *testing.T) {
	r := require.New(t)

	var seen *TaskInfo
	mw := func(outer Context) (Context, Invoker) {
		// TaskInfo can be accessed during setup.
		setup, ok := TaskInfoFrom(outer)
		r.True(ok)
		return outer, func(ctx Context, task Func) error {
			// We also ensure it's propagated to the invocation.
			info, ok := TaskInfoFrom(ctx)
			r.True(ok)
			r.Same(setup, info)
			seen = info
			return task(ctx)
		}
	}

	s := New(WithName("mw-ctx"))

	taskErr := fmt.Errorf("task-error")
	gotErr := s.Call(func(_ Context) error {
		return taskErr
	}, TaskName("mw-task"), TaskMiddleware(mw))

	r.NotNil(seen)
	r.Equal("mw-ctx", seen.Group.Name)
	r.Equal("mw-task", seen.TaskName)
	r.False(seen.Started.IsZero())

	// Done channel must be closed after Call returns.
	select {
	case <-seen.Done:
	default:
		r.Fail("expected Done channel to be closed")
	}

	// Error must wrap the original task error.
	r.ErrorIs(gotErr, taskErr)
	ptr := seen.Error.Load()
	r.NotNil(ptr)
	r.ErrorIs(*ptr, taskErr)

	// Check the error pointer for a successful task points to nil.
	r.NoError(s.Call(
		func(_ Context) error { return nil },
		TaskMiddleware(mw)),
	)
	ptr = seen.Error.Load()
	r.NotNil(ptr)
	r.NoError(*ptr)

	s.Stop()
	_ = s.Wait()
}

func TestTaskInfoFromMiddlewarePanic(t *testing.T) {
	r := require.New(t)

	var seen *TaskInfo
	mw := func(outer Context) (Context, Invoker) {
		return outer, func(ctx Context, task Func) error {
			info, ok := TaskInfoFrom(ctx)
			r.True(ok)
			seen = info
			return task(ctx)
		}
	}

	s := New(WithName("panic-ctx"))

	gotErr := s.Call(func(_ Context) error {
		panic("kaboom")
	}, TaskName("panic-task"), TaskMiddleware(mw))

	r.NotNil(seen)
	r.Equal("panic-ctx", seen.Group.Name)
	r.Equal("panic-task", seen.TaskName)

	// Done channel must be closed after Call returns.
	select {
	case <-seen.Done:
	default:
		r.Fail("expected Done channel to be closed")
	}

	// Error must contain a RecoveredError wrapping the panic value.
	r.Error(gotErr)
	var recovered *RecoveredError
	r.True(errors.As(gotErr, &recovered))
	r.Contains(recovered.Error(), "kaboom")

	ptr := seen.Error.Load()
	r.NotNil(ptr)
	r.True(errors.As(*ptr, &recovered))

	s.Stop()
	_ = s.Wait()
}

func TestTaskInfoFromNested(t *testing.T) {
	r := require.New(t)

	parent := New(WithName("parent"))
	parentGroup, ok := TaskGroupFrom(parent)
	r.True(ok)

	child := WithContext(parent, WithName("child"))
	childGroup, ok := TaskGroupFrom(child)
	r.True(ok)
	r.Same(parentGroup, childGroup.Parent)

	children := parentGroup.Children(nil)
	r.Len(children, 1)
	r.Same(childGroup, children[0])

	r.NoError(child.Call(func(ctx Context) error {
		info, ok := TaskInfoFrom(ctx)
		r.True(ok)

		r.Equal("parent.child", info.Group.Name)
		r.Same(childGroup, info.Group)
		tasks := info.Group.Tasks(nil)
		r.Len(tasks, 1)
		r.Same(info, tasks[0])

		r.Equal("nested-task", info.TaskName)
		return nil
	}, TaskName("nested-task")))

	child.Stop()
	r.Empty(parentGroup.Children(nil))
	parent.Stop()
	_ = parent.Wait()
}

func TestTaskInfoFromContextNoTaskInfo(t *testing.T) {
	r := require.New(t)

	s := New(WithName("no-info"), WithNoTaskInfo())

	r.NoError(s.Call(func(ctx Context) error {
		_, ok := TaskInfoFrom(ctx)
		r.False(ok, "TaskInfo should not be attached when WithNoTaskInfo is set")
		return nil
	}, TaskName("ignored")))

	s.Stop()
	_ = s.Wait()
}

func TestTaskInfoFromDefaults(t *testing.T) {
	r := require.New(t)

	_, newFile, newLine, _ := runtime.Caller(0)
	s := New()

	_, callFile, callLine, _ := runtime.Caller(0)
	r.NoError(s.Call(func(ctx Context) error {
		info, ok := TaskInfoFrom(ctx)
		r.True(ok)
		r.True(strings.Contains(info.Group.Name, fmt.Sprintf("%s:%d", newFile, newLine+1)))
		r.True(strings.Contains(info.TaskName, fmt.Sprintf("%s:%d", callFile, callLine+1)))
		return nil
	}))

	_, callFile, callLine, _ = runtime.Caller(0)
	r.NoError(Call(s, func(ctx Context) error {
		info, ok := TaskInfoFrom(ctx)
		r.True(ok)
		r.True(strings.Contains(info.Group.Name, fmt.Sprintf("%s:%d", newFile, newLine+1)))
		r.True(strings.Contains(info.TaskName, fmt.Sprintf("%s:%d", callFile, callLine+1)))
		return nil
	}))

	_, goMethodFile, goMethodLine, _ := runtime.Caller(0)
	r.NoError(s.Go(func(ctx Context) error {
		info, ok := TaskInfoFrom(ctx)
		r.True(ok)
		r.True(strings.Contains(info.Group.Name, fmt.Sprintf("%s:%d", newFile, newLine+1)))
		r.True(strings.Contains(info.TaskName, fmt.Sprintf("%s:%d", goMethodFile, goMethodLine+1)))
		return nil
	}))

	_, goHelperFile, goHelperLine, _ := runtime.Caller(0)
	r.NoError(Go(s, func(ctx Context) error {
		info, ok := TaskInfoFrom(ctx)
		r.True(ok)
		r.True(strings.Contains(info.Group.Name, fmt.Sprintf("%s:%d", newFile, newLine+1)))
		r.True(strings.Contains(info.TaskName, fmt.Sprintf("%s:%d", goHelperFile, goHelperLine+1)))
		return nil
	}))

	s.Stop()
	r.NoError(s.Wait())
}
