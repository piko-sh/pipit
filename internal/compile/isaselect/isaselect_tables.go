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

// Package isaselect holds the opcode and sub-opcode selection tables.
//
// Each table is a pure mapping from register kind, Go type, or operator token to the
// instruction the emitter should use. Nothing here touches the Compiler. Every entry is a
// lookup the emitters consult.
package isaselect

import (
	"go/token"
	"go/types"
	"reflect"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

// CompoundSliceOps bundles the per-direction opcodes the typed-slice compound-assignment
// path needs so the read and write halves share the same dispatch helper.
type CompoundSliceOps struct {
	// Tier1SubOp resolves the typed-direct tier-1 sub-op for a given element register kind.
	Tier1SubOp func(isa.RegisterKind) (isa.SubOpcode, bool)

	// PerKind maps each register kind to the per-kind tier-0 opcode used by the generic
	// dispatch path.
	PerKind [isa.NumRegisterKinds]isa.Opcode

	// DirectIntOpcode is the tier-0 opcode dispatched when both the slice and element are
	// int-banked.
	DirectIntOpcode isa.Opcode
}

var (
	// CompoundSliceGetOps carries the read-half opcodes for compound typed-slice
	// assignments.
	CompoundSliceGetOps = CompoundSliceOps{
		DirectIntOpcode: isa.OpGetSliceInt64Element,
		Tier1SubOp:      typedSliceDirectGetTier1SubOp,
		PerKind: [isa.NumRegisterKinds]isa.Opcode{
			isa.RegisterInt64:   isa.OpGetElementInt64,
			isa.RegisterFloat64: isa.OpGetElementFloat64,
			isa.RegisterString:  isa.OpGetElementString,
			isa.RegisterUint64:  isa.OpGetElementUint64,
			isa.RegisterBool:    isa.OpGetElementBool,
		},
	}

	// CompoundSliceSetOps carries the write-half opcodes for compound typed-slice
	// assignments.
	CompoundSliceSetOps = CompoundSliceOps{
		DirectIntOpcode: isa.OpSetSliceInt64Element,
		Tier1SubOp:      typedSliceDirectSetTier1SubOp,
		PerKind: [isa.NumRegisterKinds]isa.Opcode{
			isa.RegisterInt64:   isa.OpSetElementInt64,
			isa.RegisterFloat64: isa.OpSetElementFloat64,
			isa.RegisterString:  isa.OpSetElementString,
			isa.RegisterUint64:  isa.OpSetElementUint64,
			isa.RegisterBool:    isa.OpSetElementBool,
		},
	}
)

// structFieldSliceSubOpPair groups the typed-slice struct-field tier-1 sub-ops for a
// single element-bank: the GET (read) sub-op, the SET (write) sub-op, and the typed-slice
// register bank the operand reads/writes. Indexed by element basic-kind in
// structFieldSliceSubOpByElemKind.
type structFieldSliceSubOpPair struct {
	// get is the read-side tier-1 sub-op for the slice element bank.
	get isa.SubOpcode

	// set is the write-side tier-1 sub-op for the slice element bank.
	set isa.SubOpcode

	// bank is the typed-slice register bank read or written by the sub-ops.
	bank isa.RegisterKind
}

// TypedMakeSliceSubOp maps a typed-slice register kind to the matching
// subOpMakeSlice<Kind> opcode.
//
// Takes kind (isa.RegisterKind) which is the typed-slice bank.
//
// Returns the matching sub-op and true, or (0, false) for unsupported kinds.
func TypedMakeSliceSubOp(kind isa.RegisterKind) (isa.SubOpcode, bool) {
	switch kind {
	case isa.RegisterSliceInt64:
		return isa.SubOpMakeSliceInt64, true
	case isa.RegisterSliceFloat64:
		return isa.SubOpMakeSliceFloat64, true
	case isa.RegisterSliceString:
		return isa.SubOpMakeSliceString, true
	case isa.RegisterSliceBool:
		return isa.SubOpMakeSliceBool, true
	case isa.RegisterSliceUint64:
		return isa.SubOpMakeSliceUint64, true
	case isa.RegisterSliceByte:
		return isa.SubOpMakeSliceByte, true
	default:
	}
	return 0, false
}

// TypedSliceDirectLenSubOp maps a typed-slice register kind to the matching
// SubOpLenSlice<Kind> opcode.
//
// Takes kind (isa.RegisterKind) which is the typed-slice bank.
//
// Returns the matching sub-op and true, or (0, false) for unsupported kinds.
func TypedSliceDirectLenSubOp(kind isa.RegisterKind) (isa.SubOpcode, bool) {
	switch kind {
	case isa.RegisterSliceInt64:
		return isa.SubOpLengthSliceInt64, true
	case isa.RegisterSliceFloat64:
		return isa.SubOpLengthSliceFloat64, true
	case isa.RegisterSliceString:
		return isa.SubOpLengthSliceString, true
	case isa.RegisterSliceBool:
		return isa.SubOpLengthSliceBool, true
	case isa.RegisterSliceUint64:
		return isa.SubOpLengthSliceUint64, true
	case isa.RegisterSliceByte:
		return isa.SubOpLengthSliceByte, true
	default:
	}
	return 0, false
}

// TypedSliceDirectCapSubOp maps a typed-slice register kind to the matching
// SubOpCapSlice<Kind> opcode.
//
// Takes kind (isa.RegisterKind) which is the typed-slice bank.
//
// Returns the matching sub-op and true, or (0, false) for unsupported kinds.
func TypedSliceDirectCapSubOp(kind isa.RegisterKind) (isa.SubOpcode, bool) {
	switch kind {
	case isa.RegisterSliceInt64:
		return isa.SubOpCapacitySliceInt64, true
	case isa.RegisterSliceFloat64:
		return isa.SubOpCapacitySliceFloat64, true
	case isa.RegisterSliceString:
		return isa.SubOpCapacitySliceString, true
	case isa.RegisterSliceBool:
		return isa.SubOpCapacitySliceBool, true
	case isa.RegisterSliceUint64:
		return isa.SubOpCapacitySliceUint64, true
	case isa.RegisterSliceByte:
		return isa.SubOpCapacitySliceByte, true
	default:
	}
	return 0, false
}

// typedSliceDirectGetTier1SubOp maps a typed-slice register kind to the matching tier-1
// SubOpGetSlice<Kind>Element opcode that reads an element from the typed bank without
// entering reflect.
//
// Takes kind (isa.RegisterKind) which is the typed-slice bank.
//
// Returns the matching tier-1 sub-op and true, or (0, false) when kind has no tier-1
// direct-get variant. isa.RegisterSliceInt64 uses a tier-0 opcode instead.
func typedSliceDirectGetTier1SubOp(kind isa.RegisterKind) (isa.SubOpcode, bool) {
	switch kind {
	case isa.RegisterSliceFloat64:
		return isa.SubOpGetSliceFloat64Element, true
	case isa.RegisterSliceString:
		return isa.SubOpGetSliceStringElement, true
	case isa.RegisterSliceBool:
		return isa.SubOpGetSliceBoolElement, true
	case isa.RegisterSliceUint64:
		return isa.SubOpGetSliceUint64Element, true
	case isa.RegisterSliceByte:
		return isa.SubOpGetSliceByteElement, true
	default:
	}
	return 0, false
}

// typedSliceDirectSetTier1SubOp maps a typed-slice register kind to the matching tier-1
// SubOpSetSlice<Kind>Element opcode that writes an element to the typed bank without
// entering reflect.
//
// Takes kind (isa.RegisterKind) which is the typed-slice bank.
//
// Returns the matching tier-1 sub-op and true, or (0, false) when kind has no tier-1
// direct-set variant. isa.RegisterSliceInt64 uses a tier-0 opcode instead.
func typedSliceDirectSetTier1SubOp(kind isa.RegisterKind) (isa.SubOpcode, bool) {
	switch kind {
	case isa.RegisterSliceFloat64:
		return isa.SubOpSetSliceFloat64Element, true
	case isa.RegisterSliceString:
		return isa.SubOpSetSliceStringElement, true
	case isa.RegisterSliceBool:
		return isa.SubOpSetSliceBoolElement, true
	case isa.RegisterSliceUint64:
		return isa.SubOpSetSliceUint64Element, true
	case isa.RegisterSliceByte:
		return isa.SubOpSetSliceByteElement, true
	default:
	}
	return 0, false
}

// PickGetStructFieldUnsafeSubOp returns the read-side sub-opcode for the given leaf
// register kind.
//
// Takes kind (isa.RegisterKind) which is the leaf field's register kind.
//
// Returns the matching sub-op and true, or (0, false) for unsupported kinds.
func PickGetStructFieldUnsafeSubOp(kind isa.RegisterKind) (isa.SubOpcode, bool) {
	switch kind {
	case isa.RegisterInt64:
		return isa.SubOpGetStructFieldInt64Wide, true
	case isa.RegisterUint64:
		return isa.SubOpGetStructFieldUint64Wide, true
	case isa.RegisterFloat64:
		return isa.SubOpGetStructFieldFloat64Wide, true
	case isa.RegisterBool:
		return isa.SubOpGetStructFieldBoolWide, true
	case isa.RegisterString:
		return isa.SubOpGetStructFieldStringWide, true
	default:
	}
	return 0, false
}

// PickSetStructFieldUnsafeSubOp returns the write-side sub-opcode for the given leaf
// register kind.
//
// Takes kind (isa.RegisterKind) which is the leaf field's register kind.
//
// Returns the matching sub-op and true, or (0, false) for unsupported kinds.
func PickSetStructFieldUnsafeSubOp(kind isa.RegisterKind) (isa.SubOpcode, bool) {
	switch kind {
	case isa.RegisterInt64:
		return isa.SubOpSetStructFieldInt64Wide, true
	case isa.RegisterUint64:
		return isa.SubOpSetStructFieldUint64Wide, true
	case isa.RegisterFloat64:
		return isa.SubOpSetStructFieldFloat64Wide, true
	case isa.RegisterBool:
		return isa.SubOpSetStructFieldBoolWide, true
	case isa.RegisterString:
		return isa.SubOpSetStructFieldStringWide, true
	default:
	}
	return 0, false
}

// PickGetStructFieldSliceSubOp returns the read-side typed-slice sub-op for a slice-typed
// struct field whose element kind aligns with a typed-slice bank's canonical width.
//
// Takes fieldType (types.Type) which is the field's static slice type.
//
// Returns the sub-opcode, the destination bank's isa.RegisterKind, and true on a match,
// or (0, isa.RegisterGeneral, false) otherwise.
func PickGetStructFieldSliceSubOp(fieldType types.Type) (isa.SubOpcode, isa.RegisterKind, bool) {
	pair, ok := lookupStructFieldSliceSubOpPair(fieldType)
	if !ok {
		return 0, isa.RegisterGeneral, false
	}
	return pair.get, pair.bank, true
}

// PickSetStructFieldSliceSubOp returns the write-side tier-1 sub-op for a slice-typed
// struct field. Same canonical-width gate as PickGetStructFieldSliceSubOp.
//
// Takes fieldType (types.Type) which is the field's static slice type.
//
// Returns the sub-opcode, the source bank's isa.RegisterKind, and true on a typed-slice
// match, or (0, isa.RegisterGeneral, false) otherwise.
func PickSetStructFieldSliceSubOp(fieldType types.Type) (isa.SubOpcode, isa.RegisterKind, bool) {
	pair, ok := lookupStructFieldSliceSubOpPair(fieldType)
	if !ok {
		return 0, isa.RegisterGeneral, false
	}
	return pair.set, pair.bank, true
}

// PickGetStructFieldTier0Op returns the read-side tier-0 opcode for the given leaf
// register kind. String reads have no tier-0 variant.
//
// Takes kind (isa.RegisterKind) which is the leaf field's register kind.
//
// Returns the matching opcode and true, or (0, false) for unsupported kinds.
func PickGetStructFieldTier0Op(kind isa.RegisterKind) (isa.Opcode, bool) {
	switch kind {
	case isa.RegisterInt64:
		return isa.OpGetStructFieldInt64, true
	case isa.RegisterUint64:
		return isa.OpGetStructFieldUint64, true
	case isa.RegisterFloat64:
		return isa.OpGetStructFieldFloat64, true
	case isa.RegisterBool:
		return isa.OpGetStructFieldBool, true
	case isa.RegisterGeneral:
		return isa.OpGetStructFieldGeneral, true
	default:
	}
	return 0, false
}

// PickSetStructFieldTier0Op returns the write-side tier-0 opcode for the given leaf
// register kind. String writes have no tier-0 variant.
//
// Takes kind (isa.RegisterKind) which is the leaf field's register kind.
//
// Returns the matching opcode and true, or (0, false) for unsupported kinds.
func PickSetStructFieldTier0Op(kind isa.RegisterKind) (isa.Opcode, bool) {
	switch kind {
	case isa.RegisterInt64:
		return isa.OpSetStructFieldInt64, true
	case isa.RegisterUint64:
		return isa.OpSetStructFieldUint64, true
	case isa.RegisterFloat64:
		return isa.OpSetStructFieldFloat64, true
	case isa.RegisterBool:
		return isa.OpSetStructFieldBool, true
	case isa.RegisterGeneral:
		return isa.OpSetStructFieldGeneral, true
	default:
	}
	return 0, false
}

// RegisterKindForReflectKind maps a reflect.Kind to the matching isa.RegisterKind for the
// fast-path sub-op selection.
//
// Takes kind (reflect.Kind) which is the leaf field's kind.
//
// Returns the matching isa.RegisterKind, or isa.RegisterGeneral when no typed bank
// applies.
func RegisterKindForReflectKind(kind reflect.Kind) isa.RegisterKind {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return isa.RegisterInt64
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return isa.RegisterUint64
	case reflect.Float32, reflect.Float64:
		return isa.RegisterFloat64
	case reflect.Bool:
		return isa.RegisterBool
	case reflect.String:
		return isa.RegisterString
	default:
	}
	return isa.RegisterGeneral
}

