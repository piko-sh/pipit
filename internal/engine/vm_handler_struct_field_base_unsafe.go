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

//go:build !safe

package engine

import (
	"reflect"
	"unsafe"

	"pipit.sh/pipit/internal/isa"
)

// structFieldUnsafeBase resolves a general-bank struct or struct pointer to its storage.
//
// Takes registers (*Registers) which provides the general bank.
// Takes generalRegister (uint8) which is the general register index holding the struct
// reflect.Value.
//
// Returns unsafe.Pointer which is the struct's storage, or nil on failure.
// Returns bool which is false for an invalid value, a nil pointer or an unreachable
// storage address.
func structFieldUnsafeBase(registers *Registers, generalRegister uint8) (unsafe.Pointer, bool) {
	value := registers.General[generalRegister]
	raw := (*unsafeReflectValue)(unsafe.Pointer(&value))
	if raw.typ == nil {
		return nil, false
	}
	switch reflect.Kind(raw.flag & flagKindMask) {
	case reflect.Pointer:
		return resolvePointerBase(raw)
	case reflect.Interface:
		return resolveInterfaceBase(value)
	case reflect.Struct:
		return resolveStructBase(raw)
	default:
	}
	return nil, false
}

// resolvePointerBase extracts the underlying unsafe.Pointer when the reflect.Value holds
// a Pointer kind.
//
// When flagIndir is set, raw.ptr addresses a *T storage cell that must be dereferenced.
// When unset, raw.ptr IS the pointer value already (snapshotted from
// snapshotPointerLeaf).
//
// Takes raw (*unsafeReflectValue) which is the punned reflect.Value internals.
//
// Returns unsafe.Pointer which is the dereferenced pointer, or nil on failure.
// Returns bool which is true when the pointer is non-nil.
func resolvePointerBase(raw *unsafeReflectValue) (unsafe.Pointer, bool) {
	if raw.flag&flagIndir != 0 {
		inner := *(*unsafe.Pointer)(raw.ptr)
		if inner == nil {
			return nil, false
		}
		return inner, true
	}
	if raw.ptr == nil {
		return nil, false
	}
	return raw.ptr, true
}

// resolveInterfaceBase extracts the underlying unsafe.Pointer when the reflect.Value
// holds an Interface kind.
//
// For an addressable Value (flagAddr | flagIndir set) the internal .ptr field is the
// storage address.
//
// Takes value (reflect.Value) which is the interface-holding value.
//
// Returns unsafe.Pointer which is the storage pointer, or nil on failure.
// Returns bool which is true when a stable storage address was found.
func resolveInterfaceBase(value reflect.Value) (unsafe.Pointer, bool) {
	inner := value.Elem()
	if inner.Kind() == reflect.Pointer {
		if inner.IsNil() {
			return nil, false
		}
		return inner.UnsafePointer(), true
	}
	if inner.CanAddr() {
		return ReflectValuePtr(inner), true
	}
	return nil, false
}

// resolveStructBase returns the storage of an addressable struct value.
//
// Takes raw (*unsafeReflectValue) which is the punned reflect.Value internals.
//
// Returns unsafe.Pointer which is the storage pointer, or nil on failure.
// Returns bool which is true when the value is addressable.
func resolveStructBase(raw *unsafeReflectValue) (unsafe.Pointer, bool) {
	if raw.flag&flagAddr != 0 {
		return raw.ptr, true
	}
	return nil, false
}

// readUintFieldRunDirect makes the unsigned field reads of a run straight from the
// storage of the struct in a general register, where structFieldUnsafeBase() can address
// it.
//
// Takes frame (*CallFrame) which holds the layout table and the program counter.
// Takes registers (*Registers) which holds the struct and receives the fields.
// Takes receiver (uint8) which is the general register holding the struct.
// Takes words ([]isa.Instruction) which are the run's extension words, one for each read.
// Takes base (int) which is the program counter of the first of them.
//
// Returns bool which is false, with nothing read, when the struct cannot be addressed.
func readUintFieldRunDirect(frame *CallFrame, registers *Registers, receiver uint8, words []isa.Instruction, base int) bool {
	storage, direct := structFieldUnsafeBase(registers, receiver)
	if !direct {
		return false
	}
	readUintFieldsAt(frame, registers, storage, words, base)
	return true
}

// readUintFieldsAt reads the unsigned fields of a run by their offsets from a struct's
// storage, stepping the program counter to each read's word before making it.
//
// Takes frame (*CallFrame) which holds the layout table and the program counter.
// Takes registers (*Registers) which receives the fields.
// Takes storage (unsafe.Pointer) which addresses the struct.
// Takes words ([]isa.Instruction) which are the run's extension words, one for each read.
// Takes base (int) which is the program counter of the first of them.
func readUintFieldsAt(frame *CallFrame, registers *Registers, storage unsafe.Pointer, words []isa.Instruction, base int) {
	for ordinal, word := range words {
		frame.ProgramCounter = base + ordinal + 1
		layout := readTier0StructFieldLayout(frame, word.B)
		loadUintField(&registers.Uints[word.A], unsafe.Add(storage, uintptr(layout.Offset)), reflect.Kind(layout.Kind))
	}
}
