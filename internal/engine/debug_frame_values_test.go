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
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

func debugVariable(name string, kind isa.RegisterKind, register uint8) program.DebugVarEntry {
	return program.DebugVarEntry{
		Name:     name,
		Location: program.VarLocation{Kind: kind, Register: register},
	}
}

func TestReadFrameVariableReadsEveryBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind isa.RegisterKind
		load func(*Registers)
		want any
	}{
		{name: "the int bank", kind: isa.RegisterInt, load: func(r *Registers) { r.Ints[1] = 4 }, want: int64(4)},
		{name: "the float bank", kind: isa.RegisterFloat, load: func(r *Registers) { r.Floats[1] = 1.5 }, want: 1.5},
		{name: "the string bank", kind: isa.RegisterString, load: func(r *Registers) { r.Strings[1] = "s" }, want: "s"},
		{name: "the bool bank", kind: isa.RegisterBool, load: func(r *Registers) { r.Bools[1] = true }, want: true},
		{name: "the uint bank", kind: isa.RegisterUint, load: func(r *Registers) { r.Uints[1] = 6 }, want: uint64(6)},
		{name: "the complex bank", kind: isa.RegisterComplex, load: func(r *Registers) { r.Complex[1] = 2i }, want: 2i},
		{
			name: "the general bank", kind: isa.RegisterGeneral,
			load: func(r *Registers) { r.General[1] = reflect.ValueOf("boxed") }, want: "boxed",
		},
		{
			name: "the int-slice bank", kind: isa.RegisterSliceInt,
			load: func(r *Registers) { r.SlicesInt[1] = []int64{1} }, want: []int64{1},
		},
		{
			name: "the float-slice bank", kind: isa.RegisterSliceFloat,
			load: func(r *Registers) { r.slicesFloat[1] = []float64{1.5} }, want: []float64{1.5},
		},
		{
			name: "the string-slice bank", kind: isa.RegisterSliceString,
			load: func(r *Registers) { r.slicesString[1] = []string{"s"} }, want: []string{"s"},
		},
		{
			name: "the bool-slice bank", kind: isa.RegisterSliceBool,
			load: func(r *Registers) { r.slicesBool[1] = []bool{true} }, want: []bool{true},
		},
		{
			name: "the uint-slice bank", kind: isa.RegisterSliceUint,
			load: func(r *Registers) { r.slicesUint[1] = []uint64{2} }, want: []uint64{2},
		},
		{
			name: "the byte-slice bank", kind: isa.RegisterSliceByte,
			load: func(r *Registers) { r.slicesByte[1] = []byte{3} }, want: []byte{3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newStandardVM(t)
			tt.load(registers)

			value := vm.ReadFrameVariable(frame, debugVariable("v", tt.kind, 1))
			require.True(t, value.IsValid())
			require.Equal(t, tt.want, value.Interface())
		})
	}
}

func TestReadFrameVariableRefusesUnreachableLocations(t *testing.T) {
	t.Parallel()

	t.Run("a register past the bank reads nothing", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)

		require.False(t, vm.ReadFrameVariable(frame, debugVariable("v", isa.RegisterInt, 200)).IsValid())
	})

	t.Run("an unset general register reads nothing", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)

		require.False(t, vm.ReadFrameVariable(frame, debugVariable("v", isa.RegisterGeneral, 1)).IsValid())
	})

	t.Run("a general register past the bank reads nothing", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)

		require.False(t, vm.ReadFrameVariable(frame, debugVariable("v", isa.RegisterGeneral, 200)).IsValid())
	})

	t.Run("a bank with no debug form reads nothing", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)
		entry := debugVariable("v", isa.RegisterKind(isa.NumRegisterKinds), 0)

		require.False(t, vm.ReadFrameVariable(frame, entry).IsValid())
	})
}