// TypedDirectAppendSubOp returns the tier-1 sub-opcode that appends a same-bank element
// to a typed-slice-bank source slice.
//
// Takes bank (isa.RegisterKind) which is the source slice's typed bank.
//
// Returns the direct sub-op and true, or (0, false) for unsupported kinds.
func TypedDirectAppendSubOp(bank isa.RegisterKind) (isa.SubOpcode, bool) {
	switch bank {
	case isa.RegisterSliceInt64:
		return isa.SubOpAppendSliceInt64, true
	case isa.RegisterSliceFloat64:
		return isa.SubOpAppendSliceFloat64, true
	case isa.RegisterSliceString:
		return isa.SubOpAppendSliceString, true
	case isa.RegisterSliceBool:
		return isa.SubOpAppendSliceBool, true
	case isa.RegisterSliceUint64:
		return isa.SubOpAppendSliceUint64, true
	case isa.RegisterSliceByte:
		return isa.SubOpAppendSliceByte, true
	default:
	}
	return 0, false
}

// TypedSliceDirectCopySubOp maps a typed-slice register kind to its matching
// subOpCopySliceXDirect sub-op.
//
// Takes kind (isa.RegisterKind) which is the typed-slice bank.
//
// Returns the matching sub-op and true, or (0, false) for unsupported kinds.
func TypedSliceDirectCopySubOp(kind isa.RegisterKind) (isa.SubOpcode, bool) {
	switch kind {
	case isa.RegisterSliceInt64:
		return isa.SubOpCopySliceInt64, true
	case isa.RegisterSliceFloat64:
		return isa.SubOpCopySliceFloat64, true
	case isa.RegisterSliceString:
		return isa.SubOpCopySliceString, true
	case isa.RegisterSliceBool:
		return isa.SubOpCopySliceBool, true
	case isa.RegisterSliceUint64:
		return isa.SubOpCopySliceUint64, true
	case isa.RegisterSliceByte:
		return isa.SubOpCopySliceByte, true
	default:
	}
	return 0, false
}

