// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package seq

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"vawter.tech/stopper/v2"
)

// --- Map ---

func TestMap(t *testing.T) {
	r := require.New(t)

	items := slices.Values([]int{1, 2, 3, 4, 5})
	results := Map(t.Context(), items, 2,
		func(_ stopper.Context, _ int, v int) (int, error) {
			return v * 10, nil
		},
	)

	var got []int
	for v, err := range results {
		r.NoError(err)
		got = append(got, v)
	}
	r.Equal([]int{10, 20, 30, 40, 50}, got)
}

func TestMapCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	items := slices.Values([]int{1, 2, 3})
	results := Map(ctx, items, 2,
		func(_ stopper.Context, _ int, v int) (int, error) {
			return v, nil
		},
	)

	// A pre-canceled context must not hang.
	for range results {
	}
}

func TestMapEarlyBreak(t *testing.T) {
	r := require.New(t)

	items := slices.Values([]int{1, 2, 3, 4, 5})
	results := Map(t.Context(), items, 1,
		func(_ stopper.Context, _ int, v int) (int, error) {
			return v * 10, nil
		},
	)

	var got []int
	for v, err := range results {
		r.NoError(err)
		got = append(got, v)
		if len(got) == 2 {
			break
		}
	}
	r.Equal([]int{10, 20}, got)
}

