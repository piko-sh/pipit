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

package inline

import (
	"go/types"
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func instructionForSpec(row isa.OpSpec) isa.Instruction {
	switch row.Tier {
	case isa.TierMain:
		return isa.Instruction{Op: isa.Opcode(row.Code)}
	case isa.TierSub1:
		return isa.NewTier1Instruction(isa.SubOpcode(row.Code), 0, 0)
	case isa.TierSub2:
		return isa.NewTier2Instruction(isa.SubOpcodeTier2(row.Code), 0)
	default:
		return isa.Instruction{Op: isa.OpDrillTier1, A: uint8(isa.SubOpDrillTier2), B: uint8(isa.SubOpTier2DrillTier3), C: row.Code}
	}
}

func generalSlot(register uint8) program.VarLocation {
	return program.VarLocation{Kind: isa.RegisterGeneral, Register: register}
}

func TestEveryOperationIsSplicedOrRefusedWithAReason(t *testing.T) {
	t.Parallel()

	for _, row := range isa.AllSpecs() {
		instruction := instructionForSpec(row)
		listed := operationOnASpliceList(instruction)
		described := spliceableByDescription(instruction)
		reason := inlineRefusalReason(instruction)
		switch {
		case reason != "":
			require.Falsef(t, listed, "%s is refused (%s) and on a hand-kept list at once", row.Name, reason)
			require.Falsef(t, described, "%s is refused (%s) and still spliced by description", row.Name, reason)
		case listed:
		default:
			require.Truef(t, described,
				"%s is on no splice list, cannot be spliced from its description, and has no entry in\n"+
					"inlineRefusedOperations: decide which it is, so a new operation cannot fall out of\n"+
					"inlining without anyone noticing", row.Name)
		}
	}
}

func TestRefusedOperationsAreKeptOutOfSplicedBodies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		instruction isa.Instruction
	}{
		{name: "a closure cell read", instruction: isa.NewInstruction(isa.OpGetCapturedVariable, 0, 0, uint8(isa.RegisterInt64))},
		{name: "a panic", instruction: isa.NewTier2Instruction(isa.SubOpTier2Panic, 0)},
		{name: "a channel send", instruction: isa.NewInstruction(isa.OpSendToChannel, 0, 0, uint8(isa.RegisterInt64))},
		{name: "an append in place", instruction: isa.NewInstruction(isa.OpAppendInPlace, 0, 0, 1)},
		{name: "a typed slice allocated from the frame's arena", instruction: isa.NewTier1Instruction(isa.SubOpMakeSliceByte, 0, 0)},
		{name: "a range loop step", instruction: isa.NewTier1Instruction(isa.SubOpRangeNext, 0, 0)},
		{name: "a type-switch head", instruction: isa.NewTier1Instruction(isa.SubOpTypeSwitchJump, 0, 0)},
		{name: "a native call", instruction: isa.NewTier1Instruction(isa.SubOpCallNative, 0, 0)},
		{name: "a general append", instruction: isa.NewInstruction(isa.OpAppend, 0, 0, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.NotEmpty(t, inlineRefusalReason(tt.instruction), "the refusal carries its reason")
			require.False(t, phase2OperationAllowed(tt.instruction))
		})
	}
}