func TestReadFrameVariableFollowsAHeapPromotedVariable(t *testing.T) {
	t.Parallel()

	t.Run("an indirect location is dereferenced", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		promoted := int64(9)
		registers.General[1] = reflect.ValueOf(&promoted)
		entry := program.DebugVarEntry{
			Name:     "v",
			Location: program.VarLocation{Kind: isa.RegisterGeneral, Register: 1, IsIndirect: true, OriginalKind: isa.RegisterInt},
		}

		value := vm.ReadFrameVariable(frame, entry)
		require.True(t, value.IsValid())
		require.Equal(t, int64(9), value.Interface())
	})

	t.Run("a nil indirect pointer reads nothing", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf((*int64)(nil))
		entry := program.DebugVarEntry{
			Name:     "v",
			Location: program.VarLocation{Kind: isa.RegisterGeneral, Register: 1, IsIndirect: true},
		}

		require.False(t, vm.ReadFrameVariable(frame, entry).IsValid())
	})
}

func TestReadFrameVariableReadsACapturedCell(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cell *program.UpvalueCell
		want any
	}{
		{name: "an int cell", cell: &program.UpvalueCell{Kind: isa.RegisterInt, IntValue: 4}, want: int64(4)},
		{name: "a float cell", cell: &program.UpvalueCell{Kind: isa.RegisterFloat, FloatValue: 1.5}, want: 1.5},
		{name: "a string cell", cell: &program.UpvalueCell{Kind: isa.RegisterString, StringValue: "s"}, want: "s"},
		{name: "a bool cell", cell: &program.UpvalueCell{Kind: isa.RegisterBool, BoolValue: true}, want: true},
		{name: "a uint cell", cell: &program.UpvalueCell{Kind: isa.RegisterUint, UintValue: 6}, want: uint64(6)},
		{name: "a complex cell", cell: &program.UpvalueCell{Kind: isa.RegisterComplex, ComplexValue: 2i}, want: 2i},
		{
			name: "an int-slice cell", cell: &program.UpvalueCell{Kind: isa.RegisterSliceInt, SliceIntValue: []int64{1}},
			want: []int64{1},
		},
		{
			name: "a byte-slice cell", cell: &program.UpvalueCell{Kind: isa.RegisterSliceByte, SliceByteValue: []byte{3}},
			want: []byte{3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, _ := newStandardVM(t)
			frame.upvalues = []upvalue{{Value: tt.cell}}
			entry := program.DebugVarEntry{
				Name:     "v",
				Location: program.VarLocation{IsUpvalue: true, UpvalueIndex: 0},
			}

			value := vm.ReadFrameVariable(frame, entry)
			require.True(t, value.IsValid())
			require.Equal(t, tt.want, value.Interface())
		})
	}
}

func TestReadFrameVariableRefusesUnreachableCells(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		upvalues []upvalue
		index    int
	}{
		{name: "a negative upvalue index", index: -1},
		{name: "an index past the table", index: 4},
		{name: "a nil cell", upvalues: []upvalue{{}}, index: 0},
		{name: "an empty general cell", upvalues: []upvalue{{Value: &program.UpvalueCell{Kind: isa.RegisterGeneral}}}, index: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, _ := newStandardVM(t)
			frame.upvalues = tt.upvalues
			entry := program.DebugVarEntry{
				Name:     "v",
				Location: program.VarLocation{IsUpvalue: true, UpvalueIndex: tt.index},
			}

			require.False(t, vm.ReadFrameVariable(frame, entry).IsValid())
		})
	}
}

func TestWriteFrameVariableStoresScalarBanks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    isa.RegisterKind
		value   any
		inspect func(*testing.T, *Registers)
	}{
		{
			name: "the int bank", kind: isa.RegisterInt, value: 7,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(7), r.Ints[1]) },
		},
		{
			name: "the float bank", kind: isa.RegisterFloat, value: 2.5,
			inspect: func(t *testing.T, r *Registers) { require.InDelta(t, 2.5, r.Floats[1], 0) },
		},
		{
			name: "the string bank", kind: isa.RegisterString, value: "written",
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, "written", r.Strings[1]) },
		},
		{
			name: "the bool bank", kind: isa.RegisterBool, value: true,
			inspect: func(t *testing.T, r *Registers) { require.True(t, r.Bools[1]) },
		},
		{
			name: "the uint bank", kind: isa.RegisterUint, value: uint(8),
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, uint64(8), r.Uints[1]) },
		},
		{
			name: "the complex bank", kind: isa.RegisterComplex, value: 3i,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, 3i, r.Complex[1]) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newStandardVM(t)

			require.NoError(t, vm.WriteFrameVariable(frame, debugVariable("v", tt.kind, 1), reflect.ValueOf(tt.value)))
			tt.inspect(t, registers)
		})
	}
}

