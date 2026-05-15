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
		handlerStringIndex(),
		handlerEqString(),
		handlerNeString(),
		handlerSliceString(),
		handlerStringIndexToInt(),
		handlerLenStringLtJumpFalse(),
	}
}

// handlerStringIndex returns the STRING_INDEX handler, setting uints[A] =
// uint64(strings[B][ints[C]]).
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerStringIndex() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerStringIndex", Comment: "handlerStringIndex sets uints[A] = uint64(strings[B][ints[C]]).",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.StringOperations().EmitStringIndex(emitter)
		},
	}
}

// handlerEqString returns the EQ_STRING handler, setting ints[A] = (strings[B] ==
// strings[C]) ? 1 : 0.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerEqString() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerEqString", Comment: "handlerEqString sets ints[A] = (strings[B] == strings[C]) ? 1 : 0.",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.StringOperations().EmitEqualString(emitter)
		},
	}
}

// handlerNeString returns the NE_STRING handler, setting ints[A] = (strings[B] !=
// strings[C]) ? 1 : 0.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerNeString() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerNeString", Comment: "handlerNeString sets ints[A] = (strings[B] != strings[C]) ? 1 : 0.",
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
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerSliceString", Comment: "handlerSliceString sets strings[A] = strings[B][low:high].",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.StringOperations().EmitSliceString(emitter)
		},
	}
}

// handlerStringIndexToInt returns the STRING_INDEX_TO_INT handler, setting ints[A] =
// int64(strings[B][ints[C]]).
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerStringIndexToInt() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerStringIndexToInt", Comment: "handlerStringIndexToInt sets ints[A] = int64(strings[B][ints[C]]).",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.StringOperations().EmitStringIndexToInt(emitter)
		},
	}
}

// handlerLenStringLtJumpFalse returns the fused LEN_STRING_LT_JUMP_FALSE handler, jumping
// when ints[A] >= len(strings[B]).
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func handlerLenStringLtJumpFalse() asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name: "handlerLenStringLtJumpFalse", Comment: "handlerLenStringLtJumpFalse jumps if ints[A] >= len(strings[B]).",
		FrameSize: frameSizeZero, Flags: flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.StringOperations().EmitLenStringLtJumpFalse(emitter)
		},
	}
}
