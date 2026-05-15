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

package engine

import (
	"pipit.sh/pipit/internal/engine/asm"
	"pipit.sh/pipit/internal/isa"
)

const (
	// expectedTier2ShimCount is the pre-allocation hint for the shim spec slice; sized
	// comfortably above the current entry count so the underlying array does not grow when
	// categories are added.
	expectedTier2ShimCount = 96
)

// shimRegistration pairs an opcode with the matching handler, shim suffix, and narrow
// flag used by makeShimSpec. Every shim is one row of pathBShimRegistry, which
// gen_opcode_tables.go emits into pathb_shim_registry_generated.go from the isa spec
// rows; asmgen emits the shim bodies in that order.
type shimRegistration struct {
	// HandlerName is the trampoline base name; the full Plan-9 trampoline symbol prefixes a
	// package separator before "asmCall<HandlerName>(SB)".
	HandlerName string

	// ShimSuffix is the suffix appended to "handlerPathBShim".
	ShimSuffix string

	// Tier is the encoding tier of the operation whose jump-table slot the shim populates,
	// which selects the table the shim installs into.
	Tier isa.Tier

	// Code is the operation's value within its tier, which is its slot in that table.
	Code uint8

	// Narrow tags handlers that do not read or write frame.programCounter, selecting the
	// slim shim body + Go trampoline pair (skipping the pre-CALL CTX_PC / CTX_SAVED_PC
	// writes and the frame.programCounter sync/writeback). The narrow path roughly halves
	// the per-call trampoline self-cost on the opContinue branch.
	Narrow bool

	// InstallSuppressed keeps the shim emitted as a fallback JMP target while a pure-ASM
	// tier-0 handler owns the opcode's jump-table slot.
	InstallSuppressed bool
}

// shimJumpTableSymbol names the jump table a shim installs into for an encoding tier. The
// empty string is the tier-0 table, matching AsmHandlerJumpTableEntry.TableSymbol.
//
// Takes tier (isa.Tier) which is the operation's encoding depth.
//
// Returns string which is the table symbol.
func shimJumpTableSymbol(tier isa.Tier) string {
	switch tier {
	case isa.TierSub1:
		return tier1JumpTableSymbol
	case isa.TierSub2:
		return tier2JumpTableSymbol
	case isa.TierSub3:
		return tier3JumpTableSymbol
	default:
		return ""
	}
}

// makeShimSpec builds a PathBShimSpec using the standard naming convention.
//
// ShimSymbol is "handlerPathBShim<suffix>", TrampolineSymbol is the Plan-9 form prefixed
// with a package separator before "asmCall<HandlerName>(SB)".
//
// Takes registration (shimRegistration) which supplies the tier and code, handler name,
// shim suffix, and narrow flag.
//
// Returns asm.PathBShimSpec ready to append to the registry.
func makeShimSpec(registration shimRegistration) asm.PathBShimSpec {
	return asm.PathBShimSpec{
		ShimSymbol:        "handlerPathBShim" + registration.ShimSuffix,
		TrampolineSymbol:  "·asmCall" + registration.HandlerName + "(SB)",
		NeedsFrameRebuild: false,
		IsNarrow:          registration.Narrow,
		InstallSuppressed: registration.InstallSuppressed,
		JumpTableSymbol:   shimJumpTableSymbol(registration.Tier),
		JumpTableOffset:   int(registration.Code) * bytesPerJumpTableSlot,
	}
}

// pathBShimRegistrations converts the registry table into the canonical
// []asm.PathBShimSpec, preserving order.
//
// Returns []asm.PathBShimSpec built from the registry entries.
func pathBShimRegistrations() []asm.PathBShimSpec {
	specs := make([]asm.PathBShimSpec, 0, expectedTier2ShimCount)
	for _, registration := range pathBShimRegistry {
		specs = append(specs, makeShimSpec(registration))
	}
	return specs
}

func init() {
	for _, spec := range pathBShimRegistrations() {
		asm.RegisterPathBShim(spec)
	}
}