func TestWriteFrameVariableRefusesWhatItCannotStore(t *testing.T) {
	t.Parallel()

	t.Run("an invalid value", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)

		require.ErrorIs(t, vm.WriteFrameVariable(frame, debugVariable("v", isa.RegisterInt, 1), reflect.Value{}),
			fault.ErrVariableNotWritable)
	})

	t.Run("a heap-promoted variable", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)
		entry := program.DebugVarEntry{Location: program.VarLocation{Kind: isa.RegisterInt, IsIndirect: true}}

		require.ErrorIs(t, vm.WriteFrameVariable(frame, entry, reflect.ValueOf(1)), fault.ErrVariableNotWritable)
	})

	t.Run("a captured variable", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)
		entry := program.DebugVarEntry{Location: program.VarLocation{Kind: isa.RegisterInt, IsCaptured: true}}

		require.ErrorIs(t, vm.WriteFrameVariable(frame, entry, reflect.ValueOf(1)), fault.ErrVariableNotWritable)
	})

	t.Run("a typed-slice bank", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)

		require.ErrorIs(t, vm.WriteFrameVariable(frame, debugVariable("v", isa.RegisterSliceInt, 1), reflect.ValueOf([]int64{1})),
			fault.ErrVariableNotWritable)
	})

	t.Run("a register past the bank", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)

		require.ErrorIs(t, vm.WriteFrameVariable(frame, debugVariable("v", isa.RegisterInt, 200), reflect.ValueOf(1)),
			fault.ErrVariableNotWritable)
	})

	t.Run("a value that does not fit the bank", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)

		require.Error(t, vm.WriteFrameVariable(frame, debugVariable("v", isa.RegisterInt, 1), reflect.ValueOf(1.5)),
			"a fractional value cannot round-trip through the int bank")
	})

	t.Run("a string into a scalar bank", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)

		require.ErrorIs(t, vm.WriteFrameVariable(frame, debugVariable("v", isa.RegisterInt, 1), reflect.ValueOf("s")),
			fault.ErrVariableNotWritable)
	})
}

func TestWriteFrameVariableGuardsTheGeneralBank(t *testing.T) {
	t.Parallel()

	t.Run("a matching type replaces the value", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf("before")

		require.NoError(t, vm.WriteFrameVariable(frame, debugVariable("v", isa.RegisterGeneral, 1), reflect.ValueOf("after")))
		require.Equal(t, "after", registers.General[1].Interface())
	})

	t.Run("an unset register has no type to match", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)

		require.ErrorIs(t, vm.WriteFrameVariable(frame, debugVariable("v", isa.RegisterGeneral, 1), reflect.ValueOf("after")),
			fault.ErrVariableNotWritable)
	})

	t.Run("a mismatched type is refused", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf("before")

		require.ErrorIs(t, vm.WriteFrameVariable(frame, debugVariable("v", isa.RegisterGeneral, 1), reflect.ValueOf(4)),
			fault.ErrVariableNotWritable)
	})

	t.Run("a register past the bank is refused", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)

		require.ErrorIs(t, vm.WriteFrameVariable(frame, debugVariable("v", isa.RegisterGeneral, 200), reflect.ValueOf(4)),
			fault.ErrVariableNotWritable)
	})
}

