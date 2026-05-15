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

//go:build !safe && !(js && wasm) && (amd64 || arm64)

package engine

import (
	"reflect"

	"pipit.sh/pipit/internal/isa"
)

const (
	// pathBBatchLimit caps consecutive Go-fallback opcodes per ASM exit.
	pathBBatchLimit = 256

	// exitAddressMapInitialCapacity is the expected number of distinct ASM-side exit stubs
	// (pathBFallback plus the per-op direct exits). Used to pre-size the deduplication map
	// in initOpNeedsGoFallback.
	exitAddressMapInitialCapacity = 8

	// flatTableTier1Base is the start of the tier-1 region in flatJumpTable. Slots
	// [tier1Base..tier1Base+255] hold tier-1 handler addresses.
	flatTableTier1Base = 256

	// flatTableTier2Base is the start of the tier-2 region in flatJumpTable. Slots
	// [tier2Base..tier2Base+255] hold tier-2 handler addresses.
	flatTableTier2Base = 512

	// flatTableTier3Base is the start of the tier-3 region in flatJumpTable. Slots
	// [tier3Base..tier3Base+255] hold tier-3 handler addresses.
	flatTableTier3Base = 768
)

var (
	// flatGoFallback[index] is true when flatJumpTable[index] points at pathBFallback,
	// mirroring flatJumpTable's four 256-slot tiers and populated once at init by
	// initOpNeedsGoFallback.
	flatGoFallback [flatJumpTableSize]bool
)

// initOpNeedsGoFallback builds flatGoFallback from the finalised flatJumpTable. Must run
// after installFlatJumpTableASM.
func initOpNeedsGoFallback() {
	exitAddresses := make(map[uintptr]struct{}, exitAddressMapInitialCapacity)
	exitAddresses[reflect.ValueOf(pathBFallback).Pointer()] = struct{}{}
	for _, address := range directExitHandlerAddresses {
		exitAddresses[address] = struct{}{}
	}
	for i := range flatJumpTable {
		_, fallback := exitAddresses[flatJumpTable[i]]
		flatGoFallback[i] = fallback
	}
}

// instructionWouldTrampoline reports whether ASM dispatch would exit to Go.
//
// Takes instr (isa.Instruction) which is the 4-byte word to inspect.
//
// Returns true when dispatch of instr lands at pathBFallback.
func instructionWouldTrampoline(instr isa.Instruction) bool {
	if instr.Op != 0 {
		return flatGoFallback[uint(instr.Op)]
	}
	if instr.A != 0 {
		return flatGoFallback[flatTableTier1Base+uint(instr.A)]
	}
	if instr.B != 0 {
		return flatGoFallback[flatTableTier2Base+uint(instr.B)]
	}
	if instr.C != 0 {
		return flatGoFallback[flatTableTier3Base+uint(instr.C)]
	}
	return false
}
