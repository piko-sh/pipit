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
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

func TestZeroTypedRegisterDirect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		check func(t *testing.T, regs *Registers)
		name  string
		kind  isa.RegisterKind
	}{
		{name: "int", kind: isa.RegisterInt, check: func(t *testing.T, regs *Registers) {
			regs.Ints[0] = 42
			zeroTypedRegister(regs, program.VarLocation{Register: 0, Kind: isa.RegisterInt})
			require.Equal(t, int64(0), regs.Ints[0])
		}},
		{name: "float", kind: isa.RegisterFloat, check: func(t *testing.T, regs *Registers) {
			regs.Floats[0] = 3.14
			zeroTypedRegister(regs, program.VarLocation{Register: 0, Kind: isa.RegisterFloat})
			require.Equal(t, float64(0), regs.Floats[0])
		}},
		{name: "string", kind: isa.RegisterString, check: func(t *testing.T, regs *Registers) {
			regs.Strings[0] = "hello"
			zeroTypedRegister(regs, program.VarLocation{Register: 0, Kind: isa.RegisterString})
			require.Equal(t, "", regs.Strings[0])
		}},
		{name: "bool", kind: isa.RegisterBool, check: func(t *testing.T, regs *Registers) {
			regs.Bools[0] = true
			zeroTypedRegister(regs, program.VarLocation{Register: 0, Kind: isa.RegisterBool})
			require.Equal(t, false, regs.Bools[0])
		}},
		{name: "uint", kind: isa.RegisterUint, check: func(t *testing.T, regs *Registers) {
			regs.Uints[0] = 42
			zeroTypedRegister(regs, program.VarLocation{Register: 0, Kind: isa.RegisterUint})
			require.Equal(t, uint64(0), regs.Uints[0])
		}},
		{name: "complex", kind: isa.RegisterComplex, check: func(t *testing.T, regs *Registers) {
			regs.Complex[0] = 1 + 2i
			zeroTypedRegister(regs, program.VarLocation{Register: 0, Kind: isa.RegisterComplex})
			require.Equal(t, complex128(0), regs.Complex[0])
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.check(t, new(NewRegisters(testRegCounts())))
		})
	}
}

func TestCopyReturnToGeneral(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expect any
		setup  func(regs *Registers)
		name   string
		kind   isa.RegisterKind
	}{
		{name: "int", kind: isa.RegisterInt, setup: func(regs *Registers) { regs.Ints[0] = 42 }, expect: 42},
		{name: "float", kind: isa.RegisterFloat, setup: func(regs *Registers) { regs.Floats[0] = 3.14 }, expect: float64(3.14)},
		{name: "string", kind: isa.RegisterString, setup: func(regs *Registers) { regs.Strings[0] = "hi" }, expect: "hi"},
		{name: "bool", kind: isa.RegisterBool, setup: func(regs *Registers) { regs.Bools[0] = true }, expect: true},
		{name: "uint", kind: isa.RegisterUint, setup: func(regs *Registers) { regs.Uints[0] = 99 }, expect: uint64(99)},
		{name: "complex", kind: isa.RegisterComplex, setup: func(regs *Registers) { regs.Complex[0] = 1 + 2i }, expect: complex128(1 + 2i)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srcRegs := NewRegisters(testRegCounts())
			tt.setup(&srcRegs)

			callerRegisters := NewRegisters(testRegCounts())
			callerFrame := &CallFrame{Registers: callerRegisters}

			copyReturnToGeneral(callerFrame, &srcRegs, tt.kind, 0, 1)
			require.Equal(t, tt.expect, callerFrame.Registers.General[1].Interface())
		})
	}
}

