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

package compile

import (
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const elementFieldStoreTypes = `
type inner struct {
	depth  int
	weight float64
}

type body struct {
	inner
	x      float64
	single float32
	hits   int
	narrow int32
	wide   uint64
	seen   bool
	label  string
	next   *body
}

type world struct {
	bodies []body
}

func loud() float64 { return 1 }
`

var elementFieldStoreOpcodes = []isa.Opcode{isa.OpSetElementFieldInt64, isa.OpSetElementFieldFloat64}

func TestStoreIntoAFieldOfASliceElementIsOneOperation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   []isa.Opcode
	}{
		{
			name:   "a float64 field",
			source: "func f(s []body, i int, v float64) { s[i].x = v }",
			want:   []isa.Opcode{isa.OpSetElementFieldFloat64},
		},
		{
			name:   "a float32 field",
			source: "func f(s []body, i int, v float32) { s[i].single = v }",
			want:   []isa.Opcode{isa.OpSetElementFieldFloat64},
		},
		{
			name:   "an int field",
			source: "func f(s []body, i int, v int) { s[i].hits = v }",
			want:   []isa.Opcode{isa.OpSetElementFieldInt64},
		},
		{
			name:   "a narrow integer field",
			source: "func f(s []body, i int, v int32) { s[i].narrow = v }",
			want:   []isa.Opcode{isa.OpSetElementFieldInt64},
		},
		{
			name:   "a constant",
			source: "func f(s []body, i int) { s[i].x = 2 }",
			want:   []isa.Opcode{isa.OpSetElementFieldFloat64},
		},
		{
			name:   "a field of an embedded struct",
			source: "func f(s []body, i int, v float64) { s[i].weight = v }",
			want:   []isa.Opcode{isa.OpSetElementFieldFloat64},
		},
		{
			name:   "an index of another integer type",
			source: "func f(s []body, i uint8, v int) { s[i].hits = v }",
			want:   []isa.Opcode{isa.OpSetElementFieldInt64},
		},
		{
			name:   "a slice read out of a struct",
			source: "func f(w *world, i int, v float64) { w.bodies[i].x = v }",
			want:   []isa.Opcode{isa.OpSetElementFieldFloat64},
		},
		{
			name:   "a compound assignment reads and stores the field in place",
			source: "func f(s []body, i int, v float64) { s[i].x -= v }",
			want:   []isa.Opcode{isa.OpGetElementFieldFloat64, isa.OpSetElementFieldFloat64},
		},
		{
			name:   "a compound assignment of a constant",
			source: "func f(s []body, i int) { s[i].hits += 2 }",
			want:   []isa.Opcode{isa.OpGetElementFieldInt64, isa.OpSetElementFieldInt64},
		},
		{
			name:   "a compound assignment whose operand reads the same element",
			source: "func f(s []body, i int, step float64) { s[i].x += step * s[i].weight }",
			want:   []isa.Opcode{isa.OpGetElementFieldFloat64, isa.OpSetElementFieldFloat64},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+elementFieldStoreTypes+"\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			for _, opcode := range tt.want {
				require.Truef(t, bodyContainsOpcode(compiled, opcode), "%s must be emitted", opcode)
			}
			require.Falsef(t, bodyContainsOpcode(compiled, isa.OpGetElement),
				"the element is never read whole, so %s must not be emitted", isa.OpGetElement)
			requireElementFieldStoresNameAScratchRegister(t, compiled)
		})
	}
}