func TestWriteFrameVariableStoresIntoACapturedCell(t *testing.T) {
	t.Parallel()

	t.Run("an int cell takes the value", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)
		cell := &program.UpvalueCell{Kind: isa.RegisterInt}
		frame.upvalues = []upvalue{{Value: cell}}
		entry := program.DebugVarEntry{Location: program.VarLocation{IsUpvalue: true, UpvalueIndex: 0}}

		require.NoError(t, vm.WriteFrameVariable(frame, entry, reflect.ValueOf(5)))
		require.Equal(t, int64(5), cell.IntValue)
	})

	t.Run("a string cell takes the value", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)
		cell := &program.UpvalueCell{Kind: isa.RegisterString}
		frame.upvalues = []upvalue{{Value: cell}}
		entry := program.DebugVarEntry{Location: program.VarLocation{IsUpvalue: true, UpvalueIndex: 0}}

		require.NoError(t, vm.WriteFrameVariable(frame, entry, reflect.ValueOf("s")))
		require.Equal(t, "s", cell.StringValue)
	})

	t.Run("a general cell is refused", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)
		frame.upvalues = []upvalue{{Value: &program.UpvalueCell{Kind: isa.RegisterGeneral}}}
		entry := program.DebugVarEntry{Location: program.VarLocation{IsUpvalue: true, UpvalueIndex: 0}}

		require.ErrorIs(t, vm.WriteFrameVariable(frame, entry, reflect.ValueOf("s")), fault.ErrVariableNotWritable)
	})

	t.Run("an index past the table is refused", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)
		entry := program.DebugVarEntry{Location: program.VarLocation{IsUpvalue: true, UpvalueIndex: 4}}

		require.ErrorIs(t, vm.WriteFrameVariable(frame, entry, reflect.ValueOf(1)), fault.ErrVariableNotWritable)
	})

	t.Run("a nil cell is refused", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)
		frame.upvalues = []upvalue{{}}
		entry := program.DebugVarEntry{Location: program.VarLocation{IsUpvalue: true, UpvalueIndex: 0}}

		require.ErrorIs(t, vm.WriteFrameVariable(frame, entry, reflect.ValueOf(1)), fault.ErrVariableNotWritable)
	})
}

func TestFrameVariablesReportsTheLiveSet(t *testing.T) {
	t.Parallel()

	t.Run("a function with no debug table reports nothing", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)

		require.Nil(t, vm.FrameVariables(frame, 0))
	})

	t.Run("a nil frame reports nothing", func(t *testing.T) {
		t.Parallel()

		vm, _, _ := newStandardVM(t)

		require.Nil(t, vm.FrameVariables(nil, 0))
	})

	t.Run("each live variable is reported with its bank name", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		frame.Function.DebugVarTable = &program.DebugVarTable{Entries: []program.DebugVarEntry{
			debugVariable("count", isa.RegisterInt, 1),
			debugVariable("label", isa.RegisterString, 1),
		}}
		registers.Ints[1] = 4
		registers.Strings[1] = "s"

		variables := vm.FrameVariables(frame, 0)
		require.Len(t, variables, 2)
		require.Equal(t, "count", variables[0].Name)
		require.Equal(t, int64(4), variables[0].Value)
		require.Equal(t, isa.RegisterInt.String(), variables[0].Kind)
		require.Equal(t, "s", variables[1].Value)
	})
}

func TestVariableKindNamePrefersTheOriginalBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		location program.VarLocation
		want     string
	}{
		{name: "a direct variable names its own bank", location: program.VarLocation{Kind: isa.RegisterInt}, want: isa.RegisterInt.String()},
		{
			name:     "a heap-promoted variable names the bank it had",
			location: program.VarLocation{Kind: isa.RegisterGeneral, IsIndirect: true, OriginalKind: isa.RegisterString},
			want:     isa.RegisterString.String(),
		},
		{
			name:     "a heap-promoted variable with no recorded bank keeps its own",
			location: program.VarLocation{Kind: isa.RegisterGeneral, IsIndirect: true},
			want:     isa.RegisterGeneral.String(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, variableKindName(tt.location))
		})
	}
}

func TestRegisterIndexFollowsASpilledSlot(t *testing.T) {
	t.Parallel()

	require.Equal(t, 3, registerIndex(program.VarLocation{Register: 3}))
	require.Equal(t, isa.SpillAreaOffset+2, registerIndex(program.VarLocation{IsSpilled: true, SpillSlot: 2}))
}