func TestCopyRegisterSlot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setup func(regs *Registers)
		check func(t *testing.T, regs *Registers)
		name  string
		kind  isa.RegisterKind
	}{
		{name: "int", kind: isa.RegisterInt, setup: func(regs *Registers) { regs.Ints[0] = 42 }, check: func(t *testing.T, regs *Registers) { require.Equal(t, int64(42), regs.Ints[1]) }},
		{name: "float", kind: isa.RegisterFloat, setup: func(regs *Registers) { regs.Floats[0] = 2.5 }, check: func(t *testing.T, regs *Registers) { require.Equal(t, float64(2.5), regs.Floats[1]) }},
		{name: "string", kind: isa.RegisterString, setup: func(regs *Registers) { regs.Strings[0] = "hi" }, check: func(t *testing.T, regs *Registers) { require.Equal(t, "hi", regs.Strings[1]) }},
		{name: "bool", kind: isa.RegisterBool, setup: func(regs *Registers) { regs.Bools[0] = true }, check: func(t *testing.T, regs *Registers) { require.Equal(t, true, regs.Bools[1]) }},
		{name: "uint", kind: isa.RegisterUint, setup: func(regs *Registers) { regs.Uints[0] = 99 }, check: func(t *testing.T, regs *Registers) { require.Equal(t, uint64(99), regs.Uints[1]) }},
		{name: "complex", kind: isa.RegisterComplex, setup: func(regs *Registers) { regs.Complex[0] = 1 + 2i }, check: func(t *testing.T, regs *Registers) { require.Equal(t, complex128(1+2i), regs.Complex[1]) }},
		{name: "general", kind: isa.RegisterGeneral, setup: func(regs *Registers) { regs.General[0] = reflect.ValueOf("val") }, check: func(t *testing.T, regs *Registers) {
			require.Equal(t, "val", regs.General[1].Interface())
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			regs := NewRegisters(testRegCounts())
			tt.setup(&regs)
			copyRegisterSlot(&regs, tt.kind, 1, 0)
			tt.check(t, &regs)
		})
	}
}

func TestUnboxGeneralToScalar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		check func(t *testing.T, regs *Registers)
		value reflect.Value
		name  string
		kind  isa.RegisterKind
	}{
		{name: "int", value: reflect.ValueOf(42), kind: isa.RegisterInt, check: func(t *testing.T, regs *Registers) { require.Equal(t, int64(42), regs.Ints[0]) }},
		{name: "float", value: reflect.ValueOf(float64(3.14)), kind: isa.RegisterFloat, check: func(t *testing.T, regs *Registers) { require.Equal(t, float64(3.14), regs.Floats[0]) }},
		{name: "string", value: reflect.ValueOf("hello"), kind: isa.RegisterString, check: func(t *testing.T, regs *Registers) { require.Equal(t, "hello", regs.Strings[0]) }},
		{name: "bool", value: reflect.ValueOf(true), kind: isa.RegisterBool, check: func(t *testing.T, regs *Registers) { require.Equal(t, true, regs.Bools[0]) }},
		{name: "uint", value: reflect.ValueOf(uint64(99)), kind: isa.RegisterUint, check: func(t *testing.T, regs *Registers) { require.Equal(t, uint64(99), regs.Uints[0]) }},
		{name: "complex", value: reflect.ValueOf(complex128(1 + 2i)), kind: isa.RegisterComplex, check: func(t *testing.T, regs *Registers) { require.Equal(t, complex128(1+2i), regs.Complex[0]) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			regs := NewRegisters(testRegCounts())
			unboxGeneralToScalar(&regs, tt.value, tt.kind, 0, nil)
			tt.check(t, &regs)
		})
	}
}

func TestCopyReturnFromGeneralInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		check func(t *testing.T, regs *Registers)
		name  string
		kind  isa.RegisterKind
	}{
		{name: "int_zero", kind: isa.RegisterInt, check: func(t *testing.T, regs *Registers) { require.Equal(t, int64(0), regs.Ints[0]) }},
		{name: "string_zero", kind: isa.RegisterString, check: func(t *testing.T, regs *Registers) { require.Equal(t, "", regs.Strings[0]) }},
		{name: "bool_zero", kind: isa.RegisterBool, check: func(t *testing.T, regs *Registers) { require.Equal(t, false, regs.Bools[0]) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			regs := NewRegisters(testRegCounts())
			regs.Ints[0] = 99
			regs.Strings[0] = "dirty"
			regs.Bools[0] = true
			frame := &CallFrame{Registers: regs}

			copyReturnFromGeneral(frame, reflect.Value{}, program.VarLocation{Register: 0, Kind: tt.kind})
			tt.check(t, &frame.Registers)
		})
	}
}