func TestStoreIntoAFieldOfAnElementKeepsTheElementReadWhereFusingWouldChangeMeaning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "an unsigned field has no fused store",
			source: "func f(s []body, i int, v uint64) { s[i].wide = v }",
		},
		{
			name:   "a bool field has no fused store",
			source: "func f(s []body, i int, v bool) { s[i].seen = v }",
		},
		{
			name:   "a string field has no fused store",
			source: "func f(s []body, i int, v string) { s[i].label = v }",
		},
		{
			name:   "a pointer field has no fused store",
			source: "func f(s []body, i int, v *body) { s[i].next = v }",
		},
		{
			name:   "an array is not a slice",
			source: "func f(s *[4]body, i int, v float64) { s[i].x = v }",
		},
		{
			name:   "a slice of pointers holds no struct",
			source: "func f(s []*body, i int, v float64) { s[i].x = v }",
		},
		{
			name:   "a map element is not addressed by an index",
			source: "func f(m map[int]*body, i int, v float64) { m[i].x = v }",
		},
		{
			name:   "a compound assignment whose operand makes a call",
			source: "func f(s []body, i int) { s[i].x -= loud() }",
		},
		{
			name:   "a compound assignment whose operand receives",
			source: "func f(s []body, i int, c chan float64) { s[i].x -= <-c }",
		},
		{
			name:   "a compound shift",
			source: "func f(s []body, i int, n uint) { s[i].hits <<= n }",
		},
		{
			name:   "an increment",
			source: "func f(s []body, i int) { s[i].hits++ }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+elementFieldStoreTypes+"\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			for _, opcode := range elementFieldStoreOpcodes {
				require.Falsef(t, bodyContainsOpcode(compiled, opcode), "%s must not be emitted", opcode)
			}
		})
	}
}

func TestStoreIntoAFieldOfASliceElementNamesTheFieldAndItsOperands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		store     isa.Opcode
		wantKind  reflect.Kind
		wantValue uint8
	}{
		{
			name:   "a field of the embedded struct",
			source: "func f(s []body, i int, v float64) { s[i].weight = v }",
			store:  isa.OpSetElementFieldFloat64, wantKind: reflect.Float64, wantValue: 0,
		},
		{
			name:   "a float64 field after the embedded struct",
			source: "func f(s []body, i int, v float64) { s[i].x = v }",
			store:  isa.OpSetElementFieldFloat64, wantKind: reflect.Float64, wantValue: 0,
		},
		{
			name:   "an int field, whose value is the second int parameter",
			source: "func f(s []body, i int, v int) { s[i].hits = v }",
			store:  isa.OpSetElementFieldInt64, wantKind: reflect.Int, wantValue: 1,
		},
	}
	offsets := make([]uint32, 0, len(tests))
	for _, tt := range tests {
		root := compileSnippet(t, "package main\n"+elementFieldStoreTypes+"\n"+tt.source+"\n")
		compiled := findCompiledFunction(t, root, "f")

		at := slices.IndexFunc(compiled.Body, func(instruction isa.Instruction) bool { return instruction.Op == tt.store })
		require.GreaterOrEqual(t, at, 0, tt.name)
		store, extension := compiled.Body[at], compiled.Body[at+1]
		layout := compiled.StructLayoutTable[isa.JoinWide(extension.A, extension.B)]

		require.Equal(t, tt.wantKind, reflect.Kind(layout.Kind), tt.name)
		require.Equal(t, tt.wantValue, store.A, "%s: the value is the parameter v", tt.name)
		require.Zero(t, store.B, "%s: the slice is the first general register", tt.name)
		require.Zero(t, store.C, "%s: the index is the first int register", tt.name)
		offsets = append(offsets, layout.Offset)
	}
	require.IsIncreasing(t, offsets, "each store names its own field, in the order the struct declares them")
}

func requireElementFieldStoresNameAScratchRegister(t *testing.T, compiled *program.CompiledFunction) {
	t.Helper()
	stores := 0
	for programCounter, instruction := range compiled.Body {
		if !isa.IsElementFieldStore(instruction) {
			continue
		}
		stores++
		require.Less(t, programCounter+1, len(compiled.Body))
		extension := compiled.Body[programCounter+1]
		require.Equal(t, isa.OpExtension, extension.Op)
		layoutIndex := int(extension.A) | int(extension.B)<<8
		require.Less(t, layoutIndex, len(compiled.StructLayoutTable))
		require.Less(t, uint32(extension.C), compiled.NumRegisters[isa.RegisterGeneral],
			"the extension word has to name a general register the store may leave the element in")
		require.NotEqual(t, instruction.B, extension.C, "the element must not overwrite the slice")
	}
	require.Positive(t, stores)
}