func TestADescriptionMustAccountForEveryOperandByte(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		instruction isa.Instruction
		want        bool
	}{
		{name: "three registers of known banks", instruction: isa.NewInstruction(isa.OpGetStringByte, 0, 1, 2), want: true},
		{name: "a register whose bank the kind marker names", instruction: isa.NewInstruction(isa.OpTruncateToWidth, 0, 32, uint8(isa.RegisterUint64)), want: true},
		{name: "a layout index with a table entry", instruction: isa.NewInstruction(isa.OpGetStructFieldRawPointer, 0, 1, 2), want: true},
		{name: "extension words registered as described", instruction: isa.NewTier1Instruction(isa.SubOpAddUint64Constant, 0, 1), want: true},
		{name: "a branch registered as relative", instruction: isa.NewTier1Instruction(isa.SubOpLessThanLengthStringThenJumpIfFalse, 0, 1), want: true},
		{name: "extension words nothing is registered for", instruction: isa.NewTier1Instruction(isa.SubOpMathMod, 0, 1)},
		{name: "a branch nothing is registered for", instruction: isa.NewInstruction(isa.OpJumpIfNil, 0, 1, 0)},
		{name: "registers written that no byte names", instruction: isa.NewTier2Instruction(isa.SubOpTier2SynchroniseClosureCapturedVariables, 0)},
		{name: "an operation that ends its block", instruction: isa.NewTier1Instruction(isa.SubOpTypeSwitchJump, 0, 1)},
		{name: "a register whose bank only an extension word names", instruction: isa.NewTier1Instruction(isa.SubOpMakeSliceHeap, 0, 1)},
		{name: "a table index with no table entry", instruction: isa.NewInstruction(isa.OpMakeClosure, 0, 1, 0)},
		{name: "a call site outside the merged forms", instruction: isa.NewTier1Instruction(isa.SubOpCallNative, 0, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, descriptionAccountsForEveryOperand(tt.instruction))
		})
	}
}

func TestDescribedExtensionWordsAreDescribedForEveryBank(t *testing.T) {
	t.Parallel()

	for _, row := range isa.AllSpecs() {
		owner := instructionForSpec(row)
		pools := inlinePoolShapeFor(owner)
		if !pools.describedExtension {
			continue
		}
		require.NotZerof(t, row.Shape().Flags&isa.ShapeFlagFollowsExtension,
			"%s is registered as copying extension words it does not carry", row.Name)
		word := []isa.Instruction{isa.NewInstruction(isa.OpExtension, 1, 0, isa.ByteRunMinStores)}
		for kind := range isa.RegisterKind(isa.NumRegisterKinds) {
			_, _, described := isa.ExtensionRegisterUse(owner, word, kind)
			require.Truef(t, described,
				"%s is spliced with its extension words, but isa.ExtensionRegisterUse does not say which of\n"+
					"their bytes name a register of bank %s; the splice would refuse every callee holding it",
				row.Name, kind)
		}
	}
}