func TestCopyReturnFromGeneralInterface(t *testing.T) {
	t.Parallel()

	var v any = int64(42)
	regs := NewRegisters(testRegCounts())
	frame := &CallFrame{Registers: regs}
	copyReturnFromGeneral(frame, reflect.ValueOf(&v).Elem(), program.VarLocation{Register: 0, Kind: isa.RegisterInt})
	require.Equal(t, int64(42), frame.Registers.Ints[0])
}

func TestUnpackGeneralToTyped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		check func(t *testing.T, regs *Registers)
		value reflect.Value
		name  string
		dest  program.VarLocation
	}{
		{name: "int", value: reflect.ValueOf(42), dest: program.VarLocation{Register: 0, Kind: isa.RegisterInt}, check: func(t *testing.T, regs *Registers) { require.Equal(t, int64(42), regs.Ints[0]) }},
		{name: "float", value: reflect.ValueOf(float64(3.14)), dest: program.VarLocation{Register: 0, Kind: isa.RegisterFloat}, check: func(t *testing.T, regs *Registers) { require.Equal(t, float64(3.14), regs.Floats[0]) }},
		{name: "string", value: reflect.ValueOf("hi"), dest: program.VarLocation{Register: 0, Kind: isa.RegisterString}, check: func(t *testing.T, regs *Registers) { require.Equal(t, "hi", regs.Strings[0]) }},
		{name: "bool", value: reflect.ValueOf(true), dest: program.VarLocation{Register: 0, Kind: isa.RegisterBool}, check: func(t *testing.T, regs *Registers) { require.Equal(t, true, regs.Bools[0]) }},
		{name: "uint", value: reflect.ValueOf(uint64(99)), dest: program.VarLocation{Register: 0, Kind: isa.RegisterUint}, check: func(t *testing.T, regs *Registers) { require.Equal(t, uint64(99), regs.Uints[0]) }},
		{name: "complex", value: reflect.ValueOf(complex128(1 + 2i)), dest: program.VarLocation{Register: 0, Kind: isa.RegisterComplex}, check: func(t *testing.T, regs *Registers) { require.Equal(t, complex128(1+2i), regs.Complex[0]) }},
		{name: "bool_to_int", value: reflect.ValueOf(true), dest: program.VarLocation{Register: 0, Kind: isa.RegisterInt}, check: func(t *testing.T, regs *Registers) { require.Equal(t, int64(1), regs.Ints[0]) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			regs := NewRegisters(testRegCounts())
			unpackGeneralToTyped(&regs, tt.value, tt.dest)
			tt.check(t, &regs)
		})
	}
}

func TestReflectBinaryOp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expect any
		a      reflect.Value
		b      reflect.Value
		name   string
	}{
		{name: "int_add", a: reflect.ValueOf(int64(10)), b: reflect.ValueOf(int64(3)), expect: int64(13)},
		{name: "float_add", a: reflect.ValueOf(float64(1.5)), b: reflect.ValueOf(float64(2.5)), expect: float64(4.0)},
		{name: "string_concat", a: reflect.ValueOf("hello "), b: reflect.ValueOf("world"), expect: "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ReflectBinaryOp(tt.a, tt.b,
				func(x, y int64) int64 { return x + y },
				func(x, y float64) float64 { return x + y },
				func(x, y string) string { return x + y },
				nil,
			)
			require.Equal(t, tt.expect, result.Interface())
		})
	}
}

func TestGrowCallStack(t *testing.T) {
	t.Parallel()

	vm := &VM{
		CallStack: make([]CallFrame, 2),
	}
	originalCap := len(vm.CallStack)

	vm.growCallStack()

	require.Equal(t, originalCap*2, len(vm.CallStack))
}

