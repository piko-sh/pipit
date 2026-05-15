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
		{name: "an integer widened to a float", source: isa.RegisterInt, dest: isa.RegisterFloat, want: isa.SubOpIntToFloat, wantOK: true},
		{name: "an integer reinterpreted as unsigned", source: isa.RegisterInt, dest: isa.RegisterUint, want: isa.SubOpIntToUint, wantOK: true},
		{name: "an integer narrowed to a boolean", source: isa.RegisterInt, dest: isa.RegisterBool, want: isa.SubOpIntToBool, wantOK: true},
		{name: "a float truncated to an integer", source: isa.RegisterFloat, dest: isa.RegisterInt, want: isa.SubOpFloatToInt, wantOK: true},
		{name: "a float truncated to an unsigned integer", source: isa.RegisterFloat, dest: isa.RegisterUint, want: isa.SubOpFloatToUint, wantOK: true},
		{name: "an unsigned value reinterpreted as an integer", source: isa.RegisterUint, dest: isa.RegisterInt, want: isa.SubOpUintToInt, wantOK: true},
		{name: "an unsigned value widened to a float", source: isa.RegisterUint, dest: isa.RegisterFloat, want: isa.SubOpUintToFloat, wantOK: true},
		{name: "a boolean widened to an integer", source: isa.RegisterBool, dest: isa.RegisterInt, want: isa.SubOpBoolToInt, wantOK: true},
		{name: "a boolean has no direct float form", source: isa.RegisterBool, dest: isa.RegisterFloat, wantOK: false},
		{name: "a boolean has no direct unsigned form", source: isa.RegisterBool, dest: isa.RegisterUint, wantOK: false},
		{name: "a float has no direct boolean form", source: isa.RegisterFloat, dest: isa.RegisterBool, wantOK: false},
		{name: "an unsigned value has no direct boolean form", source: isa.RegisterUint, dest: isa.RegisterBool, wantOK: false},
		{name: "a string is never a scalar cross-bank source", source: isa.RegisterString, dest: isa.RegisterInt, wantOK: false},
		{name: "a string is never a scalar cross-bank destination", source: isa.RegisterInt, dest: isa.RegisterString, wantOK: false},
		{name: "the general bank is never a scalar cross-bank source", source: isa.RegisterGeneral, dest: isa.RegisterInt, wantOK: false},
		{name: "a bank never converts to itself", source: isa.RegisterInt, dest: isa.RegisterInt, wantOK: false},
		{name: "a typed slice bank has no scalar conversion", source: isa.RegisterSliceInt, dest: isa.RegisterInt, wantOK: false},
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
		{name: "addition is the only operator with a string form", operator: token.ADD, wantInt: isa.OpAddInt, wantFloat: isa.OpAddFloat, wantStr: isa.OpConcatString, wantGen: isa.OpAdd},
		{name: "subtraction has no string form", operator: token.SUB, wantInt: isa.OpSubInt, wantFloat: isa.OpSubFloat, wantStr: 0, wantGen: isa.OpSub},
		{name: "multiplication has no string form", operator: token.MUL, wantInt: isa.OpMulInt, wantFloat: isa.OpMulFloat, wantStr: 0, wantGen: isa.OpMul},
		{name: "division has no string form", operator: token.QUO, wantInt: isa.OpDivInt, wantFloat: isa.OpDivFloat, wantStr: 0, wantGen: isa.OpDiv},
		{name: "remainder is integral only", operator: token.REM, wantInt: isa.OpRemInt, wantFloat: 0, wantStr: 0, wantGen: isa.OpRem},
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
		{name: "addition of integers", operator: token.ADD, leftKind: isa.RegisterInt, want: isa.OpAddInt, wantKind: isa.RegisterInt},
		{name: "addition of floats", operator: token.ADD, leftKind: isa.RegisterFloat, want: isa.OpAddFloat, wantKind: isa.RegisterFloat},
		{name: "addition of strings", operator: token.ADD, leftKind: isa.RegisterString, want: isa.OpConcatString, wantKind: isa.RegisterString},
		{name: "addition of unsigned values", operator: token.ADD, leftKind: isa.RegisterUint, want: isa.OpAddUint, wantKind: isa.RegisterUint},
		{name: "addition of complex values", operator: token.ADD, leftKind: isa.RegisterComplex, want: isa.OpAddComplex, wantKind: isa.RegisterComplex},
		{name: "addition of boxed values", operator: token.ADD, leftKind: isa.RegisterGeneral, want: isa.OpAdd, wantKind: isa.RegisterGeneral},
		{name: "subtraction of unsigned values", operator: token.SUB, leftKind: isa.RegisterUint, want: isa.OpSubUint, wantKind: isa.RegisterUint},
		{name: "multiplication of unsigned values", operator: token.MUL, leftKind: isa.RegisterUint, want: isa.OpMulUint, wantKind: isa.RegisterUint},
		{name: "division of unsigned values", operator: token.QUO, leftKind: isa.RegisterUint, want: isa.OpDivUint, wantKind: isa.RegisterUint},
		{name: "remainder of unsigned values", operator: token.REM, leftKind: isa.RegisterUint, want: isa.OpRemUint, wantKind: isa.RegisterUint},
		{name: "subtraction of complex values", operator: token.SUB, leftKind: isa.RegisterComplex, want: isa.OpSubComplex, wantKind: isa.RegisterComplex},
		{name: "multiplication of complex values", operator: token.MUL, leftKind: isa.RegisterComplex, want: isa.OpMulComplex, wantKind: isa.RegisterComplex},
		{name: "division of complex values", operator: token.QUO, leftKind: isa.RegisterComplex, want: isa.OpDivComplex, wantKind: isa.RegisterComplex},
		{name: "a boolean operand falls through to the general bank", operator: token.ADD, leftKind: isa.RegisterBool, want: isa.OpAdd, wantKind: isa.RegisterGeneral},
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
			leftKind: isa.RegisterFloat,
			wantKind: isa.RegisterFloat,
			wantErr:  fault.ErrCompileArithFloatUnsupported,
		},
		{
			name:     "a remainder of complex values has no form",
			operator: token.REM,
			leftKind: isa.RegisterComplex,
			wantKind: isa.RegisterComplex,
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
		{name: "an integer slice with a checked index", collectionKind: isa.RegisterSliceInt, elementKind: isa.RegisterInt, wantOp: isa.OpSliceGetIntDirect, wantOK: true},
		{name: "an integer slice with a proven index", collectionKind: isa.RegisterSliceInt, elementKind: isa.RegisterInt, boundsSafe: true, wantOp: isa.OpSliceGetIntDirectUnchecked, wantOK: true},
		{name: "a float slice", collectionKind: isa.RegisterSliceFloat, elementKind: isa.RegisterFloat, wantTier1: isa.SubOpSliceGetFloatDirect, wantUseTier1: true, wantOK: true},
		{name: "a string slice", collectionKind: isa.RegisterSliceString, elementKind: isa.RegisterString, wantTier1: isa.SubOpSliceGetStringDirect, wantUseTier1: true, wantOK: true},
		{name: "a boolean slice", collectionKind: isa.RegisterSliceBool, elementKind: isa.RegisterBool, wantTier1: isa.SubOpSliceGetBoolDirect, wantUseTier1: true, wantOK: true},
		{name: "an unsigned slice", collectionKind: isa.RegisterSliceUint, elementKind: isa.RegisterUint, wantTier1: isa.SubOpSliceGetUintDirect, wantUseTier1: true, wantOK: true},
		{name: "a byte slice", collectionKind: isa.RegisterSliceByte, elementKind: isa.RegisterUint, wantTier1: isa.SubOpSliceGetByteDirect, wantUseTier1: true, wantOK: true},
		{name: "a boxed slice of integers", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterInt, wantOp: isa.OpSliceGetInt, wantOK: true},
		{name: "a boxed slice of floats", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterFloat, wantOp: isa.OpSliceGetFloat, wantOK: true},
		{name: "a boxed slice of strings", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterString, wantOp: isa.OpSliceGetString, wantOK: true},
		{name: "a boxed slice of booleans", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterBool, wantOp: isa.OpSliceGetBool, wantOK: true},
		{name: "a boxed slice of unsigned values", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterUint, wantOp: isa.OpSliceGetUint, wantOK: true},
		{name: "a typed slice whose element bank disagrees falls back", collectionKind: isa.RegisterSliceFloat, elementKind: isa.RegisterInt, wantOp: isa.OpSliceGetInt, wantOK: true},
		{name: "an element bank with no typed read", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterGeneral, wantOK: false},
		{name: "a complex element has no typed read", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterComplex, wantOK: false},
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
		{name: "an integer slice", collectionKind: isa.RegisterSliceInt, elementKind: isa.RegisterInt, wantOp: isa.OpSliceSetIntDirect, wantOK: true},
		{name: "a float slice", collectionKind: isa.RegisterSliceFloat, elementKind: isa.RegisterFloat, wantTier1: isa.SubOpSliceSetFloatDirect, wantUseTier1: true, wantOK: true},
		{name: "a string slice", collectionKind: isa.RegisterSliceString, elementKind: isa.RegisterString, wantTier1: isa.SubOpSliceSetStringDirect, wantUseTier1: true, wantOK: true},
		{name: "a boolean slice", collectionKind: isa.RegisterSliceBool, elementKind: isa.RegisterBool, wantTier1: isa.SubOpSliceSetBoolDirect, wantUseTier1: true, wantOK: true},
		{name: "an unsigned slice", collectionKind: isa.RegisterSliceUint, elementKind: isa.RegisterUint, wantTier1: isa.SubOpSliceSetUintDirect, wantUseTier1: true, wantOK: true},
		{name: "a byte slice", collectionKind: isa.RegisterSliceByte, elementKind: isa.RegisterUint, wantTier1: isa.SubOpSliceSetByteDirect, wantUseTier1: true, wantOK: true},
		{name: "a boxed slice of integers", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterInt, wantOp: isa.OpSliceSetInt, wantOK: true},
		{name: "a boxed slice of floats", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterFloat, wantOp: isa.OpSliceSetFloat, wantOK: true},
		{name: "a boxed slice of strings", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterString, wantOp: isa.OpSliceSetString, wantOK: true},
		{name: "a boxed slice of booleans", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterBool, wantOp: isa.OpSliceSetBool, wantOK: true},
		{name: "a boxed slice of unsigned values", collectionKind: isa.RegisterGeneral, elementKind: isa.RegisterUint, wantOp: isa.OpSliceSetUint, wantOK: true},
		{name: "a typed slice whose element bank disagrees falls back", collectionKind: isa.RegisterSliceString, elementKind: isa.RegisterInt, wantOp: isa.OpSliceSetInt, wantOK: true},
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
		{name: "a slice of int", fieldType: types.NewSlice(types.Typ[types.Int]), wantGet: isa.SubOpGetStructFieldSliceInt, wantSet: isa.SubOpSetStructFieldSliceInt, wantBank: isa.RegisterSliceInt, wantOK: true},
		{name: "a slice of int64", fieldType: types.NewSlice(types.Typ[types.Int64]), wantGet: isa.SubOpGetStructFieldSliceInt, wantSet: isa.SubOpSetStructFieldSliceInt, wantBank: isa.RegisterSliceInt, wantOK: true},
		{name: "a slice of float64", fieldType: types.NewSlice(types.Typ[types.Float64]), wantGet: isa.SubOpGetStructFieldSliceFloat, wantSet: isa.SubOpSetStructFieldSliceFloat, wantBank: isa.RegisterSliceFloat, wantOK: true},
		{name: "a slice of uint", fieldType: types.NewSlice(types.Typ[types.Uint]), wantGet: isa.SubOpGetStructFieldSliceUint, wantSet: isa.SubOpSetStructFieldSliceUint, wantBank: isa.RegisterSliceUint, wantOK: true},
		{name: "a slice of uint64", fieldType: types.NewSlice(types.Typ[types.Uint64]), wantGet: isa.SubOpGetStructFieldSliceUint, wantSet: isa.SubOpSetStructFieldSliceUint, wantBank: isa.RegisterSliceUint, wantOK: true},
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
		{name: "the integer bank", kind: isa.RegisterInt, want: true},
		{name: "the unsigned bank", kind: isa.RegisterUint, want: true},
		{name: "the float bank", kind: isa.RegisterFloat, want: true},
		{name: "the boolean bank", kind: isa.RegisterBool, want: true},
		{name: "the string bank", kind: isa.RegisterString, want: true},
		{name: "the general bank", kind: isa.RegisterGeneral, want: true},
		{name: "the complex bank", kind: isa.RegisterComplex, want: false},
		{name: "the typed integer slice bank", kind: isa.RegisterSliceInt, want: false},
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
		{name: "an integer field incremented", kind: isa.RegisterInt, operator: token.INC, want: isa.SubOpIncStructFieldInt},
		{name: "an integer field decremented", kind: isa.RegisterInt, operator: token.DEC, want: isa.SubOpDecStructFieldInt},
		{name: "an unsigned field incremented", kind: isa.RegisterUint, operator: token.INC, want: isa.SubOpIncStructFieldUint},
		{name: "an unsigned field decremented", kind: isa.RegisterUint, operator: token.DEC, want: isa.SubOpDecStructFieldUint},
		{name: "a float field has no in-place step", kind: isa.RegisterFloat, operator: token.INC, want: 0},
		{name: "a boolean field has no in-place step", kind: isa.RegisterBool, operator: token.INC, want: 0},
		{name: "a string field has no in-place step", kind: isa.RegisterString, operator: token.INC, want: 0},
		{name: "an operator that is neither a step", kind: isa.RegisterInt, operator: token.ADD, want: 0},
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
		{name: "the integer bank", kind: isa.RegisterInt, want: isa.OpSliceIndexStructFieldInt, wantOK: true},
		{name: "the unsigned bank", kind: isa.RegisterUint, want: isa.OpSliceIndexStructFieldUint, wantOK: true},
		{name: "the float bank", kind: isa.RegisterFloat, want: isa.OpSliceIndexStructFieldFloat, wantOK: true},
		{name: "the boolean bank", kind: isa.RegisterBool, want: isa.OpSliceIndexStructFieldBool, wantOK: true},
		{name: "the string bank", kind: isa.RegisterString, want: isa.OpSliceIndexStructFieldString, wantOK: true},
		{name: "the general bank has no fused element field read", kind: isa.RegisterGeneral, wantOK: false},
		{name: "the complex bank has no fused element field read", kind: isa.RegisterComplex, wantOK: false},
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

func TestReslicingATypedSliceHasADirectFormForEveryTypedBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		kind   isa.RegisterKind
		want   isa.SubOpcode
		wantOK bool
	}{
		{name: "the typed integer slice bank", kind: isa.RegisterSliceInt, want: isa.SubOpSliceSliceIntDirect, wantOK: true},
		{name: "the typed float slice bank", kind: isa.RegisterSliceFloat, want: isa.SubOpSliceSliceFloatDirect, wantOK: true},
		{name: "the typed string slice bank", kind: isa.RegisterSliceString, want: isa.SubOpSliceSliceStringDirect, wantOK: true},
		{name: "the typed boolean slice bank", kind: isa.RegisterSliceBool, want: isa.SubOpSliceSliceBoolDirect, wantOK: true},
		{name: "the typed unsigned slice bank", kind: isa.RegisterSliceUint, want: isa.SubOpSliceSliceUintDirect, wantOK: true},
		{name: "the typed byte slice bank", kind: isa.RegisterSliceByte, want: isa.SubOpSliceByteSlice, wantOK: true},
		{name: "the general bank has no direct reslice", kind: isa.RegisterGeneral, wantOK: false},
		{name: "a scalar bank has no direct reslice", kind: isa.RegisterInt, wantOK: false},
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