func TestInliningSplicesOperationsFromTheirDescription(t *testing.T) {
	t.Parallel()

	callee := &program.CompiledFunction{
		Name: "mix",
		Body: []isa.Instruction{
			isa.NewTier1Instruction(isa.SubOpConvertInt64ToUint64, 0, 1),
			isa.NewTier1Instruction(isa.SubOpAddUint64Constant, 1, 0),
			isa.NewInstruction(isa.OpExtension, 0, 0, 0),
			isa.NewInstruction(isa.OpTruncateToWidth, 1, 32, uint8(isa.RegisterUint64)),
			isa.NewTier1Instruction(isa.SubOpEqualUint64ImmediateThenJumpIfFalse, 1, 7),
			isa.NewInstruction(isa.OpExtension, 2, 0, 0),
			{},
			isa.NewTier1Instruction(isa.SubOpBitAndUint64Constant, 1, 1),
			isa.NewInstruction(isa.OpExtension, 1, 0, 0),
			isa.NewTier1Instruction(isa.SubOpConvertUint64ToInt64, 0, 1),
			isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
		},
		NumRegisters:       [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 2, isa.RegisterUint64: 2},
		ParameterKinds:     []isa.RegisterKind{isa.RegisterInt64},
		ParameterRegisters: []uint8{1},
		ResultKinds:        []isa.RegisterKind{isa.RegisterInt64},
		UintConstants:      []uint64{1013904223, 0xFFFF},
	}
	caller := callerCalling(callee, program.CallSite{
		Arguments: []program.VarLocation{intSlot(2)},
		Returns:   []program.VarLocation{intSlot(0)},
	})
	caller.NumRegisters[isa.RegisterUint64] = 5
	caller.UintConstants = []uint64{99}

	require.NoError(t, RunBytecodeInliner(t.Context(), caller, passes.DefaultOptions()))
	require.Zero(t, callsRemaining(caller), "every operation of the callee is one the description admits")

	add := slices.IndexFunc(caller.Body, func(instruction isa.Instruction) bool {
		return isa.InstrIsTier1SubOp(instruction, isa.SubOpAddUint64Constant)
	})
	require.GreaterOrEqual(t, add, 0)
	require.GreaterOrEqual(t, caller.Body[add].B, uint8(5), "the callee's uint register takes a fresh caller register")
	require.Equal(t, uint64(1013904223), caller.UintConstants[caller.Body[add+1].A],
		"the constant the extension word names is merged into the caller's pool")

	mask := slices.IndexFunc(caller.Body, func(instruction isa.Instruction) bool {
		return isa.InstrIsTier1SubOp(instruction, isa.SubOpBitAndUint64Constant)
	})
	require.GreaterOrEqual(t, mask, 0)
	require.Equal(t, uint64(0xFFFF), caller.UintConstants[caller.Body[mask+1].A])

	truncate := slices.IndexFunc(caller.Body, func(instruction isa.Instruction) bool { return instruction.Op == isa.OpTruncateToWidth })
	require.GreaterOrEqual(t, truncate, 0)
	require.Equal(t, caller.Body[add].B, caller.Body[truncate].A, "a register whose bank the kind marker names is remapped in that bank")
	require.Equal(t, uint8(32), caller.Body[truncate].B, "the width is an immediate and is copied as it stands")

	branch := slices.IndexFunc(caller.Body, func(instruction isa.Instruction) bool {
		return isa.InstrIsTier1SubOp(instruction, isa.SubOpEqualUint64ImmediateThenJumpIfFalse)
	})
	require.GreaterOrEqual(t, branch, 0)
	require.Equal(t, uint8(7), caller.Body[branch].C, "the compared immediate is copied as it stands")
	require.Equal(t, isa.NewInstruction(isa.OpExtension, 2, 0, 0), caller.Body[branch+1],
		"a relative jump offset is copied as it stands, since the whole body moves together")
}

func TestInliningCopiesAGeneralParameterOnlyWhenACallWouldHaveCopiedIt(t *testing.T) {
	t.Parallel()

	pointer := types.NewPointer(types.Typ[types.Int])
	structure := types.NewStruct(nil, nil)
	tests := []struct {
		name      string
		parameter types.Type
		body      []isa.Instruction
		layouts   []program.StructFieldLayout
		wantCopy  bool
	}{
		{
			name:      "a pointer the callee stores through shares the argument's register",
			parameter: pointer,
			body:      []isa.Instruction{isa.NewTier1Instruction(isa.SubOpIncrementStructFieldInt64, 0, 0)},
			layouts:   []program.StructFieldLayout{{}},
		},
		{
			name:      "a struct the callee stores through is copied first",
			parameter: structure,
			body:      []isa.Instruction{isa.NewTier1Instruction(isa.SubOpIncrementStructFieldInt64, 0, 0)},
			layouts:   []program.StructFieldLayout{{}},
			wantCopy:  true,
		},
		{
			name:      "a parameter of unrecorded type is copied first",
			parameter: nil,
			body:      []isa.Instruction{isa.NewTier1Instruction(isa.SubOpIncrementStructFieldInt64, 0, 0)},
			layouts:   []program.StructFieldLayout{{}},
			wantCopy:  true,
		},
		{
			name:      "a pointer whose register the callee overwrites is copied first",
			parameter: pointer,
			body:      []isa.Instruction{isa.NewTier2Instruction(isa.SubOpTier2LoadNil, 0)},
			wantCopy:  true,
		},
		{
			name:      "a struct the callee only reads shares the argument's register",
			parameter: structure,
			body:      []isa.Instruction{isa.NewTier1Instruction(isa.SubOpLengthGeneral, 0, 0)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			callee := &program.CompiledFunction{
				Name:               "touch",
				Body:               append(slices.Clone(tt.body), isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid)),
				NumRegisters:       [isa.NumRegisterKinds]uint32{isa.RegisterGeneral: 1, isa.RegisterInt64: 1},
				ParameterKinds:     []isa.RegisterKind{isa.RegisterGeneral},
				ParameterRegisters: []uint8{0},
				ParameterTypeRefs:  []types.Type{tt.parameter},
				StructLayoutTable:  tt.layouts,
				TypeTable:          []reflect.Type{reflect.TypeFor[int]()},
			}
			caller := callerCalling(callee, program.CallSite{Arguments: []program.VarLocation{generalSlot(3)}})
			caller.NumRegisters[isa.RegisterGeneral] = 4

			require.NoError(t, RunBytecodeInliner(t.Context(), caller, passes.DefaultOptions()))
			require.Zero(t, callsRemaining(caller))

			copied := slices.IndexFunc(caller.Body, func(instruction isa.Instruction) bool {
				return instruction.Op == isa.OpMoveGeneral && instruction.B == 3 && instruction.C == engine.MoveGeneralModeDynamic
			})
			if tt.wantCopy {
				require.GreaterOrEqual(t, copied, 0, "the parameter is seeded by the copy the call would have made")
				return
			}
			require.Negative(t, copied, "the argument's own register stands in for the parameter")
		})
	}
}

