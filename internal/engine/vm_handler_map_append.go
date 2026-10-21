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

package engine

import (
	"reflect"

	"pipit.sh/pipit/internal/isa"
)

const (
	// mapAppendMapRegister is the register of the step-by-step path that holds the map.
	mapAppendMapRegister = iota

	// mapAppendHeldRegister is the register that receives the slice the map holds.
	mapAppendHeldRegister

	// mapAppendGrownRegister is the register that receives the slice after the append.
	mapAppendGrownRegister

	// mapAppendGeneralRegisters is the number of general registers the path uses.
	mapAppendGeneralRegisters
)

const (
	// mapAppendKeyRegister is the string register that holds the map key.
	mapAppendKeyRegister = iota

	// mapAppendElementRegister is the string register that holds the appended element.
	mapAppendElementRegister

	// mapAppendStringRegisters is the number of string registers the path uses.
	mapAppendStringRegisters
)

// handleAppendStringToMapString handles isa.OpAppendStringToMapString, which appends
// strings[C] to the slice the map in general[A] holds under strings[B] and stores the
// slice back, as m[k] = append(m[k], v) does.
//
// A store the licence checker granted, into a map of []string, grows the slice where it
// lies in the map (appendStringInLocalMapSlot()). Every other case runs the lookup, the
// append and the store the operation replaces, one after another.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the running frame, its program counter past the
// instruction.
// Takes registers (*Registers) which holds the map, the key and the element.
// Takes instruction (isa.Instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleAppendStringToMapString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.A]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleAppendStringToMapString", registerRoleMap, instruction.A, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	key, element := registers.Strings[instruction.B], registers.Strings[instruction.C]
	if appendStringInLocalMapSlot(vm, frame, m, key, element) {
		return opContinue
	}
	return appendStringToMapEntryInSteps(vm, frame, m, key, element)
}

// appendStringToMapEntryInSteps runs OpGetMapStringGeneral, OpAppendString and
// OpSetMapStringGeneral over a register file of its own, so the fused operation does in
// every case exactly what the three did, limits, panics and hand-offs included.
//
// The frame is the running one, so the store sees the fused instruction's grant.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the running frame.
// Takes m (reflect.Value) which is the map.
// Takes key (string) which is the key.
// Takes element (string) which is the element to append.
//
// Returns OpResult which is the first result among the three that is not opContinue, or
// the store's.
func appendStringToMapEntryInSteps(vm *VM, frame *CallFrame, m reflect.Value, key, element string) OpResult {
	var general [mapAppendGeneralRegisters]reflect.Value
	var texts [mapAppendStringRegisters]string
	general[mapAppendMapRegister] = m
	texts[mapAppendKeyRegister], texts[mapAppendElementRegister] = key, element
	var steps Registers
	steps.General, steps.Strings = general[:], texts[:]
	lookup := isa.NewInstruction(isa.OpGetMapStringGeneral, mapAppendHeldRegister, mapAppendMapRegister, mapAppendKeyRegister)
	if result := handleGetMapStringGeneral(vm, frame, &steps, lookup); result != opContinue {
		return result
	}
	grow := isa.NewInstruction(isa.OpAppendString, mapAppendGrownRegister, mapAppendHeldRegister, mapAppendElementRegister)
	if result := handleAppendString(vm, frame, &steps, grow); result != opContinue {
		return result
	}
	store := isa.NewInstruction(isa.OpSetMapStringGeneral, mapAppendMapRegister, mapAppendKeyRegister, mapAppendGrownRegister)
	return handleSetMapStringGeneral(vm, frame, &steps, store)
}
