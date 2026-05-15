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

	"pipit.sh/pipit/internal/compile/inline"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/isa"
)

func makeDoubleCallee() *program.CompiledFunction {
	return &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpAddInt, 0, 0, 0),
			isa.NewInstruction(
				isa.OpDrillTier1,
				byte(isa.SubOpDrillTier2),
				byte(isa.SubOpTier2Return),
				1,
			),
		},
		ParameterKinds: []isa.RegisterKind{isa.RegisterInt},
		ResultKinds:    []isa.RegisterKind{isa.RegisterInt},
		NumRegisters:   [isa.NumRegisterKinds]uint32{isa.RegisterInt: 1},
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
			isa.NewInstruction(isa.OpNop, 0, 0, 0),
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
	if !result.Spliced {
		t.Fatalf("splice failed: %v", result.Reason)
	}

	if len(caller.Body) != 5 {
		t.Fatalf("body length %d want 5", len(caller.Body))
	}
	got := caller.Body[0]
	if got.Op != isa.OpDrillTier1 || isa.SubOpcode(got.A) != isa.SubOpJump || program.DecodeJumpOffset(got) != 2 {
		t.Fatalf("pc 0: want subOpJump offset=2, got op=%v a=%v offset=%d",
			got.Op, got.A, program.DecodeJumpOffset(got))
	}
	got = caller.Body[2]
	if got.Op != isa.OpDrillTier1 || isa.SubOpcode(got.A) != isa.SubOpJump || program.DecodeJumpOffset(got) != 2 {
		t.Fatalf("pc 2 (skip-jump): want subOpJump offset=2, got op=%v a=%v offset=%d",
			got.Op, got.A, program.DecodeJumpOffset(got))
	}
	got = caller.Body[3]
	if got.Op != isa.OpDrillTier1 || isa.SubOpcode(got.A) != isa.SubOpJump || program.DecodeJumpOffset(got) != 0 {
		t.Fatalf("pc 3 (return → retPrep): want subOpJump offset=0, got op=%v a=%v offset=%d",
			got.Op, got.A, program.DecodeJumpOffset(got))
	}
	got = caller.Body[4]
	if got.Op != isa.OpDrillTier1 || isa.SubOpcode(got.A) != isa.SubOpJump || program.DecodeJumpOffset(got) != -4 {
		t.Fatalf("pc 4 (retPrep back-jump): want subOpJump offset=-4, got op=%v a=%v offset=%d",
			got.Op, got.A, program.DecodeJumpOffset(got))
	}
}

func TestSplice_DoubleCallee_ValueReturn(t *testing.T) {
	t.Parallel()
	callee := makeDoubleCallee()
	siteIdx := uint16(0)
	caller := &program.CompiledFunction{
		Body: []isa.Instruction{
			makeOpCallSlot(siteIdx),
			isa.NewInstruction(isa.OpNop, 0, 0, 0),
		},
		CallSites: []program.CallSite{
			{
				CachedCallee: callee,
				Arguments:    []program.VarLocation{{Register: 5, Kind: isa.RegisterInt}},
				Returns:      []program.VarLocation{{Register: 6, Kind: isa.RegisterInt}},
			},
		},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt: 7},
	}
	result := inline.TrySpliceCall(caller, siteIdx)
	if !result.Spliced {
		t.Fatalf("splice failed: %v", result.Reason)
	}

	if len(caller.Body) != 8 {
		t.Fatalf("body length %d want 8; body=%+v", len(caller.Body), caller.Body)
	}
	if caller.NumRegisters[isa.RegisterInt] != 8 {
		t.Fatalf("numRegisters[int] %d want 8 (caller's 7 + 1 fresh param slot)",
			caller.NumRegisters[isa.RegisterInt])
	}

	got := caller.Body[3]
	if got.Op != isa.OpDrillTier1 || isa.SubOpcode(got.A) != isa.SubOpMoveInt {
		t.Fatalf("pc 3 want pre-copy subOpMoveInt, got op=%v a=%v", got.Op, got.A)
	}
	if got.B != 7 || got.C != 5 {
		t.Fatalf("pc 3 (pre-copy) want destination=7 source=5, got b=%d c=%d", got.B, got.C)
	}

	got = caller.Body[4]
	if got.Op != isa.OpAddInt || got.A != 7 || got.B != 7 || got.C != 7 {
		t.Fatalf("pc 4 want opAddInt 7,7,7 (fresh slot), got op=%v a=%v b=%v c=%v",
			got.Op, got.A, got.B, got.C)
	}

	got = caller.Body[6]
	if got.Op != isa.OpDrillTier1 || isa.SubOpcode(got.A) != isa.SubOpMoveInt {
		t.Fatalf("pc 6 want subOpMoveInt, got op=%v a=%v", got.Op, got.A)
	}
	if got.B != 6 || got.C != 7 {
		t.Fatalf("pc 6 (return move) want destination=6 source=7, got b=%d c=%d", got.B, got.C)
	}
	got = caller.Body[7]
	if got.Op != isa.OpDrillTier1 || isa.SubOpcode(got.A) != isa.SubOpJump || program.DecodeJumpOffset(got) != -7 {
		t.Fatalf("pc 7 want subOpJump offset=-7, got op=%v a=%v offset=%d",
			got.Op, got.A, program.DecodeJumpOffset(got))
	}
}