func TestInliningMovesAGeneralResultAsAReturnHandsItOver(t *testing.T) {
	t.Parallel()

	callee := &program.CompiledFunction{
		Name: "next",
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpGetStructFieldRawPointer, 0, 0, 0),
			isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
		},
		NumRegisters:       [isa.NumRegisterKinds]uint32{isa.RegisterGeneral: 1},
		ParameterKinds:     []isa.RegisterKind{isa.RegisterGeneral},
		ParameterRegisters: []uint8{0},
		ParameterTypeRefs:  []types.Type{types.NewPointer(types.Typ[types.Int])},
		ResultKinds:        []isa.RegisterKind{isa.RegisterGeneral},
		StructLayoutTable:  []program.StructFieldLayout{{Offset: 8}},
		TypeTable:          []reflect.Type{reflect.TypeFor[int]()},
	}
	caller := callerCalling(callee, program.CallSite{
		Arguments: []program.VarLocation{generalSlot(1)},
		Returns:   []program.VarLocation{generalSlot(2)},
	})
	caller.NumRegisters[isa.RegisterGeneral] = 3
	caller.ResultKinds = []isa.RegisterKind{isa.RegisterGeneral}

	require.NoError(t, RunBytecodeInliner(t.Context(), caller, passes.DefaultOptions()))
	require.Zero(t, callsRemaining(caller), "a callee returning a general value is spliced")

	read := slices.IndexFunc(caller.Body, func(instruction isa.Instruction) bool { return instruction.Op == isa.OpGetStructFieldRawPointer })
	require.GreaterOrEqual(t, read, 0)
	require.Equal(t, uint32(8), caller.StructLayoutTable[caller.Body[read].C].Offset, "the field layout is merged into the caller's table")
	handOver := slices.IndexFunc(caller.Body, func(instruction isa.Instruction) bool {
		return instruction.Op == isa.OpMoveGeneral && instruction.A == 2 && instruction.B == caller.Body[read].A
	})
	require.GreaterOrEqual(t, handOver, 0, "the result reaches the register the call site names")
	require.Equal(t, engine.MoveGeneralModeDynamic, caller.Body[handOver].C,
		"a struct or an array result is copied, so the caller never shares the spliced body's result slot")
}

