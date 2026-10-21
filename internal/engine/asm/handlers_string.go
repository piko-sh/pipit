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

// stringHandlers returns the handler definitions for the tier-0 string opcodes.
//
// Returns []asmgen.HandlerDefinition[BytecodeArchitecturePort] covering string opcodes.
func stringHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		handlerGetStringByte(),
		handlerEqualString(),
		handlerNotEqualString(),
		handlerSliceString(),
		handlerGetStringByteAsInt64(),
		handlerLessThanLengthStringThenJumpIfFalse(),
	}
}

// handlerGetStringByte returns the GET_STRING_BYTE handler, setting uints[A] =
// uint64(strings[B][ints[C]]).
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerGetStringByte() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerGetStringByte", Comment: "handlerGetStringByte sets uints[A] = uint64(strings[B][ints[C]]).",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.StringOperations().EmitGetStringByte(emitter)
		},
	}
}

// handlerEqualString returns the EQUAL_STRING handler, setting ints[A] = (strings[B] ==
// strings[C]) ? 1 : 0.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerEqualString() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerEqualString", Comment: "handlerEqualString sets ints[A] = (strings[B] == strings[C]) ? 1 : 0.",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.StringOperations().EmitEqualString(emitter)
		},
	}
}

// handlerNotEqualString returns the NOT_EQUAL_STRING handler, setting ints[A] =
// (strings[B] != strings[C]) ? 1 : 0.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerNotEqualString() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerNotEqualString", Comment: "handlerNotEqualString sets ints[A] = (strings[B] != strings[C]) ? 1 : 0.",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.StringOperations().EmitNotEqualString(emitter)
		},
	}
}

// handlerSliceString returns the SLICE_STRING handler, setting strings[A] =
// strings[B][low:high].
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerSliceString() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return guardPointerBankWriter(asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerSliceString", Comment: "handlerSliceString sets strings[A] = strings[B][low:high]. An empty result keeps strings[B]'s data pointer.",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.StringOperations().EmitSliceString(emitter)
		},
	})
}

// handlerGetStringByteAsInt64 returns the GET_STRING_BYTE_AS_INT64 handler, setting
// ints[A] = int64(strings[B][ints[C]]).
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerGetStringByteAsInt64() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerGetStringByteAsInt64", Comment: "handlerGetStringByteAsInt64 sets ints[A] = int64(strings[B][ints[C]]).",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.StringOperations().EmitGetStringByteAsInt64(emitter)
		},
	}
}

// handlerLessThanLengthStringThenJumpIfFalse returns the fused
// LESS_THAN_LENGTH_STRING_THEN_JUMP_IF_FALSE handler, jumping when ints[A] >=
// len(strings[B]).
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerLessThanLengthStringThenJumpIfFalse() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerLessThanLengthStringThenJumpIfFalse", Comment: "handlerLessThanLengthStringThenJumpIfFalse jumps if ints[A] >= len(strings[B]).",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.StringOperations().EmitLessThanLengthStringThenJumpIfFalse(emitter)
		},
	}
}
