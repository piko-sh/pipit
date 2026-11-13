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

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
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
	basic, ok := basicScalarFromRegister(arena, registers, kind, index, hint.Kind())
	if !ok {
		return nil, false
	}
	if hint.PkgPath() == "" && basicReflectTypeForKind(hint.Kind()) == hint {
		return basic, true
	}
	return reflect.ValueOf(basic).Convert(hint).Interface(), true
}

// basicScalarFromRegister reads the register at index of bank kind as the predeclared
// type of valueKind, boxing it once with a plain conversion, which allocates nothing for
// small integers, booleans and the zero value.
//
// Takes arena (*RegisterArena) which owns arena-resident strings. It may be nil.
// Takes registers (*Registers) which is the frame's register file.
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes index (int) which is the register slot.
// Takes valueKind (reflect.Kind) which is the result's kind.
//
// Returns any which holds the value at the predeclared type of valueKind.
// Returns bool which is false when the bank or slot cannot hold the value.
func basicScalarFromRegister(arena *RegisterArena, registers *Registers, kind isa.RegisterKind, index int, valueKind reflect.Kind) (any, bool) {
	switch valueKind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if kind != isa.RegisterInt64 || index >= len(registers.Ints) {
			return nil, false
		}
		return boxSignedAs(valueKind, registers.Ints[index]), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if kind != isa.RegisterUint64 || index >= len(registers.Uints) {
			return nil, false
		}
		return boxUnsignedAs(valueKind, registers.Uints[index]), true
	case reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return basicFloatFromRegister(registers, kind, index, valueKind)
	default:
		return basicNonNumericFromRegister(arena, registers, kind, index, valueKind)
	}
}

// basicFloatFromRegister is basicScalarFromRegister for a float or a complex kind.
//
// Takes registers (*Registers) which is the frame's register file.
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes index (int) which is the register slot.
// Takes valueKind (reflect.Kind) which is a float or complex kind.
//
// Returns any which holds the value at the predeclared type of valueKind.
// Returns bool which is false when the bank or slot cannot hold the value.
func basicFloatFromRegister(registers *Registers, kind isa.RegisterKind, index int, valueKind reflect.Kind) (any, bool) {
	switch {
	case kind == isa.RegisterFloat64 && index < len(registers.Floats) && valueKind == reflect.Float32:
		return float32(registers.Floats[index]), true
	case kind == isa.RegisterFloat64 && index < len(registers.Floats) && valueKind == reflect.Float64:
		return registers.Floats[index], true
	case kind == isa.RegisterComplex128 && index < len(registers.Complex) && valueKind == reflect.Complex64:
		return complex64(registers.Complex[index]), true
	case kind == isa.RegisterComplex128 && index < len(registers.Complex) && valueKind == reflect.Complex128:
		return registers.Complex[index], true
	default:
		return nil, false
	}
}

// basicNonNumericFromRegister is basicScalarFromRegister for a bool or a string.
//
// Takes arena (*RegisterArena) which owns arena-resident strings. It may be nil.
// Takes registers (*Registers) which is the frame's register file.
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes index (int) which is the register slot.
// Takes valueKind (reflect.Kind) which is the result's kind.
//
// Returns any which holds the value.
// Returns bool which is false when the bank or slot cannot hold the value, or the kind is
// no scalar kind.
func basicNonNumericFromRegister(arena *RegisterArena, registers *Registers, kind isa.RegisterKind, index int, valueKind reflect.Kind) (any, bool) {
	switch {
	case valueKind == reflect.Bool && kind == isa.RegisterBool && index < len(registers.Bools):
		return registers.Bools[index], true
	case valueKind == reflect.Bool && kind == isa.RegisterInt64 && index < len(registers.Ints):
		return registers.Ints[index] != 0, true
	case valueKind == reflect.String && kind == isa.RegisterString && index < len(registers.Strings):
		return materialiseString(arena, registers.Strings[index]), true
	default:
		return nil, false
	}
}

// boxSignedAs boxes value as the predeclared signed integer type of valueKind. The common
// int and int64 results stay small enough to inline, so a result that does not escape is
// not boxed on the heap.
//
// Takes valueKind (reflect.Kind) which is a signed integer kind.
// Takes value (int64) which is the register's value.
//
// Returns any which holds the value at that type.
func boxSignedAs(valueKind reflect.Kind, value int64) any {
	switch valueKind {
	case reflect.Int64:
		return value
	case reflect.Int:
		return int(value)
	default:
		return boxNarrowSignedAs(valueKind, value)
	}
}

// boxNarrowSignedAs boxes value as int8, int16 or int32, truncated as Go's conversion and
// reflect's SetInt truncate it. The value is wrapped into the type's range first, so the
// range-checked conversion that follows never clamps.
//
// Takes valueKind (reflect.Kind) which is a narrow signed integer kind.
// Takes value (int64) which is the register's value.
//
// Returns any which holds the value truncated to that type.
func boxNarrowSignedAs(valueKind reflect.Kind, value int64) any {
	switch valueKind {
	case reflect.Int8:
		return safeconv.Int64ToInt8(wrapSignedInto(value, math.MinInt8, math.MaxUint8))
	case reflect.Int16:
		return safeconv.Int64ToInt16(wrapSignedInto(value, math.MinInt16, math.MaxUint16))
	default:
		return safeconv.Int64ToInt32(wrapSignedInto(value, math.MinInt32, math.MaxUint32))
	}
}

// boxUnsignedAs boxes value as the predeclared unsigned integer type of valueKind. The
// common uint, uint64 and uintptr results stay small enough to inline.
//
// Takes valueKind (reflect.Kind) which is an unsigned integer kind.
// Takes value (uint64) which is the register's value.
//
// Returns any which holds the value at that type.
func boxUnsignedAs(valueKind reflect.Kind, value uint64) any {
	switch valueKind {
	case reflect.Uint64:
		return value
	case reflect.Uintptr:
		return uintptr(value)
	case reflect.Uint:
		return uint(value)
	default:
		return boxNarrowUnsignedAs(valueKind, value)
	}
}

// boxNarrowUnsignedAs boxes value as uint8, uint16 or uint32, truncated as Go's
// conversion and reflect's SetUint truncate it. The value is masked to the type's width
// first, so the range-checked conversion that follows never clamps.
//
// Takes valueKind (reflect.Kind) which is a narrow unsigned integer kind.
// Takes value (uint64) which is the register's value.
//
// Returns any which holds the value truncated to that type.
func boxNarrowUnsignedAs(valueKind reflect.Kind, value uint64) any {
	switch valueKind {
	case reflect.Uint8:
		return safeconv.Uint64ToUint8(value & math.MaxUint8)
	case reflect.Uint16:
		return safeconv.Uint64ToUint16(value & math.MaxUint16)
	default:
		return safeconv.Uint64ToUint32(value & math.MaxUint32)
	}
}

// wrapSignedInto keeps the low bits of value and sign-extends them, giving the value Go's
// conversion to a narrower signed integer produces, held in an int64.
//
// Takes value (int64) which is the value to wrap.
// Takes minimum (int64) which is the narrower type's minimum.
// Takes mask (int64) which is the narrower type's width as a mask of ones.
//
// Returns int64 which lies in the narrower type's range.
func wrapSignedInto(value, minimum, mask int64) int64 {
	return (value-minimum)&mask + minimum
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
