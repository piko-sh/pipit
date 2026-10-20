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
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"piko.sh/asmgen"

	archamd64 "pipit.sh/pipit/internal/engine/asm/asmgen_arch_amd64"
	archarm64 "pipit.sh/pipit/internal/engine/asm/asmgen_arch_arm64"
)

var generatedAssembly = sync.OnceValues(func() (map[string][]byte, error) {
	writer := &memWriter{files: make(map[string][]byte)}
	architectures := []BytecodeArchitecturePort{archamd64.New(), archarm64.New()}
	err := asmgen.GenerateFiles(writer, architectures, FileGroups(), nil, nil)
	return writer.files, err
})

func generatedHandlers(t *testing.T, arch wbArchitecture) map[string][]wbInstruction {
	t.Helper()
	files, err := generatedAssembly()
	require.NoError(t, err)
	handlers := map[string][]wbInstruction{}
	for path, data := range files {
		name := filepath.Base(path)
		if !strings.HasPrefix(name, "asm_vm_dispatch_") || !strings.HasSuffix(name, "_"+arch.name+".s") {
			continue
		}
		parsed, parseErr := parseHandlers(string(data))
		require.NoError(t, parseErr, name)
		maps.Copy(handlers, parsed)
	}
	require.NotEmpty(t, handlers)
	return handlers
}

func analyseAll(t *testing.T, arch wbArchitecture, handlers map[string][]wbInstruction) ([]string, map[string]int) {
	t.Helper()
	var violations []string
	stores := map[string]int{}
	for name, instructions := range handlers {
		found, count, err := analyseHandler(arch, RegisterBankBases, name, instructions)
		require.NoError(t, err)
		for _, violation := range found {
			violations = append(violations, violation.String())
		}
		if count > 0 {
			stores[name] = count
		}
	}
	slices.Sort(violations)
	return violations, stores
}

func TestEveryPointerBankStoreIsGuarded(t *testing.T) {
	t.Parallel()
	for _, arch := range wbArchitectures {
		t.Run(arch.name, func(t *testing.T) {
			t.Parallel()
			violations, _ := analyseAll(t, arch, generatedHandlers(t, arch))

			require.Empty(t, violations,
				"these stores put a pointer into a register bank while the write barrier may be on; wrap the handler in guardPointerBankWriter")
		})
	}
}

func guardedHandlers(handlers map[string][]wbInstruction) []string {
	var guarded []string
	for _, name := range slices.Sorted(maps.Keys(handlers)) {
		testsBarrier, branchesToTail := false, false
		for _, instruction := range handlers[name] {
			testsBarrier = testsBarrier || strings.Contains(instruction.text, "runtime·writeBarrier")
			branchesToTail = branchesToTail || (isBranch(instruction.op) && slices.Contains(instruction.operands, "wb_fallback"))
		}
		if testsBarrier && branchesToTail {
			guarded = append(guarded, name)
		}
	}
	return guarded
}

func TestEveryGuardIsNeeded(t *testing.T) {
	t.Parallel()
	for _, arch := range wbArchitectures {
		t.Run(arch.name, func(t *testing.T) {
			t.Parallel()
			handlers := generatedHandlers(t, arch)
			_, stores := analyseAll(t, arch, handlers)
			guarded := guardedHandlers(handlers)
			require.NotEmpty(t, guarded)

			for _, name := range guarded {
				require.Positive(t, stores[name], "%s is guarded but stores no pointer into a register bank", name)
			}
		})
	}
}

func TestGuardPointerBankWriterRejectsFramedHandlers(t *testing.T) {
	t.Parallel()
	framed := asmgen.HandlerDefinition[BytecodeArchitecturePort]{
		Name:      "framed",
		FrameSize: "$32-0",
		Flags:     flagNoSplit,
		Emit:      func(*asmgen.Emitter, BytecodeArchitecturePort) {},
	}

	require.PanicsWithValue(t, "guardPointerBankWriter: framed has a frame, so it cannot tail-jump to pathBFallback",
		func() { guardPointerBankWriter(framed) })
	require.NotPanics(t, func() { guardPointerBankWriter(handlerLoadStringConst()) }, "an already guarded frame-free handler")
}