// TypedSliceDirectSliceSliceSubOp maps a typed-slice register kind to the matching tier-1
// sub-slice opcode.
//
// Takes kind (isa.RegisterKind) which is the typed-slice bank.
//
// Returns the direct sub-op and true, or (0, false) for unsupported kinds.
func TypedSliceDirectSliceSliceSubOp(kind isa.RegisterKind) (isa.SubOpcode, bool) {
	switch kind {
	case isa.RegisterSliceInt64:
		return isa.SubOpSliceSliceInt64, true
	case isa.RegisterSliceFloat64:
		return isa.SubOpSliceSliceFloat64, true
	case isa.RegisterSliceString:
		return isa.SubOpSliceSliceString, true
	case isa.RegisterSliceBool:
		return isa.SubOpSliceSliceBool, true
	case isa.RegisterSliceUint64:
		return isa.SubOpSliceSliceUint64, true
	case isa.RegisterSliceByte:
		return isa.SubOpSliceSliceByte, true
	default:
	}
	return 0, false
}

// PickSliceIndexStructFieldStoreOp returns the fused opcode that stores into a field of a
// slice element, for the leaf register kinds that have one.
//
// Takes kind (isa.RegisterKind) which is the leaf field's register kind.
//
// Returns isa.Opcode which is the fused store opcode, or zero.
// Returns bool which is true when a fused store exists for kind.
func PickSliceIndexStructFieldStoreOp(kind isa.RegisterKind) (isa.Opcode, bool) {
	switch kind {
	case isa.RegisterInt64:
		return isa.OpSetElementFieldInt64, true
	case isa.RegisterFloat64:
		return isa.OpSetElementFieldFloat64, true
	default:
	}
	return 0, false
}

