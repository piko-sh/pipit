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

// Package asm tier-2 ASM-call-shim definitions.
package asm

import (
	"slices"

	"piko.sh/asmgen"
)

// PathBShimSpec describes one ASM shim that wraps a Go tier-2 handler. The asmgen
// Tier2CallShim primitive consumes one of these per shim and emits the per-arch ASM body.
type PathBShimSpec struct {
	// ShimSymbol is the Plan-9 ASM symbol name the shim emits under, without the leading
	// middle dot.
	ShimSymbol string

	// TrampolineSymbol is the Plan-9 ASM symbol of the Go trampoline, with the leading
	// middle dot and trailing (SB).
	TrampolineSymbol string

	// JumpTableSymbol identifies the dispatch table for the shim. Empty means the default
	// tier-0 table (asmJumpTable).
	JumpTableSymbol string

	// JumpTableOffset is the byte offset within JumpTableSymbol's table where the shim
	// address is written. For tier-0 opcodes this is int(opXxx) * 8.
	JumpTableOffset int

	// InstallSuppressed skips the jump-table installation so a pure-ASM handler can own the
	// slot and JMP to the shim as its slow path.
	InstallSuppressed bool

	// NeedsFrameRebuild reports whether the handler may return opFrameChanged.
	NeedsFrameRebuild bool

	// IsNarrow reports whether the handler ignores frame.programCounter. Narrow shims skip
	// the pre-CALL PC writes.
	IsNarrow bool
}

var (
	// pathBShims is the registry of all tier-2 shim specs to emit.
	pathBShims = []PathBShimSpec{}
)

// RegisterPathBShim appends a new shim spec to the registry.
//
// Takes spec (PathBShimSpec) which describes the shim to register.
func RegisterPathBShim(spec PathBShimSpec) {
	pathBShims = append(pathBShims, spec)
}

// PathBShims returns the current set of tier-2 shim specs to emit. Exposed for the asmgen
// driver and for the audit test in handlers_pathb_shims_audit_test.go.
//
// Returns a copy of the registered shim specs slice.
func PathBShims() []PathBShimSpec {
	return slices.Clone(pathBShims)
}

// pathBShimHandlerDefinitions returns one HandlerDefinition per registered tier-2 shim
// spec.
//
// Returns []asmgen.HandlerDefinition[BytecodeArchitecturePort] with one entry per spec.
func pathBShimHandlerDefinitions() []asmgen.HandlerDefinition[BytecodeArchitecturePort] {
	specs := PathBShims()
	if len(specs) == 0 {
		return nil
	}
	out := make([]asmgen.HandlerDefinition[BytecodeArchitecturePort], 0, len(specs))
	for _, spec := range specs {
		out = append(out, asmgen.HandlerDefinition[BytecodeArchitecturePort]{
			Name:      spec.ShimSymbol,
			Comment:   spec.ShimSymbol + " calls " + spec.TrampolineSymbol + " then tail-JMPs DISPATCH_NEXT on opContinue or dispatchExit on cold paths.",
			FrameSize: frameSizeZero,
			Flags:     flagsNoSplitNoFrame,
			ArchFlags: map[asmgen.Architecture]string{
				asmgen.ArchitectureARM64: flagNoSplit,
			},
			ArchFrameSize: map[asmgen.Architecture]string{
				asmgen.ArchitectureARM64: frameSizeShim2ArgARM64,
			},
			Emit: func(emitter *asmgen.Emitter, architecture BytecodeArchitecturePort) {
				if spec.IsNarrow {
					architecture.EmitTier2CallShimNarrow(emitter, spec.TrampolineSymbol)
				} else {
					architecture.EmitPathBCallShim(emitter, spec.TrampolineSymbol, spec.NeedsFrameRebuild)
				}
			},
		})
	}
	return out
}
