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

func TestEdgeCaseCompositeNesting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want       any
		name       string
		source     string
		entrypoint string
	}{
		{
			name: "map_of_slices",
			source: `package main

func run() int {
	m := map[string][]int{
		"a": {1, 2, 3},
		"b": {4, 5},
	}
	m["a"] = append(m["a"], 4)
	return len(m["a"]) + m["b"][1]
}

func main() {}
`,
			entrypoint: "run",
			want:       9,
		},
		{
			name: "slice_of_maps",
			source: `package main

func run() int {
	s := []map[string]int{
		{"x": 1},
		{"x": 2, "y": 3},
	}
	s[1]["z"] = 4
	return s[0]["x"] + s[1]["y"] + s[1]["z"]
}

func main() {}
`,
			entrypoint: "run",
			want:       8,
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

func TestEdgeCasePointers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want       any
		name       string
		source     string
		entrypoint string
	}{
		{
			name: "pointer_to_pointer_deref",
			source: `package main

func run() int {
	x := 42
	p := &x
	pp := &p
	return **pp
}

func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
		{
			name: "double_pointer_mutation",
			source: `package main

func run() int {
	x := 1
	p := &x
	pp := &p
	**pp = 99
	return x
}

func main() {}
`,
			entrypoint: "run",
			want:       99,
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

func TestEdgeCaseNumeric(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want       any
		name       string
		source     string
		entrypoint string
	}{
		{

			name: "uint8_wraps_modulo_256",
			source: `package main

func run() uint8 {
	var x uint8 = 255
	x += 1
	return x
}

func main() {}
`,
			entrypoint: "run",
			want:       uint8(0),
		},
		{
			name: "float_div_zero_inf",
			source: `package main

func run() bool {
	x := 1.0
	y := 0.0
	z := x / y
	return z > 1e308
}

func main() {}
`,
			entrypoint: "run",
			want:       true,
		},
		{
			name: "const_shift_expr",
			source: `package main

const x = 1 << 10

func run() int {
	return x
}

func main() {}
`,
			entrypoint: "run",
			want:       1024,
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
