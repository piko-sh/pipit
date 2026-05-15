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
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

func TestConstantPoolAddersReturnSequentialIndices(t *testing.T) {
	t.Parallel()

	t.Run("integer constants", func(t *testing.T) {
		t.Parallel()
		compiledFunction := NewNamedFunction("constants")

		first, err := AddIntConstant(compiledFunction, 1)
		require.NoError(t, err)
		second, err := AddIntConstant(compiledFunction, 2)
		require.NoError(t, err)

		require.Equal(t, uint16(0), first)
		require.Equal(t, uint16(1), second)
		require.Equal(t, []int64{1, 2}, compiledFunction.IntConstants)
	})

	t.Run("every pool numbers its entries independently", func(t *testing.T) {
		t.Parallel()
		compiledFunction := NewNamedFunction("constants")

		intIndex, err := AddIntConstant(compiledFunction, 1)
		require.NoError(t, err)
		floatIndex, err := AddFloatConstant(compiledFunction, 1.5)
		require.NoError(t, err)
		stringIndex, err := AddStringConstant(compiledFunction, "a")
		require.NoError(t, err)
		boolIndex, err := AddBoolConstant(compiledFunction, true)
		require.NoError(t, err)
		uintIndex, err := AddUintConstant(compiledFunction, 7)
		require.NoError(t, err)
		complexIndex, err := AddComplexConstant(compiledFunction, 1+2i)
		require.NoError(t, err)

		require.Equal(t, uint16(0), intIndex)
		require.Equal(t, uint16(0), floatIndex)
		require.Equal(t, uint16(0), stringIndex)
		require.Equal(t, uint16(0), boolIndex)
		require.Equal(t, uint16(0), uintIndex)
		require.Equal(t, uint16(0), complexIndex)
	})

	t.Run("a general constant records its descriptor", func(t *testing.T) {
		t.Parallel()
		compiledFunction := NewNamedFunction("constants")

		index, err := AddGeneralConstant(compiledFunction, reflect.ValueOf(42), descriptor.GeneralConstantDescriptor{})

		require.NoError(t, err)
		require.Equal(t, uint16(0), index)
		require.Len(t, compiledFunction.GeneralConstants, 1)
	})

	t.Run("a type reference is recorded in the type table", func(t *testing.T) {
		t.Parallel()
		compiledFunction := NewNamedFunction("constants")

		index, err := AddTypeRef(compiledFunction, reflect.TypeFor[int]())

		require.NoError(t, err)
		require.Equal(t, uint16(0), index)
		require.Equal(t, reflect.TypeFor[int](), compiledFunction.TypeTable[0])
	})
}

func TestConstantPoolRefusesEntriesPastTheConfiguredCap(t *testing.T) {
	t.Parallel()

	compiledFunction := NewNamedFunction("constants")
	compiledFunction.ApplyResourceLimits(2, 0, 0)

	_, err := AddIntConstant(compiledFunction, 1)
	require.NoError(t, err)
	_, err = AddIntConstant(compiledFunction, 2)
	require.NoError(t, err)

	_, err = AddIntConstant(compiledFunction, 3)

	require.Error(t, err, "the pool cap must be enforced rather than silently exceeded")
}

func TestRegisterMethodRefusesEntriesPastTheConfiguredCap(t *testing.T) {
	t.Parallel()

	compiledFunction := NewNamedFunction("methods")
	compiledFunction.ApplyResourceLimits(0, 0, 1)

	require.NoError(t, RegisterMethod(compiledFunction, "First", 0))

	require.Error(t, RegisterMethod(compiledFunction, "Second", 1),
		"the method-table cap must be enforced")
}

func TestAddCallSiteReturnsSequentialIndices(t *testing.T) {
	t.Parallel()

	compiledFunction := NewNamedFunction("sites")

	first, err := AddCallSite(compiledFunction, &CallSite{})
	require.NoError(t, err)
	second, err := AddCallSite(compiledFunction, &CallSite{})
	require.NoError(t, err)

	require.Equal(t, uint16(0), first)
	require.Equal(t, uint16(1), second)
	require.Len(t, compiledFunction.CallSites, 2)
}

