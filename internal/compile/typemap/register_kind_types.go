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

package typemap

import (
	"go/types"

	"pipit.sh/pipit/internal/isa"
)

// KindPromotionContext carries inputs for the typed-bank promotion gate. All fields are
// optional and the predicate is nil-safe.
type KindPromotionContext struct {
	// Substitutions maps generic TypeParams to concrete instantiation types. Nil for
	// non-generic or non-specialised compilations.
	Substitutions map[*types.TypeParam]types.Type

	// SubstitutionCache memoises previous substitutions so repeat lookups skip the
	// allocation.
	SubstitutionCache map[types.Type]types.Type

	// Disqualified is the candidate-name set produced by the escape gate. Nil or empty
	// disables the gate.
	Disqualified map[string]bool

	// BindingName is the source-level name being classified (parameter or local var name).
	BindingName string

	// CalleeParamPromotions, if non-nil, gates cross-function consistency so a caller cannot
	// promote a value passed into a parameter the callee has already published as
	// non-promoted. Indexed by CalleeParamIndex.
	CalleeParamPromotions []bool

	// CalleeParamIndex selects the index into CalleeParamPromotions to consult, when
	// CalleeParamPromotions is non-nil.
	CalleeParamIndex int
}

// KindForType determines the register kind for a given Go type at value-context sites
// (assignment RHS, expression results, etc.). Slices return isa.RegisterGeneral because
// the typed-slice banks are gated by separate entry points that validate origin and
// lifetime.
//
// Takes t (types.Type) which is the Go type to classify.
//
// Returns the register kind for the type's register bank.
func KindForType(t types.Type) isa.RegisterKind {
	t = t.Underlying()

	if basic, ok := t.(*types.Basic); ok {
		return KindForBasic(basic.Kind())
	}

	return isa.RegisterGeneral
}

// KindForCallSlot returns the register kind for a call-slot type, placing sub-word
// integer slices in their 64-bit sibling's bank because the typed-bank ABI stores values
// as int64/uint64 regardless of declared element width. Platform-int []int/[]uint are
// admitted because pipit targets 64-bit ABIs exclusively.
//
// Takes t (types.Type) which is the Go type of the call slot.
//
// Returns the register kind for the parameter or return slot.
func KindForCallSlot(t types.Type) isa.RegisterKind {
	slice, ok := types.Unalias(t).(*types.Slice)
	if !ok {
		return KindForType(t)
	}
	elementBasic, basicOk := types.Unalias(slice.Elem()).(*types.Basic)
	if !basicOk {
		return isa.RegisterGeneral
	}

	switch elementBasic.Kind() {
	case types.Int, types.Int64:
		return isa.RegisterSliceInt
	case types.Float64:
		return isa.RegisterSliceFloat
	case types.String:
		return isa.RegisterSliceString
	case types.Bool:
		return isa.RegisterSliceBool
	case types.Uint, types.Uint64:
		return isa.RegisterSliceUint
	case types.Uint8:
		return isa.RegisterSliceByte
	default:
	}

	return isa.RegisterGeneral
}

// KindForPromotedSlot is the unified typed-bank-promotion predicate. Returns the
// typed-slice bank and true when the type qualifies and no gate in ctx refuses it, or
// (isa.RegisterGeneral, false) otherwise.
//
// Takes t (types.Type) which is the Go type to classify.
// Takes ctx (*KindPromotionContext) which carries optional substitution map, disqualifier
// set, and cross-function escape verdicts. May be nil for a type-only verdict.
//
// Returns the register kind and whether typed-bank promotion fired.
func KindForPromotedSlot(t types.Type, ctx *KindPromotionContext) (isa.RegisterKind, bool) {
	if ctx != nil && ctx.Substitutions != nil {
		t = SubstituteType(t, ctx.Substitutions, ctx.SubstitutionCache)
	}
	typed := KindForCallSlot(t)
	if !isa.IsTypedSliceKind(typed) {
		return typed, false
	}
	if ctx != nil && ctx.BindingName != "" && ctx.Disqualified[ctx.BindingName] {
		return isa.RegisterGeneral, false
	}
	if ctx != nil && ctx.CalleeParamPromotions != nil {
		if ctx.CalleeParamIndex < 0 || ctx.CalleeParamIndex >= len(ctx.CalleeParamPromotions) {
			return typed, true
		}
		if !ctx.CalleeParamPromotions[ctx.CalleeParamIndex] {
			return isa.RegisterGeneral, false
		}
	}
	return typed, true
}