func TestMapConcurrencyBound(t *testing.T) {
	r := require.New(t)

	const maxWorkers = 3
	items := slices.Values(make([]int, 20))

	var running atomic.Int32
	var maxSeen atomic.Int32

	results := Map(t.Context(), items, maxWorkers,
		func(_ stopper.Context, _ int, _ int) (int, error) {
			cur := running.Add(1)
			defer running.Add(-1)
			for {
				old := maxSeen.Load()
				if cur <= old || maxSeen.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			return 0, nil
		},
	)

	for _, err := range results {
		r.NoError(err)
	}
	r.LessOrEqual(maxSeen.Load(), int32(maxWorkers))
	r.Greater(maxSeen.Load(), int32(0))
}

func TestMapEmpty(t *testing.T) {
	r := require.New(t)

	items := slices.Values([]int{})
	results := Map(t.Context(), items, 4,
		func(_ stopper.Context, _ int, _ int) (int, error) {
			t.Fatal("should not be called")
			return 0, nil
		},
	)

	var count int
	for _, err := range results {
		r.NoError(err)
		count++
	}
	r.Zero(count)
}

func TestMapError(t *testing.T) {
	r := require.New(t)

	errBad := errors.New("bad value")
	items := slices.Values([]int{1, 2, 3})
	results := Map(t.Context(), items, 1,
		func(_ stopper.Context, _ int, v int) (int, error) {
			if v == 2 {
				return 0, errBad
			}
			return v * 10, nil
		},
	)

	var got []int
	var sawErr bool
	for v, err := range results {
		if err != nil {
			r.ErrorIs(err, errBad)
			sawErr = true
		} else {
			got = append(got, v)
		}
	}
	r.True(sawErr)
	r.Contains(got, 10)
	r.Contains(got, 30)
}

func TestMapIndex(t *testing.T) {
	r := require.New(t)

	items := slices.Values([]string{"a", "b", "c"})
	results := Map(t.Context(), items, 1,
		func(_ stopper.Context, idx int, v string) (string, error) {
			return fmt.Sprintf("%d:%s", idx, v), nil
		},
	)

	var got []string
	for v, err := range results {
		r.NoError(err)
		got = append(got, v)
	}
	r.Equal([]string{"0:a", "1:b", "2:c"}, got)
}

func TestMapOrdered(t *testing.T) {
	r := require.New(t)

	items := slices.Values([]int{1, 2, 3, 4, 5, 6, 7, 8})
	results := Map(t.Context(), items, 4,
		func(_ stopper.Context, _ int, v int) (int, error) {
			// Varying delays to test ordering.
			if v%2 == 0 {
				time.Sleep(5 * time.Millisecond)
			}
			return v * 10, nil
		},
	)

	var got []int
	for v, err := range results {
		r.NoError(err)
		got = append(got, v)
	}
	r.Equal([]int{10, 20, 30, 40, 50, 60, 70, 80}, got)
}

func TestMapPanic(t *testing.T) {
	r := require.New(t)

	items := slices.Values([]string{"a", "b", "c"})
	results := Map(t.Context(), items, 1,
		func(_ stopper.Context, _ int, v string) (string, error) {
			if v == "b" {
				panic("boom")
			}
			return v, nil
		},
	)

	var sawPanic bool
	for _, err := range results {
		if err != nil {
			r.ErrorContains(err, "boom")
			sawPanic = true
		}
	}
	r.True(sawPanic)
}

func TestMapPanicError(t *testing.T) {
	r := require.New(t)

	errBoom := errors.New("kaboom")
	items := slices.Values([]int{1, 2, 3})
	results := Map(t.Context(), items, 1,
		func(_ stopper.Context, _ int, v int) (int, error) {
			if v == 2 {
				panic(errBoom)
			}
			return v * 10, nil
		},
	)

	var sawPanic bool
	for _, err := range results {
		if err != nil {
			r.ErrorIs(err, errBoom)
			sawPanic = true
		}
	}
	r.True(sawPanic)
}

func TestMapPanicAllWorkers(t *testing.T) {
	r := require.New(t)

	items := slices.Values([]int{1, 2, 3})
	results := Map(t.Context(), items, 3,
		func(_ stopper.Context, _ int, _ int) (int, error) {
			panic("all workers panic")
		},
	)

	var errCount int
	for _, err := range results {
		if err != nil {
			r.ErrorContains(err, "all workers panic")
			errCount++
		}
	}
	r.Greater(errCount, 0)
}

func TestMapRepeatable(t *testing.T) {
	r := require.New(t)

	items := slices.Values([]int{1, 2, 3})
	results := Map(t.Context(), items, 1,
		func(_ stopper.Context, _ int, v int) (int, error) {
			return v * 10, nil
		},
	)

	// Iterate twice to verify repeatability.
	for range 2 {
		var got []int
		for v, err := range results {
			r.NoError(err)
			got = append(got, v)
		}
		r.Equal([]int{10, 20, 30}, got)
	}
}

func TestMapSingleWorker(t *testing.T) {
	r := require.New(t)

	results := Map(t.Context(), yieldN(5), 1,
		func(_ stopper.Context, idx int, _ int) (int, error) {
			return idx, nil
		},
	)

	var got []int
	for v, err := range results {
		r.NoError(err)
		got = append(got, v)
	}
	r.Equal([]int{0, 1, 2, 3, 4}, got)
}

// --- Map2 ---

func TestMap2(t *testing.T) {
	r := require.New(t)

	items := slices.All([]string{"a", "b", "c"})
	results := Map2(t.Context(), items, 2,
		func(_ stopper.Context, _ int, k int, v string) (string, error) {
			return fmt.Sprintf("%d=%s", k, v), nil
		},
	)

	var got []string
	for v, err := range results {
		r.NoError(err)
		got = append(got, v)
	}
	r.Equal([]string{"0=a", "1=b", "2=c"}, got)
}

func TestMap2EarlyBreak(t *testing.T) {
	r := require.New(t)

	items := slices.All([]string{"a", "b", "c", "d", "e"})
	results := Map2(t.Context(), items, 1,
		func(_ stopper.Context, _ int, k int, v string) (string, error) {
			return fmt.Sprintf("%d=%s", k, v), nil
		},
	)

	var got []string
	for v, err := range results {
		r.NoError(err)
		got = append(got, v)
		if len(got) == 2 {
			break
		}
	}
	r.Equal([]string{"0=a", "1=b"}, got)
}

func TestMap2CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	items := slices.All([]int{1, 2, 3})
	results := Map2(ctx, items, 2,
		func(_ stopper.Context, _ int, _ int, _ int) (int, error) {
			return 0, nil
		},
	)

	for range results {
	}
}

