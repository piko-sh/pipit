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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func countHeapPlacedSliceMakes(dump string) int {
	lines := strings.Split(dump, "\n")
	count := 0
	for i := 0; i+1 < len(lines); i++ {
		if !strings.Contains(lines[i], ":MAKE_SLICE ") {
			continue
		}
		fields := strings.Fields(lines[i+1])
		if len(fields) > 0 && strings.HasSuffix(fields[1], ":EXT") && fields[len(fields)-1] == "1" {
			count++
		}
	}
	return count
}

func TestCapturedLocalEmission(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		body               string
		wantResets         int
		wantHeapSliceMakes int
	}{
		{
			name: "sibling-block captures need no reset",
			body: `{ x := 1; f := func() int { return x }; total += f() }
	{ y := 2; g := func() int { return y }; total += g() }`,
			wantResets:         0,
			wantHeapSliceMakes: 0,
		},
		{
			name:               "a captured short declaration in a loop resets each iteration",
			body:               `for i := 0; i < 2; i++ { a := i; f := func() int { return a }; total += f() }`,
			wantResets:         1,
			wantHeapSliceMakes: 0,
		},
		{
			name:               "a captured var declaration in a loop resets each iteration",
			body:               `for i := 0; i < 2; i++ { var b = i; f := func() int { return b }; total += f() }`,
			wantResets:         1,
			wantHeapSliceMakes: 0,
		},
		{
			name:               "a captured comma-ok value in a loop resets each iteration",
			body:               `m := map[int]int{0: 1}; for i := 0; i < 2; i++ { v, _ := m[i]; f := func() int { return v }; total += f() }`,
			wantResets:         1,
			wantHeapSliceMakes: 0,
		},
		{
			name:               "a captured slice literal is heap-placed",
			body:               `x := []int64{1, 2}; f := func() int64 { return x[0] }; x[0] = 9; total += int(f())`,
			wantResets:         0,
			wantHeapSliceMakes: 1,
		},
		{
			name:               "an uncaptured slice literal stays in the arena",
			body:               `x := []int64{1, 2}; x[0] = 9; total += int(x[0])`,
			wantResets:         0,
			wantHeapSliceMakes: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dump := compileSourceForOpcodeCheck(t, "package main\nfunc EntrypointRun() int {\n\ttotal := 0\n\t"+tt.body+"\n\treturn total\n}")

			require.Equalf(t, tt.wantResets, strings.Count(dump, "RESET_SHARED_CELL"),
				"RESET_SHARED_CELL count; disasm:\n%s", dump)
			require.Equalf(t, tt.wantHeapSliceMakes, countHeapPlacedSliceMakes(dump),
				"heap-placed MAKE_SLICE count; disasm:\n%s", dump)
		})
	}
}

func TestStoredSliceEmission(t *testing.T) {
	t.Parallel()

	const declarations = `package main

type holder struct{ xs []int64 }

func keep(xs []int64) holder { return holder{xs: xs} }

func measure(xs []int64) int { return len(xs) }

func (h *holder) set(xs []int64) { h.xs = xs }

type setter interface{ set(xs []int64) }

type counter interface{ count(xs []int64) int }

type tally struct{}

func (tally) count(xs []int64) int { return len(xs) }

`

	tests := []struct {
		name               string
		body               string
		wantHeapSliceMakes int
	}{
		{
			name:               "a slice stored in a struct literal is heap-placed",
			body:               `x := []int64{1, 2}; h := holder{xs: x}; x[0] = 9; total += int(h.xs[0])`,
			wantHeapSliceMakes: 1,
		},
		{
			name:               "a slice stored in an array literal is heap-placed",
			body:               `x := make([]int64, 2); a := [1][]int64{x}; x[0] = 9; total += int(a[0][0])`,
			wantHeapSliceMakes: 1,
		},
		{
			name:               "a slice assigned to a field is heap-placed",
			body:               `x := []int64{1, 2}; var h holder; h.xs = x; x[0] = 9; total += int(h.xs[0])`,
			wantHeapSliceMakes: 1,
		},
		{
			name:               "a slice argument to a parameter that is stored is heap-placed",
			body:               `h := keep([]int64{1, 2}); total += int(h.xs[0])`,
			wantHeapSliceMakes: 1,
		},
		{
			name:               "a slice argument to a method parameter that is stored is heap-placed",
			body:               `x := []int64{1, 2}; var h holder; h.set(x); x[0] = 9; total += int(h.xs[0])`,
			wantHeapSliceMakes: 1,
		},
		{
			name:               "a slice argument to a closure that stores it is heap-placed",
			body:               `var h holder; store := func(xs []int64) { h.xs = xs }; x := []int64{1}; store(x); total += int(h.xs[0])`,
			wantHeapSliceMakes: 1,
		},
		{
			name:               "a slice argument to a closure that only reads it stays in the arena",
			body:               `first := func(xs []int64) int64 { return xs[0] }; x := []int64{1}; total += int(first(x))`,
			wantHeapSliceMakes: 0,
		},
		{
			name:               "a slice argument to an interface method an implementation stores is heap-placed",
			body:               `var s setter = &holder{}; x := []int64{1}; s.set(x); total += int(x[0])`,
			wantHeapSliceMakes: 1,
		},
		{
			name:               "a slice argument to an interface method no implementation stores stays in the arena",
			body:               `var c counter = tally{}; x := []int64{1}; total += c.count(x)`,
			wantHeapSliceMakes: 0,
		},
		{
			name:               "a slice argument to a parameter that stays local stays in the arena",
			body:               `total += measure([]int64{1, 2})`,
			wantHeapSliceMakes: 0,
		},
		{
			name:               "a slice copied to another local stays in the arena",
			body:               `x := []int64{1, 2}; y := x; y[0] = 9; total += int(x[0])`,
			wantHeapSliceMakes: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dump := compileSourceForOpcodeCheck(t, declarations+"func EntrypointRun() int {\n\ttotal := 0\n\t"+tt.body+"\n\treturn total\n}")

			require.Equalf(t, tt.wantHeapSliceMakes, countHeapPlacedSliceMakes(dump),
				"heap-placed MAKE_SLICE count; disasm:\n%s", dump)
		})
	}
}