// KindForTypedSlice returns the typed slice bank for a slice type. Falls back to
// isa.RegisterGeneral when the type has no typed-storage bank.
//
// Takes t (types.Type) which is the Go type to classify. Non-slice types fall through to
// isa.RegisterGeneral.
//
// Returns the matching typed-slice register kind, or isa.RegisterGeneral when no typed
// bank applies.
func KindForTypedSlice(t types.Type) isa.RegisterKind {
	slice, ok := t.Underlying().(*types.Slice)
	if !ok {
		return isa.RegisterGeneral
	}

	if basic, ok := slice.Elem().Underlying().(*types.Basic); ok && basic.Kind() == types.Uint8 {
		return isa.RegisterSliceByte
	}
	elem, unnamedBasic := types.Unalias(slice.Elem()).(*types.Basic)
	if !unnamedBasic {
		return isa.RegisterGeneral
	}
	if typedSliceElementIsNarrow(elem.Kind()) {
		return isa.RegisterGeneral
	}
	switch KindForType(slice.Elem()) {
	case isa.RegisterInt:
		return isa.RegisterSliceInt
	case isa.RegisterFloat:
		return isa.RegisterSliceFloat
	case isa.RegisterString:
		return isa.RegisterSliceString
	case isa.RegisterBool:
		return isa.RegisterSliceBool
	case isa.RegisterUint:
		return isa.RegisterSliceUint
	default:
	}
	return isa.RegisterGeneral
}

// NarrowIntegerBitWidth returns the bit width of a narrow integer type, or 0 for
// full-width and non-integer types.
//
// Recognises int8/int16/int32 and uint8/uint16/uint32. The Compiler uses this to Emit
// isa.OpTruncateNarrow after arithmetic on narrow integer kinds so the register value
// matches Go's modular wrap semantics.
//
// Takes t (types.Type) which is the Go type to inspect.
//
// Returns the bit width when truncation is needed, or 0 otherwise.
func NarrowIntegerBitWidth(t types.Type) uint8 {
	if t == nil {
		return 0
	}
	basic, ok := t.Underlying().(*types.Basic)
	if !ok {
		return 0
	}
	switch basic.Kind() {
	case types.Int8, types.Uint8:
		return isa.NarrowIntegerBitWidth8
	case types.Int16, types.Uint16:
		return isa.NarrowIntegerBitWidth16
	case types.Int32, types.Uint32:
		return isa.NarrowIntegerBitWidth32
	default:
		return 0
	}
}

// KindForBasic maps a types.BasicKind to a isa.RegisterKind.
//
// Takes k (types.BasicKind) which is the basic type kind to map.
//
// Returns the corresponding register kind.
func KindForBasic(k types.BasicKind) isa.RegisterKind {
	switch k {
	case types.Bool, types.UntypedBool:
		return isa.RegisterBool

	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
		types.UntypedInt, types.UntypedRune:
		return isa.RegisterInt

	case types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64,
		types.Uintptr:
		return isa.RegisterUint

	case types.Float32, types.Float64, types.UntypedFloat:
		return isa.RegisterFloat

	case types.String, types.UntypedString:
		return isa.RegisterString

	case types.Complex64, types.Complex128, types.UntypedComplex:
		return isa.RegisterComplex

	default:
		return isa.RegisterGeneral
	}
}

// typedSliceElementIsNarrow reports whether a basic element kind is narrower than the
// typed banks' eight-byte elements.
//
// Takes kind (types.BasicKind) which is the element's basic kind.
//
// Returns bool which is true for the sized integer and float kinds below 64 bits.
func typedSliceElementIsNarrow(kind types.BasicKind) bool {
	switch kind {
	case types.Int8, types.Int16, types.Int32, types.Uint16, types.Uint32, types.Float32:
		return true
	default:
		return false
	}
}
