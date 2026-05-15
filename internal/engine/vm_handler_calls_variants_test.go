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

func scalarCallSlot(siteIndex uint16) isa.Instruction {
	return isa.NewTier1Instruction(isa.SubOpCallScalar, byte(siteIndex&0xFF), byte(siteIndex>>8))
}

func iifeCallSlot(siteIndex uint16) isa.Instruction {
	return isa.NewTier1Instruction(isa.SubOpCallIIFE, byte(siteIndex&0xFF), byte(siteIndex>>8))
}

func TestScalarCallCopiesArgumentsAndResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		argument int64
		want     any
	}{
		{name: "a positive argument", argument: 4, want: 8},
		{name: "a zero argument", argument: 0, want: 0},
		{name: "a negative argument", argument: -3, want: -6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := newBytecodeBuilder()
			builder.addIntConst(tt.argument)
			builder.intRegisters(4).returnInt()
			calleeIndex := builder.addSubFunction(doublingCallee())
			siteIndex := builder.AddCallSite(&program.CallSite{
				FunctionIndex: calleeIndex,
				Arguments:     []program.VarLocation{intSlot(2)},
				Returns:       []program.VarLocation{intSlot(0)},
			})
			builder.Emit(isa.OpLoadIntConst, 2, 0, 0)
			builder.body = append(builder.body, scalarCallSlot(siteIndex))
			builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)

			result, err := executeTestBytecode(t, builder.build())
			require.NoError(t, err)
			require.Equal(t, tt.want, result, "the scalar call form must place the result where the site says")
		})
	}
}

func TestImmediatelyInvokedCallCapturesTheCallerRegisters(t *testing.T) {
	t.Parallel()

	calleeBuilder := newBytecodeBuilder()
	calleeBuilder.intRegisters(2).returnInt()
	calleeBuilder.Emit(isa.OpGetUpvalue, 0, 0, uint8(isa.RegisterInt))
	calleeBuilder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	callee := calleeBuilder.build()
	callee.Name = "iife"
	callee.UpvalueDescriptors = []program.UpvalueDescriptor{
		{Index: 2, Kind: isa.RegisterInt, IsLocal: true},
	}

	builder := newBytecodeBuilder()
	builder.addIntConst(11)
	builder.intRegisters(4).returnInt()
	calleeIndex := builder.addSubFunction(callee)
	siteIndex := builder.AddCallSite(&program.CallSite{
		FunctionIndex: calleeIndex,
		Returns:       []program.VarLocation{intSlot(0)},
	})
	builder.Emit(isa.OpLoadIntConst, 2, 0, 0)
	builder.body = append(builder.body, iifeCallSlot(siteIndex))
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)

	result, err := executeTestBytecode(t, builder.build())
	require.NoError(t, err)
	require.Equal(t, 11, result, "the body must read the register the caller captured")
}

func TestSnapshotRegisterToCellCopiesTheNamedBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    isa.RegisterKind
		load    func(*Registers)
		inspect func(*testing.T, *program.UpvalueCell)
	}{
		{
			name: "the int bank", kind: isa.RegisterInt, load: func(r *Registers) { r.Ints[1] = 5 },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, int64(5), c.IntValue) },
		},
		{
			name: "the float bank", kind: isa.RegisterFloat, load: func(r *Registers) { r.Floats[1] = 1.5 },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.InDelta(t, 1.5, c.FloatValue, 0) },
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
			name: "the uint bank", kind: isa.RegisterUint, load: func(r *Registers) { r.Uints[1] = 6 },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, uint64(6), c.UintValue) },
		},
		{
			name: "the complex bank", kind: isa.RegisterComplex, load: func(r *Registers) { r.Complex[1] = 2i },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, 2i, c.ComplexValue) },
		},
		{
			name: "the general bank", kind: isa.RegisterGeneral,
			load:    func(r *Registers) { r.General[1] = reflect.ValueOf("boxed") },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Equal(t, "boxed", c.GeneralValue.Interface()) },
		},
		{
			name: "a typed-slice bank is captured elsewhere", kind: isa.RegisterSliceInt,
			load:    func(r *Registers) { r.SlicesInt[1] = []int64{1} },
			inspect: func(t *testing.T, c *program.UpvalueCell) { require.Nil(t, c.SliceIntValue) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registers := standardRegisters()
			tt.load(&registers)
			cell := &program.UpvalueCell{}

			snapshotRegisterToCell(cell, &registers, tt.kind, 1, nil)
			tt.inspect(t, cell)
		})
	}
}

