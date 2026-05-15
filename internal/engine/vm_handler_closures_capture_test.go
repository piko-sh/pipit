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

func closureHostVM(t *testing.T, callees ...*program.CompiledFunction) (*VM, *CallFrame, *Registers) {
	t.Helper()

	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.functions = callees
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	return newFramedVM(t, builder.build())
}

func calleeWithUpvalues(t *testing.T, descriptors ...program.UpvalueDescriptor) *program.CompiledFunction {
	t.Helper()

	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(4)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	callee := builder.build()
	callee.UpvalueDescriptors = descriptors
	return callee
}

func TestMakeClosureCachesTheUpvaluelessForm(t *testing.T) {
	t.Parallel()

	t.Run("two makes of the same function share one closure", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := closureHostVM(t, calleeWithUpvalues(t))

		require.Equal(t, opContinue, handleMakeClosure(vm, frame, registers, op(0, 0, 0)))
		require.Equal(t, opContinue, handleMakeClosure(vm, frame, registers, op(1, 0, 0)))

		require.Equal(t, registers.General[0].Pointer(), registers.General[1].Pointer(),
			"a closure that captures nothing is immutable, so one instance serves every make")
	})

	t.Run("a function index past the table is a bounds fault", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := closureHostVM(t, calleeWithUpvalues(t))

		got := handleMakeClosure(vm, frame, registers, op(0, 4, 0))
		require.Equal(t, opPanicError, got)
		require.Error(t, vm.evalError)
	})
}

func TestMakeClosureCapturesEveryBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    isa.RegisterKind
		load    func(*Registers)
		inspect func(*testing.T, *program.UpvalueCell)
	}{
		{
			name: "the int bank", kind: isa.RegisterInt, load: func(r *Registers) { r.Ints[2] = 5 },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, int64(5), c.IntValue) },
		},
		{
			name: "the float bank", kind: isa.RegisterFloat, load: func(r *Registers) { r.Floats[2] = 1.5 },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.InDelta(t, 1.5, c.FloatValue, 0) },
		},
		{
			name: "the string bank", kind: isa.RegisterString, load: func(r *Registers) { r.Strings[2] = "s" },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, "s", c.StringValue) },
		},
		{
			name: "the bool bank", kind: isa.RegisterBool, load: func(r *Registers) { r.Bools[2] = true },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.True(t, c.BoolValue) },
		},
		{
			name: "the uint bank", kind: isa.RegisterUint, load: func(r *Registers) { r.Uints[2] = 6 },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, uint64(6), c.UintValue) },
		},
		{
			name: "the complex bank", kind: isa.RegisterComplex, load: func(r *Registers) { r.Complex[2] = 2i },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, 2i, c.ComplexValue) },
		},
		{
			name: "the general bank", kind: isa.RegisterGeneral,
			load:    func(r *Registers) { r.General[2] = reflect.ValueOf("boxed") },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, "boxed", c.GeneralValue.Interface()) },
		},
		{
			name: "the int-slice bank", kind: isa.RegisterSliceInt,
			load:    func(r *Registers) { r.SlicesInt[2] = []int64{1} },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, []int64{1}, c.SliceIntValue) },
		},
		{
			name: "the float-slice bank", kind: isa.RegisterSliceFloat,
			load:    func(r *Registers) { r.slicesFloat[2] = []float64{1.5} },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, []float64{1.5}, c.SliceFloatValue) },
		},
		{
			name: "the string-slice bank", kind: isa.RegisterSliceString,
			load:    func(r *Registers) { r.slicesString[2] = []string{"s"} },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, []string{"s"}, c.SliceStringValue) },
		},
		{
			name: "the bool-slice bank", kind: isa.RegisterSliceBool,
			load:    func(r *Registers) { r.slicesBool[2] = []bool{true} },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, []bool{true}, c.SliceBoolValue) },
		},
		{
			name: "the uint-slice bank", kind: isa.RegisterSliceUint,
			load:    func(r *Registers) { r.slicesUint[2] = []uint64{2} },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, []uint64{2}, c.SliceUintValue) },
		},
		{
			name: "the byte-slice bank", kind: isa.RegisterSliceByte,
			load:    func(r *Registers) { r.slicesByte[2] = []byte{3} },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, []byte{3}, c.SliceByteValue) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			callee := calleeWithUpvalues(t, program.UpvalueDescriptor{Index: 2, Kind: tt.kind, IsLocal: true})
			vm, frame, registers := closureHostVM(t, callee)
			tt.load(registers)

			require.Equal(t, opContinue, handleMakeClosure(vm, frame, registers, op(0, 0, 0)))

			closure, ok := reflect.TypeAssert[*RuntimeClosure](registers.General[0])
			require.True(t, ok, "the destination register holds the closure")
			require.Len(t, closure.upvalues, 1)
			tt.inspect(t, closure.upvalues[0])
		})
	}
}

