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
	// goSymbolStrconvItoa is the Plan-9 ASM symbol of the Go trampoline forwarding to
	// strconv.Itoa. See asm_call_trampolines.go for the trampoline.
	goSymbolStrconvItoa = "·asmCallStrconvItoa(SB)"

	// goSymbolStrconvFormatInt is the Plan-9 ASM symbol of the Go trampoline forwarding to
	// strconv.FormatInt.
	goSymbolStrconvFormatInt = "·asmCallStrconvFormatInt(SB)"
)

// tier1StrconvHandlers returns the tier-1 strconv handler definitions.
//
// Returns []HandlerDefinition[BytecodeArchitecturePort] which is the handler definitions
// for this group.
func tier1StrconvHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		handlerStrconvFormatBool(),
		handlerStrconvItoa(),
		handlerStrconvFormatInt(),
	}
}

// handlerStrconvFormatBool builds the inlined SubOpStrconvFormatBool handler. No Go call
// is needed because strconv.FormatBool's return values are statically interned.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerStrconvFormatBool() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return guardPointerBankWriter(asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "handlerStrconvFormatBool",
		Comment:   "handlerStrconvFormatBool sets strings[B] = bools[C] ? \"true\" : \"false\" by stamping a pre-allocated header.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitSubOpStrconvFormatBool(emitter)
		},
	})
}

// handlerStrconvItoa builds the SubOpStrconvItoa handler, setting strings[B] =
// strconv.Itoa(int(ints[C])).
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerStrconvItoa() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoTwoOperandShim(
		"handlerStrconvItoa",
		"handlerStrconvItoa sets strings[B] = strconv.Itoa(int(ints[C])) via asmCallStrconvItoa.",
		goSymbolStrconvItoa,
	)
}

// handlerStrconvFormatInt builds the SubOpStrconvFormatInt handler, setting strings[B] =
// strconv.FormatInt(ints[C], int(ints[ext.A])).
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func handlerStrconvFormatInt() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return inlineGoThreeOperandShim(
		"handlerStrconvFormatInt",
		"handlerStrconvFormatInt sets strings[B] = strconv.FormatInt(ints[C], int(ints[ext.A])) via asmCallStrconvFormatInt.",
		goSymbolStrconvFormatInt,
	)
}
