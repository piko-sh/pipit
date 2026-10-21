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

package escape

import (
	"slices"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// annotateArenaSafeFieldStores populates compiledFunction.FieldStoreArenaSafePCs with the
// struct-field stores whose receiver is alias-free and was produced by an arena-safe
// allocation site, so the stored value can remain arena-resident.
//
// Takes compiledFunction (*CompiledFunction) whose ArenaSafeAllocPCs must already be set.
func annotateArenaSafeFieldStores(compiledFunction *program.CompiledFunction) {
	if len(compiledFunction.ArenaSafeAllocPCs) == 0 {
		return
	}
	body := compiledFunction.Body
	jumpTargets := program.JumpTargetsOf(body)
	aliasFree := make(map[uint8]bool)
	for pc := range body {
		inst := body[pc]
		if !isAnnotatableFieldStore(inst) {
			continue
		}
		receiver := inst.A
		if !receiverFromArenaSafeSite(compiledFunction, pc, receiver, jumpTargets) {
			continue
		}
		free, seen := aliasFree[receiver]
		if !seen {
			free = fieldStoreReceiverAliasFree(compiledFunction, receiver)
			aliasFree[receiver] = free
		}
		if !free {
			continue
		}
		if compiledFunction.FieldStoreArenaSafePCs == nil {
			compiledFunction.FieldStoreArenaSafePCs = make(map[int]bool)
		}
		compiledFunction.FieldStoreArenaSafePCs[pc] = true
	}
}

// RevalidateArenaSafeFieldStores re-checks every grant in
// compiledFunction.FieldStoreArenaSafePCs against the function's current body and drops
// the grants that no longer hold.
//
// Takes compiledFunction (*program.CompiledFunction) whose grants are re-checked.
//
// Returns int which is the number of grants dropped.
func RevalidateArenaSafeFieldStores(compiledFunction *program.CompiledFunction) int {
	if len(compiledFunction.FieldStoreArenaSafePCs) == 0 {
		return 0
	}
	body := compiledFunction.Body
	jumpTargets := program.JumpTargetsOf(body)
	aliasFree := make(map[uint8]bool)
	dropped := 0
	for programCounter := range compiledFunction.FieldStoreArenaSafePCs {
		if fieldStoreGrantHolds(compiledFunction, programCounter, jumpTargets, aliasFree) {
			continue
		}
		delete(compiledFunction.FieldStoreArenaSafePCs, programCounter)
		dropped++
	}
	return dropped
}

// fieldStoreGrantHolds reports whether the grant at programCounter still satisfies the
// rules annotateArenaSafeFieldStores granted it under.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is read.
// Takes programCounter (int) which is the granted store's program counter.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
// Takes aliasFree (map[uint8]bool) which caches the alias-free answer per receiver.
//
// Returns bool which is true when the grant holds.
func fieldStoreGrantHolds(compiledFunction *program.CompiledFunction, programCounter int, jumpTargets map[int]bool, aliasFree map[uint8]bool) bool {
	body := compiledFunction.Body
	if programCounter < 0 || programCounter >= len(body) || !isAnnotatableFieldStore(body[programCounter]) {
		return false
	}
	receiver := body[programCounter].A
	if !receiverFromArenaSafeSite(compiledFunction, programCounter, receiver, jumpTargets) {
		return false
	}
	free, seen := aliasFree[receiver]
	if !seen {
		free = fieldStoreReceiverAliasFree(compiledFunction, receiver)
		aliasFree[receiver] = free
	}
	return free
}

// isAnnotatableFieldStore reports whether inst is a struct-field store whose handler
// consults FieldStoreArenaSafePCs: the layout-table store and the reflect store with a
// direct field index (the dereferencing form writes through a pointer whose target the
// receiver rules do not cover).
//
// Takes inst (isa.Instruction) which is the instruction.
//
// Returns true for the two annotatable stores.
func isAnnotatableFieldStore(inst isa.Instruction) bool {
	switch inst.Op {
	case isa.OpSetStructFieldGeneral:
		return true
	case isa.OpSetField:
		return inst.B != isa.SentinelFieldDeref
	default:
		return false
	}
}

// receiverFromArenaSafeSite walks back from the store to the instruction that last wrote
// the receiver register and reports whether it is an arena-safe allocation site reached
// without crossing a control-flow merge.
//
// Any instruction that may write the register otherwise, including an undescribed or
// opaque one, ends the walk with a refusal, as does a jump target anywhere in the range,
// because another path could arrive there with a receiver of different origin.
//
// Takes compiledFunction (*CompiledFunction) whose body and ArenaSafeAllocPCs are read.
// Takes storePC (int) which is the store's program counter.
// Takes receiver (uint8) which is the receiver register.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
//
// Returns true when the receiver was produced by an arena-safe site in the same block.
func receiverFromArenaSafeSite(compiledFunction *program.CompiledFunction, storePC int, receiver uint8, jumpTargets map[int]bool) bool {
	body := compiledFunction.Body
	for pc := storePC; pc > 0; pc-- {
		if jumpTargets[pc] {
			return false
		}
		inst := body[pc-1]
		if inst.Op == isa.OpExtension {
			continue
		}
		if outputSlot, ok := arenaCandidateSiteAt(compiledFunction, pc-1); ok && outputSlot == receiver {
			return compiledFunction.ArenaSafeAllocPCs[pc-1]
		}
		if mayWriteGeneralRegisterAt(compiledFunction, inst, receiver) {
			return false
		}
	}
	return false
}

// fieldStoreReceiverAliasFree reports whether the general register slot holds the only
// reference to its value for the purpose of a field store, relaxing the strict alias-free
// analysis to allow arena-safe address-of operations.
//
// Takes compiledFunction (*CompiledFunction) whose body is walked.
// Takes slot (uint8) which is the receiver register.
//
// Returns true when every read of the slot is non-retaining or an arena-safe address-of.
func fieldStoreReceiverAliasFree(compiledFunction *program.CompiledFunction, slot uint8) bool {
	for paramIdx := range compiledFunction.ParameterKinds {
		if compiledFunction.ParameterKinds[paramIdx] == isa.RegisterGeneral && paramRegisterSlot(compiledFunction, paramIdx) == slot {
			return false
		}
	}
	if slices.Contains(generalResultSlots(compiledFunction), slot) {
		return false
	}
	pc := 0
	for pc < len(compiledFunction.Body) {
		inst := compiledFunction.Body[pc]
		addressOf := (inst.Op == isa.OpAllocateIndirect || inst.Op == isa.OpAddress) && isArenaCandidateSite(inst) && inst.B == slot
		if addressOf {
			if !compiledFunction.ArenaSafeAllocPCs[pc] {
				return false
			}
		} else if !aliasFreeAtInstruction(compiledFunction, inst, slot) {
			return false
		}
		pc = advancePastExtensions(compiledFunction, pc)
	}
	return true
}

// mayWriteGeneralRegisterAt reports whether inst may write general[slot], reading a call
// through its call site so a call that returns into another register does not end the
// receiver walk; every other instruction is classified by mayWriteGeneralRegister().
//
// Takes compiledFunction (*CompiledFunction) which owns the call-site table.
// Takes inst (isa.Instruction) which is the instruction.
// Takes slot (uint8) which is the general register.
//
// Returns true when the write cannot be ruled out.
func mayWriteGeneralRegisterAt(compiledFunction *program.CompiledFunction, inst isa.Instruction, slot uint8) bool {
	if inst.Op != isa.OpDrillTier1 {
		return mayWriteGeneralRegister(inst, slot)
	}
	switch isa.SubOpcode(inst.A) {
	case isa.SubOpCall, isa.SubOpCallScalar, isa.SubOpCallMethod, isa.SubOpCallMethodInlineable,
		isa.SubOpCallNative, isa.SubOpCallIIFE:
	default:
		return mayWriteGeneralRegister(inst, slot)
	}
	siteIndex := int(inst.WideIndex())
	if siteIndex >= len(compiledFunction.CallSites) {
		return true
	}
	for _, result := range compiledFunction.CallSites[siteIndex].Returns {
		if result.Kind == isa.RegisterGeneral && result.Register == slot {
			return true
		}
	}
	return false
}

// mayWriteGeneralRegister reports whether inst may write general[slot], answering true
// for any operation whose writes the shape table cannot pin down.
//
// Takes inst (isa.Instruction) which is the instruction.
// Takes slot (uint8) which is the general register.
//
// Returns true when the write cannot be ruled out.
func mayWriteGeneralRegister(inst isa.Instruction, slot uint8) bool {
	shape := isa.ShapeForInstruction(inst)
	if shape.Flags&isa.ShapeFlagDescribed == 0 || shape.Flags&isa.ShapeFlagOpaqueWrites != 0 {
		return true
	}
	roles := [isa.NumInstructionOperands]isa.OperandRole{shape.A, shape.B, shape.C}
	operands := [isa.NumInstructionOperands]uint8{inst.A, inst.B, inst.C}
	for position, role := range roles {
		if !shape.Writes[position] || operands[position] != slot {
			continue
		}
		if role == isa.RoleRegGeneral || role == isa.RoleRegDynamic {
			return true
		}
	}
	return false
}
