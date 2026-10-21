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
	"maps"
	"regexp"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

var pinnedRegisters = map[string]map[string]bool{
	"amd64": {"R8": true, "R9": true, "R10": true, "R11": true, "R12": true, "R13": true, "R15": true},
	"arm64": {"R19": true, "R21": true, "R22": true, "R23": true, "R24": true, "R25": true, "R26": true},
}

var frameSwitchingHandlers = map[string]bool{
	"dispatchLoop":      true,
	"handlerCall":       true,
	"handlerCallScalar": true,
	"handlerCallMethod": true,
	"handlerReturn":     true,
	"handlerReturnVoid": true,
}

var pinnedRestore = regexp.MustCompile(`^(?:CTX_\w+\((?:R15|R19)\)|-?\d+\((?:SP|RSP)\))$`)

func pinnedRegisterWrites(arch wbArchitecture, handlers map[string][]wbInstruction) []string {
	var writes []string
	for _, name := range slices.Sorted(maps.Keys(handlers)) {
		if frameSwitchingHandlers[name] {
			continue
		}
		for _, instruction := range handlers[name] {
			if isBranch(instruction.op) || isComparison(instruction.op) || len(instruction.operands) == 0 {
				continue
			}
			operands := make([]wbOperand, len(instruction.operands))
			for index, text := range instruction.operands {
				operands[index] = parseOperand(text)
			}
			for _, register := range destinationRegisters(instruction, operands) {
				if !pinnedRegisters[arch.name][register] {
					continue
				}
				if len(instruction.operands) == 2 && pinnedRestore.MatchString(instruction.operands[0]) {
					continue
				}
				writes = append(writes, name+": "+instruction.text)
			}
		}
	}
	return writes
}

func TestNoHandlerClobbersAPinnedRegister(t *testing.T) {
	t.Parallel()
	for _, arch := range wbArchitectures {
		t.Run(arch.name, func(t *testing.T) {
			t.Parallel()

			writes := pinnedRegisterWrites(arch, generatedHandlers(t, arch))

			require.Empty(t, writes, "these handlers overwrite a register the dispatch loop keeps pinned")
		})
	}
}

func TestPinnedRegisterCheckFlagsAScratchUse(t *testing.T) {
	t.Parallel()
	handlers, err := parseHandlers("TEXT ·fixture(SB), NOSPLIT|NOFRAME, $0\n" +
		"MOVQ CTX_INTS_BASE(R15), R8\nMOVQ CX, R10\nSHLQ $3, R10\nADDQ R10, BX\nDISPATCH_NEXT()\n")
	require.NoError(t, err)

	writes := pinnedRegisterWrites(wbArchitectures[0], handlers)

	require.Equal(t, []string{"fixture: MOVQ CX, R10", "fixture: SHLQ $3, R10"}, writes,
		"a reload from the context is fine; using the jump-table register as scratch is not")
}
