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
	"fmt"
	"slices"

	"piko.sh/asmgen"
)

// DirectExitHandlerSpec describes one direct-exit ASM stub.
type DirectExitHandlerSpec struct {
	// Name is the Plan-9 ASM symbol the stub emits under, without the leading middle dot
	// (for example "handlerGetFieldExit"). The matching Go forward declaration must use the
	// same identifier so the linker resolves the stub address used by asmJumpTable.
	Name string

	// ExitReason is the numeric ExitReason value the stub writes to CTX_EXIT_REASON before
	// returning to Go. Must match the corresponding `exitXxx int64 = N` constant in
	// engine/vm_dispatch.go.
	ExitReason int64
}

var (
	// directExitHandlers is the registry of all direct-exit stubs to emit.
	directExitHandlers = []DirectExitHandlerSpec{}
)

// RegisterDirectExit appends a new direct-exit spec to the registry.
//
// Takes spec (DirectExitHandlerSpec) which describes the stub to register.
func RegisterDirectExit(spec DirectExitHandlerSpec) {
	directExitHandlers = append(directExitHandlers, spec)
}

// DirectExitHandlers returns a copy of the registered direct-exit specs. Exposed for the
// asmgen driver.
//
// Returns a copy of the registered direct-exit specs slice.
func DirectExitHandlers() []DirectExitHandlerSpec {
	return slices.Clone(directExitHandlers)
}

// directExitHandlerDefinitions returns one HandlerDefinition per registered direct-exit
// spec. Each stub decrements PC and exits with the spec's reason code.
//
// Returns []asmgen.HandlerDefinition[BytecodeArchitecturePort] with one entry per
// registered spec.
func directExitHandlerDefinitions() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	specs := DirectExitHandlers()
	if len(specs) == 0 {
		return nil
	}
	out := make([]asmgen.HandlerDefinition[BytecodeArchitecturePort], 0, len(specs))
	for _, spec := range specs {
		reasonLiteral := fmt.Sprintf("%d", spec.ExitReason)
		out = append(out, asmgen.HandlerDefinition[BytecodeArchitecturePort]{
			Name: spec.Name,
			Comment: fmt.Sprintf(
				"Direct-exit stub - un-advance PC, write CTX_EXIT_REASON=%d, RET to Go. "+
					"Routes through handleDispatchExit by exit reason to a dedicated "+
					"processExitXxx that calls the specific Go handler without the "+
					"handlerTable[op] indirect.",
				spec.ExitReason,
			),
			FrameSize: frameSizeZero,
			Flags:     flagsNoSplitNoFrame,
			Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
				architecture.DecrementProgramCounter(emitter)
				architecture.ExitWithReason(emitter, reasonLiteral)
			},
		})
	}
	return out
}
