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
	"reflect"
	"strings"
	"testing"

	"pipit.sh/pipit/internal/engine"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

func TestStructLiteralEmitsArenaAllocForPointerFreeTypes(t *testing.T) {
	t.Parallel()
	dump := compileSourceForOpcodeCheck(t, `package main
type point struct{ x, y int }
type named struct{ id int; name string }
func EntrypointRun() int {
	p := point{x: 1}
	n := named{id: 2, name: "n"}
	return p.x + n.id + len(n.name)
}`)

	require.Equalf(t, 1, strings.Count(dump, "ALLOC_STRUCT_LITERAL"),
		"expected exactly the pointer-free literal to use the arena allocation sub-op; disasm:\n%s", dump)
	require.Equalf(t, 1, strings.Count(dump, "MAKE_MAP"),
		"expected the literal with a string field to keep the generic path; disasm:\n%s", dump)
}

func TestStructLiteralQualifiesForArena(t *testing.T) {
	t.Parallel()
	require.True(t, engine.StructLiteralQualifiesForArena(reflect.TypeFor[struct{ a, b int32 }]()))
	require.False(t, engine.StructLiteralQualifiesForArena(reflect.TypeFor[struct{ s string }]()), "strings carry a pointer")
	require.False(t, engine.StructLiteralQualifiesForArena(reflect.TypeFor[struct{}]()), "zero-sized structs use the shared sentinel")
	require.False(t, engine.StructLiteralQualifiesForArena(reflect.TypeFor[int]()), "only structs qualify")
	require.Equal(t, isa.SubOpTier2AllocStructLiteral, engine.StructLiteralAllocationSubOp(reflect.TypeFor[struct{ a int }]()))
	require.Equal(t, isa.SubOpTier2MakeMap, engine.StructLiteralAllocationSubOp(reflect.TypeFor[struct{ p *int }]()))
}

func TestStructLiteralAllocationZeroesAndSetsFields(t *testing.T) {
	t.Parallel()
	compiled := compileExpression(t, `
type acc struct{ hits, misses int; ratio float64; flag bool }
total := 0
for i := 0; i < 5000; i++ {
	a := acc{hits: i}
	if a.misses != 0 || a.ratio != 0 || a.flag { total += 1000000 }
	a.misses = i * 2
	a.flag = i%2 == 0
	if a.flag { total += a.hits + a.misses } else { total -= a.hits }
}
total`)
	vm := newTestVM(t)
	result, err := vm.Execute(compiled)
	require.NoError(t, err)

	expected := 0
	for i := range 5000 {
		if i%2 == 0 {
			expected += i + i*2
		} else {
			expected -= i
		}
	}
	require.EqualValues(t, expected, result)
}

func TestStructLiteralTablePublishesQualifyingTypesOnly(t *testing.T) {
	t.Parallel()
	compiled := compileExpression(t, `
type point struct{ x, y int }
type named struct{ id int; name string }
p := point{x: 3}
n := named{id: 4, name: "z"}
p.x + n.id`)
	vm := newTestVM(t)
	result, err := vm.Execute(compiled)
	require.NoError(t, err)
	require.EqualValues(t, 7, result)

	engine.EnsureStructLiteralTable(compiled)
	require.Len(t, compiled.StructLiteralTable, len(compiled.TypeTable))
	published := 0
	for index, entry := range compiled.TypeTable {
		row := compiled.StructLiteralTable[index]
		if engine.StructLiteralQualifiesForArena(entry) {
			require.Equal(t, entry.Size(), row.Size)
			require.Equal(t, uintptr(entry.Align())-1, row.AlignMask)
			published++
			continue
		}
		require.Zero(t, row.TypeWord, "non-qualifying type %v must keep a zero type word", entry)
	}
	require.Positive(t, published)
}
