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

	"pipit.sh/pipit/internal/symtab"

	"pipit.sh/pipit/internal/isa"
)

// handleMapIndexOkJumpIfFalseIntInt fuses isa.OpMapIndexOkIntInt and isa.OpJumpIfFalse.
//
// The handler reads ints[A] = map[int]int (or int64) in general[B] with key ints[C]; the
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
func handleMapIndexOkJumpIfFalseIntInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapIndexOkJumpIfFalseIntInt", registerRoleMap, instruction.B, instruction, frame, registers)
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
		keyReflectValue := intMapKeyScratch(vm, m.Type().Key())
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

// handleMapIndexOkJumpIfFalseStringInt fuses isa.OpMapIndexOkStringInt and
// isa.OpJumpIfFalse. Same shape as handleMapIndexOkJumpIfFalseIntInt for map[string]int
// with string keys.
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
func handleMapIndexOkJumpIfFalseStringInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapIndexOkJumpIfFalseStringInt", registerRoleMap, instruction.B, instruction, frame, registers)
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

// handleMapIndexOkJumpIfFalseStringString fuses isa.OpMapIndexOkStringString and
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
func handleMapIndexOkJumpIfFalseStringString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapIndexOkJumpIfFalseStringString", registerRoleMap, instruction.B, instruction, frame, registers)
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

// handleMapIndexOkJumpIfFalseIntString fuses isa.OpMapIndexOkIntString and
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
func handleMapIndexOkJumpIfFalseIntString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapIndexOkJumpIfFalseIntString", registerRoleMap, instruction.B, instruction, frame, registers)
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
		keyScratch := intMapKeyScratch(vm, m.Type().Key())
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

// handleMapIndexOkJumpIfFalseIntGeneral fuses isa.OpMapIndexOkIntGeneral and
// isa.OpJumpIfFalse for map[int]V where V is in the general bank.
//
// Covers pointers, slices, maps, and interfaces, as well as the `node, ok := m[k]; if !ok
// { ... }` idiom.
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
func handleMapIndexOkJumpIfFalseIntGeneral(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapIndexOkJumpIfFalseIntGeneral", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	key := registers.Ints[instruction.C]
	keyType := m.Type().Key()
	elemType := m.Type().Elem()
	ok := false
	if useMapFastLinkname() && mapKeyIsFast64Eligible(keyType) {
		resultPtr, present := mapAccessFast64ToGeneral(m, key)
		if present {
			registers.General[instruction.A] = wrapMapElemFast(vm, elemType, resultPtr)
			ok = true
		} else {
			registers.General[instruction.A] = symtab.ZeroValueForType(elemType)
		}
	} else {
		keyScratch := intMapKeyScratch(vm, keyType)
		keyScratch.SetInt(key)
		result := m.MapIndex(keyScratch)
		if result.IsValid() {
			registers.General[instruction.A] = result
			ok = true
		} else {
			registers.General[instruction.A] = symtab.ZeroValueForType(elemType)
		}
	}
	registers.Ints[extensionWord.A] = boolToInt64(ok)
	if !ok {
		offset := isa.JoinOffset(extensionWord.B, extensionWord.C)
		frame.ProgramCounter += int(offset)
	}
	return opContinue
}

// handleMapIndexOkJumpIfFalseStringGeneral fuses isa.OpMapIndexOkStringGeneral and
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
func handleMapIndexOkJumpIfFalseStringGeneral(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapIndexOkJumpIfFalseStringGeneral", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	key := registers.Strings[instruction.C]
	elemType := m.Type().Elem()
	ok := false
	if !useMapFastLinkname() {
		result := m.MapIndex(reflect.ValueOf(key))
		if result.IsValid() {
			registers.General[instruction.A] = result
			ok = true
		} else {
			registers.General[instruction.A] = symtab.ZeroValueForType(elemType)
		}
	} else {
		resultPtr, present := mapAccessFastStrToGeneral(m, key)
		if present {
			registers.General[instruction.A] = wrapMapElemFast(vm, elemType, resultPtr)
			ok = true
		} else {
			registers.General[instruction.A] = symtab.ZeroValueForType(elemType)
		}
	}
	registers.Ints[extensionWord.A] = boolToInt64(ok)
	if !ok {
		offset := isa.JoinOffset(extensionWord.B, extensionWord.C)
		frame.ProgramCounter += int(offset)
	}
	return opContinue
}