func TestResolveDirectCalleeUsesTheCachedFunctionFirst(t *testing.T) {
	t.Parallel()

	t.Run("a cached callee skips the table lookup", func(t *testing.T) {
		t.Parallel()

		cached := program.NewNamedFunction("cached")
		vm, _, _ := newStandardVM(t)

		callee, result := resolveDirectCallee(vm, &program.CallSite{CachedCallee: cached, FunctionIndex: 99})
		require.Equal(t, opContinue, result, "a cached callee needs no bounds check")
		require.Same(t, cached, callee)
	})

	t.Run("an index past the function table is a bounds fault", func(t *testing.T) {
		t.Parallel()

		vm, _, _ := newStandardVM(t)

		callee, result := resolveDirectCallee(vm, &program.CallSite{FunctionIndex: 99})
		require.Equal(t, opPanicError, result)
		require.Nil(t, callee)
		require.Error(t, vm.evalError)
	})
}

func TestUnboxToTypedSlicesWidenNarrowElements(t *testing.T) {
	t.Parallel()

	t.Run("signed slices", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			value any
			want  []int64
		}{
			{name: "an int64 slice is aliased", value: []int64{1, 2}, want: []int64{1, 2}},
			{name: "an int slice is reinterpreted", value: []int{3, 4}, want: []int64{3, 4}},
			{name: "an int32 slice is widened", value: []int32{5, 6}, want: []int64{5, 6}},
			{name: "an int8 slice sign-extends", value: []int8{-1}, want: []int64{-1}},
			{name: "a nil int32 slice stays nil", value: []int32(nil), want: nil},
			{name: "a string slice is not a signed slice", value: []string{"s"}, want: nil},
			{name: "a non-slice is refused", value: 4, want: nil},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				require.Equal(t, tt.want, unboxToTypedIntSlice(reflect.ValueOf(tt.value), nil))
			})
		}
	})

	t.Run("unsigned slices", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			value any
			want  []uint64
		}{
			{name: "a uint64 slice is aliased", value: []uint64{1, 2}, want: []uint64{1, 2}},
			{name: "a uint slice is reinterpreted", value: []uint{3, 4}, want: []uint64{3, 4}},
			{name: "a uint32 slice is widened", value: []uint32{5, 6}, want: []uint64{5, 6}},
			{name: "a uint16 slice is widened", value: []uint16{7}, want: []uint64{7}},
			{name: "a nil uint32 slice stays nil", value: []uint32(nil), want: nil},
			{name: "a string slice is not an unsigned slice", value: []string{"s"}, want: nil},
			{name: "a non-slice is refused", value: 4, want: nil},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				require.Equal(t, tt.want, unboxToTypedUintSlice(reflect.ValueOf(tt.value), nil))
			})
		}
	})

	t.Run("an arena-backed widening holds the same elements", func(t *testing.T) {
		t.Parallel()

		arena := newTestArena(t)

		require.Equal(t, []uint64{7, 8}, widenUintSliceWithArena(reflect.ValueOf([]uint32{7, 8}), arena))
		require.Nil(t, widenUintSliceWithArena(reflect.ValueOf([]uint32(nil)), arena),
			"a nil source must stay nil so the callee's nil comparison still holds")
	})
}

func callSlotOperands(siteIndex uint16) isa.Instruction {
	return op(0, byte(siteIndex&0xFF), byte(siteIndex>>8))
}

func TestScalarCallHandlerPushesTheCalleeFrame(t *testing.T) {
	t.Parallel()

	builder := newBytecodeBuilder()
	builder.intRegisters(4).returnInt()
	calleeIndex := builder.addSubFunction(doublingCallee())
	siteIndex := builder.AddCallSite(&program.CallSite{
		FunctionIndex: calleeIndex,
		Arguments:     []program.VarLocation{intSlot(2)},
		Returns:       []program.VarLocation{intSlot(0)},
	})
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)

	vm, frame, registers := newFramedVM(t, builder.build())
	registers.Ints[2] = 21
	caller := vm.FramePointer

	require.Equal(t, opFrameChanged, handleCallScalar(vm, frame, registers, callSlotOperands(siteIndex)))
	require.Equal(t, caller+1, vm.FramePointer, "the call pushes exactly one frame")
	require.Equal(t, int64(21), vm.CallStack[vm.FramePointer].Registers.Ints[1],
		"the argument lands in the callee's parameter register")
}

