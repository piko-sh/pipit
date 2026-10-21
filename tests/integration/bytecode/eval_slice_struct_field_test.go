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

package bytecode_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/debug"
)

func TestF3SliceIndexStructFieldFusion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		source         string
		entrypoint     string
		want           any
		expectOp       string
		expectFallback bool
	}{
		{
			name: "int_field_via_index",
			source: `package main

type edge struct {
	target int
	weight int64
}

func sumWeights(es []edge) int64 {
	var total int64
	for i := 0; i < len(es); i++ {
		total += es[i].weight
	}
	return total
}

func run() int64 {
	return sumWeights([]edge{
		{target: 1, weight: 10},
		{target: 2, weight: 20},
		{target: 3, weight: 30},
	})
}

func main() {}
`,
			entrypoint: "run",
			want:       int64(60),
			expectOp:   "GET_ELEMENT_FIELD_INT64",
		},
		{
			name: "float_field_via_index",
			source: `package main

type point struct {
	x float64
	y float64
}

func sumX(ps []point) float64 {
	var total float64
	for i := 0; i < len(ps); i++ {
		total += ps[i].x
	}
	return total
}

func run() float64 {
	return sumX([]point{{x: 1.5}, {x: 2.5}, {x: 3.5}})
}

func main() {}
`,
			entrypoint: "run",
			want:       float64(7.5),
			expectOp:   "GET_ELEMENT_FIELD_FLOAT64",
		},
		{
			name: "bool_field_via_index",
			source: `package main

type item struct {
	active bool
	count  int64
}

func countActive(items []item) int64 {
	var c int64
	for i := 0; i < len(items); i++ {
		if items[i].active {
			c++
		}
	}
	return c
}

func run() int64 {
	return countActive([]item{
		{active: true, count: 1},
		{active: false, count: 2},
		{active: true, count: 3},
	})
}

func main() {}
`,
			entrypoint: "run",
			want:       int64(2),
			expectOp:   "GET_ELEMENT_FIELD_BOOL",
		},
		{
			name: "string_field_via_index",
			source: `package main

type kv struct {
	key string
	val string
}

func firstKey(items []kv) string {
	return items[0].key
}

func run() string {
	return firstKey([]kv{{key: "alpha", val: "v"}, {key: "beta", val: "w"}})
}

func main() {}
`,
			entrypoint: "run",
			want:       "alpha",
			expectOp:   "GET_ELEMENT_FIELD_STRING",
		},
		{
			name: "uint_field_via_index",
			source: `package main

type tag struct {
	bits uint64
}

func bitwiseOr(tags []tag) uint64 {
	var acc uint64
	for i := 0; i < len(tags); i++ {
		acc |= tags[i].bits
	}
	return acc
}

func run() uint64 {
	return bitwiseOr([]tag{{bits: 1}, {bits: 2}, {bits: 4}})
}

func main() {}
`,
			entrypoint: "run",
			want:       uint64(7),
			expectOp:   "GET_ELEMENT_FIELD_UINT64",
		},
		{
			name: "cycle_broken_self_referential_slice_falls_back",
			source: `package main

type Tree struct {
	Name     string
	Children []Tree
}

func run() string {
	root := Tree{
		Name: "root",
		Children: []Tree{
			{Name: "alpha", Children: nil},
			{Name: "beta", Children: nil},
		},
	}
	return root.Children[0].Name
}

func main() {}
`,
			entrypoint: "run",
			want:       "alpha",

			expectOp: "GET_ELEMENT_FIELD_STRING",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			compiled, err := service.CompileFileSet(t.Context(), map[string]string{
				"main.go": tt.source,
			})
			require.NoError(t, err, "compile: %s", tt.name)
			if tt.expectOp != "" {
				dump := debug.DisassembleAssembly(compiled)
				assert.Contains(t, dump, tt.expectOp, "%s", tt.name)
			}
			result, err := service.ExecuteEntrypoint(t.Context(), compiled, tt.entrypoint)
			require.NoError(t, err, "execute: %s", tt.name)
			require.Equal(t, tt.want, result, "%s", tt.name)
		})
	}
}