// PickSliceIndexStructFieldOp returns the fused opSliceIndexStructFieldXxx opcode
// matching the leaf register kind.
//
// Takes kind (isa.RegisterKind) which is the leaf field's register kind.
//
// Returns the opcode and true, or (0, false) for unsupported kinds.
func PickSliceIndexStructFieldOp(kind isa.RegisterKind) (isa.Opcode, bool) {
	switch kind {
	case isa.RegisterInt64:
		return isa.OpGetElementFieldInt64, true
	case isa.RegisterUint64:
		return isa.OpGetElementFieldUint64, true
	case isa.RegisterFloat64:
		return isa.OpGetElementFieldFloat64, true
	case isa.RegisterBool:
		return isa.OpGetElementFieldBool, true
	case isa.RegisterString:
		return isa.OpGetElementFieldString, true
	default:
	}
	return 0, false
}

// StructFieldFastPathKindEnabled reports whether the given leaf register kind is admitted
// to the struct-field fast path.
//
// Takes kind (isa.RegisterKind) which is the leaf register kind.
//
// Returns true for int, uint, float, bool, string, and general banks.
func StructFieldFastPathKindEnabled(kind isa.RegisterKind) bool {
	switch kind {
	case isa.RegisterInt64, isa.RegisterUint64, isa.RegisterFloat64, isa.RegisterBool, isa.RegisterString,
		isa.RegisterGeneral:
		return true
	default:
	}
	return false
}

