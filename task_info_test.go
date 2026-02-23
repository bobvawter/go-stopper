// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package stopper

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTaskInfoMarshalJSON(t *testing.T) {
	r := require.New(t)
	started := time.Date(2026, 2, 23, 12, 0, 0, 0, time.UTC)

	t.Run("running", func(t *testing.T) {
		r := require.New(t)
		info := &TaskInfo{
			ContextName: "ctx",
			Started:     started,
			TaskName:    "task",
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
			ContextName: "ctx",
			Started:     started,
			TaskName:    "task",
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
			ContextName: "ctx",
			Started:     started,
			TaskName:    "task",
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
		ContextName: "rt",
		Started:     started,
		TaskName:    "rt-task",
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
			ContextName: "ctx",
			Started:     started,
			TaskName:    "task",
		}
		s := info.String()
		r.Contains(s, "ctx.task")
		r.Contains(s, "(running)")
	})

	t.Run("success", func(t *testing.T) {
		r := require.New(t)
		info := &TaskInfo{
			ContextName: "ctx",
			Started:     started,
			TaskName:    "task",
		}
		var nilErr error
		info.Error.Store(&nilErr)
		s := info.String()
		r.Contains(s, "(success)")
	})

	t.Run("failed", func(t *testing.T) {
		r := require.New(t)
		info := &TaskInfo{
			ContextName: "ctx",
			Started:     started,
			TaskName:    "task",
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
		r.Equal("my-ctx", info.ContextName)
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

	_, ok := TaskInfoFrom(t.Context())
	r.False(ok)
}

func TestTaskInfoFromMiddleware(t *testing.T) {
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

	s := New(WithName("mw-ctx"))

	taskErr := fmt.Errorf("task-error")
	gotErr := s.Call(func(_ Context) error {
		return taskErr
	}, TaskName("mw-task"), TaskMiddleware(mw))

	r.NotNil(seen)
	r.Equal("mw-ctx", seen.ContextName)
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
	r.Equal("panic-ctx", seen.ContextName)
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
	child := WithContext(parent, WithName("child"))

	r.NoError(child.Call(func(ctx Context) error {
		info, ok := TaskInfoFrom(ctx)
		r.True(ok)
		r.Equal("parent.child", info.ContextName)
		r.Equal("nested-task", info.TaskName)
		return nil
	}, TaskName("nested-task")))

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

	s := New()

	r.NoError(s.Call(func(ctx Context) error {
		info, ok := TaskInfoFrom(ctx)
		r.True(ok)
		r.Equal("stopper", info.ContextName)
		r.Equal("task", info.TaskName)
		return nil
	}))

	s.Stop()
	_ = s.Wait()
}
