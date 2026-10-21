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

//go:build integration

package bytecode_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/compile/inline"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func makeDoubleCallee() *program.CompiledFunction {
	return &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpAddInt64, 0, 0, 0),
			isa.NewInstruction(
				isa.OpDrillTier1,
				byte(isa.SubOpDrillTier2),
				byte(isa.SubOpTier2Return),
				1,
			),
		},
		ParameterKinds: []isa.RegisterKind{isa.RegisterInt64},
		ResultKinds:    []isa.RegisterKind{isa.RegisterInt64},
		NumRegisters:   [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 1},
	}
}

func makeVoidNoopCallee() *program.CompiledFunction {
	return &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewInstruction(
				isa.OpDrillTier1,
				byte(isa.SubOpDrillTier2),
				byte(isa.SubOpTier2DrillTier3),
				byte(isa.SubOpTier3ReturnVoid),
			),
		},
		NumRegisters: [isa.NumRegisterKinds]uint32{},
	}
}

func makeOpCallSlot(siteIdx uint16) isa.Instruction {
	return isa.NewTier1Instruction(isa.SubOpCall, byte(siteIdx&0xFF), byte(siteIdx>>8))
}

func TestSplice_VoidNoop_ReplacesOpCallWithJump(t *testing.T) {
	t.Parallel()
	callee := makeVoidNoopCallee()
	siteIdx := uint16(0)
	caller := &program.CompiledFunction{
		Body: []isa.Instruction{
			makeOpCallSlot(siteIdx),
			isa.NewInstruction(isa.OpNoOperation, 0, 0, 0),
		},
		CallSites: []program.CallSite{
			{
				CachedCallee: callee,
				Arguments:    nil,
				Returns:      nil,
			},
		},
	}
	result := inline.TrySpliceCall(caller, siteIdx)
	require.True(t, result.Spliced, "the splice succeeds: %v", result.Reason)

	require.Len(t, caller.Body, 5)
	got := caller.Body[0]
	require.Equal(t, isa.OpDrillTier1, got.Op, "pc 0 jumps by 2: %v", got.A)
	require.Equal(t, isa.SubOpJump, isa.SubOpcode(got.A))
	require.Equal(t, 2, program.DecodeJumpOffset(got))
	got = caller.Body[2]
	require.Equal(t, isa.OpDrillTier1, got.Op, "pc 2, the skip jump, jumps by 2: %v", got.A)
	require.Equal(t, isa.SubOpJump, isa.SubOpcode(got.A))
	require.Equal(t, 2, program.DecodeJumpOffset(got))
	got = caller.Body[3]
	require.Equal(t, isa.OpDrillTier1, got.Op, "pc 3, the return to return-prep, jumps by 0: %v", got.A)
	require.Equal(t, isa.SubOpJump, isa.SubOpcode(got.A))
	require.Equal(t, 0, program.DecodeJumpOffset(got))
	got = caller.Body[4]
	require.Equal(t, isa.OpDrillTier1, got.Op, "pc 4, the return-prep back jump, jumps by -4: %v", got.A)
	require.Equal(t, isa.SubOpJump, isa.SubOpcode(got.A))
	require.Equal(t, -4, program.DecodeJumpOffset(got))
}

func TestSplice_DoubleCallee_ValueReturn(t *testing.T) {
	t.Parallel()
	callee := makeDoubleCallee()
	siteIdx := uint16(0)
	caller := &program.CompiledFunction{
		Body: []isa.Instruction{
			makeOpCallSlot(siteIdx),
			isa.NewInstruction(isa.OpNoOperation, 0, 0, 0),
		},
		CallSites: []program.CallSite{
			{
				CachedCallee: callee,
				Arguments:    []program.VarLocation{{Register: 5, Kind: isa.RegisterInt64}},
				Returns:      []program.VarLocation{{Register: 6, Kind: isa.RegisterInt64}},
			},
		},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 7},
	}
	result := inline.TrySpliceCall(caller, siteIdx)
	require.True(t, result.Spliced, "the splice succeeds: %v", result.Reason)

	require.Len(t, caller.Body, 7, "the result slot is the caller's own register 6, so no move hands the result over")
	require.Equal(t, uint32(8), caller.NumRegisters[isa.RegisterInt64])

	got := caller.Body[3]
	require.Equal(t, isa.OpDrillTier1, got.Op, "pc 3 is the pre-copy MOVE_INT64: %v", got.A)
	require.Equal(t, isa.SubOpMoveInt64, isa.SubOpcode(got.A))
	require.Equal(t, uint8(6), got.B, "pc 3, the pre-copy, seeds the result's register: 6 <- 5")
	require.Equal(t, uint8(5), got.C)

	got = caller.Body[4]
	require.Equal(t, isa.OpAddInt64, got.Op, "pc 4 is ADD_INT64 6,6,6, leaving argument register 5 as it was")
	require.Equal(t, uint8(6), got.A)
	require.Equal(t, uint8(6), got.B)
	require.Equal(t, uint8(6), got.C)

	got = caller.Body[6]
	require.Equal(t, isa.OpDrillTier1, got.Op, "pc 6 is a jump by -6: %v", got.A)
	require.Equal(t, isa.SubOpJump, isa.SubOpcode(got.A))
	require.Equal(t, -6, program.DecodeJumpOffset(got))
}

