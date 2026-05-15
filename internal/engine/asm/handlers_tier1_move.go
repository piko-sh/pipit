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

// contextBaseBank identifies which context-loaded bank a context-based move handler
// should access. The architecture port's LoadFromBoolBank / LoadFromUintBank primitives
// accept the bank implicitly via their named entry points; this enum lets one helper
// dispatch to the right pair.
type contextBaseBank int

const (
	// contextBaseBool selects the boolean context bank.
	contextBaseBool contextBaseBank = iota

	// contextBaseUint selects the unsigned-integer context bank.
	contextBaseUint
)

// tier1MoveHandlers returns the tier-1 register-move ASM handlers covering int, float,
// bool, uint, and string bank moves.
//
// Returns []HandlerDefinition[BytecodeArchitecturePort] which is the handler definitions
// for this group.
func tier1MoveHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		moveHandler("handlerSubOpMoveInt", asmgen.RegisterBankInteger),
		moveHandlerFloat("handlerSubOpMoveFloat"),
		moveHandlerContextBased("handlerSubOpMoveBool", contextBaseBool),
		moveHandlerContextBased("handlerSubOpMoveUint", contextBaseUint),
		moveHandlerString("handlerSubOpMoveString"),
	}
}

// moveHandlerString builds the string-bank tier-1 register-move handler.
//
// Takes name (string) which is the handler function name.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func moveHandlerString(name string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   name + " copies strings[C] to strings[B] for the tier-1 register-move sub-op.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractB(emitter, scratches[0])
			architecture.ExtractC(emitter, scratches[1])
			architecture.StringCopy(emitter, scratches[0], scratches[1])
			architecture.DispatchNext(emitter)
		},
	}
}

// moveHandler builds a tier-1 register-to-register move handler.
//
// Takes name (string) which is the handler function name.
// Takes bank (RegisterBank) which is the register bank to move within.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func moveHandler(name string, bank asmgen.RegisterBank) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   name + " copies the value at bank[C] into bank[B] for the tier-1 register-move sub-op.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractB(emitter, scratches[0])
			architecture.ExtractC(emitter, scratches[1])
			temp := architecture.DataTemporary(dataTempScratch0)
			architecture.LoadFromBank(emitter, bank, scratches[1], temp)
			architecture.StoreToBank(emitter, bank, temp, scratches[0])
			architecture.DispatchNext(emitter)
		},
	}
}

// moveHandlerFloat builds the float-bank tier-1 register-move handler.
//
// Takes name (string) which is the handler function name.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func moveHandlerFloat(name string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   name + " copies floats[C] to floats[B] for the tier-1 register-move sub-op.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			floatScratches := architecture.FloatScratchRegisters()
			architecture.ExtractB(emitter, scratches[0])
			architecture.ExtractC(emitter, scratches[1])
			architecture.LoadFromBank(emitter, asmgen.RegisterBankFloat, scratches[1], floatScratches[0])
			architecture.StoreToBank(emitter, asmgen.RegisterBankFloat, floatScratches[0], scratches[0])
			architecture.DispatchNext(emitter)
		},
	}
}

// moveHandlerContextBased builds a tier-1 register-move handler for a context-base-loaded
// bank.
//
// Takes name (string) which is the handler function name.
// Takes bank (contextBaseBank) which is the context-loaded bank selector.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func moveHandlerContextBased(name string, bank contextBaseBank) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   name + " copies bank[C] to bank[B] for a context-base-loaded register-move sub-op.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			const (
				scratchDstReg  = 0
				scratchSrcReg  = 1
				scratchTemp    = 2
				scratchBaseReg = 3
			)
			scratches := architecture.ScratchRegisters()

			architecture.ExtractB(emitter, scratches[scratchDstReg])
			architecture.ExtractC(emitter, scratches[scratchSrcReg])
			temp := scratches[scratchTemp]
			baseScratch := scratches[scratchBaseReg]
			switch bank {
			case contextBaseBool:
				architecture.LoadFromBoolBank(emitter, scratches[scratchSrcReg], temp, baseScratch)
				architecture.StoreToBoolBank(emitter, temp, scratches[scratchDstReg], baseScratch)
			case contextBaseUint:
				architecture.LoadFromUintBank(emitter, scratches[scratchSrcReg], temp, baseScratch)
				architecture.StoreToUintBank(emitter, temp, scratches[scratchDstReg], baseScratch)
			}
			architecture.DispatchNext(emitter)
		},
	}
}