func TestInliningAdmitsAScalarCallOnlyWhenItsCalleeCannotSeeItsCallers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		arrange    func(leaf *program.CompiledFunction)
		wantInline bool
	}{
		{name: "a leaf that calls nothing", arrange: func(*program.CompiledFunction) {}, wantInline: true},
		{name: "a leaf that inspects the call stack", arrange: func(leaf *program.CompiledFunction) { leaf.InspectsCallStack = true }},
		{name: "a leaf that makes a call whose target is chosen at run time", arrange: func(leaf *program.CompiledFunction) {
			leaf.Body = append([]isa.Instruction{isa.NewTier1Instruction(isa.SubOpCallMethod, 0, 0)}, leaf.Body...)
			leaf.CallSites = []program.CallSite{{IsMethod: true, CachedCallee: doublingCallee()}}
		}},
		{name: "a leaf that calls a function which inspects the call stack", arrange: func(leaf *program.CompiledFunction) {
			inspector := doublingCallee()
			inspector.InspectsCallStack = true
			leaf.Body = append([]isa.Instruction{isa.NewTier1Instruction(isa.SubOpCall, 0, 0)}, leaf.Body...)
			leaf.CallSites = []program.CallSite{{CachedCallee: inspector}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			leaf := doublingCallee()
			leaf.Body = []isa.Instruction{
				isa.NewInstruction(isa.OpAddInt64, 0, 1, 1),
				isa.NewTier1Instruction(isa.SubOpJump, 0, 0),
				isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
			}
			require.True(t, program.SetJumpTarget(leaf.Body, 1, 2))
			tt.arrange(leaf)
			middle := &program.CompiledFunction{
				Name: "middle",
				Body: []isa.Instruction{
					isa.NewTier1Instruction(isa.SubOpCallScalar, 0, 0),
					isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
				},
				NumRegisters:       [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 2},
				ParameterKinds:     []isa.RegisterKind{isa.RegisterInt64},
				ParameterRegisters: []uint8{1},
				ResultKinds:        []isa.RegisterKind{isa.RegisterInt64},
				CallSites: []program.CallSite{{
					FunctionIndex: 1,
					CachedCallee:  leaf,
					Arguments:     []program.VarLocation{intSlot(1)},
					Returns:       []program.VarLocation{intSlot(0)},
				}},
			}

			require.NoError(t, markFunctionsThatCannotObserveCallers(t.Context(), []*program.CompiledFunction{middle}))

			got := phase2OpcodeScan(middle)

			if tt.wantInline {
				require.Equal(t, program.InlineEligible, got)
				caller := callerCalling(middle, program.CallSite{
					Arguments: []program.VarLocation{intSlot(2)},
					Returns:   []program.VarLocation{intSlot(0)},
				})
				caller.Functions = []*program.CompiledFunction{middle, leaf}
				require.NoError(t, RunBytecodeInliner(t.Context(), caller, passes.DefaultOptions()))
				require.Zero(t, callsRemaining(caller), "the function holding the scalar call is spliced")
				return
			}
			require.NotEqual(t, program.InlineEligible, got,
				"splicing the holder would change what the function it calls sees above it")
		})
	}
}