func TestScalarCallHandlerRefusesIndicesPastTheirTables(t *testing.T) {
	t.Parallel()

	t.Run("a call site index past the table", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)

		require.Equal(t, opPanicError, handleCallScalar(vm, frame, registers, callSlotOperands(99)))
		require.Error(t, vm.evalError)
	})

	t.Run("a function index past the table", func(t *testing.T) {
		t.Parallel()

		builder := newBytecodeBuilder()
		builder.numRegisters = wideRegCounts(4)
		siteIndex := builder.AddCallSite(&program.CallSite{FunctionIndex: 99})
		builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

		vm, frame, registers := newFramedVM(t, builder.build())

		require.Equal(t, opPanicError, handleCallScalar(vm, frame, registers, callSlotOperands(siteIndex)))
		require.Error(t, vm.evalError)
	})

	t.Run("a stack already at the depth limit overflows", func(t *testing.T) {
		t.Parallel()

		builder := newBytecodeBuilder()
		builder.numRegisters = wideRegCounts(4)
		calleeIndex := builder.addSubFunction(doublingCallee())
		siteIndex := builder.AddCallSite(&program.CallSite{FunctionIndex: calleeIndex})
		builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

		vm, frame, registers := newFramedVM(t, builder.build())
		vm.FramePointer = vm.callDepthLimit()

		require.Equal(t, opStackOverflow, handleCallScalar(vm, frame, registers, callSlotOperands(siteIndex)))
	})
}

func TestReturnVoidHandlerPopsTheFrame(t *testing.T) {
	t.Parallel()

	t.Run("returning from the base frame finishes execution", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)

		require.Equal(t, opDone, handleReturnVoid(vm, frame, registers, op(0, 0, 0)))
	})

	t.Run("returning from a nested frame hands control back to the caller", func(t *testing.T) {
		t.Parallel()

		callee := voidCallee()
		vm, _, _ := closureHostVM(t, callee)
		caller := vm.FramePointer
		vm.PushFrame(callee)
		frame := &vm.CallStack[vm.FramePointer]

		require.Equal(t, opFrameChanged, handleReturnVoid(vm, frame, &frame.Registers, op(0, 0, 0)))
		require.Equal(t, caller, vm.FramePointer)
	})
}

func TestReturnHandlerCopiesResultsToTheCaller(t *testing.T) {
	t.Parallel()

	callee := doublingCallee()
	vm, callerFrame, callerRegisters := closureHostVM(t, callee)
	caller := vm.FramePointer

	vm.PushFrame(callee)
	frame := &vm.CallStack[vm.FramePointer]
	frame.returnDestination = []program.VarLocation{intSlot(3)}
	frame.Registers.Ints[0] = 42

	require.Equal(t, opFrameChanged, handleReturn(vm, frame, &frame.Registers, op(1, 0, 0)))
	require.Equal(t, caller, vm.FramePointer)
	require.Equal(t, int64(42), callerRegisters.Ints[3], "the result lands where the site said")
	require.NotNil(t, callerFrame)
}

func TestPackDeferredVariadicArgumentsFillsTheTrailingSlice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		callee    func() *program.CompiledFunction
		arguments []reflect.Value
		spread    bool
		want      []any
	}{
		{
			name:      "a non-variadic callee is left alone",
			callee:    func() *program.CompiledFunction { return program.NewNamedFunction("f") },
			arguments: []reflect.Value{reflect.ValueOf(1)},
			want:      []any{1},
		},
		{
			name:      "a nil callee is left alone",
			callee:    func() *program.CompiledFunction { return nil },
			arguments: []reflect.Value{reflect.ValueOf(1)},
			want:      []any{1},
		},
		{
			name: "a spread call already carries its slice",
			callee: func() *program.CompiledFunction {
				callee := program.NewNamedFunction("f")
				callee.IsVariadic = true
				return callee
			},
			arguments: []reflect.Value{reflect.ValueOf([]any{1})}, spread: true,
			want: []any{[]any{1}},
		},
		{
			name: "a variadic callee with no recorded slice type packs into an any slice",
			callee: func() *program.CompiledFunction {
				callee := program.NewNamedFunction("f")
				callee.IsVariadic = true
				callee.ParameterKinds = []isa.RegisterKind{isa.RegisterInt, isa.RegisterGeneral}
				return callee
			},
			arguments: []reflect.Value{reflect.ValueOf(1), reflect.ValueOf(2), reflect.ValueOf(3)},
			want:      []any{1, []any{2, 3}},
		},
		{
			name: "a recorded slice type is honoured",
			callee: func() *program.CompiledFunction {
				callee := program.NewNamedFunction("f")
				callee.IsVariadic = true
				callee.ParameterKinds = []isa.RegisterKind{isa.RegisterGeneral}
				callee.VariadicSliceType = reflect.TypeFor[[]int]()
				return callee
			},
			arguments: []reflect.Value{reflect.ValueOf(1), reflect.ValueOf(2)},
			want:      []any{[]int{1, 2}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := newTestVM(t)

			packed := packDeferredVariadicArguments(vm, tt.callee(), tt.arguments, tt.spread)
			require.Len(t, packed, len(tt.want))
			for i, want := range tt.want {
				require.Equal(t, want, packed[i].Interface())
			}
		})
	}
}