func TestEmitHelpersAppendTheExpectedEncoding(t *testing.T) {
	t.Parallel()

	t.Run("a plain instruction is appended at the current program counter", func(t *testing.T) {
		t.Parallel()
		compiledFunction := NewNamedFunction("body")

		require.Equal(t, 0, CurrentPC(compiledFunction))
		position := Emit(compiledFunction, isa.OpAddInt, 1, 2, 3)

		require.Equal(t, 0, position)
		require.Equal(t, 1, CurrentPC(compiledFunction))
		require.Equal(t, isa.Instruction{Op: isa.OpAddInt, A: 1, B: 2, C: 3}, compiledFunction.Body[0])
	})

	t.Run("a wide operand splits across the low and high bytes", func(t *testing.T) {
		t.Parallel()
		compiledFunction := NewNamedFunction("body")

		EmitWide(compiledFunction, isa.OpLoadIntConst, 0, 300)

		require.Equal(t, uint16(300), compiledFunction.Body[0].WideIndex())
	})

	t.Run("an extension word packs its wide payload into the first two operands", func(t *testing.T) {
		t.Parallel()
		compiledFunction := NewNamedFunction("body")

		EmitExtension(compiledFunction, 4000, 7)

		extension := compiledFunction.Body[0]
		require.Equal(t, isa.OpExt, extension.Op)
		require.Equal(t, uint16(4000), isa.JoinWide(extension.A, extension.B),
			"an extension word carries its payload in operands A and B")
		require.Equal(t, uint8(7), extension.C, "operand C stays free for the owning opcode's flag")
		require.NotEqual(t, uint16(4000), extension.WideIndex(),
			"WideIndex reads operands B and C, so it is the wrong decoder for an extension word")
	})

	t.Run("tier one, two and three encodings drill in order", func(t *testing.T) {
		t.Parallel()
		compiledFunction := NewNamedFunction("body")

		EmitTier1(compiledFunction, isa.SubOpNegInt, 1, 2)
		EmitTier2(compiledFunction, isa.SubOpTier2Return, 1)
		EmitTier3(compiledFunction, isa.SubOpTier3ReturnVoid)

		require.Equal(t, isa.NewTier1Instruction(isa.SubOpNegInt, 1, 2), compiledFunction.Body[0])
		require.Equal(t, isa.NewTier2Instruction(isa.SubOpTier2Return, 1), compiledFunction.Body[1])
		require.Equal(t, isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid), compiledFunction.Body[2])
	})

	t.Run("a tier one wide operand splits across the low and high bytes", func(t *testing.T) {
		t.Parallel()
		compiledFunction := NewNamedFunction("body")

		EmitTier1Wide(compiledFunction, isa.SubOpCallMethod, 500)

		require.Equal(t, uint16(500), compiledFunction.Body[0].WideIndex())
	})
}

func TestPatchJumpWritesTheDistanceToTheCurrentPosition(t *testing.T) {
	t.Parallel()

	compiledFunction := NewNamedFunction("body")
	patchPC := EmitJump(compiledFunction, isa.OpJumpIfFalse, 0)
	Emit(compiledFunction, isa.OpAddInt, 0, 0, 0)
	Emit(compiledFunction, isa.OpAddInt, 0, 0, 0)

	PatchJump(compiledFunction, patchPC)

	require.Equal(t, 2, DecodeJumpOffset(compiledFunction.Body[patchPC]),
		"the patched offset is the distance from the word after the jump to the current position")
}

func TestEncodeJumpOffsetRoundTripsThroughTheOperandBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		delta int
	}{
		{name: "a zero delta", delta: 0},
		{name: "a forward delta", delta: 42},
		{name: "a backward delta", delta: -42},
		{name: "the largest forward delta", delta: math.MaxInt16},
		{name: "the largest backward delta", delta: math.MinInt16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := NewNamedFunction("body")

			low, high := EncodeJumpOffset(compiledFunction, tt.delta)

			require.Equal(t, tt.delta, DecodeJumpOffset(isa.NewInstruction(isa.OpJumpIfFalse, 0, low, high)))
		})
	}
}

func TestFitsJumpOffsetBoundsTheSignedSixteenBitRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		offset int
		want   bool
	}{
		{name: "zero fits", offset: 0, want: true},
		{name: "the largest forward offset fits", offset: math.MaxInt16, want: true},
		{name: "the largest backward offset fits", offset: math.MinInt16, want: true},
		{name: "one past the forward limit does not fit", offset: math.MaxInt16 + 1, want: false},
		{name: "one past the backward limit does not fit", offset: math.MinInt16 - 1, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, FitsJumpOffset(tt.offset))
		})
	}
}

func TestBlockerForInstructionNamesWhyInliningWasRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		instruction isa.Instruction
		name        string
		want        InlineRefusal
	}{
		{name: "a defer blocks inlining", instruction: isa.Instruction{Op: isa.OpDefer}, want: InlineRefusalDefer},
		{name: "a goroutine launch blocks inlining", instruction: isa.Instruction{Op: isa.OpGo}, want: InlineRefusalGo},
		{name: "making a closure blocks inlining", instruction: isa.Instruction{Op: isa.OpMakeClosure}, want: InlineRefusalClosureOps},
		{name: "reading an upvalue blocks inlining", instruction: isa.Instruction{Op: isa.OpGetUpvalue}, want: InlineRefusalClosureOps},
		{name: "writing an upvalue blocks inlining", instruction: isa.Instruction{Op: isa.OpSetUpvalue}, want: InlineRefusalClosureOps},
		{name: "a channel send blocks inlining", instruction: isa.Instruction{Op: isa.OpChannelSend}, want: InlineRefusalChannelOps},
		{name: "synchronising closure upvalues blocks inlining", instruction: isa.NewTier2Instruction(isa.SubOpTier2SyncClosureUpvalues, 0), want: InlineRefusalClosureOps},
		{name: "an ordinary instruction gives no named reason", instruction: isa.Instruction{Op: isa.OpAddInt}, want: InlineRefusalUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, BlockerForInstruction(tt.instruction))
		})
	}
}