func TestMakeClosureSharesOneCellPerCapturedVariable(t *testing.T) {
	t.Parallel()

	t.Run("two closures over the same register share a cell", func(t *testing.T) {
		t.Parallel()

		descriptor := program.UpvalueDescriptor{Index: 2, Kind: isa.RegisterInt, IsLocal: true}
		vm, frame, registers := closureHostVM(t, calleeWithUpvalues(t, descriptor), calleeWithUpvalues(t, descriptor))
		registers.Ints[2] = 5

		require.Equal(t, opContinue, handleMakeClosure(vm, frame, registers, op(0, 0, 0)))
		require.Equal(t, opContinue, handleMakeClosure(vm, frame, registers, op(1, 1, 0)))

		first, _ := reflect.TypeAssert[*RuntimeClosure](registers.General[0])
		second, _ := reflect.TypeAssert[*RuntimeClosure](registers.General[1])
		require.Same(t, first.upvalues[0], second.upvalues[0],
			"both closures must observe one another's writes to the captured variable")
	})

	t.Run("a capture count past the inline cells spills to a slice", func(t *testing.T) {
		t.Parallel()

		descriptors := make([]program.UpvalueDescriptor, inlineUpvalueCells+2)
		for i := range descriptors {
			descriptors[i] = program.UpvalueDescriptor{Index: uint8(i), Kind: isa.RegisterInt, IsLocal: true}
		}
		vm, frame, registers := closureHostVM(t, calleeWithUpvalues(t, descriptors...))

		require.Equal(t, opContinue, handleMakeClosure(vm, frame, registers, op(0, 0, 0)))

		closure, _ := reflect.TypeAssert[*RuntimeClosure](registers.General[0])
		require.Len(t, closure.upvalues, len(descriptors))
	})

	t.Run("a transitive capture reuses the parent frame's own upvalue", func(t *testing.T) {
		t.Parallel()

		callee := calleeWithUpvalues(t, program.UpvalueDescriptor{Index: 0, Kind: isa.RegisterInt, IsLocal: false})
		vm, frame, registers := closureHostVM(t, callee)
		parentCell := &program.UpvalueCell{IntValue: 9, Kind: isa.RegisterInt}
		frame.upvalues = []upvalue{{Value: parentCell}}

		require.Equal(t, opContinue, handleMakeClosure(vm, frame, registers, op(0, 0, 0)))

		closure, _ := reflect.TypeAssert[*RuntimeClosure](registers.General[0])
		require.Same(t, parentCell, closure.upvalues[0],
			"a transitive capture must not snapshot: it forwards the cell it already has")
	})
}

func TestMakeClosureCapturesAHeapPromotedVariableByPointer(t *testing.T) {
	t.Parallel()

	callee := calleeWithUpvalues(t, program.UpvalueDescriptor{
		Index: 2, Kind: isa.RegisterGeneral, IsLocal: true, IsIndirect: true, OriginalKind: isa.RegisterInt,
	})
	vm, frame, registers := closureHostVM(t, callee)
	promoted := int64(7)
	registers.General[2] = reflect.ValueOf(&promoted)

	require.Equal(t, opContinue, handleMakeClosure(vm, frame, registers, op(0, 0, 0)))

	closure, _ := reflect.TypeAssert[*RuntimeClosure](registers.General[0])
	cell := closure.upvalues[0]
	require.True(t, cell.IsIndirect)
	require.Equal(t, isa.RegisterInt, cell.OriginalKind)
	require.Equal(t, reflect.ValueOf(&promoted).Pointer(), cell.GeneralValue.Pointer())
}