func TestGrowCallStackWithASMArrays(t *testing.T) {
	t.Parallel()

	vm := &VM{
		CallStack:        make([]CallFrame, 2),
		asmCallInfoBases: make([]uintptr, 2),
		asmDispatchSaves: make([]asmDispatchSave, 2),
	}

	vm.growCallStack()

	require.Equal(t, 4, len(vm.CallStack))
	require.Equal(t, 4, len(vm.asmCallInfoBases))
	require.Equal(t, 4, len(vm.asmDispatchSaves))
}

func TestKindDefaultReflectType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expect reflect.Type
		name   string
		kind   isa.RegisterKind
	}{
		{name: "int", kind: isa.RegisterInt, expect: reflect.TypeFor[int]()},
		{name: "float", kind: isa.RegisterFloat, expect: reflect.TypeFor[float64]()},
		{name: "string", kind: isa.RegisterString, expect: reflect.TypeFor[string]()},
		{name: "bool", kind: isa.RegisterBool, expect: reflect.TypeFor[bool]()},
		{name: "uint", kind: isa.RegisterUint, expect: reflect.TypeFor[uint64]()},
		{name: "complex", kind: isa.RegisterComplex, expect: reflect.TypeFor[complex128]()},
		{name: "general", kind: isa.RegisterGeneral, expect: reflect.TypeFor[any]()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := program.KindDefaultReflectType(tt.kind)
			require.Equal(t, tt.expect, result)
		})
	}
}

func TestWriteRegisterValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		check func(t *testing.T, regs *Registers)
		value reflect.Value
		name  string
		kind  isa.RegisterKind
	}{
		{name: "int", kind: isa.RegisterInt, value: reflect.ValueOf(42), check: func(t *testing.T, regs *Registers) { require.Equal(t, int64(42), regs.Ints[0]) }},
		{name: "float", kind: isa.RegisterFloat, value: reflect.ValueOf(float64(3.14)), check: func(t *testing.T, regs *Registers) { require.Equal(t, float64(3.14), regs.Floats[0]) }},
		{name: "string", kind: isa.RegisterString, value: reflect.ValueOf("hello"), check: func(t *testing.T, regs *Registers) { require.Equal(t, "hello", regs.Strings[0]) }},
		{name: "bool", kind: isa.RegisterBool, value: reflect.ValueOf(true), check: func(t *testing.T, regs *Registers) { require.Equal(t, true, regs.Bools[0]) }},
		{name: "uint", kind: isa.RegisterUint, value: reflect.ValueOf(uint64(99)), check: func(t *testing.T, regs *Registers) { require.Equal(t, uint64(99), regs.Uints[0]) }},
		{name: "complex", kind: isa.RegisterComplex, value: reflect.ValueOf(complex128(1 + 2i)), check: func(t *testing.T, regs *Registers) { require.Equal(t, complex128(1+2i), regs.Complex[0]) }},
		{name: "general", kind: isa.RegisterGeneral, value: reflect.ValueOf("general_val"), check: func(t *testing.T, regs *Registers) {
			require.Equal(t, "general_val", regs.General[0].Interface())
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			regs := NewRegisters(testRegCounts())
			writeRegisterValue(&regs, 0, tt.kind, tt.value)
			tt.check(t, &regs)
		})
	}
}

func TestBoxScalarToGeneral(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expect any
		setup  func(regs *Registers)
		name   string
		kind   isa.RegisterKind
	}{
		{name: "int", kind: isa.RegisterInt, setup: func(regs *Registers) { regs.Ints[0] = 42 }, expect: 42},
		{name: "float", kind: isa.RegisterFloat, setup: func(regs *Registers) { regs.Floats[0] = 3.14 }, expect: float64(3.14)},
		{name: "string", kind: isa.RegisterString, setup: func(regs *Registers) { regs.Strings[0] = "hi" }, expect: "hi"},
		{name: "bool", kind: isa.RegisterBool, setup: func(regs *Registers) { regs.Bools[0] = true }, expect: true},
		{name: "uint", kind: isa.RegisterUint, setup: func(regs *Registers) { regs.Uints[0] = 99 }, expect: uint64(99)},
		{name: "complex", kind: isa.RegisterComplex, setup: func(regs *Registers) { regs.Complex[0] = 1 + 2i }, expect: complex128(1 + 2i)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := NewRegisters(testRegCounts())
			destination := NewRegisters(testRegCounts())
			tt.setup(&source)
			boxScalarToGeneral(&destination, &source, tt.kind, 1, 0, nil)
			require.Equal(t, tt.expect, destination.General[1].Interface())
		})
	}
}