// PickIncDecStructFieldSubOp selects the fused inc/dec sub-opcode for the given field
// kind and INC/DEC token. Returns 0 when no fused variant exists.
//
// Takes fieldKind (isa.RegisterKind) which is the field's register kind.
// Takes operatorToken (token.Token) which is the INC or DEC token.
//
// Returns the chosen sub-opcode, or 0 when there is no fused variant.
func PickIncDecStructFieldSubOp(fieldKind isa.RegisterKind, operatorToken token.Token) isa.SubOpcode {
	switch {
	case fieldKind == isa.RegisterInt64 && operatorToken == token.INC:
		return isa.SubOpIncrementStructFieldInt64
	case fieldKind == isa.RegisterInt64 && operatorToken == token.DEC:
		return isa.SubOpDecrementStructFieldInt64
	case fieldKind == isa.RegisterUint64 && operatorToken == token.INC:
		return isa.SubOpIncrementStructFieldUint64
	case fieldKind == isa.RegisterUint64 && operatorToken == token.DEC:
		return isa.SubOpDecrementStructFieldUint64
	}
	return 0
}

// ScalarCrossBankSubOp returns the cross-bank scalar conversion sub-op.
//
// Takes sourceKind (isa.RegisterKind) which is the source bank.
// Takes destKind (isa.RegisterKind) which is the destination bank.
//
// Returns the conversion sub-op and true, or (0, false) when no direct sub-op covers the
// pair.
func ScalarCrossBankSubOp(sourceKind, destKind isa.RegisterKind) (isa.SubOpcode, bool) {
	switch sourceKind {
	case isa.RegisterInt64:
		switch destKind {
		case isa.RegisterFloat64:
			return isa.SubOpConvertInt64ToFloat64, true
		case isa.RegisterUint64:
			return isa.SubOpConvertInt64ToUint64, true
		case isa.RegisterBool:
			return isa.SubOpConvertInt64ToBool, true
		default:
		}
	case isa.RegisterFloat64:
		switch destKind {
		case isa.RegisterInt64:
			return isa.SubOpConvertFloat64ToInt64, true
		case isa.RegisterUint64:
			return isa.SubOpConvertFloat64ToUint64, true
		default:
		}
	case isa.RegisterUint64:
		switch destKind {
		case isa.RegisterInt64:
			return isa.SubOpConvertUint64ToInt64, true
		case isa.RegisterFloat64:
			return isa.SubOpConvertUint64ToFloat64, true
		default:
		}
	case isa.RegisterBool:
		if destKind == isa.RegisterInt64 {
			return isa.SubOpConvertBoolToInt64, true
		}
	default:
	}
	return 0, false
}

