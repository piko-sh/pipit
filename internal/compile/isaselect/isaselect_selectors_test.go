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

package isaselect

import (
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

func TestScalarCrossBankSubOpCoversTheConvertiblePairsOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source isa.RegisterKind
		dest   isa.RegisterKind
		want   isa.SubOpcode
		wantOK bool
	}{
		{name: "an integer widened to a float", source: isa.RegisterInt64, dest: isa.RegisterFloat64, want: isa.SubOpConvertInt64ToFloat64, wantOK: true},
		{name: "an integer reinterpreted as unsigned", source: isa.RegisterInt64, dest: isa.RegisterUint64, want: isa.SubOpConvertInt64ToUint64, wantOK: true},
		{name: "an integer narrowed to a boolean", source: isa.RegisterInt64, dest: isa.RegisterBool, want: isa.SubOpConvertInt64ToBool, wantOK: true},
		{name: "a float truncated to an integer", source: isa.RegisterFloat64, dest: isa.RegisterInt64, want: isa.SubOpConvertFloat64ToInt64, wantOK: true},
		{name: "a float truncated to an unsigned integer", source: isa.RegisterFloat64, dest: isa.RegisterUint64, want: isa.SubOpConvertFloat64ToUint64, wantOK: true},
		{name: "an unsigned value reinterpreted as an integer", source: isa.RegisterUint64, dest: isa.RegisterInt64, want: isa.SubOpConvertUint64ToInt64, wantOK: true},
		{name: "an unsigned value widened to a float", source: isa.RegisterUint64, dest: isa.RegisterFloat64, want: isa.SubOpConvertUint64ToFloat64, wantOK: true},
		{name: "a boolean widened to an integer", source: isa.RegisterBool, dest: isa.RegisterInt64, want: isa.SubOpConvertBoolToInt64, wantOK: true},
		{name: "a boolean has no direct float form", source: isa.RegisterBool, dest: isa.RegisterFloat64, wantOK: false},
		{name: "a boolean has no direct unsigned form", source: isa.RegisterBool, dest: isa.RegisterUint64, wantOK: false},
		{name: "a float has no direct boolean form", source: isa.RegisterFloat64, dest: isa.RegisterBool, wantOK: false},
		{name: "an unsigned value has no direct boolean form", source: isa.RegisterUint64, dest: isa.RegisterBool, wantOK: false},
		{name: "a string is never a scalar cross-bank source", source: isa.RegisterString, dest: isa.RegisterInt64, wantOK: false},
		{name: "a string is never a scalar cross-bank destination", source: isa.RegisterInt64, dest: isa.RegisterString, wantOK: false},
		{name: "the general bank is never a scalar cross-bank source", source: isa.RegisterGeneral, dest: isa.RegisterInt64, wantOK: false},
		{name: "a bank never converts to itself", source: isa.RegisterInt64, dest: isa.RegisterInt64, wantOK: false},
		{name: "a typed slice bank has no scalar conversion", source: isa.RegisterSliceInt64, dest: isa.RegisterInt64, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ScalarCrossBankSubOp(tt.source, tt.dest)

			require.Equal(t, tt.wantOK, ok,
				"a pair without a single-word conversion has to be refused so the caller boxes instead")
			if !tt.wantOK {
				require.Zero(t, got, "a refused pair must not leave a usable sub-op behind")
				return
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestArithmeticOpcodesGivesTheBankCandidatesOfEachOperator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		operator  token.Token
		wantInt   isa.Opcode
		wantFloat isa.Opcode
		wantStr   isa.Opcode
		wantGen   isa.Opcode
	}{
		{name: "addition is the only operator with a string form", operator: token.ADD, wantInt: isa.OpAddInt64, wantFloat: isa.OpAddFloat64, wantStr: isa.OpConcatenateString, wantGen: isa.OpAddGeneral},
		{name: "subtraction has no string form", operator: token.SUB, wantInt: isa.OpSubtractInt64, wantFloat: isa.OpSubtractFloat64, wantStr: 0, wantGen: isa.OpSubtractGeneral},
		{name: "multiplication has no string form", operator: token.MUL, wantInt: isa.OpMultiplyInt64, wantFloat: isa.OpMultiplyFloat64, wantStr: 0, wantGen: isa.OpMultiplyGeneral},
		{name: "division has no string form", operator: token.QUO, wantInt: isa.OpDivideInt64, wantFloat: isa.OpDivideFloat64, wantStr: 0, wantGen: isa.OpDivideGeneral},
		{name: "remainder is integral only", operator: token.REM, wantInt: isa.OpRemainderInt64, wantFloat: 0, wantStr: 0, wantGen: isa.OpRemainderGeneral},
		{name: "a comparison is not an arithmetic operator", operator: token.EQL, wantInt: 0, wantFloat: 0, wantStr: 0, wantGen: 0},
		{name: "a bitwise operator is not an arithmetic operator", operator: token.AND, wantInt: 0, wantFloat: 0, wantStr: 0, wantGen: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			intOp, floatOp, strOp, genOp := ArithmeticOpcodes(tt.operator)

			require.Equal(t, tt.wantInt, intOp)
			require.Equal(t, tt.wantFloat, floatOp)
			require.Equal(t, tt.wantStr, strOp,
				"a zero here is what makes the resolver refuse the string bank for this operator")
			require.Equal(t, tt.wantGen, genOp)
		})
	}
}

