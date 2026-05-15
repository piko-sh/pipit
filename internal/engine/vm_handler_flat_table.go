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

//go:generate go run gen_opcode_tables.go
//go:generate go run gen_flat_switch.go

package engine

import (
	"fmt"

	"pipit.sh/pipit/internal/isa"
)

const (
	// flatHandlerTier1Base is the start of the tier-1 region in the 1024-slot flat dispatch
	// space. See flatDispatchIndex and the generated flatDispatchSwitch in
	// vm_handler_flat_switch.go for how the layout is consumed.
	flatHandlerTier1Base = 256

	// flatHandlerTier2Base is the start of the tier-2 region.
	flatHandlerTier2Base = 512

	// flatHandlerTier3Base is the start of the tier-3 region.
	flatHandlerTier3Base = 768
)

// handleFlatUnknownTier1 is the flat-dispatch arm for an unrecognised tier-1 sub-op.
//
// Reports the offending sub-op A and panics.
//
// Takes vm (*VM) which receives the eval error.
// Takes instr (instruction) which carries the offending sub-op.
//
// Returns OpResult which is opPanicError.
func handleFlatUnknownTier1(vm *VM, _ *CallFrame, _ *Registers, instr isa.Instruction) OpResult {
	vm.evalError = fmt.Errorf("invalid umbrella sub-op: %d", instr.A)
	return opPanicError
}

// handleFlatUnknownTier2 mirrors dispatchTier2's default arm.
//
// Reports the offending sub-op B and panics.
//
// Takes vm (*VM) which receives the eval error.
// Takes instr (instruction) which carries the offending sub-op.
//
// Returns OpResult which is opPanicError.
func handleFlatUnknownTier2(vm *VM, _ *CallFrame, _ *Registers, instr isa.Instruction) OpResult {
	vm.evalError = fmt.Errorf("invalid tier-2 sub-op: %d", instr.B)
	return opPanicError
}

// handleFlatUnknownTier3 mirrors dispatchTier3's default arm.
//
// Reports the offending sub-op C and panics.
//
// Takes vm (*VM) which receives the eval error.
// Takes instr (instruction) which carries the offending sub-op.
//
// Returns OpResult which is opPanicError.
func handleFlatUnknownTier3(vm *VM, _ *CallFrame, _ *Registers, instr isa.Instruction) OpResult {
	vm.evalError = fmt.Errorf("invalid tier-3 sub-op: %d", instr.C)
	return opPanicError
}

// flatDispatchIndex computes the 0..1023 slot in flatDispatchSwitch (and the ASM-side
// flatJumpTable) that should handle the given instruction.
//
// The first non-zero byte selects the tier (op=tier-0, a=tier-1, b=tier-2, c=tier-3) and
// its value is the sub-op id. The tier is an encoding choice driven by operand count, not
// dispatch speed. All tiers land in the same 1024-arm switch.
//
// Takes instr (instruction) which is the 4-byte word to classify.
//
// Returns the flat-table index in the range [0, 1023].
func flatDispatchIndex(instr isa.Instruction) uint {
	if instr.Op != 0 {
		return uint(instr.Op)
	}
	if instr.A != 0 {
		return flatHandlerTier1Base + uint(instr.A)
	}
	if instr.B != 0 {
		return flatHandlerTier2Base + uint(instr.B)
	}
	return flatHandlerTier3Base + uint(instr.C)
}

// handleFlatSubOpTier3Nop is the no-op handler for the tier-3 nop slot.
//
// Returns OpResult indicating the next execution step.
func handleFlatSubOpTier3Nop(_ *VM, _ *CallFrame, _ *Registers, _ isa.Instruction) OpResult {
	return opContinue
}
