// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

//go:build integration

package language_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
)

func TestEdgeCaseRangeClosureCapture(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want       any
		name       string
		source     string
		entrypoint string
	}{
		{
			name: "map_range_closure_capture",
			source: `package main

func run() int {
	m := map[int]int{0: 10, 1: 20, 2: 30}
	fns := make([]func() int, 3)
	for k, v := range m {
		v := v
		fns[k] = func() int { return v }
	}
	return fns[0]() + fns[1]() + fns[2]()
}

func main() {}
`,
			entrypoint: "run",
			want:       60,
		},
		{
			name: "channel_range_closure_capture",
			source: `package main

func run() int {
	ch := make(chan int, 3)
	ch <- 10
	ch <- 20
	ch <- 30
	close(ch)
	fns := make([]func() int, 3)
	index := 0
	for v := range ch {
		v := v
		i := index
		fns[i] = func() int { return v }
		index++
	}
	return fns[0]() + fns[1]() + fns[2]()
}

func main() {}
`,
			entrypoint: "run",
			want:       60,
		},
		{
			name: "string_range_closure_capture",
			source: `package main

func run() int {
	fns := make([]func() int, 3)
	index := 0
	for _, r := range "abc" {
		r := r
		i := index
		fns[i] = func() int { return int(r) }
		index++
	}
	return fns[0]() + fns[1]() + fns[2]()
}

func main() {}
`,
			entrypoint: "run",
			want:       294,
		},
		{
			name: "map_range_closure_no_shadow",
			source: `package main

func run() int {
	m := map[int]int{0: 100, 1: 200, 2: 300}
	fns := make([]func() int, 3)
	for k, v := range m {
		fns[k] = func() int { return v }
	}
	return fns[0]() + fns[1]() + fns[2]()
}

func main() {}
`,
			entrypoint: "run",
			want:       600,
		},
		{
			name: "slice_range_value_closure",
			source: `package main

func run() int {
	fns := make([]func() int, 3)
	for i, v := range []int{10, 20, 30} {
		fns[i] = func() int { return v }
	}
	return fns[0]() + fns[1]() + fns[2]()
}

func main() {}
`,
			entrypoint: "run",
			want:       60,
		},
		{
			name: "slice_range_kv_closure",
			source: `package main

func run() int {
	fns := make([]func() int, 3)
	for i, v := range []int{10, 20, 30} {
		fns[i] = func() int { return i*100 + v }
	}
	return fns[0]() + fns[1]() + fns[2]()
}

func main() {}
`,
			entrypoint: "run",
			want:       360,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.EvalFile(t.Context(), tt.source, tt.entrypoint)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}

func TestEdgeCaseRecursiveClosures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want       any
		name       string
		source     string
		entrypoint string
	}{
		{
			name: "recursive_closure_variable",
			source: `package main

func run() int {
	var f func(int) int
	f = func(n int) int {
		if n <= 1 {
			return 1
		}
		return n * f(n-1)
	}
	return f(5)
}

func main() {}
`,
			entrypoint: "run",
			want:       120,
		},
		{
			name: "mutual_recursion_closures",
			source: `package main

func run() bool {
	var isEven, isOdd func(int) bool
	isEven = func(n int) bool {
		if n == 0 { return true }
		return isOdd(n - 1)
	}
	isOdd = func(n int) bool {
		if n == 0 { return false }
		return isEven(n - 1)
	}
	return isEven(10)
}

func main() {}
`,
			entrypoint: "run",
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.EvalFile(t.Context(), tt.source, tt.entrypoint)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}

func TestEdgeCaseDeepNestedClosure(t *testing.T) {
	t.Parallel()

	source := `package main

func run() int {
	x := 0
	f := func() func() func() int {
		return func() func() int {
			return func() int {
				x++
				return x
			}
		}
	}
	g := f()()
	g()
	g()
	return g()
}

func main() {}
`
	service := app.NewService()
	result, err := service.EvalFile(t.Context(), source, "run")
	require.NoError(t, err)
	require.Equal(t, 3, result)
}

func TestEdgeCaseScopeVariable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want       any
		name       string
		source     string
		entrypoint string
	}{
		{
			name: "short_var_redecl",
			source: `package main

func run() int {
	a := 10
	a, b := 20, 30
	return a + b
}

func main() {}
`,
			entrypoint: "run",
			want:       50,
		},
		{
			name: "range_string_runes",
			source: `package main

func run() int {
	sum := 0
	for _, r := range "ABC" {
		sum += int(r)
	}
	return sum
}

func main() {}
`,
			entrypoint: "run",
			want:       198,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.EvalFile(t.Context(), tt.source, tt.entrypoint)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}