func TestWriteBarrierAnalyserFlagsAStrippedGuard(t *testing.T) {
	t.Parallel()
	for _, arch := range wbArchitectures {
		t.Run(arch.name, func(t *testing.T) {
			t.Parallel()
			handlers := generatedHandlers(t, arch)
			for _, name := range guardedHandlers(handlers) {
				stripped := slices.DeleteFunc(slices.Clone(handlers[name]), func(instruction wbInstruction) bool {
					return strings.Contains(instruction.text, "runtime·writeBarrier")
				})

				violations, _, err := analyseHandler(arch, RegisterBankBases, name, stripped)

				require.NoError(t, err)
				require.NotEmpty(t, violations, "%s without its guard must be reported", name)
			}
		})
	}
}

func TestWriteBarrierAnalyserFixtures(t *testing.T) {
	t.Parallel()

	amd64, arm64 := wbArchitectures[0], wbArchitectures[1]
	const amd64Guard = "CMPB runtime·writeBarrier(SB), $0\nJE wb_clear\nJMP ·pathBFallback(SB)\nwb_clear:\n"
	const arm64Guard = "MOVD $runtime·writeBarrier(SB), R4\nMOVBU (R4), R4\nCBZ R4, wb_clear\nB ·pathBFallback(SB)\nwb_clear:\n"
	const amd64TailGuard = "CMPB runtime·writeBarrier(SB), $0\nJNE wb_fallback\n"
	const amd64Tail = "\nwb_fallback:\nJMP ·pathBFallback(SB)"
	const arm64TailGuard = "MOVD $runtime·writeBarrier(SB), R4\nMOVBU (R4), R4\nCBNZ R4, wb_fallback\n"
	const arm64Tail = "\nwb_fallback:\nB ·pathBFallback(SB)"
	tests := []struct {
		name           string
		arch           wbArchitecture
		body           string
		wantViolations int
		wantStores     int
	}{
		{
			name:           "an unguarded store into the string bank",
			arch:           amd64,
			body:           "MOVQ CTX_STRINGS_BASE(R15), SI\nMOVQ CX, (SI)(AX*1)\nDISPATCH_NEXT()",
			wantViolations: 1, wantStores: 1,
		},
		{
			name:           "a guarded store into the string bank",
			arch:           amd64,
			body:           amd64Guard + "MOVQ CTX_STRINGS_BASE(R15), SI\nMOVQ CX, (SI)(AX*1)\nMOVQ DI, 8(SI)(AX*1)\nDISPATCH_NEXT()",
			wantViolations: 0, wantStores: 2,
		},
		{
			name:           "a store behind a guard that falls through to the body",
			arch:           amd64,
			body:           amd64TailGuard + "MOVQ CTX_STRINGS_BASE(R15), SI\nMOVQ CX, (SI)(AX*1)\nDISPATCH_NEXT()" + amd64Tail,
			wantViolations: 0, wantStores: 1,
		},
		{
			name:           "an arm64 store behind a guard that falls through to the body",
			arch:           arm64,
			body:           arm64TailGuard + "MOVD CTX_STRINGS_BASE(R19), R5\nMOVD R8, (R5)\nDISPATCH_NEXT()" + arm64Tail,
			wantViolations: 0, wantStores: 1,
		},
		{
			name:           "a store in the out-of-line tail of a guard",
			arch:           amd64,
			body:           amd64TailGuard + "DISPATCH_NEXT()\nwb_fallback:\nMOVQ CTX_STRINGS_BASE(R15), SI\nMOVQ CX, (SI)\nJMP ·pathBFallback(SB)",
			wantViolations: 1, wantStores: 1,
		},
		{
			name:           "a store into a bank without pointers",
			arch:           amd64,
			body:           "MOVQ CTX_INTS_BASE(R15), SI\nMOVQ CX, (SI)(AX*8)\nDISPATCH_NEXT()",
			wantViolations: 0, wantStores: 0,
		},
		{
			name:           "a bank base added straight from the context",
			arch:           amd64,
			body:           "MOVQ AX, BX\nADDQ CTX_GENERALS_BASE(R15), BX\nMOVQ CX, (BX)\nDISPATCH_NEXT()",
			wantViolations: 1, wantStores: 1,
		},
		{
			name:           "a comparison leaves the taint alone",
			arch:           amd64,
			body:           "MOVQ CTX_STRINGS_BASE(R15), SI\nCMPQ SI, $0\nJE done\nMOVQ CX, (SI)\ndone:\nDISPATCH_NEXT()",
			wantViolations: 1, wantStores: 1,
		},
		{
			name:           "an address computed from a tainted base",
			arch:           amd64,
			body:           "MOVQ CTX_STRINGS_BASE(R15), SI\nLEAQ (SI)(AX*1), DI\nMOVQ DI, BX\nMOVQ CX, 8(BX)\nDISPATCH_NEXT()",
			wantViolations: 1, wantStores: 1,
		},
		{
			name:           "a symbolic offset on a tainted base",
			arch:           amd64,
			body:           "MOVQ CTX_SLICES_INT_BASE(R15), SI\nMOVQ CX, SLICE_LEN(SI)\nDISPATCH_NEXT()",
			wantViolations: 1, wantStores: 1,
		},
		{
			name:           "a store into the context itself",
			arch:           amd64,
			body:           "MOVQ CTX_STRINGS_BASE(R15), SI\nMOVQ SI, CTX_EXIT_PC(R15)\nDISPATCH_NEXT()",
			wantViolations: 0, wantStores: 0,
		},
		{
			name:           "a load through a tainted base yields data",
			arch:           amd64,
			body:           "MOVQ CTX_STRINGS_BASE(R15), SI\nMOVQ (SI), DI\nMOVQ CX, (DI)\nDISPATCH_NEXT()",
			wantViolations: 0, wantStores: 0,
		},
		{
			name:           "a conditional move carries the taint",
			arch:           amd64,
			body:           "MOVQ CTX_STRINGS_BASE(R15), SI\nXORQ DI, DI\nTESTQ AX, AX\nCMOVQNE SI, DI\nMOVQ CX, (DI)\nDISPATCH_NEXT()",
			wantViolations: 1, wantStores: 1,
		},
		{
			name:           "a guard on only one path into a store",
			arch:           amd64,
			body:           "TESTQ AX, AX\nJZ join\n" + amd64Guard + "join:\nMOVQ CTX_STRINGS_BASE(R15), SI\nMOVQ CX, (SI)\nDISPATCH_NEXT()",
			wantViolations: 1, wantStores: 1,
		},
		{
			name:           "a guard after the store",
			arch:           amd64,
			body:           "MOVQ CTX_STRINGS_BASE(R15), SI\nMOVQ CX, (SI)\n" + amd64Guard + "DISPATCH_NEXT()",
			wantViolations: 1, wantStores: 1,
		},
		{
			name:           "a call between the guard and the store",
			arch:           amd64,
			body:           amd64Guard + "CALL ·somewhere(SB)\nMOVQ CTX_STRINGS_BASE(R15), SI\nMOVQ CX, (SI)\nDISPATCH_NEXT()",
			wantViolations: 1, wantStores: 1,
		},
		{
			name:           "an unguarded arm64 store pair",
			arch:           arm64,
			body:           "MOVD CTX_STRINGS_BASE(R19), R5\nADD R3, R5, R6\nSTP (R8, R9), (R6)\nDISPATCH_NEXT()",
			wantViolations: 1, wantStores: 1,
		},
		{
			name:           "a guarded arm64 store",
			arch:           arm64,
			body:           arm64Guard + "MOVD CTX_GENERALS_BASE(R19), R5\nADD R3, R5, R6\nMOVD R8, (R6)\nMOVD R9, 8(R6)\nDISPATCH_NEXT()",
			wantViolations: 0, wantStores: 2,
		},
		{
			name:           "an arm64 store through a register offset",
			arch:           arm64,
			body:           "MOVD CTX_SLICES_BYTE_BASE(R19), R5\nMOVD R8, (R5)(R3<<3)\nDISPATCH_NEXT()",
			wantViolations: 1, wantStores: 1,
		},
		{
			name:           "an arm64 load pair through a tainted base yields data",
			arch:           arm64,
			body:           "MOVD CTX_STRINGS_BASE(R19), R5\nLDP (R5), (R6, R7)\nMOVD R8, (R6)\nDISPATCH_NEXT()",
			wantViolations: 0, wantStores: 0,
		},
		{
			name:           "an arm64 guard after the store",
			arch:           arm64,
			body:           "MOVD CTX_STRINGS_BASE(R19), R5\nMOVD R8, (R5)\n" + arm64Guard + "DISPATCH_NEXT()",
			wantViolations: 1, wantStores: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			handlers, err := parseHandlers("TEXT ·fixture(SB), NOSPLIT|NOFRAME, $0\n" + tt.body + "\n")
			require.NoError(t, err)

			violations, stores, err := analyseHandler(tt.arch, RegisterBankBases, "fixture", handlers["fixture"])

			require.NoError(t, err)
			require.Len(t, violations, tt.wantViolations, "%v", violations)
			require.Equal(t, tt.wantStores, stores)
		})
	}
}
