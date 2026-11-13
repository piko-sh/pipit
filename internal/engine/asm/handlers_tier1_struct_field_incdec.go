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

package asm

import (
	"piko.sh/asmgen"
)

const (
	// goSymbolIncrementStructFieldInt64 names the Go trampoline target the
	// inc-int-struct-field handler CALLs.
	goSymbolIncrementStructFieldInt64 = "·asmCallIncrementStructFieldInt64(SB)"

	// goSymbolDecrementStructFieldInt64 names the Go trampoline target the
	// dec-int-struct-field handler CALLs.
	goSymbolDecrementStructFieldInt64 = "·asmCallDecrementStructFieldInt64(SB)"

	// goSymbolIncrementStructFieldUint64 names the Go trampoline target the
	// inc-uint-struct-field handler CALLs.
	goSymbolIncrementStructFieldUint64 = "·asmCallIncrementStructFieldUint64(SB)"

	// goSymbolDecrementStructFieldUint64 names the Go trampoline target the
	// dec-uint-struct-field handler CALLs.
	goSymbolDecrementStructFieldUint64 = "·asmCallDecrementStructFieldUint64(SB)"
)

// tier1StructFieldIncDecHandlers returns the four single-level handler definitions for
// the inc/dec struct field super-instructions.
//
// Returns the inc/dec int and uint struct-field handler definitions.
func tier1StructFieldIncDecHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		handlerIncrementStructFieldInt64(),
		handlerDecrementStructFieldInt64(),
		handlerIncrementStructFieldUint64(),
		handlerDecrementStructFieldUint64(),
	}
}

// handlerIncrementStructFieldInt64 builds the tier-1 inc-int-struct-field handler.
//
// Returns the handler definition that calls asmCallIncrementStructFieldInt64.
func handlerIncrementStructFieldInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoTwoOperandShim(
		"handlerIncrementStructFieldInt64",
		"handlerIncrementStructFieldInt64 increments the int field on the struct at ints[B] by one via asmCallIncrementStructFieldInt64.",
		goSymbolIncrementStructFieldInt64,
	)
}

// handlerDecrementStructFieldInt64 builds the tier-1 dec-int-struct-field handler.
//
// Returns the handler definition that calls asmCallDecrementStructFieldInt64.
func handlerDecrementStructFieldInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoTwoOperandShim(
		"handlerDecrementStructFieldInt64",
		"handlerDecrementStructFieldInt64 decrements the int field on the struct at ints[B] by one via asmCallDecrementStructFieldInt64.",
		goSymbolDecrementStructFieldInt64,
	)
}

// handlerIncrementStructFieldUint64 builds the tier-1 inc-uint-struct-field handler.
//
// Returns the handler definition that calls asmCallIncrementStructFieldUint64.
func handlerIncrementStructFieldUint64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoTwoOperandShim(
		"handlerIncrementStructFieldUint64",
		"handlerIncrementStructFieldUint64 increments the uint field on the struct at ints[B] by one via asmCallIncrementStructFieldUint64.",
		goSymbolIncrementStructFieldUint64,
	)
}

// handlerDecrementStructFieldUint64 builds the tier-1 dec-uint-struct-field handler.
//
// Returns the handler definition that calls asmCallDecrementStructFieldUint64.
func handlerDecrementStructFieldUint64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoTwoOperandShim(
		"handlerDecrementStructFieldUint64",
		"handlerDecrementStructFieldUint64 single-level shim wrapping asmCallDecrementStructFieldUint64.",
		goSymbolDecrementStructFieldUint64,
	)
}
