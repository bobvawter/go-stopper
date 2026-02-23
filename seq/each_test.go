// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package seq

import (
	"context"
	"errors"
	"iter"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"vawter.tech/stopper/v2"
)

func TestForEach(t *testing.T) {
	r := require.New(t)

	items := slices.Values([]string{"a", "b", "c"})

	var mu sync.Mutex
	var collected []string
	err := ForEach(t.Context(), items, 2, func(_ stopper.Context, _ int, v string) error {
		mu.Lock()
		defer mu.Unlock()
		collected = append(collected, v)
		return nil
	})
	r.NoError(err)
	slices.Sort(collected)
	r.Equal([]string{"a", "b", "c"}, collected)
}

func TestForEach2(t *testing.T) {
	r := require.New(t)

	m := map[string]int{"a": 1, "b": 2, "c": 3}
	items := func(yield func(string, int) bool) {
		for k, v := range m {
			if !yield(k, v) {
				return
			}
		}
	}

	var mu sync.Mutex
	collected := make(map[string]int)
	err := ForEach2(t.Context(), items, 3, func(_ stopper.Context, _ int, k string, v int) error {
		mu.Lock()
		defer mu.Unlock()
		collected[k] = v
		return nil
	})
	r.NoError(err)
	r.Equal(m, collected)
}

func TestForEach2CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	items := func(yield func(int, int) bool) {
		for i := range 3 {
			if !yield(i, i) {
				return
			}
		}
	}
	// A pre-canceled context must not cause ForEach2 to hang.
	// Whether an error is returned depends on timing.
	_ = ForEach2(ctx, items, 2, func(_ stopper.Context, _ int, _ int, _ int) error {
		return nil
	})
}

func TestForEach2Empty(t *testing.T) {
	r := require.New(t)

	items := func(yield func(string, int) bool) {}
	err := ForEach2(t.Context(), items, 4, func(_ stopper.Context, _ int, _ string, _ int) error {
		t.Fatal("should not be called")
		return nil
	})
	r.NoError(err)
}

func TestForEach2ErrorAggregation(t *testing.T) {
	r := require.New(t)

	errX := errors.New("error x")

	items := func(yield func(int, int) bool) {
		for i := range 5 {
			if !yield(i, i*10) {
				return
			}
		}
	}

	err := ForEach2(t.Context(), items, 5, func(_ stopper.Context, _ int, k int, _ int) error {
		if k == 2 {
			return errX
		}
		return nil
	})
	r.Error(err)
	r.ErrorIs(err, errX)
}

func TestForEach2Panic(t *testing.T) {
	r := require.New(t)

	items := func(yield func(string, int) bool) {
		_ = yield("a", 1) && yield("b", 2) && yield("c", 3)
	}

	err := ForEach2(t.Context(), items, 1, func(_ stopper.Context, _ int, k string, _ int) error {
		if k == "b" {
			panic("boom")
		}
		return nil
	})
	r.Error(err)
	r.ErrorContains(err, "boom")
}

func TestForEach2PanicError(t *testing.T) {
	r := require.New(t)

	errBoom := errors.New("kaboom")

	items := func(yield func(int, int) bool) {
		_ = yield(0, 10) && yield(1, 20)
	}

	err := ForEach2(t.Context(), items, 1, func(_ stopper.Context, _ int, k int, _ int) error {
		if k == 1 {
			panic(errBoom)
		}
		return nil
	})
	r.Error(err)
	r.ErrorIs(err, errBoom)
}

func TestForEach2Index(t *testing.T) {
	r := require.New(t)

	items := func(yield func(string, int) bool) {
		pairs := []struct {
			k string
			v int
		}{{"a", 10}, {"b", 20}, {"c", 30}}
		for _, p := range pairs {
			if !yield(p.k, p.v) {
				return
			}
		}
	}

	var mu sync.Mutex
	idxMap := make(map[int]string)
	err := ForEach2(t.Context(), items, 1, func(_ stopper.Context, idx int, k string, _ int) error {
		mu.Lock()
		defer mu.Unlock()
		idxMap[idx] = k
		return nil
	})
	r.NoError(err)
	r.Equal(map[int]string{0: "a", 1: "b", 2: "c"}, idxMap)
}