func TestMap2Empty(t *testing.T) {
	r := require.New(t)

	items := slices.All([]int{})
	results := Map2(t.Context(), items, 4,
		func(_ stopper.Context, _ int, _ int, _ int) (int, error) {
			t.Fatal("should not be called")
			return 0, nil
		},
	)

	var count int
	for _, err := range results {
		r.NoError(err)
		count++
	}
	r.Zero(count)
}

func TestMap2Error(t *testing.T) {
	r := require.New(t)

	errBad := errors.New("bad pair")
	items := slices.All([]string{"a", "b", "c"})
	results := Map2(t.Context(), items, 1,
		func(_ stopper.Context, _ int, k int, _ string) (string, error) {
			if k == 1 {
				return "", errBad
			}
			return "ok", nil
		},
	)

	var sawErr bool
	for _, err := range results {
		if err != nil {
			r.ErrorIs(err, errBad)
			sawErr = true
		}
	}
	r.True(sawErr)
}

func TestMap2Ordered(t *testing.T) {
	r := require.New(t)

	items := slices.All([]int{10, 20, 30, 40})
	results := Map2(t.Context(), items, 4,
		func(_ stopper.Context, _ int, k int, v int) (int, error) {
			if k%2 == 0 {
				time.Sleep(5 * time.Millisecond)
			}
			return k + v, nil
		},
	)

	var got []int
	for v, err := range results {
		r.NoError(err)
		got = append(got, v)
	}
	r.Equal([]int{10, 21, 32, 43}, got)
}

func TestMap2Panic(t *testing.T) {
	r := require.New(t)

	items := slices.All([]string{"a", "b", "c"})
	results := Map2(t.Context(), items, 1,
		func(_ stopper.Context, _ int, k int, _ string) (string, error) {
			if k == 1 {
				panic("map2 boom")
			}
			return "ok", nil
		},
	)

	var sawPanic bool
	for _, err := range results {
		if err != nil {
			r.ErrorContains(err, "map2 boom")
			sawPanic = true
		}
	}
	r.True(sawPanic)
}

func TestMap2PanicError(t *testing.T) {
	r := require.New(t)

	errBoom := errors.New("map2 kaboom")
	items := slices.All([]int{1, 2, 3})
	results := Map2(t.Context(), items, 1,
		func(_ stopper.Context, _ int, k int, _ int) (int, error) {
			if k == 1 {
				panic(errBoom)
			}
			return 0, nil
		},
	)

	var sawPanic bool
	for _, err := range results {
		if err != nil {
			r.ErrorIs(err, errBoom)
			sawPanic = true
		}
	}
	r.True(sawPanic)
}

// --- MapUnordered ---

func TestMapUnordered(t *testing.T) {
	r := require.New(t)

	items := slices.Values([]int{1, 2, 3, 4, 5})
	results := MapUnordered(t.Context(), items, 3,
		func(_ stopper.Context, _ int, v int) (int, error) {
			return v * 10, nil
		},
	)

	var got []int
	for v, err := range results {
		r.NoError(err)
		got = append(got, v)
	}
	sort.Ints(got)
	r.Equal([]int{10, 20, 30, 40, 50}, got)
}

func TestMapUnorderedEarlyBreak(t *testing.T) {
	r := require.New(t)

	items := slices.Values([]int{1, 2, 3, 4, 5})
	results := MapUnordered(t.Context(), items, 1,
		func(_ stopper.Context, _ int, v int) (int, error) {
			return v * 10, nil
		},
	)

	var got []int
	for v, err := range results {
		r.NoError(err)
		got = append(got, v)
		if len(got) == 2 {
			break
		}
	}
	r.Len(got, 2)
}

func TestMapUnorderedCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	items := slices.Values([]int{1, 2, 3})
	results := MapUnordered(ctx, items, 2,
		func(_ stopper.Context, _ int, v int) (int, error) {
			return v, nil
		},
	)

	for range results {
	}
}

func TestMapUnorderedConcurrencyBound(t *testing.T) {
	r := require.New(t)

	const maxWorkers = 3
	items := slices.Values(make([]int, 20))

	var running atomic.Int32
	var maxSeen atomic.Int32

	results := MapUnordered(t.Context(), items, maxWorkers,
		func(_ stopper.Context, _ int, _ int) (int, error) {
			cur := running.Add(1)
			defer running.Add(-1)
			for {
				old := maxSeen.Load()
				if cur <= old || maxSeen.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			return 0, nil
		},
	)

	for _, err := range results {
		r.NoError(err)
	}
	r.LessOrEqual(maxSeen.Load(), int32(maxWorkers))
	r.Greater(maxSeen.Load(), int32(0))
}

func TestMapUnorderedEmpty(t *testing.T) {
	r := require.New(t)

	items := slices.Values([]int{})
	results := MapUnordered(t.Context(), items, 4,
		func(_ stopper.Context, _ int, _ int) (int, error) {
			t.Fatal("should not be called")
			return 0, nil
		},
	)

	var count int
	for _, err := range results {
		r.NoError(err)
		count++
	}
	r.Zero(count)
}

func TestMapUnorderedError(t *testing.T) {
	r := require.New(t)

	errBad := errors.New("unordered error")
	items := slices.Values([]int{1, 2, 3})
	results := MapUnordered(t.Context(), items, 1,
		func(_ stopper.Context, _ int, v int) (int, error) {
			if v == 2 {
				return 0, errBad
			}
			return v * 10, nil
		},
	)

	var sawErr bool
	for _, err := range results {
		if err != nil {
			r.ErrorIs(err, errBad)
			sawErr = true
		}
	}
	r.True(sawErr)
}

func TestMapUnorderedPanic(t *testing.T) {
	r := require.New(t)

	items := slices.Values([]string{"a", "b", "c"})
	results := MapUnordered(t.Context(), items, 1,
		func(_ stopper.Context, _ int, v string) (string, error) {
			if v == "b" {
				panic("unordered boom")
			}
			return v, nil
		},
	)

	var sawPanic bool
	for _, err := range results {
		if err != nil {
			r.ErrorContains(err, "unordered boom")
			sawPanic = true
		}
	}
	r.True(sawPanic)
}

func TestMapUnorderedPanicError(t *testing.T) {
	r := require.New(t)

	errBoom := errors.New("unordered kaboom")
	items := slices.Values([]int{1, 2, 3})
	results := MapUnordered(t.Context(), items, 1,
		func(_ stopper.Context, _ int, v int) (int, error) {
			if v == 2 {
				panic(errBoom)
			}
			return v * 10, nil
		},
	)

	var sawPanic bool
	for _, err := range results {
		if err != nil {
			r.ErrorIs(err, errBoom)
			sawPanic = true
		}
	}
	r.True(sawPanic)
}

func TestMapUnorderedPanicAllWorkers(t *testing.T) {
	r := require.New(t)

	items := slices.Values([]int{1, 2, 3})
	results := MapUnordered(t.Context(), items, 3,
		func(_ stopper.Context, _ int, _ int) (int, error) {
			panic("all unordered panic")
		},
	)

	var errCount int
	for _, err := range results {
		if err != nil {
			r.ErrorContains(err, "all unordered panic")
			errCount++
		}
	}
	r.Greater(errCount, 0)
}

// --- MapUnordered2 ---

func TestMapUnordered2(t *testing.T) {
	r := require.New(t)

	items := slices.All([]string{"a", "b", "c"})
	results := MapUnordered2(t.Context(), items, 3,
		func(_ stopper.Context, _ int, k int, v string) (string, error) {
			return fmt.Sprintf("%d=%s", k, v), nil
		},
	)

	var got []string
	for v, err := range results {
		r.NoError(err)
		got = append(got, v)
	}
	sort.Strings(got)
	r.Equal([]string{"0=a", "1=b", "2=c"}, got)
}

func TestMapUnordered2EarlyBreak(t *testing.T) {
	r := require.New(t)

	items := slices.All([]string{"a", "b", "c", "d", "e"})
	results := MapUnordered2(t.Context(), items, 1,
		func(_ stopper.Context, _ int, k int, v string) (string, error) {
			return fmt.Sprintf("%d=%s", k, v), nil
		},
	)

	var got []string
	for v, err := range results {
		r.NoError(err)
		got = append(got, v)
		if len(got) == 2 {
			break
		}
	}
	r.Len(got, 2)
}

func TestMapUnordered2CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	items := slices.All([]int{1, 2, 3})
	results := MapUnordered2(ctx, items, 2,
		func(_ stopper.Context, _ int, _ int, _ int) (int, error) {
			return 0, nil
		},
	)

	for range results {
	}
}