func TestSplice_RemapsLocalRegisters(t *testing.T) {
	t.Parallel()
	callee := &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpAddInt64, 0, 0, 0),
			isa.NewInstruction(
				isa.OpDrillTier1,
				byte(isa.SubOpDrillTier2),
				byte(isa.SubOpTier2DrillTier3),
				byte(isa.SubOpTier3ReturnVoid),
			),
		},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 1},
	}
	caller := &program.CompiledFunction{
		Body: []isa.Instruction{
			makeOpCallSlot(0),
			isa.NewInstruction(isa.OpNoOperation, 0, 0, 0),
		},
		CallSites: []program.CallSite{
			{CachedCallee: callee},
		},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 4},
	}
	result := inline.TrySpliceCall(caller, 0)
	require.True(t, result.Spliced, "the splice succeeds: %v", result.Reason)

	require.Equal(t, uint32(5), caller.NumRegisters[isa.RegisterInt64])

	got := caller.Body[3]
	require.Equal(t, isa.OpAddInt64, got.Op, "the appended ADD_INT64 is 4,4,4")
	require.Equal(t, uint8(4), got.A)
	require.Equal(t, uint8(4), got.B)
	require.Equal(t, uint8(4), got.C)
}

func TestSplice_LoadIntConst_MergesPool(t *testing.T) {
	t.Parallel()
	callee := &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpLoadInt64Constant, 0, 0, 0),
			isa.NewInstruction(
				isa.OpDrillTier1,
				byte(isa.SubOpDrillTier2),
				byte(isa.SubOpTier2DrillTier3),
				byte(isa.SubOpTier3ReturnVoid),
			),
		},
		IntConstants: []int64{7},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 1},
	}
	caller := &program.CompiledFunction{
		Body: []isa.Instruction{
			makeOpCallSlot(0),
			isa.NewInstruction(isa.OpNoOperation, 0, 0, 0),
		},
		CallSites: []program.CallSite{
			{CachedCallee: callee},
		},
		IntConstants: []int64{99},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 3},
	}
	result := inline.TrySpliceCall(caller, 0)
	require.True(t, result.Spliced, "the phase 3 splice with a constant pool succeeds: %v", result.Reason)

	require.Len(t, caller.IntConstants, 2)
	require.Equal(t, int64(7), caller.IntConstants[1])

	got := caller.Body[3]
	require.Equal(t, isa.OpLoadInt64Constant, got.Op)
	require.Equal(t, uint8(3), got.A)
	index := uint16(got.B) | uint16(got.C)<<8
	require.Equal(t, uint16(1), index)
}

func TestSplice_InternalJumpPreserved(t *testing.T) {
	t.Parallel()
	lo, hi := inline.SplitJumpOffsetBytes(1)
	callee := &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpJumpIfFalse, 0, lo, hi),
			isa.NewInstruction(isa.OpAddInt64, 0, 0, 0),
			isa.NewInstruction(
				isa.OpDrillTier1,
				byte(isa.SubOpDrillTier2),
				byte(isa.SubOpTier2DrillTier3),
				byte(isa.SubOpTier3ReturnVoid),
			),
		},
		ParameterKinds: []isa.RegisterKind{isa.RegisterInt64},
		NumRegisters:   [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 1},
	}
	caller := &program.CompiledFunction{
		Body: []isa.Instruction{
			makeOpCallSlot(0),
			isa.NewInstruction(isa.OpNoOperation, 0, 0, 0),
		},
		CallSites: []program.CallSite{
			{
				CachedCallee: callee,
				Arguments:    []program.VarLocation{{Register: 3, Kind: isa.RegisterInt64}},
			},
		},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 4},
	}
	result := inline.TrySpliceCall(caller, 0)
	require.True(t, result.Spliced, "the splice succeeds: %v", result.Reason)

	got := caller.Body[4]
	require.Equal(t, isa.OpJumpIfFalse, got.Op)
	require.Equal(t, uint8(4), got.A)
	require.Equal(t, 1, program.DecodeJumpOffset(got))

	pre := caller.Body[3]
	require.Equal(t, isa.OpDrillTier1, pre.Op, "pc 3 is the pre-copy MOVE_INT64: %v", pre.A)
	require.Equal(t, isa.SubOpMoveInt64, isa.SubOpcode(pre.A))
	require.Equal(t, uint8(4), pre.B, "pc 3, the pre-copy, is 4 <- 3")
	require.Equal(t, uint8(3), pre.C)
}