func TestRuntimePanicErrorSatisfiesTheRuntimeErrorInterface(t *testing.T) {
	t.Parallel()

	failure := newRuntimePanicError("runtime error: something %d", 4)

	require.EqualError(t, failure, "runtime error: something 4")
	require.NotPanics(t, failure.RuntimeError, "the marker method exists so errors.As finds a runtime.Error")
}

func TestUncaughtPanicStackIsEmptyUntilRecorded(t *testing.T) {
	t.Parallel()

	failure := newuncaughtPanicError("boom")

	require.Empty(t, failure.Stack())
	require.Equal(t, "boom", failure.Value())
}

func TestRunArgCopyProgramMovesEachBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entry   program.CallArgCopy
		load    func(*Registers)
		inspect func(*testing.T, *Registers)
	}{
		{
			name: "the int bank", entry: program.CallArgCopy{Op: program.CopyIntToInt, SourceRegister: 1, DestinationRegister: 0},
			load:    func(r *Registers) { r.Ints[1] = 4 },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(4), r.Ints[0]) },
		},
		{
			name: "the float bank", entry: program.CallArgCopy{Op: program.CopyFloatToFloat, SourceRegister: 1, DestinationRegister: 0},
			load:    func(r *Registers) { r.Floats[1] = 1.5 },
			inspect: func(t *testing.T, r *Registers) { require.InDelta(t, 1.5, r.Floats[0], 0) },
		},
		{
			name: "the string bank", entry: program.CallArgCopy{Op: program.CopyStringToString, SourceRegister: 1, DestinationRegister: 0},
			load:    func(r *Registers) { r.Strings[1] = "s" },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, "s", r.Strings[0]) },
		},
		{
			name: "the general bank", entry: program.CallArgCopy{Op: program.CopyGeneralToGeneral, SourceRegister: 1, DestinationRegister: 0},
			load:    func(r *Registers) { r.General[1] = reflect.ValueOf("boxed") },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, "boxed", r.General[0].Interface()) },
		},
		{
			name: "a struct in the general bank is copied at the boundary", entry: program.CallArgCopy{Op: program.CopyGeneralToGeneral, SourceRegister: 1, DestinationRegister: 0},
			load: func(r *Registers) { r.General[1] = reflect.ValueOf(recordHolder{Count: 3}) },
			inspect: func(t *testing.T, r *Registers) {
				require.Equal(t, int64(3), r.General[0].Interface().(recordHolder).Count)
			},
		},
		{
			name: "an array in the general bank is copied at the boundary", entry: program.CallArgCopy{Op: program.CopyGeneralToGeneral, SourceRegister: 1, DestinationRegister: 0},
			load:    func(r *Registers) { r.General[1] = reflect.ValueOf([2]int{1, 2}) },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, [2]int{1, 2}, r.General[0].Interface()) },
		},
		{
			name: "the bool bank", entry: program.CallArgCopy{Op: program.CopyBoolToBool, SourceRegister: 1, DestinationRegister: 0},
			load:    func(r *Registers) { r.Bools[1] = true },
			inspect: func(t *testing.T, r *Registers) { require.True(t, r.Bools[0]) },
		},
		{
			name: "the uint bank", entry: program.CallArgCopy{Op: program.CopyUintToUint, SourceRegister: 1, DestinationRegister: 0},
			load:    func(r *Registers) { r.Uints[1] = 6 },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, uint64(6), r.Uints[0]) },
		},
		{
			name: "the complex bank", entry: program.CallArgCopy{Op: program.CopyComplexToComplex, SourceRegister: 1, DestinationRegister: 0},
			load:    func(r *Registers) { r.Complex[1] = 2i },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, 2i, r.Complex[0]) },
		},
		{
			name: "the int-slice bank", entry: program.CallArgCopy{Op: program.CopySliceIntToSliceInt, SourceRegister: 1, DestinationRegister: 0},
			load:    func(r *Registers) { r.SlicesInt[1] = []int64{1} },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []int64{1}, r.SlicesInt[0]) },
		},
		{
			name: "the float-slice bank", entry: program.CallArgCopy{Op: program.CopySliceFloatToSliceFloat, SourceRegister: 1, DestinationRegister: 0},
			load:    func(r *Registers) { r.slicesFloat[1] = []float64{1.5} },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []float64{1.5}, r.slicesFloat[0]) },
		},
		{
			name: "the string-slice bank", entry: program.CallArgCopy{Op: program.CopySliceStringToSliceString, SourceRegister: 1, DestinationRegister: 0},
			load:    func(r *Registers) { r.slicesString[1] = []string{"s"} },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []string{"s"}, r.slicesString[0]) },
		},
		{
			name: "the bool-slice bank", entry: program.CallArgCopy{Op: program.CopySliceBoolToSliceBool, SourceRegister: 1, DestinationRegister: 0},
			load:    func(r *Registers) { r.slicesBool[1] = []bool{true} },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []bool{true}, r.slicesBool[0]) },
		},
		{
			name: "the uint-slice bank", entry: program.CallArgCopy{Op: program.CopySliceUintToSliceUint, SourceRegister: 1, DestinationRegister: 0},
			load:    func(r *Registers) { r.slicesUint[1] = []uint64{2} },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []uint64{2}, r.slicesUint[0]) },
		},
		{
			name: "the byte-slice bank", entry: program.CallArgCopy{Op: program.CopySliceByteToSliceByte, SourceRegister: 1, DestinationRegister: 0},
			load:    func(r *Registers) { r.slicesByte[1] = []byte{3} },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []byte{3}, r.slicesByte[0]) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			caller := standardRegisters()
			destination := standardRegisters()
			tt.load(&caller)

			runArgCopyProgram(nil, nil, &caller, &destination, []program.CallArgCopy{tt.entry})
			tt.inspect(t, &destination)
		})
	}
}