func TestSameIndirectCellComparesPointees(t *testing.T) {
	t.Parallel()

	value := int64(1)
	other := int64(1)
	pointer := reflect.ValueOf(&value)

	tests := []struct {
		name     string
		existing *program.UpvalueCell
		current  reflect.Value
		want     bool
	}{
		{name: "the same pointee", existing: &program.UpvalueCell{IsIndirect: true, GeneralValue: pointer}, current: pointer, want: true},
		{
			name: "a different pointee", existing: &program.UpvalueCell{IsIndirect: true, GeneralValue: pointer},
			current: reflect.ValueOf(&other), want: false,
		},
		{name: "a direct cell never matches", existing: &program.UpvalueCell{GeneralValue: pointer}, current: pointer, want: false},
		{name: "an empty cell never matches", existing: &program.UpvalueCell{IsIndirect: true}, current: pointer, want: false},
		{
			name: "an invalid register never matches", existing: &program.UpvalueCell{IsIndirect: true, GeneralValue: pointer},
			current: reflect.Value{}, want: false,
		},
		{
			name:     "a non-pointer cell never matches",
			existing: &program.UpvalueCell{IsIndirect: true, GeneralValue: reflect.ValueOf(1)}, current: pointer, want: false,
		},
		{
			name:     "a non-pointer register never matches",
			existing: &program.UpvalueCell{IsIndirect: true, GeneralValue: pointer}, current: reflect.ValueOf(1), want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, sameIndirectCell(tt.existing, tt.current))
		})
	}
}

func TestSetUpvalueWritesEveryBankIntoTheCell(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    isa.RegisterKind
		load    func(*Registers)
		inspect func(*testing.T, *program.UpvalueCell)
	}{
		{
			name: "the int bank", kind: isa.RegisterInt, load: func(r *Registers) { r.Ints[1] = 4 },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, int64(4), c.IntValue) },
		},
		{
			name: "the float bank", kind: isa.RegisterFloat, load: func(r *Registers) { r.Floats[1] = 2.5 },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.InDelta(t, 2.5, c.FloatValue, 0) },
		},
		{
			name: "the string bank", kind: isa.RegisterString, load: func(r *Registers) { r.Strings[1] = "s" },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, "s", c.StringValue) },
		},
		{
			name: "the bool bank", kind: isa.RegisterBool, load: func(r *Registers) { r.Bools[1] = true },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.True(t, c.BoolValue) },
		},
		{
			name: "the uint bank", kind: isa.RegisterUint, load: func(r *Registers) { r.Uints[1] = 8 },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, uint64(8), c.UintValue) },
		},
		{
			name: "the complex bank", kind: isa.RegisterComplex, load: func(r *Registers) { r.Complex[1] = 3i },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, 3i, c.ComplexValue) },
		},
		{
			name: "the general bank", kind: isa.RegisterGeneral,
			load:    func(r *Registers) { r.General[1] = reflect.ValueOf("boxed") },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, "boxed", c.GeneralValue.Interface()) },
		},
		{
			name: "the int-slice bank", kind: isa.RegisterSliceInt,
			load:    func(r *Registers) { r.SlicesInt[1] = []int64{2} },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, []int64{2}, c.SliceIntValue) },
		},
		{
			name: "the byte-slice bank", kind: isa.RegisterSliceByte,
			load:    func(r *Registers) { r.slicesByte[1] = []byte{4} },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, []byte{4}, c.SliceByteValue) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newStandardVM(t)
			cell := &program.UpvalueCell{Kind: tt.kind}
			frame.upvalues = []upvalue{{Value: cell}}
			tt.load(registers)

			require.Equal(t, opContinue, handleSetUpvalue(vm, frame, registers, op(1, 0, uint8(tt.kind))))
			tt.inspect(t, cell)
		})
	}
}

