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
)

const (
	// bytesPerJumpTableSlot is the stride (in bytes) between consecutive uintptr entries in
	// the ASM dispatch jump tables. Each slot stores a handler address (uintptr = 8 bytes on
	// every supported target).
	bytesPerJumpTableSlot = 8

	// tier1JumpTableSymbol is the ASM symbol name for the tier-1 jump table.
	tier1JumpTableSymbol = "tier1JumpTable"

	// tier2JumpTableSymbol is the ASM symbol name for the tier-2 jump table.
	tier2JumpTableSymbol = "tier2JumpTable"

	// tier3JumpTableSymbol is the ASM symbol name for the tier-3 jump table.
	tier3JumpTableSymbol = "tier3JumpTable"
)

// ProvideAsmHandlerJumpTableEntries returns the list of (handler, offset) pairs for every
// ASM handler that needs a jump-table installation.
//
// Returns []asm.AsmHandlerJumpTableEntry which lists every handler installation entry,
// with tier-2 shim entries appended after the static set.
func ProvideAsmHandlerJumpTableEntries() []asm.AsmHandlerJumpTableEntry {
	staticEntries := buildStaticJumpTableEntries()
	shims := asm.PathBShims()
	entries := make([]asm.AsmHandlerJumpTableEntry, len(staticEntries), len(staticEntries)+len(shims))
	copy(entries, staticEntries)
	return appendPathBShimEntries(entries, shims)
}

// appendPathBShimEntries appends tier-2 ASM-call shim install entries to entries.
//
// Each shim wraps a Go tier-2 handler so the common opContinue path resumes DISPATCH_NEXT
// in ASM (see asm/handlers_pathb_shims.go and pathb_shim_registry.go). The registry is
// populated at package init from pathBShimRegistrations; iterating it here means adding a
// new shim only requires editing pathBShimRegistrations.
//
// Takes entries ([]asm.AsmHandlerJumpTableEntry) which are the pre-existing static
// jump-table entries to extend.
// Takes shims ([]asm.PathBShimSpec) which are the tier-2 shim specifications to append.
//
// Returns the entries slice with one appended item per shim specification.
func appendPathBShimEntries(entries []asm.AsmHandlerJumpTableEntry, shims []asm.PathBShimSpec) []asm.AsmHandlerJumpTableEntry {
	for _, spec := range shims {
		if spec.InstallSuppressed {
			continue
		}
		entries = append(entries, asm.AsmHandlerJumpTableEntry{
			Name:        spec.ShimSymbol,
			TableSymbol: spec.JumpTableSymbol,
			Offset:      spec.JumpTableOffset,
		})
	}
	return entries
}
