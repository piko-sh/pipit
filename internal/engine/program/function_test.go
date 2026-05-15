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

package program

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func returnInstruction(count uint8) isa.Instruction {
	return isa.NewTier2Instruction(isa.SubOpTier2Return, count)
}

func TestNewNamedFunctionStartsEmpty(t *testing.T) {
	t.Parallel()

	compiledFunction := NewNamedFunction("example")

	require.Equal(t, "example", compiledFunction.FunctionName())
	require.Equal(t, 0, compiledFunction.BodyLen())
	require.Empty(t, compiledFunction.SubFunctions())
	require.Equal(t, [isa.NumRegisterKinds]uint32{}, compiledFunction.RegisterCounts())
}

func TestAppendFunctionReturnsTheIndexItWroteTo(t *testing.T) {
	t.Parallel()

	root := NewNamedFunction("root")

	require.Equal(t, uint16(0), root.AppendFunction(NewNamedFunction("first")))
	require.Equal(t, uint16(1), root.AppendFunction(NewNamedFunction("second")))
	require.Equal(t, uint16(2), root.AppendFunction(NewNamedFunction("third")))
	require.Len(t, root.SubFunctions(), 3)
	require.Equal(t, "second", root.SubFunctions()[1].FunctionName())
}

func TestTruncateFunctionsIgnoresAnOutOfRangeLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		truncate  int
		wantCount int
	}{
		{name: "truncating to zero empties the table", truncate: 0, wantCount: 0},
		{name: "truncating to a shorter length drops the tail", truncate: 1, wantCount: 1},
		{name: "truncating to the current length is a no-op", truncate: 3, wantCount: 3},
		{name: "a negative length is ignored", truncate: -1, wantCount: 3},
		{name: "a length past the table is ignored", truncate: 9, wantCount: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := NewNamedFunction("root")
			root.AppendFunction(NewNamedFunction("a"))
			root.AppendFunction(NewNamedFunction("b"))
			root.AppendFunction(NewNamedFunction("c"))

			root.TruncateFunctions(tt.truncate)

			require.Len(t, root.SubFunctions(), tt.wantCount)
		})
	}
}

func TestShareFunctionsWithAliasesTheSourceTable(t *testing.T) {
	t.Parallel()

	source := NewNamedFunction("source")
	source.AppendFunction(NewNamedFunction("child"))
	target := NewNamedFunction("target")

	target.ShareFunctionsWith(source)

	require.Len(t, target.SubFunctions(), 1)
	require.Equal(t, "child", target.SubFunctions()[0].FunctionName())
}

func TestSetAndReadTheMethodTable(t *testing.T) {
	t.Parallel()

	compiledFunction := NewNamedFunction("methods")
	require.Nil(t, compiledFunction.MethodTable(), "a fresh function has no method table")

	compiledFunction.SetMethodTable(map[string]uint16{"Add": 3})

	require.Equal(t, uint16(3), compiledFunction.MethodTable()["Add"])
}

func TestSetFunctionsReplacesTheWholeTable(t *testing.T) {
	t.Parallel()

	root := NewNamedFunction("root")
	root.AppendFunction(NewNamedFunction("old"))

	root.SetFunctions([]*CompiledFunction{NewNamedFunction("new")})

	require.Len(t, root.SubFunctions(), 1)
	require.Equal(t, "new", root.SubFunctions()[0].FunctionName())
}

func TestSetVariableInitFunctionRecordsTheInitialiser(t *testing.T) {
	t.Parallel()

	root := NewNamedFunction("root")
	initialiser := NewNamedFunction("init")

	root.SetVariableInitFunction(initialiser)

	require.Same(t, initialiser, root.VariableInitFunction)
}

