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

// generalBankHandlers lists the tier-0 handlers that operate on general-bank
// reflect.Value slots without leaving the dispatch loop.
var generalBankHandlers = []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
	{
		Name: "handlerSliceIndexStructFieldFloat",
		Comment: "handlerSliceIndexStructFieldFloat sets floats[A] = slice(general[B])[ints[C]].field(layout[ext]) for float64 " +
			"leaves; other shapes fall back to handlerPathBShimSliceIndexStructFieldFloat.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitSliceIndexStructFieldScalar(emitter, "$14", "", true, "handlerPathBShimSliceIndexStructFieldFloat")
		},
	},
	{
		Name: "handlerSliceIndexStructFieldInt",
		Comment: "handlerSliceIndexStructFieldInt sets ints[A] = slice(general[B])[ints[C]].field(layout[ext]) for full-width " +
			"int leaves; other shapes fall back to handlerPathBShimSliceIndexStructFieldInt.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitSliceIndexStructFieldScalar(emitter, "$2", "$6", false, "handlerPathBShimSliceIndexStructFieldInt")
		},
	},
	{
		Name:      "handlerDerefSliceGetInt",
		Comment:   "handlerDerefSliceGetInt sets ints[A] = (*general[B])[ints[C]] with a live bounds check; unsupported shapes and range panics fall back to handlerPathBShimDerefSliceGetInt.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitDerefSliceIntAccess(emitter, false, "handlerPathBShimDerefSliceGetInt")
		},
	},
	{
		Name:      "handlerDerefSliceSetInt",
		Comment:   "handlerDerefSliceSetInt sets (*general[A])[ints[B]] = ints[C] with a live bounds check; unsupported shapes and range panics fall back to handlerPathBShimDerefSliceSetInt.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitDerefSliceIntAccess(emitter, true, "handlerPathBShimDerefSliceSetInt")
		},
	},
	{
		Name: "handlerGetStructFieldGeneralT0",
		Comment: "handlerGetStructFieldGeneralT0 reads a plain pointer-kind struct field into general[A] under the write-barrier " +
			"guard; other field kinds and receiver shapes fall back to handlerPathBShimGetStructFieldGeneralT0.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitGetStructFieldGeneralPointerT0(emitter)
		},
	},
	{
		Name: "handlerSetStructFieldGeneralT0",
		Comment: "handlerSetStructFieldGeneralT0 stores general[B] into a plain pointer-kind or cycle-broken any field of " +
			"receiver general[A] under the write-barrier guard; other field kinds, value kinds and receiver shapes fall " +
			"back to handlerPathBShimSetStructFieldGeneralT0.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitSetStructFieldGeneralT0(emitter)
		},
	},
	{
		Name:      "handlerGetStructFieldRawPointerT0",
		Comment:   "handlerGetStructFieldRawPointerT0 re-wraps the cycle-broken eface field at layout[C] through receiver[B] into a direct pointer value in general[A] under the write-barrier guard.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitGetStructFieldRawPointerT0(emitter)
		},
	},
	{
		Name: "handlerEqGeneral",
		Comment: "handlerEqGeneral sets ints[A] = (general[B] == general[C]) for invalid pairs, differing type words, and " +
			"pointer-identity kinds; other shapes fall back to handlerPathBShimEqGeneral.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitEqGeneral(emitter)
		},
	},
	{
		Name:      "handlerMoveGeneral",
		Comment:   "handlerMoveGeneral copies general[B] to general[A] for alias mode under the write-barrier guard; other modes and marking phases fall back to handlerPathBShimMoveGeneral.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitMoveGeneralAlias(emitter)
		},
	},
	{
		Name:      "handlerTestNilJumpTrue",
		Comment:   "handlerTestNilJumpTrue jumps by signed offset B|C<<8 when general[A] is invalid or a nil value of a nilable kind.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTestNilJump(emitter, true)
		},
	},
	{
		Name:      "handlerTestNilJumpFalse",
		Comment:   "handlerTestNilJumpFalse jumps by signed offset B|C<<8 when general[A] is valid and non-nil.",
		FrameSize: frameSizeZero,
		Flags:     flagsNoSplitNoFrame,
		Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
			architecture.EmitTestNilJump(emitter, false)
		},
	},
}