func TestResolveArithOpcodeMapsTheIntegerFormOntoTheOperandBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		operator token.Token
		leftKind isa.RegisterKind
		want     isa.Opcode
		wantKind isa.RegisterKind
		wantErr  error
	}{
		{name: "addition of integers", operator: token.ADD, leftKind: isa.RegisterInt64, want: isa.OpAddInt64, wantKind: isa.RegisterInt64},
		{name: "addition of floats", operator: token.ADD, leftKind: isa.RegisterFloat64, want: isa.OpAddFloat64, wantKind: isa.RegisterFloat64},
		{name: "addition of strings", operator: token.ADD, leftKind: isa.RegisterString, want: isa.OpConcatenateString, wantKind: isa.RegisterString},
		{name: "addition of unsigned values", operator: token.ADD, leftKind: isa.RegisterUint64, want: isa.OpAddUint64, wantKind: isa.RegisterUint64},
		{name: "addition of complex values", operator: token.ADD, leftKind: isa.RegisterComplex128, want: isa.OpAddComplex128, wantKind: isa.RegisterComplex128},
		{name: "addition of boxed values", operator: token.ADD, leftKind: isa.RegisterGeneral, want: isa.OpAddGeneral, wantKind: isa.RegisterGeneral},
		{name: "subtraction of unsigned values", operator: token.SUB, leftKind: isa.RegisterUint64, want: isa.OpSubtractUint64, wantKind: isa.RegisterUint64},
		{name: "multiplication of unsigned values", operator: token.MUL, leftKind: isa.RegisterUint64, want: isa.OpMultiplyUint64, wantKind: isa.RegisterUint64},
		{name: "division of unsigned values", operator: token.QUO, leftKind: isa.RegisterUint64, want: isa.OpDivideUint64, wantKind: isa.RegisterUint64},
		{name: "remainder of unsigned values", operator: token.REM, leftKind: isa.RegisterUint64, want: isa.OpRemainderUint64, wantKind: isa.RegisterUint64},
		{name: "subtraction of complex values", operator: token.SUB, leftKind: isa.RegisterComplex128, want: isa.OpSubtractComplex128, wantKind: isa.RegisterComplex128},
		{name: "multiplication of complex values", operator: token.MUL, leftKind: isa.RegisterComplex128, want: isa.OpMultiplyComplex128, wantKind: isa.RegisterComplex128},
		{name: "division of complex values", operator: token.QUO, leftKind: isa.RegisterComplex128, want: isa.OpDivideComplex128, wantKind: isa.RegisterComplex128},
		{name: "a boolean operand falls through to the general bank", operator: token.ADD, leftKind: isa.RegisterBool, want: isa.OpAddGeneral, wantKind: isa.RegisterGeneral},
		{
			name:     "subtraction of strings has no form at all",
			operator: token.SUB,
			leftKind: isa.RegisterString,
			wantKind: isa.RegisterString,
			wantErr:  fault.ErrCompileArithStringUnsupported,
		},
		{
			name:     "a remainder of floats has no form",
			operator: token.REM,
			leftKind: isa.RegisterFloat64,
			wantKind: isa.RegisterFloat64,
			wantErr:  fault.ErrCompileArithFloatUnsupported,
		},
		{
			name:     "a remainder of complex values has no form",
			operator: token.REM,
			leftKind: isa.RegisterComplex128,
			wantKind: isa.RegisterComplex128,
			wantErr:  fault.ErrCompileArithComplexUnsupported,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			intOp, floatOp, strOp, genOp := ArithmeticOpcodes(tt.operator)

			got, gotKind, err := ResolveArithOpcode(intOp, floatOp, strOp, genOp, tt.leftKind)

			require.Equal(t, tt.wantKind, gotKind,
				"the result bank is reported even when the operator has no form for it")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Zero(t, got, "a refused bank must not leave a usable opcode behind")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestPlanningATypedSliceReadPrefersTheDirectForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		collectionKind isa.RegisterKind
		elementKind    isa.RegisterKind
		boundsSafe     bool
		wantOp         isa.Opcode
		wantTier1      isa.SubOpcode
		wantUseTier1   bool
		wantOK         bool
	}{
		{name: "an integer slice with a checked index", collectionKind: isa.RegisterSliceInt64, elementKind: isa.RegisterInt64, wantOp: isa.OpGetSliceInt64Element, wantOK: true},
		{name: "an integer slice with a proven index", collectionKind: isa.RegisterSliceInt64, elementKind: isa.RegisterInt64, boundsSafe: true, wantOp: isa.OpGetSliceInt64ElementUnchecked, wantOK: true},
		{name: "a float slice", collectionKind: isa.RegisterSliceFloat64, elementKind: isa.RegisterFloat64, wantTier1: isa.SubOpGetSliceFloat64Element, wantUseTier1: true, wantOK: true},
		{name: "a string slice", collectionKind: isa.RegisterSliceString, elementKind: isa.RegisterString, wantTier1: isa.SubOpGetSliceStringElement, wantUseTier1: true, wantOK: true},
		{name: "a boolean slice", collectionKind: isa.RegisterSliceBool, elementKind: isa.RegisterBool, wantTier1: isa.SubOpGetSliceBoolElement, wantUseTier1: true, wantOK: true},
		{name: "an unsigned slice", collectionKind: isa.RegisterSliceUint64, elementKind: isa.RegisterUint64, wantTier1: isa.SubOpGetSliceUint64Element, wantUseTier1: true, wantOK: true},
		{name: "a byte slice", collectionKind: isa.RegisterSliceByte, elementKind: isa.RegisterUint64, wantTier1: isa.SubOpGetSliceByteElement, wantUseTier1: true, wantOK: true},
		{name: "a boxed slice of integers", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterInt64, wantOp: isa.OpGetElementInt64, wantOK: true},
		{name: "a boxed slice of floats", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterFloat64, wantOp: isa.OpGetElementFloat64, wantOK: true},
		{name: "a boxed slice of strings", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterString, wantOp: isa.OpGetElementString, wantOK: true},
		{name: "a boxed slice of booleans", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterBool, wantOp: isa.OpGetElementBool, wantOK: true},
		{name: "a boxed slice of unsigned values", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterUint64, wantOp: isa.OpGetElementUint64, wantOK: true},
		{name: "a typed slice whose element bank disagrees falls back", collectionKind: isa.RegisterSliceFloat64, elementKind: isa.RegisterInt64, wantOp: isa.OpGetElementInt64, wantOK: true},
		{name: "an element bank with no typed read", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterGeneral, wantOK: false},
		{name: "a complex element has no typed read", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterComplex128, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan, ok := PlanSliceGet(tt.collectionKind, tt.elementKind, tt.boundsSafe)

			require.Equal(t, tt.wantOK, ok)
			if !tt.wantOK {
				return
			}
			require.Equal(t, tt.wantUseTier1, plan.UseTier1,
				"the direct typed form is a tier-1 sub-op, and everything else is a tier-0 opcode")
			require.Equal(t, tt.wantOp, plan.Op)
			require.Equal(t, tt.wantTier1, plan.Tier1)
		})
	}
}