func TestAResultLandsInTheCallersRegisterOnlyWhenNothingCanSeeItEarly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		arrange     func(caller, callee *program.CompiledFunction)
		wantInPlace bool
	}{
		{name: "a plain destination no argument shares", arrange: func(*program.CompiledFunction, *program.CompiledFunction) {}, wantInPlace: true},
		{name: "a destination that is also the argument", arrange: func(caller, _ *program.CompiledFunction) {
			caller.CallSites[0].Arguments = []program.VarLocation{intSlot(0)}
		}},
		{name: "a caller that defers", arrange: func(caller, _ *program.CompiledFunction) {
			caller.Body = append([]isa.Instruction{isa.NewInstruction(isa.OpDefer, 0, 0, 0)}, caller.Body...)
		}},
		{name: "a debug build", arrange: func(caller, _ *program.CompiledFunction) {
			caller.DebugVarTable = &program.DebugVarTable{Entries: nil}
		}},
		{name: "a destination captured by a closure", arrange: func(caller, _ *program.CompiledFunction) {
			caller.CallSites[0].Returns[0].IsCaptured = true
		}},
		{name: "a destination held through a pointer", arrange: func(caller, _ *program.CompiledFunction) {
			caller.CallSites[0].Returns[0].IsIndirect = true
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			callee := doublingCallee()
			caller := callerCalling(callee, program.CallSite{
				Arguments: []program.VarLocation{intSlot(2)},
				Returns:   []program.VarLocation{intSlot(0)},
			})
			tt.arrange(caller, callee)

			require.Equal(t, 1, inlineCallsIn(caller, passes.DefaultOptions()))

			add := slices.IndexFunc(caller.Body, func(instruction isa.Instruction) bool { return instruction.Op == isa.OpAddInt64 })
			require.GreaterOrEqual(t, add, 0)
			handOver := slices.IndexFunc(caller.Body, func(instruction isa.Instruction) bool {
				return isa.InstrIsTier1SubOp(instruction, isa.SubOpMoveInt64) && instruction.B == 0
			})
			if tt.wantInPlace {
				require.Equal(t, uint8(0), caller.Body[add].A, "the body computes into the register the call site names")
				require.Negative(t, handOver, "so nothing is left to move when it returns")
				return
			}
			require.GreaterOrEqual(t, caller.Body[add].A, uint8(4), "the body computes into a slot of its own")
			require.GreaterOrEqual(t, handOver, 0, "and the result is moved across when it returns")
			require.Equal(t, caller.Body[add].A, caller.Body[handOver].C)
		})
	}
}

func TestAGeneralResultLandsInPlaceOnlyWhenItCanBeNothingButAReference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		result      types.Type
		wantInPlace bool
	}{
		{name: "a pointer", result: types.NewPointer(types.Typ[types.Int]), wantInPlace: true},
		{name: "a map", result: types.NewMap(types.Typ[types.String], types.Typ[types.Int]), wantInPlace: true},
		{name: "a struct", result: types.NewStruct(nil, nil)},
		{name: "an array", result: types.NewArray(types.Typ[types.Int], 4)},
		{name: "an interface, which may hold either", result: types.NewInterfaceType(nil, nil)},
		{name: "a result of unrecorded type", result: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			callee := &program.CompiledFunction{
				Name: "next",
				Body: []isa.Instruction{
					isa.NewInstruction(isa.OpGetStructFieldRawPointer, 0, 0, 0),
					isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
				},
				NumRegisters:       [isa.NumRegisterKinds]uint32{isa.RegisterGeneral: 1},
				ParameterKinds:     []isa.RegisterKind{isa.RegisterGeneral},
				ParameterRegisters: []uint8{0},
				ParameterTypeRefs:  []types.Type{types.NewPointer(types.Typ[types.Int])},
				ResultKinds:        []isa.RegisterKind{isa.RegisterGeneral},
				ResultTypeRefs:     []types.Type{tt.result},
				StructLayoutTable:  []program.StructFieldLayout{{}},
				TypeTable:          []reflect.Type{reflect.TypeFor[int]()},
			}
			caller := callerCalling(callee, program.CallSite{
				Arguments: []program.VarLocation{generalSlot(1)},
				Returns:   []program.VarLocation{generalSlot(2)},
			})
			caller.NumRegisters[isa.RegisterGeneral] = 3

			require.Equal(t, 1, inlineCallsIn(caller, passes.DefaultOptions()))

			read := slices.IndexFunc(caller.Body, func(instruction isa.Instruction) bool { return instruction.Op == isa.OpGetStructFieldRawPointer })
			require.GreaterOrEqual(t, read, 0)
			copied := slices.IndexFunc(caller.Body[read:], func(instruction isa.Instruction) bool {
				return instruction.Op == isa.OpMoveGeneral && instruction.A == 2 && instruction.C == engine.MoveGeneralModeDynamic
			})
			if tt.wantInPlace {
				require.Equal(t, uint8(2), caller.Body[read].A, "a reference is the same value wherever it is held")
				require.Negative(t, copied, "nothing is moved once the body has run")
				return
			}
			require.NotEqual(t, uint8(2), caller.Body[read].A)
			require.GreaterOrEqual(t, copied, 0, "a value that may be a struct or an array is handed over as a copy")
		})
	}
}