func TestSyncCellToRegister(t *testing.T) {
	t.Parallel()

	tests := []struct {
		check func(t *testing.T, regs *Registers)
		name  string
		cell  program.UpvalueCell
		kind  isa.RegisterKind
	}{
		{name: "int", kind: isa.RegisterInt, cell: program.UpvalueCell{IntValue: 42}, check: func(t *testing.T, regs *Registers) { require.Equal(t, int64(42), regs.Ints[0]) }},
		{name: "float", kind: isa.RegisterFloat, cell: program.UpvalueCell{FloatValue: 3.14}, check: func(t *testing.T, regs *Registers) { require.Equal(t, float64(3.14), regs.Floats[0]) }},
		{name: "string", kind: isa.RegisterString, cell: program.UpvalueCell{StringValue: "hi"}, check: func(t *testing.T, regs *Registers) { require.Equal(t, "hi", regs.Strings[0]) }},
		{name: "bool", kind: isa.RegisterBool, cell: program.UpvalueCell{BoolValue: true}, check: func(t *testing.T, regs *Registers) { require.Equal(t, true, regs.Bools[0]) }},
		{name: "uint", kind: isa.RegisterUint, cell: program.UpvalueCell{UintValue: 99}, check: func(t *testing.T, regs *Registers) { require.Equal(t, uint64(99), regs.Uints[0]) }},
		{name: "complex", kind: isa.RegisterComplex, cell: program.UpvalueCell{ComplexValue: 1 + 2i}, check: func(t *testing.T, regs *Registers) { require.Equal(t, complex128(1+2i), regs.Complex[0]) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			regs := NewRegisters(testRegCounts())
			syncCellToRegister(&regs, &tt.cell, program.VarLocation{Register: 0, Kind: tt.kind})
			tt.check(t, &regs)
		})
	}
}

func TestAssignReflectArg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		check func(t *testing.T, regs *Registers)
		value reflect.Value
		name  string
		kind  isa.RegisterKind
	}{
		{name: "int", kind: isa.RegisterInt, value: reflect.ValueOf(int64(42)), check: func(t *testing.T, regs *Registers) { require.Equal(t, int64(42), regs.Ints[0]) }},
		{name: "float", kind: isa.RegisterFloat, value: reflect.ValueOf(float64(3.14)), check: func(t *testing.T, regs *Registers) { require.Equal(t, float64(3.14), regs.Floats[0]) }},
		{name: "string", kind: isa.RegisterString, value: reflect.ValueOf("hi"), check: func(t *testing.T, regs *Registers) { require.Equal(t, "hi", regs.Strings[0]) }},
		{name: "bool", kind: isa.RegisterBool, value: reflect.ValueOf(true), check: func(t *testing.T, regs *Registers) { require.Equal(t, true, regs.Bools[0]) }},
		{name: "uint", kind: isa.RegisterUint, value: reflect.ValueOf(uint64(99)), check: func(t *testing.T, regs *Registers) { require.Equal(t, uint64(99), regs.Uints[0]) }},
		{name: "complex", kind: isa.RegisterComplex, value: reflect.ValueOf(complex128(1 + 2i)), check: func(t *testing.T, regs *Registers) { require.Equal(t, complex128(1+2i), regs.Complex[0]) }},
		{name: "general", kind: isa.RegisterGeneral, value: reflect.ValueOf("gen_val"), check: func(t *testing.T, regs *Registers) {
			require.Equal(t, "gen_val", regs.General[0].Interface())
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			regs := NewRegisters(testRegCounts())
			assignReflectArg(&regs, tt.kind, 0, tt.value)
			tt.check(t, &regs)
		})
	}
}