func TestPlanningATypedSliceWriteMirrorsTheRead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		collectionKind isa.RegisterKind
		elementKind    isa.RegisterKind
		wantOp         isa.Opcode
		wantTier1      isa.SubOpcode
		wantUseTier1   bool
		wantOK         bool
	}{
		{name: "an integer slice", collectionKind: isa.RegisterSliceInt64, elementKind: isa.RegisterInt64, wantOp: isa.OpSetSliceInt64Element, wantOK: true},
		{name: "a float slice", collectionKind: isa.RegisterSliceFloat64, elementKind: isa.RegisterFloat64, wantTier1: isa.SubOpSetSliceFloat64Element, wantUseTier1: true, wantOK: true},
		{name: "a string slice", collectionKind: isa.RegisterSliceString, elementKind: isa.RegisterString, wantTier1: isa.SubOpSetSliceStringElement, wantUseTier1: true, wantOK: true},
		{name: "a boolean slice", collectionKind: isa.RegisterSliceBool, elementKind: isa.RegisterBool, wantTier1: isa.SubOpSetSliceBoolElement, wantUseTier1: true, wantOK: true},
		{name: "an unsigned slice", collectionKind: isa.RegisterSliceUint64, elementKind: isa.RegisterUint64, wantTier1: isa.SubOpSetSliceUint64Element, wantUseTier1: true, wantOK: true},
		{name: "a byte slice", collectionKind: isa.RegisterSliceByte, elementKind: isa.RegisterUint64, wantTier1: isa.SubOpSetSliceByteElement, wantUseTier1: true, wantOK: true},
		{name: "a boxed slice of integers", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterInt64, wantOp: isa.OpSetElementInt64, wantOK: true},
		{name: "a boxed slice of floats", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterFloat64, wantOp: isa.OpSetElementFloat64, wantOK: true},
		{name: "a boxed slice of strings", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterString, wantOp: isa.OpSetElementString, wantOK: true},
		{name: "a boxed slice of booleans", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterBool, wantOp: isa.OpSetElementBool, wantOK: true},
		{name: "a boxed slice of unsigned values", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterUint64, wantOp: isa.OpSetElementUint64, wantOK: true},
		{name: "a typed slice whose element bank disagrees falls back", collectionKind: isa.RegisterSliceString, elementKind: isa.RegisterInt64, wantOp: isa.OpSetElementInt64, wantOK: true},
		{name: "an element bank with no typed write", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterGeneral, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan, ok := PlanSliceSet(tt.collectionKind, tt.elementKind)

			require.Equal(t, tt.wantOK, ok)
			if !tt.wantOK {
				return
			}
			require.Equal(t, tt.wantUseTier1, plan.UseTier1)
			require.Equal(t, tt.wantOp, plan.Op)
			require.Equal(t, tt.wantTier1, plan.Tier1)
		})
	}
}

