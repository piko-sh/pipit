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

package isa

const (
	// NumRegisterKinds is the count of register bank types.
	NumRegisterKinds = int(RegisterSliceByte) + 1

	// NarrowIntegerBitWidth8 selects an 8-bit truncation for int8 and uint8 arithmetic
	// results emitted via OpTruncateNarrow.
	NarrowIntegerBitWidth8 uint8 = 8

	// NarrowIntegerBitWidth16 selects a 16-bit truncation for int16 and uint16 arithmetic
	// results emitted via OpTruncateNarrow.
	NarrowIntegerBitWidth16 uint8 = 16

	// NarrowIntegerBitWidth32 selects a 32-bit truncation for int32 and uint32 arithmetic
	// results emitted via OpTruncateNarrow. Width 64 is intentionally absent because
	// int64/uint64 already hold the full declared width.
	NarrowIntegerBitWidth32 uint8 = 32
)

// RegisterKind identifies which typed register bank a value belongs to. Using separate
// banks for common primitive types avoids the overhead of boxing and unboxing values in
// reflect.Value for the majority of operations.
type RegisterKind uint8

const (
	// RegisterInt stores int64 values. All Go signed integer types (int, int8, int16, int32,
	// int64) and untyped int/rune are stored here, using int64 as the common representation.
	RegisterInt RegisterKind = iota

	// RegisterFloat stores float64 values. Both float32 and float64 are stored here, with
	// float32 promoted to float64.
	RegisterFloat

	// RegisterString stores string values natively.
	RegisterString

	// RegisterGeneral stores reflect.Value for all other types: interfaces, pointers,
	// slices, maps, arrays, structs, channels, and functions.
	RegisterGeneral

	// RegisterBool stores bool values natively.
	RegisterBool

	// RegisterUint stores uint64 values. All Go unsigned integer types (uint, uint8, uint16,
	// uint32, uint64, uintptr) are stored here.
	RegisterUint

	// RegisterComplex stores complex128 values. Both complex64 and complex128 are stored
	// here, with complex64 promoted to complex128.
	RegisterComplex

	// RegisterSliceInt stores []int64 slice headers natively. The compiler selects this bank
	// when the slice element kind resolves to RegisterInt at compile time, eliminating
	// reflect.Value boxing for the slice header and per-element access.
	RegisterSliceInt

	// RegisterSliceFloat stores []float64 slice headers natively for slices whose element
	// kind resolves to RegisterFloat.
	RegisterSliceFloat

	// RegisterSliceString stores []string slice headers natively for slices whose element
	// kind resolves to RegisterString.
	RegisterSliceString

	// RegisterSliceBool stores []bool slice headers natively for slices whose element kind
	// resolves to RegisterBool.
	RegisterSliceBool

	// RegisterSliceUint stores []uint64 slice headers natively for slices whose element kind
	// resolves to RegisterUint.
	RegisterSliceUint

	// RegisterSliceByte stores []byte slice headers natively. Distinct from
	// RegisterSliceUint because the 1-byte element width enables tighter ASM element access.
	RegisterSliceByte
)

// String returns the human-readable name of the register kind.
//
// Returns the register kind name as a string.
func (k RegisterKind) String() string {
	switch k {
	case RegisterInt:
		return "int"
	case RegisterFloat:
		return "float"
	case RegisterString:
		return "string"
	case RegisterGeneral:
		return "general"
	case RegisterBool:
		return "bool"
	case RegisterUint:
		return "uint"
	case RegisterComplex:
		return "complex"
	case RegisterSliceInt:
		return "sliceInt"
	case RegisterSliceFloat:
		return "sliceFloat"
	case RegisterSliceString:
		return "sliceString"
	case RegisterSliceBool:
		return "sliceBool"
	case RegisterSliceUint:
		return "sliceUint"
	case RegisterSliceByte:
		return "sliceByte"
	default:
		return "unknown"
	}
}

// IsTypedSliceKind reports whether kind is one of the six typed slice register banks.
//
// Takes kind (RegisterKind) which is the kind under test.
//
// Returns true when kind matches one of the typed-slice banks.
func IsTypedSliceKind(kind RegisterKind) bool {
	switch kind {
	case RegisterSliceInt, RegisterSliceFloat, RegisterSliceString, RegisterSliceBool, RegisterSliceUint, RegisterSliceByte:
		return true
	default:
	}
	return false
}

// ElementKindForTypedSlice returns the element register kind that the given typed-slice
// bank stores.
//
// For example RegisterSliceFloat holds RegisterFloat elements. Returns RegisterGeneral
// when kind is not a typed-slice bank.
//
// Takes kind (RegisterKind) which is the typed-slice bank.
//
// Returns the element register kind, or RegisterGeneral on non-typed-slice inputs.
func ElementKindForTypedSlice(kind RegisterKind) RegisterKind {
	switch kind {
	case RegisterSliceInt:
		return RegisterInt
	case RegisterSliceFloat:
		return RegisterFloat
	case RegisterSliceString:
		return RegisterString
	case RegisterSliceBool:
		return RegisterBool
	case RegisterSliceUint, RegisterSliceByte:
		return RegisterUint
	default:
	}
	return RegisterGeneral
}