func TestMapUnordered2Empty(t *testing.T) {
	r := require.New(t)

	items := slices.All([]int{})
	results := MapUnordered2(t.Context(), items, 4,
		func(_ stopper.Context, _ int, _ int, _ int) (int, error) {
			t.Fatal("should not be called")
			return 0, nil
		},
	)

	var count int
	for _, err := range results {
		r.NoError(err)
		count++
	}
	r.Zero(count)
}

func TestMapUnordered2Error(t *testing.T) {
	r := require.New(t)

	errBad := errors.New("unordered2 error")
	items := slices.All([]string{"a", "b", "c"})
	results := MapUnordered2(t.Context(), items, 1,
		func(_ stopper.Context, _ int, k int, _ string) (string, error) {
			if k == 1 {
				return "", errBad
			}
			return "ok", nil
		},
	)

	var sawErr bool
	for _, err := range results {
		if err != nil {
			r.ErrorIs(err, errBad)
			sawErr = true
		}
	}
	r.True(sawErr)
}

func TestMapUnordered2Panic(t *testing.T) {
	r := require.New(t)

	items := slices.All([]string{"a", "b", "c"})
	results := MapUnordered2(t.Context(), items, 1,
		func(_ stopper.Context, _ int, k int, _ string) (string, error) {
			if k == 1 {
				panic("unordered2 boom")
			}
			return "ok", nil
		},
	)

	var sawPanic bool
	for _, err := range results {
		if err != nil {
			r.ErrorContains(err, "unordered2 boom")
			sawPanic = true
		}
	}
	r.True(sawPanic)
}

func TestMapUnordered2PanicError(t *testing.T) {
	r := require.New(t)

	errBoom := errors.New("unordered2 kaboom")
	items := slices.All([]int{1, 2, 3})
	results := MapUnordered2(t.Context(), items, 1,
		func(_ stopper.Context, _ int, k int, _ int) (int, error) {
			if k == 1 {
				panic(errBoom)
			}
			return 0, nil
		},
	)

	var sawPanic bool
	for _, err := range results {
		if err != nil {
			r.ErrorIs(err, errBoom)
			sawPanic = true
		}
	}
	r.True(sawPanic)
}

// --- yieldN2 helper ---

func yieldN2(n int) iter.Seq2[int, int] {
	return func(yield func(int, int) bool) {
		for i := range n {
			if !yield(i, i*10) {
				return
			}
		}
	}
}

// Verify that yieldN2 helper works (used by other potential tests).
func TestYieldN2(t *testing.T) {
	r := require.New(t)

	var keys []int
	var vals []int
	for k, v := range yieldN2(3) {
		keys = append(keys, k)
		vals = append(vals, v)
	}
	r.Equal([]int{0, 1, 2}, keys)
	r.Equal([]int{0, 10, 20}, vals)
}