// StructFieldFastPathWriteKindEnabled reports whether the given value register kind is
// enabled for the struct-field write fast path.
//
// Takes kind (isa.RegisterKind) which is the value register kind being written.
//
// Returns true for int, uint, float, bool, string, and general banks.
func StructFieldFastPathWriteKindEnabled(kind isa.RegisterKind) bool {
	switch kind {
	case isa.RegisterInt64, isa.RegisterUint64, isa.RegisterFloat64, isa.RegisterBool, isa.RegisterString, isa.RegisterGeneral:
		return true
	default:
	}
	return false
}

// ResolveArithOpcode picks the bank-specific opcode and result bank for an arithmetic
// operation.
//
// Takes intOp (isa.Opcode) which is the integer-bank opcode.
// Takes floatOp (isa.Opcode) which is the float-bank opcode, or zero for unsupported.
// Takes strOp (isa.Opcode) which is the string-bank opcode, or zero for unsupported.
// Takes genOp (isa.Opcode) which is the general-bank opcode, or zero for unsupported.
// Takes leftKind (isa.RegisterKind) which is the left operand's bank.
//
// Returns the opcode, the result bank, and an error when the bank does not support the
// operation.
func ResolveArithOpcode(intOp, floatOp, strOp, genOp isa.Opcode, leftKind isa.RegisterKind) (isa.Opcode, isa.RegisterKind, error) {
	switch leftKind {
	case isa.RegisterInt64:
		return intOp, isa.RegisterInt64, nil
	case isa.RegisterFloat64:
		if floatOp == 0 {
			return 0, isa.RegisterFloat64, fault.ErrCompileArithFloatUnsupported
		}
		return floatOp, isa.RegisterFloat64, nil
	case isa.RegisterString:
		if strOp == 0 {
			return 0, isa.RegisterString, fault.ErrCompileArithStringUnsupported
		}
		return strOp, isa.RegisterString, nil
	case isa.RegisterUint64:
		uintOp, ok := intToUintArithOp(intOp)
		if !ok {
			return 0, isa.RegisterUint64, fault.ErrCompileArithUintUnsupported
		}
		return uintOp, isa.RegisterUint64, nil
	case isa.RegisterComplex128:
		complexOp, ok := intToComplexArithOp(intOp)
		if !ok {
			return 0, isa.RegisterComplex128, fault.ErrCompileArithComplexUnsupported
		}
		return complexOp, isa.RegisterComplex128, nil
	default:
		if genOp == 0 {
			return 0, isa.RegisterGeneral, fault.ErrCompileArithGeneralUnsupported
		}
		return genOp, isa.RegisterGeneral, nil
	}
}

// ArithmeticOpcodes returns the per-bank opcode candidates for an arithmetic operator,
// matching the table emitBinaryOp uses.
//
// Takes op (token.Token) which is one of + - * / %.
//
// Returns the int, float, string and general opcodes. Zero marks an unsupported bank.
func ArithmeticOpcodes(op token.Token) (intOp, floatOp, strOp, genOp isa.Opcode) {
	switch op {
	case token.ADD:
		return isa.OpAddInt64, isa.OpAddFloat64, isa.OpConcatenateString, isa.OpAddGeneral
	case token.SUB:
		return isa.OpSubtractInt64, isa.OpSubtractFloat64, 0, isa.OpSubtractGeneral
	case token.MUL:
		return isa.OpMultiplyInt64, isa.OpMultiplyFloat64, 0, isa.OpMultiplyGeneral
	case token.QUO:
		return isa.OpDivideInt64, isa.OpDivideFloat64, 0, isa.OpDivideGeneral
	case token.REM:
		return isa.OpRemainderInt64, 0, 0, isa.OpRemainderGeneral
	default:
		return 0, 0, 0, 0
	}
}

