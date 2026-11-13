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

//go:build bench

package bench

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
)

func BenchmarkCallOverhead(b *testing.B) {
	benchmarks := []struct {
		name   string
		source string
	}{
		{
			name: "no_args",
			source: `package main

func noop() int { return 1 }

func run() int {
	s := 0
	for i := 0; i < 1000; i++ {
		s += noop()
	}
	return s
}
`,
		},
		{
			name: "one_int_arg",
			source: `package main

func inc(x int) int { return x + 1 }

func run() int {
	s := 0
	for i := 0; i < 1000; i++ {
		s += inc(i)
	}
	return s
}
`,
		},
		{
			name: "two_int_args",
			source: `package main

func add(a, b int) int { return a + b }

func run() int {
	s := 0
	for i := 0; i < 1000; i++ {
		s += add(i, i+1)
	}
	return s
}
`,
		},
		{
			name: "four_int_args",
			source: `package main

func sum4(a, b, c, d int) int { return a + b + c + d }

func run() int {
	s := 0
	for i := 0; i < 1000; i++ {
		s += sum4(i, i+1, i+2, i+3)
	}
	return s
}
`,
		},
		{
			name: "mixed_int_string",
			source: `package main

func mixed(x int, s string) int { return x + len(s) }

func run() int {
	s := 0
	for i := 0; i < 1000; i++ {
		s += mixed(i, "hello")
	}
	return s
}
`,
		},
		{
			name: "mixed_int_string_float",
			source: `package main

func mixed3(x int, s string, f float64) int {
	return x + len(s) + int(f)
}

func run() int {
	s := 0
	for i := 0; i < 1000; i++ {
		s += mixed3(i, "hello", 3.14)
	}
	return s
}
`,
		},
		{
			name: "general_arg_slice",
			source: `package main

func withSlice(s []int) int { return len(s) }

func run() int {
	sl := []int{1, 2, 3}
	s := 0
	for i := 0; i < 1000; i++ {
		s += withSlice(sl)
	}
	return s
}
`,
		},
		{
			name: "two_returns",
			source: `package main

func divmod(a, b int) (int, int) { return a / b, a % b }

func run() int {
	s := 0
	for i := 1; i < 1000; i++ {
		q, r := divmod(i*7, 3)
		s += q + r
	}
	return s
}
`,
		},
		{
			name: "general_arg_struct_value",
			source: `package main

type Box struct{ a, b, c int }

func sumBox(box Box) int { return box.a + box.b + box.c }

func run() int {
	box := Box{a: 1, b: 2, c: 3}
	s := 0
	for i := 0; i < 1000; i++ {
		s += sumBox(box)
	}
	return s
}
`,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			service := app.NewService()
			cfs, err := service.CompileFileSet(b.Context(), map[string]string{"main.go": bm.source})
			require.NoError(b, err)

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, err = service.ExecuteEntrypoint(b.Context(), cfs, "run")
				if err != nil {
					require.NoError(b, err)
				}
			}
		})
	}
}

func BenchmarkCallDepthExecOnly(b *testing.B) {
	depths := []struct {
		name   string
		source string
	}{
		{
			name: "depth_10",
			source: `package main

func recurse(n int) int {
	if n <= 0 { return 0 }
	return recurse(n - 1) + 1
}

func run() int { return recurse(10) }
`,
		},
		{
			name: "depth_100",
			source: `package main

func recurse(n int) int {
	if n <= 0 { return 0 }
	return recurse(n - 1) + 1
}

func run() int { return recurse(100) }
`,
		},
		{
			name: "depth_1000",
			source: `package main

func recurse(n int) int {
	if n <= 0 { return 0 }
	return recurse(n - 1) + 1
}

func run() int { return recurse(1000) }
`,
		},
	}

	for _, d := range depths {
		b.Run(d.name, func(b *testing.B) {
			service := app.NewService()
			cfs, err := service.CompileFileSet(b.Context(), map[string]string{"main.go": d.source})
			require.NoError(b, err)

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, err = service.ExecuteEntrypoint(b.Context(), cfs, "run")
				if err != nil {
					require.NoError(b, err)
				}
			}
		})
	}
}
