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

// upvalueHandlers returns the tier-0 upvalue handler family: pure-ASM GET_UPVALUE for
// direct scalar cells (int, float, uint, bool). The op's tier-2 shim is still emitted
// (install-suppressed) as the fallback target for indirect cells and non-scalar kinds.
//
// Returns the slice of handler definitions in jump-table order.
func upvalueHandlers() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	return []asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		{
			Name:      "handlerGetUpvalueScalar",
			Comment:   "handlerGetUpvalueScalar sets bank(C)[A] = upvalues[B].scalarField(C) for direct scalar cells; other shapes fall back to handlerPathBShimGetUpvalue.",
			FrameSize: frameSizeZero,
			Flags:     flagsNoSplitNoFrame,
			Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
				architecture.EmitGetUpvalueScalar(emitter, "handlerPathBShimGetUpvalue")
			},
		},
	}
}
