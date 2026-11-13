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

// handleGetMapInt64Int64OkThenJumpIfFalse fuses isa.OpGetMapInt64Int64Ok and
// isa.OpJumpIfFalse.
//
// The handler reads ints[A] = map[int]int (or int64) in general[B] with key ints[C]. The
// extension word carries the ok-register index in ext.a and the signed jump offset packed
// into ext.b (lo) and ext.c (hi). On !ok programCounter advances by jumpOffset.
//
// Takes vm (*VM) which supplies the per-VM scratch key value.
// Takes frame (*CallFrame) which carries the program counter and function body.
// Takes registers (*Registers) which holds the source map and key and receives the value
// and ok bit.
// Takes instruction (instruction) which encodes the operand indices for value, map, and
// key.
//
// Returns opContinue once the extension word is consumed and the program counter is
// optionally adjusted.
func handleGetMapInt64Int64OkThenJumpIfFalse(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleGetMapInt64Int64OkThenJumpIfFalse", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	key := registers.Ints[instruction.C]
	ok := false
	if concrete, hit := reflect.TypeAssert[map[int]int](m); hit {
		value, present := concrete[int(key)]
		registers.Ints[instruction.A] = int64(value)
		ok = present
	} else if concrete, hit := reflect.TypeAssert[map[int64]int64](m); hit {
		value, present := concrete[key]
		registers.Ints[instruction.A] = value
		ok = present
	} else {
		keyReflectValue := resolveMapAccessCache(vm, m.Type()).keyScratch
		keyReflectValue.SetInt(key)
		result := m.MapIndex(keyReflectValue)
		if result.IsValid() {
			registers.Ints[instruction.A] = result.Int()
			ok = true
		} else {
			registers.Ints[instruction.A] = 0
		}
	}
	registers.Ints[extensionWord.A] = boolToInt64(ok)
	if !ok {
		offset := isa.JoinOffset(extensionWord.B, extensionWord.C)
		frame.ProgramCounter += int(offset)
	}
	return opContinue
}

// handleGetMapStringInt64OkThenJumpIfFalse fuses isa.OpGetMapStringInt64Ok and
// isa.OpJumpIfFalse. Same shape as handleGetMapInt64Int64OkThenJumpIfFalse for
// map[string]int with string keys.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which carries the program counter and function body used to
// read the extension word.
// Takes registers (*Registers) which holds the source map and key and receives the value
// and ok bit.
// Takes instruction (instruction) which encodes (a, b, c) operand indices for value, map,
// and key.
//
// Returns opContinue once dispatch consumes the extension word and optionally adjusts the
// program counter.
func handleGetMapStringInt64OkThenJumpIfFalse(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleGetMapStringInt64OkThenJumpIfFalse", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	key := registers.Strings[instruction.C]
	ok := false
	if concrete, hit := reflect.TypeAssert[map[string]int](m); hit {
		value, present := concrete[key]
		registers.Ints[instruction.A] = int64(value)
		ok = present
	} else if concrete, hit := reflect.TypeAssert[map[string]int64](m); hit {
		value, present := concrete[key]
		registers.Ints[instruction.A] = value
		ok = present
	} else {
		keyReflectValue := reflect.ValueOf(key).Convert(m.Type().Key())
		result := m.MapIndex(keyReflectValue)
		if result.IsValid() {
			registers.Ints[instruction.A] = result.Int()
			ok = true
		} else {
			registers.Ints[instruction.A] = 0
		}
	}
	registers.Ints[extensionWord.A] = boolToInt64(ok)
	if !ok {
		offset := isa.JoinOffset(extensionWord.B, extensionWord.C)
		frame.ProgramCounter += int(offset)
	}
	return opContinue
}

// handleGetMapStringStringOkThenJumpIfFalse fuses isa.OpGetMapStringStringOk and
// isa.OpJumpIfFalse for map[string]string lookups.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which carries the program counter and function body used to
// read the extension word.
// Takes registers (*Registers) which holds the source map and key and receives the value
// and ok bit.
// Takes instruction (instruction) which encodes (a, b, c) operand indices for value, map,
// and key.
//
// Returns opContinue once dispatch consumes the extension word and optionally adjusts the
// program counter.
func handleGetMapStringStringOkThenJumpIfFalse(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleGetMapStringStringOkThenJumpIfFalse", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	key := registers.Strings[instruction.C]
	ok := false
	if concrete, hit := reflect.TypeAssert[map[string]string](m); hit {
		value, present := concrete[key]
		registers.Strings[instruction.A] = value
		ok = present
	} else {
		keyReflectValue := reflect.ValueOf(key).Convert(m.Type().Key())
		result := m.MapIndex(keyReflectValue)
		if result.IsValid() {
			registers.Strings[instruction.A] = result.String()
			ok = true
		} else {
			registers.Strings[instruction.A] = ""
		}
	}
	registers.Ints[extensionWord.A] = boolToInt64(ok)
	if !ok {
		offset := isa.JoinOffset(extensionWord.B, extensionWord.C)
		frame.ProgramCounter += int(offset)
	}
	return opContinue
}