func TestApplyResourceLimitsIgnoresNonPositiveCaps(t *testing.T) {
	t.Parallel()

	t.Run("positive caps are recorded", func(t *testing.T) {
		t.Parallel()
		compiledFunction := NewNamedFunction("limits")

		compiledFunction.ApplyResourceLimits(10, 20, 30)

		require.Equal(t, 10, compiledFunction.maxConstantPoolSize)
		require.Equal(t, 20, compiledFunction.maxSpecialisations)
		require.Equal(t, 30, compiledFunction.maxMethods)
	})

	t.Run("zero and negative caps leave the existing values", func(t *testing.T) {
		t.Parallel()
		compiledFunction := NewNamedFunction("limits")
		compiledFunction.ApplyResourceLimits(10, 20, 30)

		compiledFunction.ApplyResourceLimits(0, -1, 0)

		require.Equal(t, 10, compiledFunction.maxConstantPoolSize)
		require.Equal(t, 20, compiledFunction.maxSpecialisations)
		require.Equal(t, 30, compiledFunction.maxMethods)
	})
}

func TestRegisterCountsMirrorTheDeclaredBanks(t *testing.T) {
	t.Parallel()

	compiledFunction := NewNamedFunction("registers")
	compiledFunction.NumRegisters[isa.RegisterInt] = 4
	compiledFunction.NumRegisters[isa.RegisterGeneral] = 2

	counts := compiledFunction.RegisterCounts()

	require.Equal(t, uint32(4), counts[isa.RegisterInt])
	require.Equal(t, uint32(2), counts[isa.RegisterGeneral])
	require.Equal(t, uint32(0), counts[isa.RegisterFloat])
}

func TestSlotKindReportsGeneralForIndirectVariables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		info GlobalVariableInfo
		want isa.RegisterKind
	}{
		{name: "a direct integer slot keeps its bank", info: GlobalVariableInfo{Kind: isa.RegisterInt}, want: isa.RegisterInt},
		{name: "a direct string slot keeps its bank", info: GlobalVariableInfo{Kind: isa.RegisterString}, want: isa.RegisterString},
		{name: "an indirect slot is held in the general bank", info: GlobalVariableInfo{Kind: isa.RegisterInt, IsIndirect: true}, want: isa.RegisterGeneral},
		{name: "an indirect general slot stays general", info: GlobalVariableInfo{Kind: isa.RegisterGeneral, IsIndirect: true}, want: isa.RegisterGeneral},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tt.info.SlotKind())
		})
	}
}

func TestKindDefaultReflectTypeCoversEveryScalarBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want reflect.Type
		name string
		kind isa.RegisterKind
	}{
		{name: "the integer bank defaults to int", kind: isa.RegisterInt, want: reflect.TypeFor[int]()},
		{name: "the float bank defaults to float64", kind: isa.RegisterFloat, want: reflect.TypeFor[float64]()},
		{name: "the string bank defaults to string", kind: isa.RegisterString, want: reflect.TypeFor[string]()},
		{name: "the boolean bank defaults to bool", kind: isa.RegisterBool, want: reflect.TypeFor[bool]()},
		{name: "the unsigned bank defaults to uint64", kind: isa.RegisterUint, want: reflect.TypeFor[uint64]()},
		{name: "the complex bank defaults to complex128", kind: isa.RegisterComplex, want: reflect.TypeFor[complex128]()},
		{name: "the general bank defaults to the empty interface", kind: isa.RegisterGeneral, want: reflect.TypeFor[any]()},
		{name: "a typed slice bank defaults to the empty interface", kind: isa.RegisterSliceInt, want: reflect.TypeFor[any]()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, KindDefaultReflectType(tt.kind))
		})
	}
}

func TestIsReturnInstructionAcceptsBothReturnEncodings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		instruction isa.Instruction
		name        string
		want        bool
	}{
		{name: "a tier-two return of one value", instruction: returnInstruction(1), want: true},
		{name: "a tier-two return of no values", instruction: returnInstruction(0), want: true},
		{name: "a tier-three void return", instruction: isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid), want: true},
		{name: "a tier-zero opcode is not a return", instruction: isa.Instruction{Op: isa.OpAddInt}, want: false},
		{name: "another tier-one sub-op is not a return", instruction: isa.NewTier1Instruction(isa.SubOpNegInt, 0, 0), want: false},
		{name: "another tier-two sub-op is not a return", instruction: isa.NewTier2Instruction(isa.SubOpTier2MakeMap, 0), want: false},
		{name: "another tier-three sub-op is not a return", instruction: isa.NewTier3Instruction(isa.SubOpTier3Nop), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, IsReturnInstruction(tt.instruction))
		})
	}
}

