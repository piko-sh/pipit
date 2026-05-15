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
		DirectIntOpcode: isa.OpSliceGetIntDirect,
		Tier1SubOp:      typedSliceDirectGetTier1SubOp,
		PerKind: [isa.NumRegisterKinds]isa.Opcode{
			isa.RegisterInt:    isa.OpSliceGetInt,
			isa.RegisterFloat:  isa.OpSliceGetFloat,
			isa.RegisterString: isa.OpSliceGetString,
			isa.RegisterUint:   isa.OpSliceGetUint,
			isa.RegisterBool:   isa.OpSliceGetBool,
		},
	}

	// CompoundSliceSetOps carries the write-half opcodes for compound typed-slice
	// assignments.
	CompoundSliceSetOps = CompoundSliceOps{
		DirectIntOpcode: isa.OpSliceSetIntDirect,
		Tier1SubOp:      typedSliceDirectSetTier1SubOp,
		PerKind: [isa.NumRegisterKinds]isa.Opcode{
			isa.RegisterInt:    isa.OpSliceSetInt,
			isa.RegisterFloat:  isa.OpSliceSetFloat,
			isa.RegisterString: isa.OpSliceSetString,
			isa.RegisterUint:   isa.OpSliceSetUint,
			isa.RegisterBool:   isa.OpSliceSetBool,
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
	case isa.RegisterSliceInt:
		return isa.SubOpMakeSliceInt, true
	case isa.RegisterSliceFloat:
		return isa.SubOpMakeSliceFloat, true
	case isa.RegisterSliceString:
		return isa.SubOpMakeSliceString, true
	case isa.RegisterSliceBool:
		return isa.SubOpMakeSliceBool, true
	case isa.RegisterSliceUint:
		return isa.SubOpMakeSliceUint, true
	case isa.RegisterSliceByte:
		return isa.SubOpMakeSliceByte, true
	default:
	}
	return 0, false
}

// TypedSliceDirectLenSubOp maps a typed-slice register kind to the matching
// subOpLenSlice<Kind>Direct opcode.
//
// Takes kind (isa.RegisterKind) which is the typed-slice bank.
//
// Returns the matching sub-op and true, or (0, false) for unsupported kinds.
func TypedSliceDirectLenSubOp(kind isa.RegisterKind) (isa.SubOpcode, bool) {
	switch kind {
	case isa.RegisterSliceInt:
		return isa.SubOpLenSliceIntDirect, true
	case isa.RegisterSliceFloat:
		return isa.SubOpLenSliceFloatDirect, true
	case isa.RegisterSliceString:
		return isa.SubOpLenSliceStringDirect, true
	case isa.RegisterSliceBool:
		return isa.SubOpLenSliceBoolDirect, true
	case isa.RegisterSliceUint:
		return isa.SubOpLenSliceUintDirect, true
	case isa.RegisterSliceByte:
		return isa.SubOpLenSliceByteDirect, true
	default:
	}
	return 0, false
}

// TypedSliceDirectCapSubOp maps a typed-slice register kind to the matching
// subOpCapSlice<Kind>Direct opcode.
//
// Takes kind (isa.RegisterKind) which is the typed-slice bank.
//
// Returns the matching sub-op and true, or (0, false) for unsupported kinds.
func TypedSliceDirectCapSubOp(kind isa.RegisterKind) (isa.SubOpcode, bool) {
	switch kind {
	case isa.RegisterSliceInt:
		return isa.SubOpCapSliceIntDirect, true
	case isa.RegisterSliceFloat:
		return isa.SubOpCapSliceFloatDirect, true
	case isa.RegisterSliceString:
		return isa.SubOpCapSliceStringDirect, true
	case isa.RegisterSliceBool:
		return isa.SubOpCapSliceBoolDirect, true
	case isa.RegisterSliceUint:
		return isa.SubOpCapSliceUintDirect, true
	case isa.RegisterSliceByte:
		return isa.SubOpCapSliceByteDirect, true
	default:
	}
	return 0, false
}

// typedSliceDirectGetTier1SubOp maps a typed-slice register kind to the matching tier-1
// subOpSliceGet<Kind>Direct opcode that reads an element from the typed bank without
// entering reflect.
//
// Takes kind (isa.RegisterKind) which is the typed-slice bank.
//
// Returns the matching tier-1 sub-op and true, or (0, false) when kind has no tier-1
// direct-get variant. isa.RegisterSliceInt uses a tier-0 opcode instead.
func typedSliceDirectGetTier1SubOp(kind isa.RegisterKind) (isa.SubOpcode, bool) {
	switch kind {
	case isa.RegisterSliceFloat:
		return isa.SubOpSliceGetFloatDirect, true
	case isa.RegisterSliceString:
		return isa.SubOpSliceGetStringDirect, true
	case isa.RegisterSliceBool:
		return isa.SubOpSliceGetBoolDirect, true
	case isa.RegisterSliceUint:
		return isa.SubOpSliceGetUintDirect, true
	case isa.RegisterSliceByte:
		return isa.SubOpSliceGetByteDirect, true
	default:
	}
	return 0, false
}