// handleGetMapInt64StringOkThenJumpIfFalse fuses isa.OpGetMapInt64StringOk and
// isa.OpJumpIfFalse for map[int]string / map[int64]string lookups.
//
// Takes vm (*VM) which supplies the per-VM scratch key value.
// Takes frame (*CallFrame) which carries the program counter and function body used to
// read the extension word.
// Takes registers (*Registers) which holds the source map and key and receives the value
// and ok bit.
// Takes instruction (instruction) which encodes (a, b, c) operand indices for value, map,
// and key.
//
// Returns opContinue once dispatch consumes the extension word and optionally adjusts the
// program counter.
func handleGetMapInt64StringOkThenJumpIfFalse(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleGetMapInt64StringOkThenJumpIfFalse", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	key := registers.Ints[instruction.C]
	ok := false
	if concrete, hit := reflect.TypeAssert[map[int]string](m); hit {
		value, present := concrete[int(key)]
		registers.Strings[instruction.A] = value
		ok = present
	} else if concrete, hit := reflect.TypeAssert[map[int64]string](m); hit {
		value, present := concrete[key]
		registers.Strings[instruction.A] = value
		ok = present
	} else {
		keyScratch := resolveMapAccessCache(vm, m.Type()).keyScratch
		keyScratch.SetInt(key)
		result := m.MapIndex(keyScratch)
		if result.IsValid() {
			registers.Strings[instruction.A] = result.String()
			ok = true
		} else {
			registers.Strings[instruction.A] = ""
		}
	}
	registers.Ints[extensionWord.A] = boolToInt64(ok)
	if !ok {
		offset := isa.JoinOffset(extensionWord.B, extensionWord.C)
		frame.ProgramCounter += int(offset)
	}
	return opContinue
}

// handleGetMapInt64GeneralOkThenJumpIfFalse fuses isa.OpGetMapInt64GeneralOk and
// isa.OpJumpIfFalse for map[int]V where V is in the general bank.
//
// Takes vm (*VM) which supplies the per-VM scratch key value.
// Takes frame (*CallFrame) which carries the program counter and function body used to
// read the extension word.
// Takes registers (*Registers) which holds the source map and key and receives the value
// and ok bit.
// Takes instruction (instruction) which encodes (a, b, c) operand indices for value, map,
// and key.
//
// Returns opContinue once dispatch consumes the extension word and optionally adjusts the
// program counter.
func handleGetMapInt64GeneralOkThenJumpIfFalse(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleGetMapInt64GeneralOkThenJumpIfFalse", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	key := registers.Ints[instruction.C]
	cacheEntry := resolveMapAccessCache(vm, m.Type())
	ok := false
	if useMapFastLinkname() && cacheEntry.fast64Eligible && cacheEntry.elemInline {
		resultPtr, present := mapAccessFast64ToGeneral(m, key)
		if present {
			registers.General[instruction.A] = wrapMapElemFast(vm, cacheEntry.elemType, resultPtr)
			ok = true
		} else {
			registers.General[instruction.A] = cacheEntry.zeroElement
		}
	} else {
		cacheEntry.keyScratch.SetInt(key)
		result := m.MapIndex(cacheEntry.keyScratch)
		if result.IsValid() {
			registers.General[instruction.A] = result
			ok = true
		} else {
			registers.General[instruction.A] = cacheEntry.zeroElement
		}
	}
	registers.Ints[extensionWord.A] = boolToInt64(ok)
	if !ok {
		offset := isa.JoinOffset(extensionWord.B, extensionWord.C)
		frame.ProgramCounter += int(offset)
	}
	return opContinue
}

// handleGetMapStringGeneralOkThenJumpIfFalse fuses isa.OpGetMapStringGeneralOk and
// isa.OpJumpIfFalse for map[string]V where V is in the general bank.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which carries the program counter and function body used to
// read the extension word.
// Takes registers (*Registers) which holds the source map and key and receives the value
// and ok bit.
// Takes instruction (instruction) which encodes (a, b, c) operand indices for value, map,
// and key.
//
// Returns opContinue once dispatch consumes the extension word and optionally adjusts the
// program counter.
func handleGetMapStringGeneralOkThenJumpIfFalse(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleGetMapStringGeneralOkThenJumpIfFalse", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	value, ok := mapLookupStringKeyGeneral(vm, m, registers.Strings[instruction.C])
	registers.General[instruction.A] = value
	registers.Ints[extensionWord.A] = boolToInt64(ok)
	if !ok {
		offset := isa.JoinOffset(extensionWord.B, extensionWord.C)
		frame.ProgramCounter += int(offset)
	}
	return opContinue
}