func TestSplice_RemapsLocalRegisters(t *testing.T) {
	t.Parallel()
	callee := &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpAddInt, 0, 0, 0),
			isa.NewInstruction(
				isa.OpDrillTier1,
				byte(isa.SubOpDrillTier2),
				byte(isa.SubOpTier2DrillTier3),
				byte(isa.SubOpTier3ReturnVoid),
			),
		},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt: 1},
	}
	caller := &program.CompiledFunction{
		Body: []isa.Instruction{
			makeOpCallSlot(0),
			isa.NewInstruction(isa.OpNop, 0, 0, 0),
		},
		CallSites: []program.CallSite{
			{CachedCallee: callee},
		},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt: 4},
	}
	result := inline.TrySpliceCall(caller, 0)
	if !result.Spliced {
		t.Fatalf("splice failed: %v", result.Reason)
	}

	if caller.NumRegisters[isa.RegisterInt] != 5 {
		t.Fatalf("caller numRegisters[int] = %d want 5", caller.NumRegisters[isa.RegisterInt])
	}

	got := caller.Body[3]
	if got.Op != isa.OpAddInt || got.A != 4 || got.B != 4 || got.C != 4 {
		t.Fatalf("appended opAddInt = {a:%d b:%d c:%d} want {4,4,4}", got.A, got.B, got.C)
	}
}

func TestSplice_LoadIntConst_MergesPool(t *testing.T) {
	t.Parallel()
	callee := &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpLoadIntConst, 0, 0, 0),
			isa.NewInstruction(
				isa.OpDrillTier1,
				byte(isa.SubOpDrillTier2),
				byte(isa.SubOpTier2DrillTier3),
				byte(isa.SubOpTier3ReturnVoid),
			),
		},
		IntConstants: []int64{7},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt: 1},
	}
	caller := &program.CompiledFunction{
		Body: []isa.Instruction{
			makeOpCallSlot(0),
			isa.NewInstruction(isa.OpNop, 0, 0, 0),
		},
		CallSites: []program.CallSite{
			{CachedCallee: callee},
		},
		IntConstants: []int64{99},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt: 3},
	}
	result := inline.TrySpliceCall(caller, 0)
	if !result.Spliced {
		t.Fatalf("Phase 3 splice with constant pool failed: %v", result.Reason)
	}

	if len(caller.IntConstants) != 2 || caller.IntConstants[1] != 7 {
		t.Fatalf("intConstants %v want [99 7]", caller.IntConstants)
	}

	got := caller.Body[3]
	if got.Op != isa.OpLoadIntConst {
		t.Fatalf("pc 3 op = %v want opLoadIntConst", got.Op)
	}
	if got.A != 3 {
		t.Fatalf("pc 3 destination reg %d want 3 (remapped local)", got.A)
	}
	index := uint16(got.B) | uint16(got.C)<<8
	if index != 1 {
		t.Fatalf("pc 3 const index %d want 1 (after pool merge)", index)
	}
}

func TestSplice_InternalJumpPreserved(t *testing.T) {
	t.Parallel()
	lo, hi := inline.SplitJumpOffsetBytes(1)
	callee := &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpJumpIfFalse, 0, lo, hi),
			isa.NewInstruction(isa.OpAddInt, 0, 0, 0),
			isa.NewInstruction(
				isa.OpDrillTier1,
				byte(isa.SubOpDrillTier2),
				byte(isa.SubOpTier2DrillTier3),
				byte(isa.SubOpTier3ReturnVoid),
			),
		},
		ParameterKinds: []isa.RegisterKind{isa.RegisterInt},
		NumRegisters:   [isa.NumRegisterKinds]uint32{isa.RegisterInt: 1},
	}
	caller := &program.CompiledFunction{
		Body: []isa.Instruction{
			makeOpCallSlot(0),
			isa.NewInstruction(isa.OpNop, 0, 0, 0),
		},
		CallSites: []program.CallSite{
			{
				CachedCallee: callee,
				Arguments:    []program.VarLocation{{Register: 3, Kind: isa.RegisterInt}},
			},
		},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt: 4},
	}
	result := inline.TrySpliceCall(caller, 0)
	if !result.Spliced {
		t.Fatalf("splice failed: %v", result.Reason)
	}

	got := caller.Body[4]
	if got.Op != isa.OpJumpIfFalse {
		t.Fatalf("pc 4 op = %v want opJumpIfFalse", got.Op)
	}
	if got.A != 4 {
		t.Fatalf("pc 4 cond reg %d want 4 (remapped to fresh param slot)", got.A)
	}
	if decoded := program.DecodeJumpOffset(got); decoded != 1 {
		t.Fatalf("pc 4 jump offset %d want 1 (preserved)", decoded)
	}

	pre := caller.Body[3]
	if pre.Op != isa.OpDrillTier1 || isa.SubOpcode(pre.A) != isa.SubOpMoveInt {
		t.Fatalf("pc 3 want pre-copy subOpMoveInt, got op=%v a=%v", pre.Op, pre.A)
	}
	if pre.B != 4 || pre.C != 3 {
		t.Fatalf("pc 3 (pre-copy) want destination=4 source=3, got b=%d c=%d", pre.B, pre.C)
	}
}

