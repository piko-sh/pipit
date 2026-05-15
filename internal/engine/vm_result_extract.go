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
	"math"
	"reflect"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

// resultReflectTypeHint returns the exact type recorded for result position index, or nil
// when none was recorded.
//
// Takes function (*program.CompiledFunction) which owns the result metadata.
// Takes index (int) which is the result position.
//
// Returns reflect.Type which may be nil.
func resultReflectTypeHint(function *program.CompiledFunction, index int) reflect.Type {
	if index < len(function.ResultReflectTypes) {
		return function.ResultReflectTypes[index]
	}
	return nil
}

// extractScalarRegisterValueAs reads a scalar register and clothes it in hint, the exact
// static type of the value, so a host receives int32 or a named type rather than the
// bank's canonical representation.
//
// Takes arena (*RegisterArena) which string data may live in; a string is materialised
// against it so the result outlives frame teardown, exactly as the canonical path does.
// Takes registers (*Registers) which is the frame's register file.
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes index (int) which is the register slot.
// Takes hint (reflect.Type) which is the exact type; must be non-nil.
//
// Returns the value and true when the bank is a scalar bank whose value hint can hold;
// false leaves the caller to the canonical path.
func extractScalarRegisterValueAs(arena *RegisterArena, registers *Registers, kind isa.RegisterKind, index int, hint reflect.Type) (any, bool) {
	if typemodel.IsNamedScalarPoolType(hint) {
		hint = basicReflectTypeForKind(hint.Kind())
	}
	out := reflect.New(hint).Elem()
	if !setScalarFromRegister(arena, out, registers, kind, index) {
		return nil, false
	}
	return out.Interface(), true
}

// setScalarFromRegister stores the register at index of bank kind into out, whose kind
// decides which bank may hold it; an arena string is materialised on the way out.
//
// Takes arena (*RegisterArena) which owns arena-resident strings; may be nil.
// Takes out (reflect.Value) which is a settable value of the exact static type.
// Takes registers (*Registers) which is the frame's register file.
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes index (int) which is the register slot.
//
// Returns bool which is false when the bank or slot cannot hold the value.
func setScalarFromRegister(arena *RegisterArena, out reflect.Value, registers *Registers, kind isa.RegisterKind, index int) bool {
	switch out.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return setIntFromRegister(out, registers, kind, index)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return setUintFromRegister(out, registers, kind, index)
	case reflect.Float32, reflect.Float64:
		return setFloatFromRegister(out, registers, kind, index)
	case reflect.Bool:
		return setBoolFromRegister(out, registers, kind, index)
	case reflect.String:
		return setStringFromRegister(arena, out, registers, kind, index)
	case reflect.Complex64, reflect.Complex128:
		return setComplexFromRegister(out, registers, kind, index)
	default:
		return false
	}
}

// setIntFromRegister stores an int-bank register into out.
//
// Takes out (reflect.Value) which is a settable integer.
// Takes registers (*Registers) which is the frame's register file.
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes index (int) which is the register slot.
//
// Returns bool which is false when the bank or slot cannot hold the value.
func setIntFromRegister(out reflect.Value, registers *Registers, kind isa.RegisterKind, index int) bool {
	if kind != isa.RegisterInt || index >= len(registers.Ints) {
		return false
	}
	out.SetInt(registers.Ints[index])
	return true
}

// setUintFromRegister stores a uint-bank register into out.
//
// Takes out (reflect.Value) which is a settable unsigned integer.
// Takes registers (*Registers) which is the frame's register file.
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes index (int) which is the register slot.
//
// Returns bool which is false when the bank or slot cannot hold the value.
func setUintFromRegister(out reflect.Value, registers *Registers, kind isa.RegisterKind, index int) bool {
	if kind != isa.RegisterUint || index >= len(registers.Uints) {
		return false
	}
	out.SetUint(registers.Uints[index])
	return true
}

// setFloatFromRegister stores a float-bank register into out.
//
// Takes out (reflect.Value) which is a settable float.
// Takes registers (*Registers) which is the frame's register file.
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes index (int) which is the register slot.
//
// Returns bool which is false when the bank or slot cannot hold the value.
func setFloatFromRegister(out reflect.Value, registers *Registers, kind isa.RegisterKind, index int) bool {
	if kind != isa.RegisterFloat || index >= len(registers.Floats) {
		return false
	}
	out.SetFloat(registers.Floats[index])
	return true
}

// setBoolFromRegister stores a bool held in the bool bank, or as an int flag, into out.
//
// Takes out (reflect.Value) which is a settable bool.
// Takes registers (*Registers) which is the frame's register file.
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes index (int) which is the register slot.
//
// Returns bool which is false when the bank or slot cannot hold the value.
func setBoolFromRegister(out reflect.Value, registers *Registers, kind isa.RegisterKind, index int) bool {
	if kind == isa.RegisterBool && index < len(registers.Bools) {
		out.SetBool(registers.Bools[index])
		return true
	}
	if kind == isa.RegisterInt && index < len(registers.Ints) {
		out.SetBool(registers.Ints[index] != 0)
		return true
	}
	return false
}

// setStringFromRegister stores a string-bank register into out, materialising an
// arena-resident string first.
//
// Takes arena (*RegisterArena) which owns arena-resident strings; may be nil.
// Takes out (reflect.Value) which is a settable string.
// Takes registers (*Registers) which is the frame's register file.
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes index (int) which is the register slot.
//
// Returns bool which is false when the bank or slot cannot hold the value.
func setStringFromRegister(arena *RegisterArena, out reflect.Value, registers *Registers, kind isa.RegisterKind, index int) bool {
	if kind != isa.RegisterString || index >= len(registers.Strings) {
		return false
	}
	out.SetString(materialiseString(arena, registers.Strings[index]))
	return true
}

// setComplexFromRegister stores a complex-bank register into out.
//
// Takes out (reflect.Value) which is a settable complex number.
// Takes registers (*Registers) which is the frame's register file.
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes index (int) which is the register slot.
//
// Returns bool which is false when the bank or slot cannot hold the value.
func setComplexFromRegister(out reflect.Value, registers *Registers, kind isa.RegisterKind, index int) bool {
	if kind != isa.RegisterComplex || index >= len(registers.Complex) {
		return false
	}
	out.SetComplex(registers.Complex[index])
	return true
}

// extractRegisterValueAs reads a result register and clothes it in hint, the exact static
// type of the value: scalars through extractScalarRegisterValueAs, typed-bank slices
// through the width-aware boxer, so a []uint or a []int32 result leaves the interpreter
// as that type rather than the bank's canonical slice.
//
// Takes arena (*RegisterArena) which owns arena-backed slices.
// Takes registers (*Registers) which is the frame's register file.
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes index (int) which is the register slot.
// Takes hint (reflect.Type) which is the exact type; must be non-nil.
//
// Returns the value and true when the bank is one the hint can be applied to.
func extractRegisterValueAs(arena *RegisterArena, registers *Registers, kind isa.RegisterKind, index int, hint reflect.Type) (any, bool) {
	if isa.IsTypedSliceKind(kind) {
		if hint.Kind() != reflect.Slice || index > math.MaxUint8 {
			return nil, false
		}
		value, ok := boxTypedSliceAs(arena, hint, registers, safeconv.MustIntToUint8(index), kind)
		if !ok || !value.IsValid() {
			return nil, false
		}
		return value.Interface(), true
	}
	return extractScalarRegisterValueAs(arena, registers, kind, index, hint)
}