func TestAResultSlotSharedWithAWrittenParameterIsSeededInTheCallersRegister(t *testing.T) {
	t.Parallel()

	callee := &program.CompiledFunction{
		Name: "twice",
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpAddInt64, 0, 0, 0),
			isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
		},
		NumRegisters:       [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 1},
		ParameterKinds:     []isa.RegisterKind{isa.RegisterInt64},
		ParameterRegisters: []uint8{0},
		ResultKinds:        []isa.RegisterKind{isa.RegisterInt64},
	}
	caller := callerCalling(callee, program.CallSite{
		Arguments: []program.VarLocation{intSlot(2)},
		Returns:   []program.VarLocation{intSlot(3)},
	})

	require.Equal(t, 1, inlineCallsIn(caller, passes.DefaultOptions()))

	seed := slices.IndexFunc(caller.Body, func(instruction isa.Instruction) bool {
		return isa.InstrIsTier1SubOp(instruction, isa.SubOpMoveInt64) && instruction.B == 3 && instruction.C == 2
	})
	require.GreaterOrEqual(t, seed, 0, "the copy of the argument seeds the register the result is wanted in")
	add := slices.IndexFunc(caller.Body, func(instruction isa.Instruction) bool { return instruction.Op == isa.OpAddInt64 })
	require.Greater(t, add, seed)
	require.Equal(t, isa.NewInstruction(isa.OpAddInt64, 3, 3, 3), caller.Body[add], "and the body works there, leaving the argument as it was")
	moves := 0
	for _, instruction := range caller.Body {
		if isa.InstrIsTier1SubOp(instruction, isa.SubOpMoveInt64) {
			moves++
		}
	}
	require.Equal(t, 1, moves, "no second move hands the result over")
}

func TestFunctionsAreMarkedByWhetherTheyCanObserveTheirCallers(t *testing.T) {
	t.Parallel()

	functions := callGraphOf(6, [][2]int{{0, 1}, {1, 2}, {3, 4}, {4, 3}, {5, -1}})
	functions[2].InspectsCallStack = true
	outside := &program.CompiledFunction{}

	require.NoError(t, markFunctionsThatCannotObserveCallers(t.Context(), functions))

	require.False(t, functions[2].CannotObserveCallers, "it inspects the call stack itself")
	require.False(t, functions[1].CannotObserveCallers, "it calls a function that does")
	require.False(t, functions[0].CannotObserveCallers, "it reaches one through another")
	require.True(t, functions[3].CannotObserveCallers, "two functions that only call each other observe nothing")
	require.True(t, functions[4].CannotObserveCallers)
	require.False(t, functions[5].CannotObserveCallers, "a call that names no callee may arrive anywhere")
	require.False(t, outside.CannotObserveCallers, "a function no run has read is taken to observe its callers")
}

func TestEverySpliceableOperationDescribesItsExtensionWordsForEveryBank(t *testing.T) {
	t.Parallel()

	words := []isa.Instruction{isa.NewInstruction(isa.OpExtension, 1, 2, 3), isa.NewInstruction(isa.OpExtension, 1, 2, 3)}
	var undescribed []string
	for _, row := range isa.AllSpecs() {
		instruction := instructionForSpec(row)
		if !phase2OperationAllowed(instruction) || isa.ShapeForInstruction(instruction).Flags&isa.ShapeFlagFollowsExtension == 0 {
			continue
		}
		for kind := range isa.RegisterKind(isa.NumRegisterKinds) {
			if _, _, described := isa.ExtensionRegisterUse(instruction, words[:1], kind); !described {
				undescribed = append(undescribed, row.Name+" in bank "+kind.String())
			}
		}
	}

	require.Emptyf(t, undescribed,
		"a callee holding one of these has every parameter copied before it is spliced, because what\n"+
			"its extension word writes is not known. Describe the word in isa.ExtensionRegisterUse: %v", undescribed)
}
