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

// generalBankHandlers lists handlers for general registers and package variables.
var generalBankHandlers = []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
	{
		Name: "handlerGetGlobal",
		Comment: "handlerGetGlobal loads the package variable B (shifted by the function's global base) of kind C into " +
			"bank(C)[A] for the int, float, bool, uint and general banks of an unshared store, a general one under the " +
			"write-barrier guard. Other kinds and shapes fall back to handlerPathBShimGetGlobal.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitGetGlobal(emitter, "handlerPathBShimGetGlobal")
		},
	},
	{
		Name: "handlerGetGlobalWide",
		Comment: "handlerGetGlobalWide loads the package variable whose index is in the extension word (shifted by the " +
			"function's global base) of kind C into bank(C)[B], as handlerGetGlobal does.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitGetGlobalWide(emitter, "handlerPathBShimGetGlobalWide")
		},
	},
	{
		Name: "handlerSetGlobal",
		Comment: "handlerSetGlobal stores bank(C)[A] into the package variable B (shifted by the function's global base) of " +
			"kind C, for the int, uint, float and bool banks of an unshared store.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitSetGlobal(emitter, "handlerPathBShimSetGlobal")
		},
	},
	{
		Name: "handlerSetGlobalWide",
		Comment: "handlerSetGlobalWide stores bank(C)[B] into the package variable whose index is in the extension word, as " +
			"handlerSetGlobal does.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitSetGlobalWide(emitter, "handlerPathBShimSetGlobalWide")
		},
	},
	{
		Name: "handlerGetElementInt64",
		Comment: "handlerGetElementInt64 sets ints[A] = general[B][ints[C]] for an integer element of a slice, array or " +
			"array pointer whose shape the typed element cache's front slot holds. Other shapes fall back to handlerPathBShimGetElementInt64.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitSliceGetTypedElement(emitter, true, "handlerPathBShimGetElementInt64")
		},
	},
	{
		Name: "handlerGetElementUint64",
		Comment: "handlerGetElementUint64 sets uints[A] = general[B][ints[C]] for an unsigned element of a slice, array or " +
			"array pointer whose shape the typed element cache's front slot holds. Other shapes fall back to handlerPathBShimGetElementUint64.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitSliceGetTypedElement(emitter, false, "handlerPathBShimGetElementUint64")
		},
	},
	{
		Name: "handlerSetElementInt64",
		Comment: "handlerSetElementInt64 stores ints[C] into integer element ints[B] of the slice, array or array pointer " +
			"in general[A] whose shape the typed element cache's front slot holds.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitSliceSetTypedElement(emitter, true, "handlerPathBShimSetElementInt64")
		},
	},
	{
		Name: "handlerSetElementUint64",
		Comment: "handlerSetElementUint64 stores uints[C] into unsigned element ints[B] of the slice, array or array " +
			"pointer in general[A] whose shape the typed element cache's front slot holds.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitSliceSetTypedElement(emitter, false, "handlerPathBShimSetElementUint64")
		},
	},
	{
		Name: "handlerGetElementFieldFloat64",
		Comment: "handlerGetElementFieldFloat64 sets floats[A] = slice(general[B])[ints[C]].field(layout[ext]) for float64 " +
			"leaves. Other shapes fall back to handlerPathBShimGetElementFieldFloat64.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitSliceIndexStructFieldScalar(emitter, layoutKindFloat64, layoutKindNone, true, "handlerPathBShimGetElementFieldFloat64")
		},
	},
	{
		Name: "handlerGetElementFieldInt64",
		Comment: "handlerGetElementFieldInt64 sets ints[A] = slice(general[B])[ints[C]].field(layout[ext]) for full-width " +
			"int leaves. Other shapes fall back to handlerPathBShimGetElementFieldInt64.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitSliceIndexStructFieldScalar(emitter, layoutKindInt, layoutKindInt64, false, "handlerPathBShimGetElementFieldInt64")
		},
	},
	{
		Name: "handlerSetElementFieldInt64",
		Comment: "handlerSetElementFieldInt64 stores ints[A] into slice(general[B])[ints[C]].field(layout[ext]) for " +
			"full-width int fields.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitSliceIndexStructFieldScalarStore(emitter, layoutKindInt, layoutKindInt64, false, "handlerPathBShimSetElementFieldInt64")
		},
	},
	{
		Name: "handlerSetElementFieldFloat64",
		Comment: "handlerSetElementFieldFloat64 stores floats[A] into slice(general[B])[ints[C]].field(layout[ext]) for " +
			"float64 fields.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitSliceIndexStructFieldScalarStore(emitter, layoutKindFloat64, layoutKindNone, true, "handlerPathBShimSetElementFieldFloat64")
		},
	},
	{
		Name: "handlerGetElementInt64ViaPointer",
		Comment: "handlerGetElementInt64ViaPointer sets ints[A] = (*general[B])[ints[C]] with a live bounds check. " +
			"Unsupported shapes and range panics fall back to handlerPathBShimGetElementInt64ViaPointer.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitElementInt64ViaPointerAccess(emitter, false, "handlerPathBShimGetElementInt64ViaPointer")
		},
	},
	{
		Name: "handlerSetElementInt64ViaPointer",
		Comment: "handlerSetElementInt64ViaPointer sets (*general[A])[ints[B]] = ints[C] with a live bounds check. " +
			"Unsupported shapes and range panics fall back to handlerPathBShimSetElementInt64ViaPointer.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitElementInt64ViaPointerAccess(emitter, true, "handlerPathBShimSetElementInt64ViaPointer")
		},
	},
	{
		Name: "handlerGetStructFieldGeneral",
		Comment: "handlerGetStructFieldGeneral reads a plain pointer-kind struct field into general[A] under the write-barrier " +
			"guard. Other field kinds and receiver shapes fall back to handlerPathBShimGetStructFieldGeneral.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitGetStructFieldGeneralPointer(emitter)
		},
	},
	{
		Name: "handlerSetStructFieldGeneral",
		Comment: "handlerSetStructFieldGeneral stores general[B] into a pointer or any field of general[A] when the store " +
			"can skip the barrier. It skips it for a nil pointer, or a pointer while the arena's epoch is revoked and local-blind. Every " +
			"other store falls back to handlerPathBShimSetStructFieldGeneral and its store barrier.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitSetStructFieldGeneral(emitter)
		},
	},
	{
		Name:      "handlerGetStructFieldRawPointer",
		Comment:   "handlerGetStructFieldRawPointer re-wraps the cycle-broken eface field at layout[C] through receiver[B] into a direct pointer value in general[A] under the write-barrier guard.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitGetStructFieldRawPointer(emitter)
		},
	},
	{
		Name: "handlerEqualGeneral",
		Comment: "handlerEqualGeneral sets ints[A] = (general[B] == general[C]) for invalid pairs, differing type words, and " +
			"pointer-identity kinds. Other shapes fall back to handlerPathBShimEqualGeneral.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitEqualGeneral(emitter)
		},
	},
	{
		Name:      "handlerMoveGeneral",
		Comment:   "handlerMoveGeneral copies general[B] to general[A] for alias mode under the write-barrier guard. Other modes and marking phases fall back to handlerPathBShimMoveGeneral.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitMoveGeneralAlias(emitter)
		},
	},
	{
		Name:      "handlerJumpIfNil",
		Comment:   "handlerJumpIfNil jumps by signed offset B|C<<8 when general[A] is invalid or a nil value of a nilable kind.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTestNilJump(emitter, true)
		},
	},
	{
		Name:      "handlerJumpIfNotNil",
		Comment:   "handlerJumpIfNotNil jumps by signed offset B|C<<8 when general[A] is valid and non-nil.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTestNilJump(emitter, false)
		},
	},
}
