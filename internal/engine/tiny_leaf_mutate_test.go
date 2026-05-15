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

package engine

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type leafCounter struct {
	Signed   int64
	Unsigned uint64
	Ratio    float64
	Flag     bool
}

type leafNested struct {
	Inner leafCounter
	Other int64
}

func leafLayout(fieldIndex uint8, kind reflect.Kind, bank isa.RegisterKind) program.StructFieldLayout {
	field := reflect.TypeFor[leafCounter]().Field(int(fieldIndex))
	layout := program.StructFieldLayout{
		Offset:       uint32(field.Offset),
		PathLength:   1,
		Kind:         uint8(kind),
		RegisterKind: uint8(bank),
	}
	layout.Path[0] = fieldIndex
	return layout
}

func leafSite(argumentKinds ...isa.RegisterKind) *program.CallSite {
	site := &program.CallSite{}
	for index, kind := range argumentKinds {
		site.Arguments = append(site.Arguments, program.VarLocation{Kind: kind, Register: uint8(index)})
	}
	return site
}

func outOfRangeLayout() program.StructFieldLayout {
	layout := program.StructFieldLayout{PathLength: 1, Kind: uint8(reflect.Int64), RegisterKind: uint8(isa.RegisterInt)}
	layout.Path[0] = 9
	return layout
}

func TestTinyLeafReflectFieldWalksThePathItIsGiven(t *testing.T) {
	t.Parallel()

	counter := &leafCounter{Signed: 7}
	nested := &leafNested{Inner: leafCounter{Signed: 9}}

	nestedLayout := program.StructFieldLayout{PathLength: 2}
	nestedLayout.Path[0] = 0
	nestedLayout.Path[1] = 0

	tests := []struct {
		receiver reflect.Value
		name     string
		layout   program.StructFieldLayout
		want     bool
	}{
		{name: "a direct field of a pointer receiver", receiver: reflect.ValueOf(counter), layout: leafLayout(0, reflect.Int64, isa.RegisterInt), want: true},
		{name: "a direct field of a value receiver", receiver: reflect.ValueOf(*counter), layout: leafLayout(0, reflect.Int64, isa.RegisterInt), want: true},
		{name: "a nested field is reached through its path", receiver: reflect.ValueOf(nested), layout: nestedLayout, want: true},
		{name: "an invalid receiver has no field", receiver: reflect.Value{}, layout: leafLayout(0, reflect.Int64, isa.RegisterInt), want: false},
		{name: "a non-struct receiver has no field", receiver: reflect.ValueOf(42), layout: leafLayout(0, reflect.Int64, isa.RegisterInt), want: false},
		{name: "a field index past the struct has no field", receiver: reflect.ValueOf(counter), layout: outOfRangeLayout(), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, ok := tinyLeafReflectField(tt.receiver, tt.layout)

			require.Equal(t, tt.want, ok)
		})
	}
}

func TestTinyLeafIncrementRunnersMutateThroughTheReceiver(t *testing.T) {
	t.Parallel()

	t.Run("a signed field is incremented in place", func(t *testing.T) {
		t.Parallel()
		vm, _, registers := newStandardVM(t)
		target := &leafCounter{Signed: 41}
		registers.General[0] = reflect.ValueOf(target)

		callee := program.NewNamedFunction("inc")
		callee.TinyLeafLayout = leafLayout(0, reflect.Int64, isa.RegisterInt)

		require.Equal(t, opContinue, runTinyLeafIncIntField(vm, registers, leafSite(isa.RegisterGeneral), callee))
		require.Equal(t, int64(42), target.Signed)
	})

	t.Run("an unsigned field is incremented in place", func(t *testing.T) {
		t.Parallel()
		vm, _, registers := newStandardVM(t)
		target := &leafCounter{Unsigned: 41}
		registers.General[0] = reflect.ValueOf(target)

		callee := program.NewNamedFunction("inc")
		callee.TinyLeafLayout = leafLayout(1, reflect.Uint64, isa.RegisterUint)

		require.Equal(t, opContinue, runTinyLeafIncUintField(vm, registers, leafSite(isa.RegisterGeneral), callee))
		require.Equal(t, uint64(42), target.Unsigned)
	})

	t.Run("a nested field is reached through its path", func(t *testing.T) {
		t.Parallel()
		vm, _, registers := newStandardVM(t)
		target := &leafNested{Inner: leafCounter{Signed: 1}}
		registers.General[0] = reflect.ValueOf(target)

		outer := reflect.TypeFor[leafNested]().Field(0)
		inner := reflect.TypeFor[leafCounter]().Field(0)
		layout := program.StructFieldLayout{
			Offset:       uint32(outer.Offset + inner.Offset),
			PathLength:   2,
			Kind:         uint8(reflect.Int64),
			RegisterKind: uint8(isa.RegisterInt),
		}
		layout.Path[0] = 0
		layout.Path[1] = 0
		callee := program.NewNamedFunction("inc")
		callee.TinyLeafLayout = layout

		require.Equal(t, opContinue, runTinyLeafIncIntField(vm, registers, leafSite(isa.RegisterGeneral), callee))
		require.Equal(t, int64(2), target.Inner.Signed)
	})
}