func TestSliceTypedStructFieldSelectorsAgreeOnTheirBanks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fieldType types.Type
		wantGet   isa.SubOpcode
		wantSet   isa.SubOpcode
		wantBank  isa.RegisterKind
		wantOK    bool
	}{
		{name: "a slice of int", fieldType: types.NewSlice(types.Typ[types.Int]), wantGet: isa.SubOpGetStructFieldSliceInt64, wantSet: isa.SubOpSetStructFieldSliceInt64, wantBank: isa.RegisterSliceInt64, wantOK: true},
		{name: "a slice of int64", fieldType: types.NewSlice(types.Typ[types.Int64]), wantGet: isa.SubOpGetStructFieldSliceInt64, wantSet: isa.SubOpSetStructFieldSliceInt64, wantBank: isa.RegisterSliceInt64, wantOK: true},
		{name: "a slice of float64", fieldType: types.NewSlice(types.Typ[types.Float64]), wantGet: isa.SubOpGetStructFieldSliceFloat64, wantSet: isa.SubOpSetStructFieldSliceFloat64, wantBank: isa.RegisterSliceFloat64, wantOK: true},
		{name: "a slice of uint", fieldType: types.NewSlice(types.Typ[types.Uint]), wantGet: isa.SubOpGetStructFieldSliceUint64, wantSet: isa.SubOpSetStructFieldSliceUint64, wantBank: isa.RegisterSliceUint64, wantOK: true},
		{name: "a slice of uint64", fieldType: types.NewSlice(types.Typ[types.Uint64]), wantGet: isa.SubOpGetStructFieldSliceUint64, wantSet: isa.SubOpSetStructFieldSliceUint64, wantBank: isa.RegisterSliceUint64, wantOK: true},
		{name: "a slice of string", fieldType: types.NewSlice(types.Typ[types.String]), wantGet: isa.SubOpGetStructFieldSliceString, wantSet: isa.SubOpSetStructFieldSliceString, wantBank: isa.RegisterSliceString, wantOK: true},
		{name: "a slice of bool", fieldType: types.NewSlice(types.Typ[types.Bool]), wantGet: isa.SubOpGetStructFieldSliceBool, wantSet: isa.SubOpSetStructFieldSliceBool, wantBank: isa.RegisterSliceBool, wantOK: true},
		{name: "a slice of byte", fieldType: types.NewSlice(types.Typ[types.Uint8]), wantGet: isa.SubOpGetStructFieldSliceByte, wantSet: isa.SubOpSetStructFieldSliceByte, wantBank: isa.RegisterSliceByte, wantOK: true},
		{name: "a slice of a narrower signed element takes the reflect path", fieldType: types.NewSlice(types.Typ[types.Int32]), wantOK: false},
		{name: "a slice of a narrower unsigned element takes the reflect path", fieldType: types.NewSlice(types.Typ[types.Uint32]), wantOK: false},
		{name: "a slice of a non-basic element takes the reflect path", fieldType: types.NewSlice(types.NewSlice(types.Typ[types.Int])), wantOK: false},
		{name: "a field that is not a slice at all", fieldType: types.Typ[types.Int], wantOK: false},
		{name: "an array field is not a slice", fieldType: types.NewArray(types.Typ[types.Int], 4), wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			getSub, getBank, getOK := PickGetStructFieldSliceSubOp(tt.fieldType)
			setSub, setBank, setOK := PickSetStructFieldSliceSubOp(tt.fieldType)

			require.Equal(t, tt.wantOK, getOK)
			require.Equal(t, getOK, setOK,
				"a field the reader admits must be writable through the matching sub-op, or a read would have no store")
			if !tt.wantOK {
				require.Equal(t, isa.RegisterGeneral, getBank,
					"a refused field reports the boxed bank so the caller falls back cleanly")
				return
			}
			require.Equal(t, tt.wantGet, getSub)
			require.Equal(t, tt.wantSet, setSub)
			require.Equal(t, tt.wantBank, getBank)
			require.Equal(t, getBank, setBank, "read and write of one field must agree on its bank")
		})
	}
}

