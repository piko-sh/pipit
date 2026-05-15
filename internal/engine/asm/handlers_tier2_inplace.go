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

// tier2InPlaceHandlers returns the tier-2 in-place inc/dec handlers.
//
// Returns []HandlerDefinition[BytecodeArchitecturePort] which is the handler definitions
// for this group.
func tier2InPlaceHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		tier2IntegerInPlaceHandler("handlerSubOpTier2IncInt", "INC",
			"handlerSubOpTier2IncInt increments ints[C] by one (tier-2 in-place sub-op)."),
		tier2IntegerInPlaceHandler("handlerSubOpTier2DecInt", "DEC",
			"handlerSubOpTier2DecInt decrements ints[C] by one (tier-2 in-place sub-op)."),
		tier2UintInPlaceHandler("handlerSubOpTier2IncUint", "INC",
			"handlerSubOpTier2IncUint increments uints[C] by one (tier-2 in-place sub-op)."),
		tier2UintInPlaceHandler("handlerSubOpTier2DecUint", "DEC",
			"handlerSubOpTier2DecUint decrements uints[C] by one (tier-2 in-place sub-op)."),
	}
}

// tier2IntegerInPlaceHandler builds a tier-2 integer in-place handler.
//
// Takes name (string) which is the handler function name.
// Takes operation (string) which is the in-place operation mnemonic.
// Takes comment (string) which is the handler's doc comment.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func tier2IntegerInPlaceHandler(name, operation, comment string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   comment,
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractC(emitter, scratches[0])
			architecture.IntegerInPlace(emitter, operation, scratches[0])
			architecture.DispatchNext(emitter)
		},
	}
}

// tier2UintInPlaceHandler is the uint sibling of tier2IntegerInPlaceHandler, addressing
// the uint register bank via UintInPlace.
//
// Takes name (string) which is the handler function name.
// Takes operation (string) which is the in-place operation mnemonic.
// Takes comment (string) which is the handler's doc comment.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func tier2UintInPlaceHandler(name, operation, comment string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   comment,
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractC(emitter, scratches[0])
			architecture.UintInPlace(emitter, operation, scratches[0], scratches[1])
			architecture.DispatchNext(emitter)
		},
	}
}
