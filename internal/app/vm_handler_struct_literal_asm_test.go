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

//go:build !safe && (amd64 || arm64)

package app

import (
	"context"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	structLiteralHotLoopAttempts = 4
	structLiteralHotLoopGoBudget = 16
)

func TestStructLiteralHotLoopStaysInAssembly(t *testing.T) {
	compiled := compileExpression(t, `
type pair struct{ a, b int }
total := 0
for i := 0; i < 4000; i++ {
	p := pair{a: i, b: i + 1}
	total += p.a + p.b
}
total`)
	goAllocations := make([]int, 0, structLiteralHotLoopAttempts)
	for range structLiteralHotLoopAttempts {
		runtime.GC()
		vm := newTestVM(t)
		result, err := vm.Execute(compiled)
		require.NoError(t, err)
		require.EqualValues(t, 4000*4000, result)
		goAllocations = append(goAllocations, vm.StructLiteralGoAllocations)
		if vm.StructLiteralGoAllocations <= structLiteralHotLoopGoBudget {
			return
		}
	}
	require.Failf(t, "struct literal loop stayed on the Go path",
		"expected at most %d Go-path allocations (table publish plus slab growth) in one attempt, got %v",
		structLiteralHotLoopGoBudget, goAllocations)
}

func TestStructLiteralAllocationSurvivesSlabGrowth(t *testing.T) {
	t.Parallel()
	compiled := compileExpression(t, `
type wide struct{ a, b, c, d, e, f, g, h int64 }
keep := make([]wide, 0, 40000)
for i := 0; i < 40000; i++ {
	w := wide{a: int64(i), h: int64(-i)}
	keep = append(keep, w)
}
total := int64(0)
for i := 0; i < len(keep); i++ {
	total += keep[i].a - keep[i].h + keep[i].d
}
total`)
	vm := newTestVM(t)
	result, err := vm.Execute(compiled)
	require.NoError(t, err)
	require.EqualValues(t, int64(2)*int64(40000*39999/2), result)
}

func TestStructLiteralStoredInGlobalSurvivesArenaReset(t *testing.T) {
	t.Parallel()
	session := NewService().NewSession()
	submit := func(code string) any {
		t.Helper()
		result, err := session.Submit(context.Background(), code)
		require.NoError(t, err, code)
		return result
	}
	submit(`type cell struct{ v, w int }`)
	submit(`var kept []cell`)
	submit(`for i := 0; i < 64; i++ { c := cell{v: i, w: i * 3}; kept = append(kept, c) }`)
	submit(`s := ""; for i := 0; i < 4096; i++ { s = s + "0123456789abcdef" }; len(s)`)
	submit(`for i := 0; i < 64; i++ { c := cell{v: -i, w: 1} ; kept[0].w += c.w }`)
	require.EqualValues(t, 63*3, submit(`kept[63].w`))
	require.EqualValues(t, 64, submit(`kept[0].w`))
	require.EqualValues(t, 7, submit(`kept[7].v`))
}