func TestStructFieldFastPathAdmitsTheSameBanksForReadingAndWriting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind isa.RegisterKind
		want bool
	}{
		{name: "the integer bank", kind: isa.RegisterInt64, want: true},
		{name: "the unsigned bank", kind: isa.RegisterUint64, want: true},
		{name: "the float bank", kind: isa.RegisterFloat64, want: true},
		{name: "the boolean bank", kind: isa.RegisterBool, want: true},
		{name: "the string bank", kind: isa.RegisterString, want: true},
		{name: "the general bank", kind: isa.RegisterGeneral, want: true},
		{name: "the complex bank", kind: isa.RegisterComplex128, want: false},
		{name: "the typed integer slice bank", kind: isa.RegisterSliceInt64, want: false},
		{name: "the typed byte slice bank", kind: isa.RegisterSliceByte, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, StructFieldFastPathKindEnabled(tt.kind))
			require.Equal(t, StructFieldFastPathKindEnabled(tt.kind), StructFieldFastPathWriteKindEnabled(tt.kind),
				"the two predicates gate the same fast path, so a bank admitted for reading has to be admitted for writing")
		})
	}
}

func TestSteppingAStructFieldInPlaceIsOfferedForTheWholeWidthIntegerBanks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		kind     isa.RegisterKind
		operator token.Token
		want     isa.SubOpcode
	}{
		{name: "an integer field incremented", kind: isa.RegisterInt64, operator: token.INC, want: isa.SubOpIncrementStructFieldInt64},
		{name: "an integer field decremented", kind: isa.RegisterInt64, operator: token.DEC, want: isa.SubOpDecrementStructFieldInt64},
		{name: "an unsigned field incremented", kind: isa.RegisterUint64, operator: token.INC, want: isa.SubOpIncrementStructFieldUint64},
		{name: "an unsigned field decremented", kind: isa.RegisterUint64, operator: token.DEC, want: isa.SubOpDecrementStructFieldUint64},
		{name: "a float field has no in-place step", kind: isa.RegisterFloat64, operator: token.INC, want: 0},
		{name: "a boolean field has no in-place step", kind: isa.RegisterBool, operator: token.INC, want: 0},
		{name: "a string field has no in-place step", kind: isa.RegisterString, operator: token.INC, want: 0},
		{name: "an operator that is neither a step", kind: isa.RegisterInt64, operator: token.ADD, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, PickIncDecStructFieldSubOp(tt.kind, tt.operator))
		})
	}
}