// typedSliceDirectSetTier1SubOp maps a typed-slice register kind to the matching tier-1
// subOpSliceSet<Kind>Direct opcode that writes an element to the typed bank without
// entering reflect.
//
// Takes kind (isa.RegisterKind) which is the typed-slice bank.
//
// Returns the matching tier-1 sub-op and true, or (0, false) when kind has no tier-1
// direct-set variant. isa.RegisterSliceInt uses a tier-0 opcode instead.
func typedSliceDirectSetTier1SubOp(kind isa.RegisterKind) (isa.SubOpcode, bool) {
	switch kind {
	case isa.RegisterSliceFloat:
		return isa.SubOpSliceSetFloatDirect, true
	case isa.RegisterSliceString:
		return isa.SubOpSliceSetStringDirect, true
	case isa.RegisterSliceBool:
		return isa.SubOpSliceSetBoolDirect, true
	case isa.RegisterSliceUint:
		return isa.SubOpSliceSetUintDirect, true
	case isa.RegisterSliceByte:
		return isa.SubOpSliceSetByteDirect, true
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
	case isa.RegisterInt:
		return isa.SubOpGetStructFieldInt, true
	case isa.RegisterUint:
		return isa.SubOpGetStructFieldUint, true
	case isa.RegisterFloat:
		return isa.SubOpGetStructFieldFloat, true
	case isa.RegisterBool:
		return isa.SubOpGetStructFieldBool, true
	case isa.RegisterString:
		return isa.SubOpGetStructFieldString, true
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
	case isa.RegisterInt:
		return isa.SubOpSetStructFieldInt, true
	case isa.RegisterUint:
		return isa.SubOpSetStructFieldUint, true
	case isa.RegisterFloat:
		return isa.SubOpSetStructFieldFloat, true
	case isa.RegisterBool:
		return isa.SubOpSetStructFieldBool, true
	case isa.RegisterString:
		return isa.SubOpSetStructFieldString, true
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
	case isa.RegisterInt:
		return isa.OpGetStructFieldIntT0, true
	case isa.RegisterUint:
		return isa.OpGetStructFieldUint, true
	case isa.RegisterFloat:
		return isa.OpGetStructFieldFloat, true
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
	case isa.RegisterInt:
		return isa.OpSetStructFieldIntT0, true
	case isa.RegisterUint:
		return isa.OpSetStructFieldUint, true
	case isa.RegisterFloat:
		return isa.OpSetStructFieldFloat, true
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
		return isa.RegisterInt
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return isa.RegisterUint
	case reflect.Float32, reflect.Float64:
		return isa.RegisterFloat
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
	case isa.RegisterSliceInt:
		return isa.SubOpAppendSliceIntDirect, true
	case isa.RegisterSliceFloat:
		return isa.SubOpAppendSliceFloatDirect, true
	case isa.RegisterSliceString:
		return isa.SubOpAppendSliceStringDirect, true
	case isa.RegisterSliceBool:
		return isa.SubOpAppendSliceBoolDirect, true
	case isa.RegisterSliceUint:
		return isa.SubOpAppendSliceUintDirect, true
	case isa.RegisterSliceByte:
		return isa.SubOpAppendSliceByteDirect, true
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
	case isa.RegisterSliceInt:
		return isa.SubOpCopySliceIntDirect, true
	case isa.RegisterSliceFloat:
		return isa.SubOpCopySliceFloatDirect, true
	case isa.RegisterSliceString:
		return isa.SubOpCopySliceStringDirect, true
	case isa.RegisterSliceBool:
		return isa.SubOpCopySliceBoolDirect, true
	case isa.RegisterSliceUint:
		return isa.SubOpCopySliceUintDirect, true
	case isa.RegisterSliceByte:
		return isa.SubOpCopySliceByteDirect, true
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
	case isa.RegisterSliceInt:
		return isa.SubOpSliceSliceIntDirect, true
	case isa.RegisterSliceFloat:
		return isa.SubOpSliceSliceFloatDirect, true
	case isa.RegisterSliceString:
		return isa.SubOpSliceSliceStringDirect, true
	case isa.RegisterSliceBool:
		return isa.SubOpSliceSliceBoolDirect, true
	case isa.RegisterSliceUint:
		return isa.SubOpSliceSliceUintDirect, true
	case isa.RegisterSliceByte:
		return isa.SubOpSliceByteSlice, true
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
	case isa.RegisterInt:
		return isa.OpSliceIndexStructFieldInt, true
	case isa.RegisterUint:
		return isa.OpSliceIndexStructFieldUint, true
	case isa.RegisterFloat:
		return isa.OpSliceIndexStructFieldFloat, true
	case isa.RegisterBool:
		return isa.OpSliceIndexStructFieldBool, true
	case isa.RegisterString:
		return isa.OpSliceIndexStructFieldString, true
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
	case isa.RegisterInt, isa.RegisterUint, isa.RegisterFloat, isa.RegisterBool, isa.RegisterString,
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
	case fieldKind == isa.RegisterInt && operatorToken == token.INC:
		return isa.SubOpIncStructFieldInt
	case fieldKind == isa.RegisterInt && operatorToken == token.DEC:
		return isa.SubOpDecStructFieldInt
	case fieldKind == isa.RegisterUint && operatorToken == token.INC:
		return isa.SubOpIncStructFieldUint
	case fieldKind == isa.RegisterUint && operatorToken == token.DEC:
		return isa.SubOpDecStructFieldUint
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
	case isa.RegisterInt:
		switch destKind {
		case isa.RegisterFloat:
			return isa.SubOpIntToFloat, true
		case isa.RegisterUint:
			return isa.SubOpIntToUint, true
		case isa.RegisterBool:
			return isa.SubOpIntToBool, true
		default:
		}
	case isa.RegisterFloat:
		switch destKind {
		case isa.RegisterInt:
			return isa.SubOpFloatToInt, true
		case isa.RegisterUint:
			return isa.SubOpFloatToUint, true
		default:
		}
	case isa.RegisterUint:
		switch destKind {
		case isa.RegisterInt:
			return isa.SubOpUintToInt, true
		case isa.RegisterFloat:
			return isa.SubOpUintToFloat, true
		default:
		}
	case isa.RegisterBool:
		if destKind == isa.RegisterInt {
			return isa.SubOpBoolToInt, true
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
	case isa.RegisterInt, isa.RegisterUint, isa.RegisterFloat, isa.RegisterBool, isa.RegisterString, isa.RegisterGeneral:
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
	case isa.RegisterInt:
		return intOp, isa.RegisterInt, nil
	case isa.RegisterFloat:
		if floatOp == 0 {
			return 0, isa.RegisterFloat, fault.ErrCompileArithFloatUnsupported
		}
		return floatOp, isa.RegisterFloat, nil
	case isa.RegisterString:
		if strOp == 0 {
			return 0, isa.RegisterString, fault.ErrCompileArithStringUnsupported
		}
		return strOp, isa.RegisterString, nil
	case isa.RegisterUint:
		uintOp, ok := intToUintArithOp(intOp)
		if !ok {
			return 0, isa.RegisterUint, fault.ErrCompileArithUintUnsupported
		}
		return uintOp, isa.RegisterUint, nil
	case isa.RegisterComplex:
		complexOp, ok := intToComplexArithOp(intOp)
		if !ok {
			return 0, isa.RegisterComplex, fault.ErrCompileArithComplexUnsupported
		}
		return complexOp, isa.RegisterComplex, nil
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
		return isa.OpAddInt, isa.OpAddFloat, isa.OpConcatString, isa.OpAdd
	case token.SUB:
		return isa.OpSubInt, isa.OpSubFloat, 0, isa.OpSub
	case token.MUL:
		return isa.OpMulInt, isa.OpMulFloat, 0, isa.OpMul
	case token.QUO:
		return isa.OpDivInt, isa.OpDivFloat, 0, isa.OpDiv
	case token.REM:
		return isa.OpRemInt, 0, 0, isa.OpRem
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
		return structFieldSliceSubOpPair{get: isa.SubOpGetStructFieldSliceInt, set: isa.SubOpSetStructFieldSliceInt, bank: isa.RegisterSliceInt}, true
	case types.Float64:
		return structFieldSliceSubOpPair{get: isa.SubOpGetStructFieldSliceFloat, set: isa.SubOpSetStructFieldSliceFloat, bank: isa.RegisterSliceFloat}, true
	case types.Uint, types.Uint64:
		return structFieldSliceSubOpPair{get: isa.SubOpGetStructFieldSliceUint, set: isa.SubOpSetStructFieldSliceUint, bank: isa.RegisterSliceUint}, true
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
// Takes intOp which is the int-bank arithmetic opcode to translate.
//
// Returns the matching uint opcode and true, or (0, false) when no mapping exists.
func intToUintArithOp(intOp isa.Opcode) (isa.Opcode, bool) {
	switch intOp {
	case isa.OpAddInt:
		return isa.OpAddUint, true
	case isa.OpSubInt:
		return isa.OpSubUint, true
	case isa.OpMulInt:
		return isa.OpMulUint, true
	case isa.OpDivInt:
		return isa.OpDivUint, true
	case isa.OpRemInt:
		return isa.OpRemUint, true
	default:
		return 0, false
	}
}

// intToComplexArithOp maps an int arithmetic opcode to its complex counterpart, returning
// (0, false) when no mapping exists.
//
// Takes intOp which is the int-bank arithmetic opcode to translate.
//
// Returns the matching complex opcode and true, or (0, false) when no mapping exists.
func intToComplexArithOp(intOp isa.Opcode) (isa.Opcode, bool) {
	switch intOp {
	case isa.OpAddInt:
		return isa.OpAddComplex, true
	case isa.OpSubInt:
		return isa.OpSubComplex, true
	case isa.OpMulInt:
		return isa.OpMulComplex, true
	case isa.OpDivInt:
		return isa.OpDivComplex, true
	default:
		return 0, false
	}
}