func TestHandleOpResult(t *testing.T) {
	t.Parallel()

	t.Run("opContinue returns non-terminal", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)
		result, terminal, err := vm.handleOpResult(opContinue)
		require.Nil(t, result)
		require.False(t, terminal)
		require.NoError(t, err)
	})

	t.Run("opDone returns result and clears evalResult", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)
		vm.evalResult = "hello"
		result, terminal, err := vm.handleOpResult(opDone)
		require.Equal(t, "hello", result)
		require.True(t, terminal)
		require.NoError(t, err)
		require.Nil(t, vm.evalResult)
	})

	t.Run("opDivByZero", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)
		result, terminal, err := vm.handleOpResult(opDivByZero)
		require.Nil(t, result)
		require.True(t, terminal)
		require.ErrorIs(t, err, fault.ErrDivisionByZero)
	})

	t.Run("opStackOverflow", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)
		result, terminal, err := vm.handleOpResult(opStackOverflow)
		require.Nil(t, result)
		require.True(t, terminal)
		require.ErrorIs(t, err, fault.ErrStackOverflow)
	})

	t.Run("opPanicError returns error and clears evalError", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)
		testErr := errors.New("test panic")
		vm.evalError = testErr
		result, terminal, err := vm.handleOpResult(opPanicError)
		require.Nil(t, result)
		require.True(t, terminal)
		require.ErrorIs(t, err, testErr)
		require.Nil(t, vm.evalError)
	})

	t.Run("unknown opResult returns non-terminal", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)
		result, terminal, err := vm.handleOpResult(OpResult(99))
		require.Nil(t, result)
		require.False(t, terminal)
		require.NoError(t, err)
	})
}

func TestCopyReturnValueAt(t *testing.T) {
	t.Parallel()

	t.Run("same kind int to int", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)
		vm.CallStack = make([]CallFrame, 2)
		vm.FramePointer = 1
		vm.CallStack[0].Registers = NewRegisters(testRegCounts())
		vm.CallStack[1].Registers = NewRegisters(testRegCounts())
		vm.CallStack[1].Registers.Ints[0] = 42

		vm.copyReturnValueAt(
			&vm.CallStack[1],
			isa.RegisterInt, 0,
			program.VarLocation{Register: 1, Kind: isa.RegisterInt},
		)
		require.Equal(t, int64(42), vm.CallStack[0].Registers.Ints[1])
	})

	t.Run("same kind string to string", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)
		vm.CallStack = make([]CallFrame, 2)
		vm.FramePointer = 1
		vm.CallStack[0].Registers = NewRegisters(testRegCounts())
		vm.CallStack[1].Registers = NewRegisters(testRegCounts())
		vm.CallStack[1].Registers.Strings[0] = "hello"

		vm.copyReturnValueAt(
			&vm.CallStack[1],
			isa.RegisterString, 0,
			program.VarLocation{Register: 1, Kind: isa.RegisterString},
		)
		require.Equal(t, "hello", vm.CallStack[0].Registers.Strings[1])
	})

	t.Run("general to scalar unpacks", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)
		vm.CallStack = make([]CallFrame, 2)
		vm.FramePointer = 1
		vm.CallStack[0].Registers = NewRegisters(testRegCounts())
		vm.CallStack[1].Registers = NewRegisters(testRegCounts())
		vm.CallStack[1].Registers.General[0] = reflect.ValueOf(int64(42))

		vm.copyReturnValueAt(
			&vm.CallStack[1],
			isa.RegisterGeneral, 0,
			program.VarLocation{Register: 1, Kind: isa.RegisterInt},
		)
		require.Equal(t, int64(42), vm.CallStack[0].Registers.Ints[1])
	})

	t.Run("scalar to general packs", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)
		vm.CallStack = make([]CallFrame, 2)
		vm.FramePointer = 1
		vm.CallStack[0].Registers = NewRegisters(testRegCounts())
		vm.CallStack[1].Registers = NewRegisters(testRegCounts())
		vm.CallStack[1].Registers.Ints[0] = 42

		vm.copyReturnValueAt(
			&vm.CallStack[1],
			isa.RegisterInt, 0,
			program.VarLocation{Register: 1, Kind: isa.RegisterGeneral},
		)
		require.Equal(t, 42, vm.CallStack[0].Registers.General[1].Interface())
	})

	t.Run("mismatch int to string is silent no-op", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)
		vm.CallStack = make([]CallFrame, 2)
		vm.FramePointer = 1
		vm.CallStack[0].Registers = NewRegisters(testRegCounts())
		vm.CallStack[1].Registers = NewRegisters(testRegCounts())
		vm.CallStack[0].Registers.Strings[1] = "original"
		vm.CallStack[1].Registers.Ints[0] = 42

		vm.copyReturnValueAt(
			&vm.CallStack[1],
			isa.RegisterInt, 0,
			program.VarLocation{Register: 1, Kind: isa.RegisterString},
		)

		require.Equal(t, "original", vm.CallStack[0].Registers.Strings[1])
	})

	t.Run("mismatch float to bool is silent no-op", func(t *testing.T) {
		t.Parallel()
		vm := newTestVM(t)
		vm.CallStack = make([]CallFrame, 2)
		vm.FramePointer = 1
		vm.CallStack[0].Registers = NewRegisters(testRegCounts())
		vm.CallStack[1].Registers = NewRegisters(testRegCounts())
		vm.CallStack[0].Registers.Bools[1] = true
		vm.CallStack[1].Registers.Floats[0] = 3.14

		vm.copyReturnValueAt(
			&vm.CallStack[1],
			isa.RegisterFloat, 0,
			program.VarLocation{Register: 1, Kind: isa.RegisterBool},
		)
		require.Equal(t, true, vm.CallStack[0].Registers.Bools[1])
	})
}