func TestSliceIndexStructFieldOpCoversTheScalarBanksOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		kind   isa.RegisterKind
		want   isa.Opcode
		wantOK bool
	}{
		{name: "the integer bank", kind: isa.RegisterInt64, want: isa.OpGetElementFieldInt64, wantOK: true},
		{name: "the unsigned bank", kind: isa.RegisterUint64, want: isa.OpGetElementFieldUint64, wantOK: true},
		{name: "the float bank", kind: isa.RegisterFloat64, want: isa.OpGetElementFieldFloat64, wantOK: true},
		{name: "the boolean bank", kind: isa.RegisterBool, want: isa.OpGetElementFieldBool, wantOK: true},
		{name: "the string bank", kind: isa.RegisterString, want: isa.OpGetElementFieldString, wantOK: true},
		{name: "the general bank has no fused element field read", kind: isa.RegisterGeneral, wantOK: false},
		{name: "the complex bank has no fused element field read", kind: isa.RegisterComplex128, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := PickSliceIndexStructFieldOp(tt.kind)

			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestStoringIntoAFieldOfASliceElementIsFusedForTheIntegerAndFloatBanksOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		kind   isa.RegisterKind
		want   isa.Opcode
		wantOK bool
	}{
		{name: "the integer bank", kind: isa.RegisterInt64, want: isa.OpSetElementFieldInt64, wantOK: true},
		{name: "the float bank", kind: isa.RegisterFloat64, want: isa.OpSetElementFieldFloat64, wantOK: true},
		{name: "the unsigned bank has no fused element field store", kind: isa.RegisterUint64},
		{name: "the boolean bank has no fused element field store", kind: isa.RegisterBool},
		{name: "the string bank has no fused element field store", kind: isa.RegisterString},
		{name: "the general bank has no fused element field store", kind: isa.RegisterGeneral},
		{name: "the complex bank has no fused element field store", kind: isa.RegisterComplex128},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := PickSliceIndexStructFieldStoreOp(tt.kind)

			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestReslicingATypedSliceHasADirectFormForEveryTypedBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		kind   isa.RegisterKind
		want   isa.SubOpcode
		wantOK bool
	}{
		{name: "the typed integer slice bank", kind: isa.RegisterSliceInt64, want: isa.SubOpSliceSliceInt64, wantOK: true},
		{name: "the typed float slice bank", kind: isa.RegisterSliceFloat64, want: isa.SubOpSliceSliceFloat64, wantOK: true},
		{name: "the typed string slice bank", kind: isa.RegisterSliceString, want: isa.SubOpSliceSliceString, wantOK: true},
		{name: "the typed boolean slice bank", kind: isa.RegisterSliceBool, want: isa.SubOpSliceSliceBool, wantOK: true},
		{name: "the typed unsigned slice bank", kind: isa.RegisterSliceUint64, want: isa.SubOpSliceSliceUint64, wantOK: true},
		{name: "the typed byte slice bank", kind: isa.RegisterSliceByte, want: isa.SubOpSliceSliceByte, wantOK: true},
		{name: "the general bank has no direct reslice", kind: isa.RegisterGeneral, wantOK: false},
		{name: "a scalar bank has no direct reslice", kind: isa.RegisterInt64, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := TypedSliceDirectSliceSliceSubOp(tt.kind)

			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}
