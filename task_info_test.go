// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"vawter.tech/stopper/v2/internal/tctx"
)

//go:embed testdata/task_tree.golden
var taskTreeGolden string

func TestSortTasks(t *testing.T) {
	r := require.New(t)
	g1 := &TaskGroup{Name: "group1"}
	g2 := &TaskGroup{Name: "group2"}
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC)

	running := func(g *TaskGroup, name string, started time.Time) *TaskInfo {
		return &TaskInfo{Group: g, TaskName: name, Started: started}
	}
	success := func(g *TaskGroup, name string, started time.Time) *TaskInfo {
		info := &TaskInfo{Group: g, TaskName: name, Started: started}
		var err error
		info.Error.Store(&err)
		return info
	}
	failed := func(g *TaskGroup, name string, started time.Time, msg string) *TaskInfo {
		info := &TaskInfo{Group: g, TaskName: name, Started: started}
		err := errors.New(msg)
		info.Error.Store(&err)
		return info
	}

	tasks := []*TaskInfo{
		failed(g1, "task1", t1, "err2"),
		failed(g1, "task1", t1, "err1"),
		failed(g1, "task1", t1, "err1"), // Same error message
		success(g1, "task1", t1),
		success(g1, "task1", t1), // Same success
		running(g1, "task1", t1),
		running(g1, "task1", t2),
		running(g1, "task2", t1),
		running(g2, "task1", t1),
		running(g2, "task1", t1), // Duplicate for equality check
	}

	slices.SortFunc(tasks, sortTasks)

	expected := []string{
		"group1.task1 (started 2026-01-01T00:00:00Z) (running)",
		"group1.task1 (started 2026-01-01T00:00:00Z) (success)",
		"group1.task1 (started 2026-01-01T00:00:00Z) (success)",
		"group1.task1 (started 2026-01-01T00:00:00Z) (failed err1)",
		"group1.task1 (started 2026-01-01T00:00:00Z) (failed err1)",
		"group1.task1 (started 2026-01-01T00:00:00Z) (failed err2)",
		"group1.task1 (started 2026-01-01T00:00:01Z) (running)",
		"group1.task2 (started 2026-01-01T00:00:00Z) (running)",
		"group2.task1 (started 2026-01-01T00:00:00Z) (running)",
		"group2.task1 (started 2026-01-01T00:00:00Z) (running)",
	}

	for i, task := range tasks {
		r.Equal(expected[i], task.String(), "at index %d", i)
	}
}

func TestTaskGroupMarshalJSONStability(t *testing.T) {
	r := require.New(t)

	setup := func() *TaskGroup {
		parent := &TaskGroup{Name: "parent"}
		for i := 0; i < 50; i++ {
			child := &TaskGroup{Name: fmt.Sprintf("child-%02d", i), Parent: parent}
			parent.children.Store(child, struct{}{})

			info := &TaskInfo{
				Group:    parent,
				TaskName: fmt.Sprintf("task-%02d", i),
			}
			parent.tasks.Store(info, struct{}{})
		}
		return parent
	}

	data1, err := json.Marshal(setup())
	r.NoError(err)

	data2, err := json.Marshal(setup())
	r.NoError(err)

	r.Equal(string(data1), string(data2), "JSON output should be stable")
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

func TestTaskTree(t *testing.T) {
	r := require.New(t)
	started := time.Date(2026, 2, 23, 12, 0, 0, 0, time.UTC)

	root := &TaskGroup{Name: "root"}
	child1 := &TaskGroup{Name: "child1", Parent: root}
	child2 := &TaskGroup{Name: "child2", Parent: root}
	root.children.Store(child1, struct{}{})
	root.children.Store(child2, struct{}{})

	task1 := &TaskInfo{
		Group:    root,
		Started:  started,
		TaskName: "task1",
	}
	root.tasks.Store(task1, struct{}{})

	task2 := &TaskInfo{
		Group:    child1,
		Started:  started,
		TaskName: "task2",
	}
	child1.tasks.Store(task2, struct{}{})

	var sb strings.Builder
	TaskTree(root, &sb)
	actual := sb.String()

	r.Equal(taskTreeGolden, actual)
}