// lookupStructFieldSliceSubOpPair returns the typed-slice sub-op pair for a slice-typed
// struct field whose element kind aligns with a canonical typed-slice bank width.
//
// Only 64-bit element widths (int64, uint64, float64) plus string, bool and byte are
// admitted. Narrower widths fall through to the general-bank reflect path.
//
// Takes fieldType (types.Type) which is the struct field's static type.
//
// Returns structFieldSliceSubOpPair which carries get/set/bank on a match.
// Returns bool which is true when a typed-slice match exists.
func lookupStructFieldSliceSubOpPair(fieldType types.Type) (structFieldSliceSubOpPair, bool) {
	slice, ok := fieldType.Underlying().(*types.Slice)
	if !ok {
		return structFieldSliceSubOpPair{}, false
	}
	basic, ok := slice.Elem().Underlying().(*types.Basic)
	if !ok {
		return structFieldSliceSubOpPair{}, false
	}

	switch basic.Kind() {
	case types.Int, types.Int64:
		return structFieldSliceSubOpPair{get: isa.SubOpGetStructFieldSliceInt64, set: isa.SubOpSetStructFieldSliceInt64, bank: isa.RegisterSliceInt64}, true
	case types.Float64:
		return structFieldSliceSubOpPair{get: isa.SubOpGetStructFieldSliceFloat64, set: isa.SubOpSetStructFieldSliceFloat64, bank: isa.RegisterSliceFloat64}, true
	case types.Uint, types.Uint64:
		return structFieldSliceSubOpPair{get: isa.SubOpGetStructFieldSliceUint64, set: isa.SubOpSetStructFieldSliceUint64, bank: isa.RegisterSliceUint64}, true
	case types.String:
		return structFieldSliceSubOpPair{get: isa.SubOpGetStructFieldSliceString, set: isa.SubOpSetStructFieldSliceString, bank: isa.RegisterSliceString}, true
	case types.Bool:
		return structFieldSliceSubOpPair{get: isa.SubOpGetStructFieldSliceBool, set: isa.SubOpSetStructFieldSliceBool, bank: isa.RegisterSliceBool}, true
	case types.Uint8:
		return structFieldSliceSubOpPair{get: isa.SubOpGetStructFieldSliceByte, set: isa.SubOpSetStructFieldSliceByte, bank: isa.RegisterSliceByte}, true
	}
	return structFieldSliceSubOpPair{}, false
}

// intToUintArithOp maps an int arithmetic opcode to its uint counterpart, returning (0,
// false) when no mapping exists.
//
// Takes intOp (isa.Opcode) which is the int-bank arithmetic opcode to translate.
//
// Returns the matching uint opcode and true, or (0, false) when no mapping exists.
func intToUintArithOp(intOp isa.Opcode) (isa.Opcode, bool) {
	switch intOp {
	case isa.OpAddInt64:
		return isa.OpAddUint64, true
	case isa.OpSubtractInt64:
		return isa.OpSubtractUint64, true
	case isa.OpMultiplyInt64:
		return isa.OpMultiplyUint64, true
	case isa.OpDivideInt64:
		return isa.OpDivideUint64, true
	case isa.OpRemainderInt64:
		return isa.OpRemainderUint64, true
	default:
		return 0, false
	}
}

// intToComplexArithOp maps an int arithmetic opcode to its complex counterpart, returning
// (0, false) when no mapping exists.
//
// Takes intOp (isa.Opcode) which is the int-bank arithmetic opcode to translate.
//
// Returns the matching complex opcode and true, or (0, false) when no mapping exists.
func intToComplexArithOp(intOp isa.Opcode) (isa.Opcode, bool) {
	switch intOp {
	case isa.OpAddInt64:
		return isa.OpAddComplex128, true
	case isa.OpSubtractInt64:
		return isa.OpSubtractComplex128, true
	case isa.OpMultiplyInt64:
		return isa.OpMultiplyComplex128, true
	case isa.OpDivideInt64:
		return isa.OpDivideComplex128, true
	default:
		return 0, false
	}
}
