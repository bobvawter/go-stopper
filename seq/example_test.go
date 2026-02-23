// Copyright 2026 Bob Vawter (bob@vawter.org)
// SPDX-License-Identifier: Apache-2.0

package seq_test

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"vawter.tech/stopper/v2"
	"vawter.tech/stopper/v2/seq"
)

func ExampleForEach() {
	// Process items concurrently with up to 3 workers.
	items := slices.Values([]string{"alpha", "bravo", "charlie"})
	err := seq.ForEach(context.Background(), items, 3,
		func(_ stopper.Context, idx int, s string) error {
			fmt.Printf("%d: %s\n", idx, strings.ToUpper(s))
			return nil
		},
	)
	if err != nil {
		panic(err)
	}

	// Unordered output:
	// 0: ALPHA
	// 1: BRAVO
	// 2: CHARLIE
}

func ExampleForEach2() {
	// Iterate over key-value pairs from a map.
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	err := seq.ForEach2(context.Background(), maps.All(m), 2,
		func(_ stopper.Context, _ int, k string, v int) error {
			fmt.Printf("%s=%d\n", k, v)
			return nil
		},
	)
	if err != nil {
		panic(err)
	}

	// Unordered output:
	// a=1
	// b=2
	// c=3
}

func ExampleMap() {
	// Double each integer, preserving input order.
	items := slices.Values([]int{1, 2, 3, 4, 5})
	doubled := seq.Map(context.Background(), items, 3,
		func(_ stopper.Context, _ int, v int) (int, error) {
			return v * 2, nil
		},
	)

	for val, err := range doubled {
		if err != nil {
			panic(err)
		}
		fmt.Println(val)
	}

	// Output:
	// 2
	// 4
	// 6
	// 8
	// 10
}

func ExampleMap2() {
	// Transform map entries into formatted strings, preserving order
	// of the input sequence.
	pairs := slices.All([]string{"hello", "world"})
	results := seq.Map2(context.Background(), pairs, 2,
		func(_ stopper.Context, _ int, idx int, s string) (string, error) {
			return fmt.Sprintf("%d:%s", idx, strings.ToUpper(s)), nil
		},
	)

	for val, err := range results {
		if err != nil {
			panic(err)
		}
		fmt.Println(val)
	}

	// Output:
	// 0:HELLO
	// 1:WORLD
}

func ExampleMapUnordered() {
	// Square each number. Results may arrive in any order.
	items := slices.Values([]int{1, 2, 3, 4, 5})
	squared := seq.MapUnordered(context.Background(), items, 3,
		func(_ stopper.Context, _ int, v int) (int, error) {
			return v * v, nil
		},
	)

	for val, err := range squared {
		if err != nil {
			panic(err)
		}
		fmt.Println(val)
	}

	// Unordered output:
	// 1
	// 4
	// 9
	// 16
	// 25
}

func ExampleMapUnordered2() {
	// Transform key-value pairs. Results may arrive in any order.
	pairs := slices.All([]string{"alpha", "bravo", "charlie"})
	results := seq.MapUnordered2(context.Background(), pairs, 2,
		func(_ stopper.Context, _ int, idx int, s string) (string, error) {
			return fmt.Sprintf("%d=%s", idx, s), nil
		},
	)

	for val, err := range results {
		if err != nil {
			panic(err)
		}
		fmt.Println(val)
	}

	// Unordered output:
	// 0=alpha
	// 1=bravo
	// 2=charlie
}
