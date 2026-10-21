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
	// layoutKindBool is the reflect Kind byte for bool struct fields, matched against the
	// layout table's Kind column by the tier-0 struct-field handlers.
	layoutKindBool = "$1"

	// layoutKindInt is the reflect Kind byte for machine-word int struct fields.
	layoutKindInt = "$2"

	// layoutKindInt64 is the reflect Kind byte for int64 struct fields.
	layoutKindInt64 = "$6"

	// layoutKindUint is the reflect Kind byte for machine-word uint struct fields.
	layoutKindUint = "$7"

	// layoutKindUint64 is the reflect Kind byte for uint64 struct fields.
	layoutKindUint64 = "$11"

	// layoutKindFloat64 is the reflect Kind byte for float64 struct fields.
	layoutKindFloat64 = "$14"

	// layoutKindNone marks a single-kind handler family (no secondary accepted Kind).
	layoutKindNone = ""
)

// structFieldScalarGetDefinition builds one tier-0 scalar struct-field GET handler.
// Narrow widths, flagged layouts and exotic receiver shapes fall back to the suppressed
// tier-2 shim named by shimSymbol, which re-runs the instruction through the Go handler.
//
// Takes name (string) which is the handler symbol, e.g. "handlerGetStructFieldInt64".
// Takes kindA (string) which is the primary accepted layout Kind byte immediate.
// Takes kindB (string) which is the secondary accepted layout Kind byte immediate.
// Takes bank (string) which names the destination register bank.
// Takes shimSymbol (string) which is the fallback tier-2 shim handler name.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func structFieldScalarGetDefinition(name, kindA, kindB, bank, shimSymbol string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   name + " sets " + bank + "[A] = receiver[B].field(layout[C]) for full-width scalar fields. Other shapes fall back to " + shimSymbol + ".",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitStructFieldScalarGet(emitter, kindA, kindB, bank, shimSymbol)
		},
	}
}

// structFieldScalarSetDefinition builds one tier-0 scalar struct-field SET handler.
// Scalar stores carry no pointers so the handlers need no write barrier.
//
// Takes name (string) which is the handler symbol, e.g. "handlerSetStructFieldInt64".
// Takes kindA (string) which is the primary accepted layout Kind byte immediate.
// Takes kindB (string) which is the secondary accepted layout Kind byte immediate.
// Takes bank (string) which names the source register bank.
// Takes shimSymbol (string) which is the fallback tier-2 shim handler name.
//
// Returns asmgen.HandlerDefinition[BytecodeArchitecturePort] for this opcode.
func structFieldScalarSetDefinition(name, kindA, kindB, bank, shimSymbol string) asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      name,
		Comment:   name + " sets receiver[A].field(layout[C]) = " + bank + "[B] for full-width scalar fields. Other shapes fall back to " + shimSymbol + ".",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitStructFieldScalarSet(emitter, kindA, kindB, bank, shimSymbol)
		},
	}
}

// structFieldHandlers returns the tier-0 scalar struct-field handler family: pure-ASM
// GET/SET for int, uint, float64 and bool fields on pointer or addressable-struct
// receivers. Each op's tier-2 shim is still emitted (install-suppressed) as the fallback
// target for shapes the ASM gates reject.
//
// Returns the slice of handler definitions in jump-table order.
func structFieldHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		structFieldScalarGetDefinition("handlerGetStructFieldInt64", layoutKindInt, layoutKindInt64, "ints", "handlerPathBShimGetStructFieldInt64"),
		structFieldScalarSetDefinition("handlerSetStructFieldInt64", layoutKindInt, layoutKindInt64, "ints", "handlerPathBShimSetStructFieldInt64"),
		structFieldScalarGetDefinition("handlerGetStructFieldUint64", layoutKindUint, layoutKindUint64, "uints", "handlerPathBShimGetStructFieldUint64"),
		structFieldScalarSetDefinition("handlerSetStructFieldUint64", layoutKindUint, layoutKindUint64, "uints", "handlerPathBShimSetStructFieldUint64"),
		structFieldScalarGetDefinition("handlerGetStructFieldFloat64", layoutKindFloat64, layoutKindNone, "floats", "handlerPathBShimGetStructFieldFloat64"),
		structFieldScalarSetDefinition("handlerSetStructFieldFloat64", layoutKindFloat64, layoutKindNone, "floats", "handlerPathBShimSetStructFieldFloat64"),
		structFieldScalarGetDefinition("handlerGetStructFieldBool", layoutKindBool, layoutKindNone, "bools", "handlerPathBShimGetStructFieldBool"),
		structFieldScalarSetDefinition("handlerSetStructFieldBool", layoutKindBool, layoutKindNone, "bools", "handlerPathBShimSetStructFieldBool"),
		{
			Name: "handlerGetStructFieldElementField",
			Comment: "handlerGetStructFieldElementField sets bank(ext2.C)[A] = " +
				"receiver[B].field(layout[C])[ints[ext1.C]].subfield(ext2.wide). Other shapes " +
				"fall back to handlerPathBShimGetStructFieldElementField.",
			FrameSize: frameSizeZero,
			Flags:     flagsNoSplitNoFrame,
			Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
				architecture.EmitStructFieldSliceIndexScalar(emitter, "handlerPathBShimGetStructFieldElementField")
			},
		},
		{
			Name:      "handlerLengthStructField",
			Comment:   "handlerLengthStructField sets ints[A] = len(receiver[B].field(layout[C])) for slice-kind fields. Other shapes fall back to handlerPathBShimLengthStructField.",
			FrameSize: frameSizeZero,
			Flags:     flagsNoSplitNoFrame,
			Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
				architecture.EmitStructFieldSliceLen(emitter, "handlerPathBShimLengthStructField")
			},
		},
	}
}