func TestMayAliasIsConservativeWhenItCannotProveOtherwise(t *testing.T) {
	t.Parallel()

	info := &PointerAliasInfo{PerPCEnv: []AliasEnvironment{{Class: [GeneralAliasBankSize]AliasClass{}}}}
	info.PerPCEnv[0].Class[1] = 1
	info.PerPCEnv[0].Class[2] = 2
	info.PerPCEnv[0].Class[3] = 1
	info.PerPCEnv[0].Class[4] = AliasClassWild

	tests := []struct {
		info *PointerAliasInfo
		name string
		pc   int
		regA uint8
		regB uint8
		want bool
	}{
		{name: "a nil analysis assumes aliasing", info: nil, pc: 0, regA: 1, regB: 2, want: true},
		{name: "a register always aliases itself", info: info, pc: 0, regA: 1, regB: 1, want: true},
		{name: "a negative position assumes aliasing", info: info, pc: -1, regA: 1, regB: 2, want: true},
		{name: "a position past the analysis assumes aliasing", info: info, pc: 99, regA: 1, regB: 2, want: true},
		{name: "a wild class assumes aliasing", info: info, pc: 0, regA: 4, regB: 1, want: true},
		{name: "different classes cannot alias", info: info, pc: 0, regA: 1, regB: 2, want: false},
		{name: "the same class may alias", info: info, pc: 0, regA: 1, regB: 3, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tt.info.MayAlias(tt.pc, tt.regA, tt.regB))
		})
	}
}

func TestCompiledFileSetExposesItsEntrypoints(t *testing.T) {
	t.Parallel()

	root := NewNamedFunction("root")
	root.AppendFunction(NewNamedFunction("main"))
	initialiser := NewNamedFunction("init")
	fileSet := NewCompiledFileSet(root, map[string]uint16{"main": 0}, []uint16{0}, initialiser)

	t.Run("a known entrypoint resolves to its function", func(t *testing.T) {
		t.Parallel()
		found, err := fileSet.FindFunction("main")

		require.NoError(t, err)
		require.Equal(t, "main", found.FunctionName())
	})

	t.Run("an unknown entrypoint reports the sentinel", func(t *testing.T) {
		t.Parallel()
		_, err := fileSet.FindFunction("absent")

		require.ErrorIs(t, err, fault.ErrEntrypointNotFound)
	})

	t.Run("the bundle reports its own metadata", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, []string{"main"}, fileSet.FunctionNames())
		require.Same(t, root, fileSet.Root())
		require.Same(t, initialiser, fileSet.VariableInitFunction())
		require.Equal(t, []uint16{0}, fileSet.InitFunctions())
		require.Equal(t, map[string]uint16{"main": 0}, fileSet.Entrypoints())
	})

	t.Run("a nil bundle names no functions", func(t *testing.T) {
		t.Parallel()
		var absent *CompiledFileSet

		require.Nil(t, absent.FunctionNames())
	})

	t.Run("slot allocation and package variables round-trip", func(t *testing.T) {
		t.Parallel()
		bundle := NewCompiledFileSet(NewNamedFunction("root"), nil, nil, nil)
		allocation := SlotAllocation{}
		allocation[isa.RegisterInt] = 3

		bundle.SetSlotAllocation(allocation)
		bundle.SetPackageVariables([]PackageVariableMetadata{{Name: "Counter"}})

		require.Equal(t, allocation, bundle.SlotAllocation())
		require.Len(t, bundle.PackageVariables(), 1)
		require.Equal(t, "Counter", bundle.PackageVariables()[0].Name)
	})
}

func TestDebugEventStringNamesEachEvent(t *testing.T) {
	t.Parallel()

	const knownEventCount = 8

	unknown := DebugEvent(255).String()
	seen := map[string]bool{}
	for code := range knownEventCount {
		rendered := DebugEvent(code).String()
		require.NotEmpty(t, rendered, "every debug event must render a name")
		if rendered == unknown {
			continue
		}
		require.False(t, seen[rendered], "debug event names must be distinct: %s", rendered)
		seen[rendered] = true
	}
}