func TestRunArgCopyProgramBoxesAcrossBanks(t *testing.T) {
	t.Parallel()

	caller := standardRegisters()
	destination := standardRegisters()
	caller.Ints[1] = 5

	runArgCopyProgram(nil, nil, &caller, &destination, []program.CallArgCopy{{
		Op:                  program.CopyBoxOrUnbox,
		SourceRegister:      1,
		DestinationRegister: 0,
		KindByte:            byte(isa.RegisterInt) | byte(isa.RegisterGeneral)<<4,
	}})

	require.Equal(t, int64(5), destination.General[0].Int(), "the boxing entry crosses from the int bank to the general one")
}

func TestCopyOneCallArgumentBridgesTheRepresentations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		sourceKind      isa.RegisterKind
		destinationKind isa.RegisterKind
		load            func(*Registers)
		inspect         func(*testing.T, *Registers)
	}{
		{
			name: "a matching bank is copied directly", sourceKind: isa.RegisterInt, destinationKind: isa.RegisterInt,
			load:    func(r *Registers) { r.Ints[1] = 4 },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(4), r.Ints[0]) },
		},
		{
			name: "a scalar is boxed into the general bank", sourceKind: isa.RegisterString, destinationKind: isa.RegisterGeneral,
			load:    func(r *Registers) { r.Strings[1] = "s" },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, "s", r.General[0].Interface()) },
		},
		{
			name: "a general value is unboxed into its bank", sourceKind: isa.RegisterGeneral, destinationKind: isa.RegisterInt,
			load:    func(r *Registers) { r.General[1] = reflect.ValueOf(int64(7)) },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(7), r.Ints[0]) },
		},
		{
			name: "a non-zero comparison result becomes true", sourceKind: isa.RegisterInt, destinationKind: isa.RegisterBool,
			load:    func(r *Registers) { r.Ints[1] = 1 },
			inspect: func(t *testing.T, r *Registers) { require.True(t, r.Bools[0]) },
		},
		{
			name: "a zero comparison result becomes false", sourceKind: isa.RegisterInt, destinationKind: isa.RegisterBool,
			load:    func(r *Registers) { r.Ints[1] = 0 },
			inspect: func(t *testing.T, r *Registers) { require.False(t, r.Bools[0]) },
		},
		{
			name: "a bool becomes the int bank's encoding", sourceKind: isa.RegisterBool, destinationKind: isa.RegisterInt,
			load:    func(r *Registers) { r.Bools[1] = true },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(1), r.Ints[0]) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			caller := standardRegisters()
			destination := standardRegisters()
			tt.load(&caller)

			copyOneCallArgument(&destination, &caller, tt.destinationKind, tt.sourceKind, 0, 1, nil)
			tt.inspect(t, &destination)
		})
	}
}