func TestSetUpvalueWritesThroughAnIndirectCell(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		kind  isa.RegisterKind
		build func() (reflect.Value, func(*testing.T))
		load  func(*Registers)
	}{
		{
			name: "an int pointee", kind: isa.RegisterInt, load: func(r *Registers) { r.Ints[1] = 11 },
			build: func() (reflect.Value, func(*testing.T)) {
				value := int64(0)
				return reflect.ValueOf(&value), func(t *testing.T) { require.Equal(t, int64(11), value) }
			},
		},
		{
			name: "a float pointee", kind: isa.RegisterFloat, load: func(r *Registers) { r.Floats[1] = 1.25 },
			build: func() (reflect.Value, func(*testing.T)) {
				value := 0.0
				return reflect.ValueOf(&value), func(t *testing.T) { require.InDelta(t, 1.25, value, 0) }
			},
		},
		{
			name: "a string pointee", kind: isa.RegisterString, load: func(r *Registers) { r.Strings[1] = "written" },
			build: func() (reflect.Value, func(*testing.T)) {
				value := ""
				return reflect.ValueOf(&value), func(t *testing.T) { require.Equal(t, "written", value) }
			},
		},
		{
			name: "a bool pointee", kind: isa.RegisterBool, load: func(r *Registers) { r.Bools[1] = true },
			build: func() (reflect.Value, func(*testing.T)) {
				value := false
				return reflect.ValueOf(&value), func(t *testing.T) { require.True(t, value) }
			},
		},
		{
			name: "a uint pointee", kind: isa.RegisterUint, load: func(r *Registers) { r.Uints[1] = 12 },
			build: func() (reflect.Value, func(*testing.T)) {
				value := uint64(0)
				return reflect.ValueOf(&value), func(t *testing.T) { require.Equal(t, uint64(12), value) }
			},
		},
		{
			name: "a complex pointee", kind: isa.RegisterComplex, load: func(r *Registers) { r.Complex[1] = 4i },
			build: func() (reflect.Value, func(*testing.T)) {
				value := complex128(0)
				return reflect.ValueOf(&value), func(t *testing.T) { require.Equal(t, 4i, value) }
			},
		},
		{
			name: "a general pointee", kind: isa.RegisterGeneral,
			load: func(r *Registers) { r.General[1] = reflect.ValueOf("boxed") },
			build: func() (reflect.Value, func(*testing.T)) {
				value := ""
				return reflect.ValueOf(&value), func(t *testing.T) { require.Equal(t, "boxed", value) }
			},
		},
		{
			name: "an int-slice pointee", kind: isa.RegisterSliceInt,
			load: func(r *Registers) { r.SlicesInt[1] = []int64{5, 6} },
			build: func() (reflect.Value, func(*testing.T)) {
				value := []int64(nil)
				return reflect.ValueOf(&value), func(t *testing.T) { require.Equal(t, []int64{5, 6}, value) }
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newStandardVM(t)
			pointer, verify := tt.build()
			cell := &program.UpvalueCell{IsIndirect: true, GeneralValue: pointer, Kind: isa.RegisterGeneral}
			frame.upvalues = []upvalue{{Value: cell}}
			tt.load(registers)

			require.Equal(t, opContinue, handleSetUpvalue(vm, frame, registers, op(1, 0, uint8(tt.kind))))
			verify(t)
		})
	}
}

func TestSetUpvalueIgnoresAnIndirectCellWithNoPointer(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newStandardVM(t)
	cell := &program.UpvalueCell{IsIndirect: true}
	frame.upvalues = []upvalue{{Value: cell}}
	registers.Ints[1] = 3

	require.Equal(t, opContinue, handleSetUpvalue(vm, frame, registers, op(1, 0, uint8(isa.RegisterInt))),
		"a cell with nothing to write through is not a fault")
	require.Zero(t, cell.IntValue)
}

func TestSyncClosureUpvaluesWritesSharedCellsBack(t *testing.T) {
	t.Parallel()

	t.Run("a frame that created no shared cell has nothing to sync", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.Ints[2] = 1

		require.Equal(t, opContinue, handleSyncClosureUpvalues(vm, frame, registers, op(0, 0, 0)))
		require.Equal(t, int64(1), registers.Ints[2])
	})

	t.Run("a shared cell the closure wrote lands back in the register", func(t *testing.T) {
		t.Parallel()

		descriptor := program.UpvalueDescriptor{Index: 2, Kind: isa.RegisterInt, IsLocal: true}
		vm, frame, registers := closureHostVM(t, calleeWithUpvalues(t, descriptor))
		registers.Ints[2] = 1

		require.Equal(t, opContinue, handleMakeClosure(vm, frame, registers, op(0, 0, 0)))
		closure, _ := reflect.TypeAssert[*RuntimeClosure](registers.General[0])
		closure.upvalues[0].IntValue = 42

		require.Equal(t, opContinue, handleSyncClosureUpvalues(vm, frame, registers, op(0, 0, 0)))
		require.Equal(t, int64(42), registers.Ints[2], "the caller must see what the closure wrote")
	})

	t.Run("a register holding something other than a closure is ignored", func(t *testing.T) {
		t.Parallel()

		descriptor := program.UpvalueDescriptor{Index: 2, Kind: isa.RegisterInt, IsLocal: true}
		vm, frame, registers := closureHostVM(t, calleeWithUpvalues(t, descriptor))
		require.Equal(t, opContinue, handleMakeClosure(vm, frame, registers, op(0, 0, 0)))
		registers.General[1] = reflect.ValueOf("not a closure")

		require.Equal(t, opContinue, handleSyncClosureUpvalues(vm, frame, registers, op(0, 0, 1)))
	})
}