func TestForEach2SliceAll(t *testing.T) {
	r := require.New(t)

	source := []string{"a", "b", "c"}
	items := slices.All(source)

	var mu sync.Mutex
	collected := make(map[int]string)
	err := ForEach2(t.Context(), items, 3, func(_ stopper.Context, _ int, k int, v string) error {
		mu.Lock()
		defer mu.Unlock()
		collected[k] = v
		return nil
	})
	r.NoError(err)
	r.Equal(map[int]string{0: "a", 1: "b", 2: "c"}, collected)
}

func TestForEachCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	items := slices.Values([]int{1, 2, 3})
	// A pre-canceled context must not cause ForEach to hang.
	// Whether an error is returned depends on timing.
	_ = ForEach(ctx, items, 2, func(_ stopper.Context, _ int, _ int) error {
		return nil
	})
}

func TestForEachConcurrencyBound(t *testing.T) {
	r := require.New(t)

	const maxWorkers = 2
	items := slices.Values(make([]int, 20))

	var running atomic.Int32
	var maxSeen atomic.Int32

	err := ForEach(t.Context(), items, maxWorkers, func(_ stopper.Context, _ int, _ int) error {
		cur := running.Add(1)
		defer running.Add(-1)
		for {
			old := maxSeen.Load()
			if cur <= old || maxSeen.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		return nil
	})
	r.NoError(err)
	r.LessOrEqual(maxSeen.Load(), int32(maxWorkers))
	r.Greater(maxSeen.Load(), int32(0))
}

func TestForEachEmpty(t *testing.T) {
	r := require.New(t)

	items := slices.Values([]int{})
	err := ForEach(t.Context(), items, 4, func(_ stopper.Context, _ int, _ int) error {
		t.Fatal("should not be called")
		return nil
	})
	r.NoError(err)
}

func TestForEachPanic(t *testing.T) {
	r := require.New(t)

	items := slices.Values([]string{"a", "b", "c"})

	err := ForEach(t.Context(), items, 1, func(_ stopper.Context, _ int, v string) error {
		if v == "b" {
			panic("boom")
		}
		return nil
	})
	r.Error(err)
	r.ErrorContains(err, "boom")
}

func TestForEachPanicError(t *testing.T) {
	r := require.New(t)

	errBoom := errors.New("kaboom")

	items := slices.Values([]int{1, 2, 3})

	err := ForEach(t.Context(), items, 1, func(_ stopper.Context, _ int, v int) error {
		if v == 2 {
			panic(errBoom)
		}
		return nil
	})
	r.Error(err)
	r.ErrorIs(err, errBoom)
}

func TestForEachErrorAggregation(t *testing.T) {
	r := require.New(t)

	errA := errors.New("error a")
	errB := errors.New("error b")

	items := slices.Values([]int{0, 1, 2, 3})
	err := ForEach(t.Context(), items, 4, func(_ stopper.Context, idx int, _ int) error {
		switch idx {
		case 1:
			return errA
		case 3:
			return errB
		default:
			return nil
		}
	})
	r.Error(err)
	r.ErrorIs(err, errA)
	r.ErrorIs(err, errB)
}

func TestForEachIndex(t *testing.T) {
	r := require.New(t)

	items := slices.Values([]string{"x", "y", "z"})

	var mu sync.Mutex
	idxMap := make(map[int]string)
	err := ForEach(t.Context(), items, 1, func(_ stopper.Context, idx int, v string) error {
		mu.Lock()
		defer mu.Unlock()
		idxMap[idx] = v
		return nil
	})
	r.NoError(err)
	r.Equal(map[int]string{0: "x", 1: "y", 2: "z"}, idxMap)
}

func TestForEachSingleWorker(t *testing.T) {
	r := require.New(t)

	var order []int
	err := ForEach(t.Context(), yieldN(5), 1, func(_ stopper.Context, idx int, _ int) error {
		order = append(order, idx)
		return nil
	})
	r.NoError(err)
	r.Equal([]int{0, 1, 2, 3, 4}, order)
}

func TestForEachSliceSeq(t *testing.T) {
	r := require.New(t)

	source := []int{10, 20, 30, 40, 50}
	items := slices.Values(source)

	var sum atomic.Int64
	err := ForEach(t.Context(), items, 3, func(_ stopper.Context, _ int, v int) error {
		sum.Add(int64(v))
		return nil
	})
	r.NoError(err)
	r.Equal(int64(150), sum.Load())
}

// yieldN returns an iter.Seq that yields n items with their index as the value.
func yieldN(n int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := range n {
			if !yield(i) {
				return
			}
		}
	}
}