func TestSplice_CalleeWithCallBoolConstAndFusedCompare(t *testing.T) {
	t.Parallel()
	leaf := makeDoubleCallee()
	lo, hi := isa.SplitOffset(1)
	callee := &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewTier1Instruction(isa.SubOpLoadBoolConst, 0, 0),
			isa.NewTier1Instruction(isa.SubOpLtIntJumpFalse, 0, 1),
			isa.NewInstruction(isa.OpExt, lo, hi, 0),
			makeOpCallSlot(0),
			isa.NewTier1Instruction(isa.SubOpMoveInt, 0, 2),
			isa.NewInstruction(isa.OpDrillTier1, byte(isa.SubOpDrillTier2), byte(isa.SubOpTier2Return), 1),
		},
		ParameterKinds:     []isa.RegisterKind{isa.RegisterInt, isa.RegisterInt},
		ParameterRegisters: []uint8{0, 1},
		ResultKinds:        []isa.RegisterKind{isa.RegisterInt},
		NumRegisters:       [isa.NumRegisterKinds]uint32{isa.RegisterInt: 3, isa.RegisterBool: 1},
		BoolConstants:      []bool{true},
		CallSites: []program.CallSite{{
			CachedCallee: leaf,
			Arguments:    []program.VarLocation{{Register: 0, Kind: isa.RegisterInt}},
			Returns:      []program.VarLocation{{Register: 2, Kind: isa.RegisterInt}},
		}},
	}
	caller := &program.CompiledFunction{
		Body: []isa.Instruction{
			makeOpCallSlot(0),
			isa.NewInstruction(isa.OpNop, 0, 0, 0),
		},
		CallSites: []program.CallSite{{
			CachedCallee: callee,
			Arguments:    []program.VarLocation{{Register: 5, Kind: isa.RegisterInt}, {Register: 6, Kind: isa.RegisterInt}},
			Returns:      []program.VarLocation{{Register: 7, Kind: isa.RegisterInt}},
		}},
		NumRegisters:  [isa.NumRegisterKinds]uint32{isa.RegisterInt: 8, isa.RegisterBool: 2},
		BoolConstants: []bool{false},
	}

	result := inline.TrySpliceCall(caller, 0)
	if !result.Spliced {
		t.Fatalf("splice failed: %v", result.Reason)
	}

	var boolLoad, fused, ext, call isa.Instruction
	var found int
	for i, instr := range caller.Body {
		switch {
		case isa.InstrIsTier1SubOp(instr, isa.SubOpLoadBoolConst):
			boolLoad = instr
			found++
		case isa.InstrIsTier1SubOp(instr, isa.SubOpLtIntJumpFalse):
			fused = instr
			ext = caller.Body[i+1]
			found++
		case isa.InstrIsTier1SubOp(instr, isa.SubOpCall) && instr.WideIndex() != 0:
			call = instr
			found++
		}
	}
	if found != 3 {
		t.Fatalf("expected the bool load, the fused compare and the inner call in the spliced body, found %d", found)
	}
	if boolLoad.C != 1 || caller.BoolConstants[1] != true {
		t.Fatalf("bool constant index: want 1 pointing at true, got C=%d pool=%v", boolLoad.C, caller.BoolConstants)
	}
	if boolLoad.B < 2 {
		t.Fatalf("bool destination should be a fresh caller slot at or above 2, got %d", boolLoad.B)
	}

	if fused.B < 8 || fused.C != 6 {
		t.Fatalf("fused compare should read a fresh copy of the first parameter and argument register 6, got B=%d C=%d", fused.B, fused.C)
	}
	if ext.Op != isa.OpExt || ext.A != lo || ext.B != hi {
		t.Fatalf("offset word should be copied verbatim, got %v", ext)
	}
	if int(call.WideIndex()) != 1 || len(caller.CallSites) != 2 {
		t.Fatalf("inner call should index the appended site 1, got %d with %d sites", call.WideIndex(), len(caller.CallSites))
	}
	site := caller.CallSites[1]
	if site.CachedCallee != leaf || len(site.Arguments) != 1 || site.Arguments[0].Register != fused.B {
		t.Fatalf("appended site should keep the leaf callee and read the remapped first parameter %d, got %+v", fused.B, site)
	}
	if len(site.Returns) != 1 || site.Returns[0].Register < 8 {
		t.Fatalf("appended site should write a fresh caller slot at or above 8, got %+v", site.Returns)
	}
}
