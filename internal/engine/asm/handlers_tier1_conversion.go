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

// bankCrossing names a conversion that moves a value from one scalar bank to another
// without changing its bits, or by reducing it to a truth value.
type bankCrossing int

const (
	// uintBankIntoIntBank reinterprets uints[C] as ints[B].
	uintBankIntoIntBank bankCrossing = iota

	// intBankIntoUintBank reinterprets ints[C] as uints[B].
	intBankIntoUintBank

	// boolBankIntoIntBank widens bools[C] to ints[B], zero or one.
	boolBankIntoIntBank

	// intBankIntoBoolBank sets bools[B] to whether ints[C] is non-zero.
	intBankIntoBoolBank
)

// tier1ConversionHandlers returns the scalar-bank conversion handlers.
//
// Returns []HandlerDefinition[BytecodeArchitecturePort] which is the handler definitions
// for this group.
func tier1ConversionHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		conversionHandler("handlerConvertInt64ToFloat64", "INTEGER_TO_FLOAT",
			"handlerConvertInt64ToFloat64 sets floats[B] = float64(ints[C])."),
		conversionHandler("handlerConvertFloat64ToInt64", "FLOAT_TO_INTEGER",
			"handlerConvertFloat64ToInt64 sets ints[B] = int64(floats[C])."),
		conversionHandler("handlerConvertUint64ToFloat64", "UNSIGNED_TO_FLOAT",
			"handlerConvertUint64ToFloat64 sets floats[B] = float64(uints[C])."),
		conversionHandler("handlerConvertFloat64ToUint64", "FLOAT_TO_UNSIGNED",
			"handlerConvertFloat64ToUint64 sets uints[B] = uint64(floats[C])."),
		bankCrossingHandler("handlerConvertBoolToInt64", boolBankIntoIntBank,
			"handlerConvertBoolToInt64 sets ints[B] to one when bools[C] is true and zero otherwise."),
		bankCrossingHandler("handlerConvertInt64ToBool", intBankIntoBoolBank,
			"handlerConvertInt64ToBool sets bools[B] = ints[C] != 0."),
		bankCrossingHandler("handlerConvertInt64ToUint64", intBankIntoUintBank,
			"handlerConvertInt64ToUint64 sets uints[B] = uint64(ints[C])."),
		bankCrossingHandler("handlerConvertUint64ToInt64", uintBankIntoIntBank,
			"handlerConvertUint64ToInt64 sets ints[B] = int64(uints[C])."),
	}
}

// bankCrossingHandler builds a tier-1 handler that carries a value between the int bank
// and the uint or bool bank.
//
// Takes name (string) which is the handler function name.
// Takes crossing (bankCrossing) which selects the source and destination banks.
// Takes comment (string) which is the handler's doc comment.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func bankCrossingHandler(name string, crossing bankCrossing, comment string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   comment,
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			const (
				scratchDestination = 0
				scratchSource      = 1
				scratchValue       = 2
				scratchBase        = 3
			)
			scratches := architecture.ScratchRegisters()
			destination, source := scratches[scratchDestination], scratches[scratchSource]
			value, base := scratches[scratchValue], scratches[scratchBase]

			architecture.ExtractB(emitter, destination)
			architecture.ExtractC(emitter, source)
			switch crossing {
			case uintBankIntoIntBank:
				architecture.LoadFromUintBank(emitter, source, value, base)
				architecture.StoreToBank(emitter, asmgen.RegisterBankInteger, value, destination)
			case intBankIntoUintBank:
				architecture.LoadFromBank(emitter, asmgen.RegisterBankInteger, source, value)
				architecture.StoreToUintBank(emitter, value, destination, base)
			case boolBankIntoIntBank:
				architecture.LoadFromBoolBank(emitter, source, value, base)
				architecture.StoreToBank(emitter, asmgen.RegisterBankInteger, value, destination)
			case intBankIntoBoolBank:
				architecture.LoadFromBank(emitter, asmgen.RegisterBankInteger, source, base)
				architecture.LogicalSetNonZero(emitter, value, base)
				architecture.StoreToBoolBank(emitter, value, destination, base)
			}
			architecture.DispatchNext(emitter)
		},
	}
}

// conversionHandler builds a tier-1 conversion handler.
//
// Takes name (string) which is the handler function name.
// Takes direction (string) which is the conversion direction mnemonic.
// Takes comment (string) which is the handler's doc comment.
//
// Returns HandlerDefinition[BytecodeArchitecturePort] which is the handler definition.
func conversionHandler(name, direction, comment string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   comment,
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			scratches := architecture.ScratchRegisters()
			architecture.ExtractB(emitter, scratches[0])
			architecture.ExtractC(emitter, scratches[1])
			architecture.FloatConversion(emitter, direction, scratches[0], scratches[1])
			architecture.DispatchNext(emitter)
		},
	}
}