func TestDecodeJumpOffsetReadsASignedSixteenBitOperand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want int
	}{
		{name: "a zero offset", want: 0},
		{name: "a small forward offset", want: 5},
		{name: "a small backward offset", want: -5},
		{name: "the largest forward offset", want: 32767},
		{name: "the largest backward offset", want: -32768},
		{name: "a byte-boundary offset", want: 256},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			low, high := isa.SplitOffset(int16(tt.want))
			instruction := isa.NewInstruction(isa.OpJumpIfFalse, 0, low, high)

			require.Equal(t, tt.want, DecodeJumpOffset(instruction))
		})
	}
}

func TestIsAnonymousFieldRecognisesEmbeddedAndPrefixedFields(t *testing.T) {
	t.Parallel()

	type embedded struct{ X int }
	type host struct {
		embedded
		Named int
	}

	hostType := reflect.TypeFor[host]()

	require.True(t, IsAnonymousField(hostType.Field(0)), "an embedded field is anonymous")
	require.False(t, IsAnonymousField(hostType.Field(1)), "a named field is not anonymous")
}

func TestArenaCountsFromNumRegsMirrorsEveryBank(t *testing.T) {
	t.Parallel()

	var numRegs [isa.NumRegisterKinds]uint32
	numRegs[isa.RegisterInt] = 3
	numRegs[isa.RegisterFloat] = 4
	numRegs[isa.RegisterString] = 5
	numRegs[isa.RegisterGeneral] = 6
	numRegs[isa.RegisterBool] = 7
	numRegs[isa.RegisterUint] = 8
	numRegs[isa.RegisterComplex] = 9

	counts := ArenaCountsFromNumRegs(numRegs)

	require.Equal(t, 3, counts.Ints)
	require.Equal(t, 4, counts.Floats)
	require.Equal(t, 5, counts.Strings)
	require.Equal(t, 6, counts.Generals)
	require.Equal(t, 7, counts.Bools)
	require.Equal(t, 8, counts.Uints)
	require.Equal(t, 9, counts.Complexes)
}

func TestEnsurePrecomputedAllocCountsIsIdempotent(t *testing.T) {
	t.Parallel()

	compiledFunction := NewNamedFunction("counts")
	compiledFunction.NumRegisters[isa.RegisterInt] = 2

	compiledFunction.EnsurePrecomputedAllocCounts()
	first := compiledFunction.PrecomputedAllocCounts

	compiledFunction.NumRegisters[isa.RegisterInt] = 9
	compiledFunction.EnsurePrecomputedAllocCounts()

	require.Equal(t, first, compiledFunction.PrecomputedAllocCounts,
		"the precomputed counts are published once and must not drift afterwards")
}

func TestExportFunctionsReturnsTheSubFunctionTable(t *testing.T) {
	t.Parallel()

	root := NewNamedFunction("root")
	root.AppendFunction(NewNamedFunction("child"))

	require.Len(t, ExportFunctions(root), 1)
	require.Equal(t, "child", ExportFunctions(root)[0].FunctionName())
}

func TestHeldKindIsDirectPointerRecognisesThePointerShapedKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind reflect.Kind
		want bool
	}{
		{name: "a pointer is directly pointer-shaped", kind: reflect.Pointer, want: true},
		{name: "a map is directly pointer-shaped", kind: reflect.Map, want: true},
		{name: "a channel is directly pointer-shaped", kind: reflect.Chan, want: true},
		{name: "an unsafe pointer is directly pointer-shaped", kind: reflect.UnsafePointer, want: true},
		{name: "a slice is not a single word", kind: reflect.Slice, want: false},
		{name: "a string is not a single word", kind: reflect.String, want: false},
		{name: "an integer is not pointer-shaped", kind: reflect.Int, want: false},
		{name: "a struct is not pointer-shaped", kind: reflect.Struct, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, HeldKindIsDirectPointer(tt.kind))
		})
	}
}