func TestReflectBinaryOpAtTypeBoundaries(t *testing.T) {
	t.Parallel()

	t.Run("uint addition", func(t *testing.T) {
		t.Parallel()
		a := reflect.ValueOf(uint64(10))
		b := reflect.ValueOf(uint64(32))
		result := ReflectBinaryOp(a, b,
			func(x, y int64) int64 { return x + y },
			nil, nil, nil,
		)
		require.True(t, result.IsValid())
		require.Equal(t, uint64(42), result.Interface())
	})

	t.Run("string concatenation with nil int and float ops", func(t *testing.T) {
		t.Parallel()
		a := reflect.ValueOf("hello ")
		b := reflect.ValueOf("world")
		result := ReflectBinaryOp(a, b,
			nil, nil,
			func(x, y string) string { return x + y }, nil,
		)
		require.True(t, result.IsValid())
		require.Equal(t, "hello world", result.Interface())
	})

	t.Run("int with nil intOp returns invalid", func(t *testing.T) {
		t.Parallel()
		a := reflect.ValueOf(int64(10))
		b := reflect.ValueOf(int64(20))
		result := ReflectBinaryOp(a, b, nil, nil, nil, nil)
		require.False(t, result.IsValid())
	})

	t.Run("float with nil floatOp returns invalid", func(t *testing.T) {
		t.Parallel()
		a := reflect.ValueOf(float64(1.5))
		b := reflect.ValueOf(float64(2.5))
		result := ReflectBinaryOp(a, b, nil, nil, nil, nil)
		require.False(t, result.IsValid())
	})

	t.Run("unhandled kind returns invalid", func(t *testing.T) {
		t.Parallel()
		a := reflect.ValueOf(true)
		b := reflect.ValueOf(false)
		result := ReflectBinaryOp(a, b,
			func(x, y int64) int64 { return x + y },
			func(x, y float64) float64 { return x + y },
			func(x, y string) string { return x + y },
			nil,
		)
		require.False(t, result.IsValid())
	})
}