func TestSplice_CalleeWithCallBoolConstAndFusedCompare(t *testing.T) {
	t.Parallel()
	leaf := makeDoubleCallee()
	lo, hi := isa.SplitOffset(1)
	callee := &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewTier1Instruction(isa.SubOpLoadBoolConstant, 0, 0),
			isa.NewTier1Instruction(isa.SubOpLessThanInt64ThenJumpIfFalse, 0, 1),
			isa.NewInstruction(isa.OpExtension, lo, hi, 0),
			makeOpCallSlot(0),
			isa.NewTier1Instruction(isa.SubOpMoveInt64, 0, 2),
			isa.NewInstruction(isa.OpDrillTier1, byte(isa.SubOpDrillTier2), byte(isa.SubOpTier2Return), 1),
		},
		ParameterKinds:     []isa.RegisterKind{isa.RegisterInt64, isa.RegisterInt64},
		ParameterRegisters: []uint8{0, 1},
		ResultKinds:        []isa.RegisterKind{isa.RegisterInt64},
		NumRegisters:       [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 3, isa.RegisterBool: 1},
		BoolConstants:      []bool{true},
		CallSites: []program.CallSite{{
			CachedCallee: leaf,
			Arguments:    []program.VarLocation{{Register: 0, Kind: isa.RegisterInt64}},
			Returns:      []program.VarLocation{{Register: 2, Kind: isa.RegisterInt64}},
		}},
	}
	caller := &program.CompiledFunction{
		Body: []isa.Instruction{
			makeOpCallSlot(0),
			isa.NewInstruction(isa.OpNoOperation, 0, 0, 0),
		},
		CallSites: []program.CallSite{{
			CachedCallee: callee,
			Arguments:    []program.VarLocation{{Register: 5, Kind: isa.RegisterInt64}, {Register: 6, Kind: isa.RegisterInt64}},
			Returns:      []program.VarLocation{{Register: 7, Kind: isa.RegisterInt64}},
		}},
		NumRegisters:  [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 8, isa.RegisterBool: 2},
		BoolConstants: []bool{false},
	}

	result := inline.TrySpliceCall(caller, 0)
	require.True(t, result.Spliced, "the splice succeeds: %v", result.Reason)

	var boolLoad, fused, ext, call isa.Instruction
	var found int
	for i, instr := range caller.Body {
		switch {
		case isa.InstrIsTier1SubOp(instr, isa.SubOpLoadBoolConstant):
			boolLoad = instr
			found++
		case isa.InstrIsTier1SubOp(instr, isa.SubOpLessThanInt64ThenJumpIfFalse):
			fused = instr
			ext = caller.Body[i+1]
			found++
		case isa.InstrIsTier1SubOp(instr, isa.SubOpCall) && instr.WideIndex() != 0:
			call = instr
			found++
		}
	}
	require.Equal(t, 3, found, "the spliced body holds the bool load, the fused compare and the inner call")
	require.Equal(t, uint8(1), boolLoad.C, "the bool load reads constant 1")
	require.True(t, caller.BoolConstants[1], "constant 1 is true: %v", caller.BoolConstants)
	require.GreaterOrEqual(t, boolLoad.B, uint8(2), "the bool destination is a fresh caller slot at or above 2")

	require.Equal(t, uint8(7), fused.B,
		"the fused compare reads the copy of the first parameter, which is seeded in the result's register, and argument register 6")
	require.Equal(t, uint8(6), fused.C)
	require.Equal(t, isa.OpExtension, ext.Op, "the offset word is copied verbatim: %v", ext)
	require.Equal(t, lo, ext.A)
	require.Equal(t, hi, ext.B)
	require.Equal(t, 1, int(call.WideIndex()), "the inner call indexes the appended site 1")
	require.Len(t, caller.CallSites, 2)
	site := caller.CallSites[1]
	require.Same(t, leaf, site.CachedCallee, "the appended site keeps the leaf callee and reads the remapped first parameter: %+v", site)
	require.Len(t, site.Arguments, 1)
	require.Equal(t, fused.B, site.Arguments[0].Register)
	require.Len(t, site.Returns, 1, "the appended site writes a fresh caller slot at or above 8")
	require.GreaterOrEqual(t, site.Returns[0].Register, uint8(8))
}