func TestTinyLeafScalarAssignmentWritesEveryScalarBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		seed  func(registers *Registers)
		check func(t *testing.T, target *leafCounter)
		name  string
		field uint8
		kind  reflect.Kind
		bank  isa.RegisterKind
	}{
		{
			name: "a signed field", field: 0, kind: reflect.Int64, bank: isa.RegisterInt,
			seed:  func(registers *Registers) { registers.Ints[1] = 42 },
			check: func(t *testing.T, target *leafCounter) { require.Equal(t, int64(42), target.Signed) },
		},
		{
			name: "an unsigned field", field: 1, kind: reflect.Uint64, bank: isa.RegisterUint,
			seed:  func(registers *Registers) { registers.Uints[1] = 42 },
			check: func(t *testing.T, target *leafCounter) { require.Equal(t, uint64(42), target.Unsigned) },
		},
		{
			name: "a float field", field: 2, kind: reflect.Float64, bank: isa.RegisterFloat,
			seed:  func(registers *Registers) { registers.Floats[1] = 1.5 },
			check: func(t *testing.T, target *leafCounter) { require.InDelta(t, 1.5, target.Ratio, 0) },
		},
		{
			name: "a boolean field", field: 3, kind: reflect.Bool, bank: isa.RegisterBool,
			seed:  func(registers *Registers) { registers.Bools[1] = true },
			check: func(t *testing.T, target *leafCounter) { require.True(t, target.Flag) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, _, registers := newStandardVM(t)
			target := &leafCounter{}
			registers.General[0] = reflect.ValueOf(target)
			tt.seed(registers)

			callee := program.NewNamedFunction("set")
			callee.TinyLeafLayout = leafLayout(tt.field, tt.kind, tt.bank)

			got := runTinyLeafSetScalarFieldFromArg(vm, registers, leafSite(isa.RegisterGeneral, tt.bank), callee)

			require.Equal(t, opContinue, got)
			tt.check(t, target)
		})
	}
}

func TestCopyStructElementIntoWritesAtTheIndexedOffset(t *testing.T) {
	t.Parallel()

	addressable := func(value leafCounter) reflect.Value {
		return reflect.ValueOf(&value).Elem()
	}

	t.Run("elements are written at their own stride", func(t *testing.T) {
		t.Parallel()
		backing := make([]leafCounter, 3)
		size := reflect.TypeFor[leafCounter]().Size()

		copyStructElementInto(reflect.ValueOf(backing).UnsafePointer(), 1, size,
			addressable(leafCounter{Signed: 42}))

		require.Equal(t, int64(42), backing[1].Signed)
		require.Equal(t, int64(0), backing[0].Signed, "a write must not reach the previous element")
		require.Equal(t, int64(0), backing[2].Signed, "a write must not reach the next element")
	})

	t.Run("the first element is written at offset zero", func(t *testing.T) {
		t.Parallel()
		backing := make([]leafCounter, 2)
		size := reflect.TypeFor[leafCounter]().Size()

		copyStructElementInto(reflect.ValueOf(backing).UnsafePointer(), 0, size,
			addressable(leafCounter{Unsigned: 7}))

		require.Equal(t, uint64(7), backing[0].Unsigned)
	})
}
