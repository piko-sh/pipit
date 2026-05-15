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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"piko.sh/asmgen"
	"pipit.sh/pipit/internal/engine/asm"
	archamd64 "pipit.sh/pipit/internal/engine/asm/asmgen_arch_amd64"
)

func TestProvidedOffsetsMatchLiveStructs(t *testing.T) {
	t.Parallel()

	headers := asm.HeaderFiles(
		ProvideDispatchContextOffsets(),
		ProvideCallFrameOffsets(),
		ProvideASMCallInfoOffsets(),
		ProvideVarLocationOffsets(),
	)

	var generated string
	for _, header := range headers {
		if header.Name != "asm_dispatch_offsets.h" {
			continue
		}
		generated = header.Emit([]asmgen.ArchitecturePort{archamd64.New()})
		break
	}
	require.NotEmpty(t, generated, "HeaderFiles did not return asm_dispatch_offsets.h")

	livePath := filepath.Join("asm_dispatch_offsets.h")
	liveBytes, err := os.ReadFile(livePath)
	require.NoError(t, err, "read live header")
	assert.Equalf(t, string(liveBytes), generated,
		"asmgen-generated header differs from live %s.\n"+
			"Live and asmgen-derived offsets are out of sync. Either:\n"+
			"  1. A runtime struct grew but ProvideDispatchContextOffsets / "+
			"ProvideCallFrameOffsets / ProvideASMCallInfoOffsets / "+
			"ProvideVarLocationOffsets was not updated to expose the new field, OR\n"+
			"  2. asmgen has not been re-run since the struct change "+
			"(run hack/generate/asmgen.sh to refresh the live header).\n"+
			"\nFirst diverging line:\n%s", livePath, firstDiff(string(liveBytes), generated))
}

func firstDiff(expected, actual string) string {
	expectedLines := strings.Split(expected, "\n")
	actualLines := strings.Split(actual, "\n")
	limit := min(len(actualLines), len(expectedLines))
	for i := range limit {
		if expectedLines[i] != actualLines[i] {
			return formatLineDiff(i, expectedLines[i], actualLines[i])
		}
	}
	if len(expectedLines) != len(actualLines) {
		return "  expected " + strconv.Itoa(len(expectedLines)) + " lines, got " + strconv.Itoa(len(actualLines))
	}
	return ""
}

func formatLineDiff(index int, expectedLine, actualLine string) string {
	return "  line " + strconv.Itoa(index+1) + ":\n" +
		"    expected: " + expectedLine + "\n" +
		"    actual:   " + actualLine
}

func TestProvidedOffsetsAreSelfConsistent(t *testing.T) {
	t.Parallel()

	contextOffsets := ProvideDispatchContextOffsets()
	var ctx dispatchContext
	assert.Equalf(t, unsafe.Offsetof(ctx.slicesIntBase), contextOffsets.SlicesIntBase,
		"SlicesIntBase offset mismatch: provider=%d struct=%d", contextOffsets.SlicesIntBase, unsafe.Offsetof(ctx.slicesIntBase))
	assert.Equalf(t, unsafe.Offsetof(ctx.asmCallInfoBase), contextOffsets.AsmCallInfoBase,
		"AsmCallInfoBase offset mismatch: provider=%d struct=%d", contextOffsets.AsmCallInfoBase, unsafe.Offsetof(ctx.asmCallInfoBase))

	frameOffsets := ProvideCallFrameOffsets()
	var frame CallFrame
	assert.Equalf(t, unsafe.Sizeof(frame), frameOffsets.Size,
		"CallFrame Size mismatch: provider=%d struct=%d", frameOffsets.Size, unsafe.Sizeof(frame))

	assert.Equalf(t,
		unsafe.Offsetof(frame.Registers)+unsafe.Offsetof(frame.Registers.lastAllocMask),
		frameOffsets.RegsLastAllocMask,
		"RegsLastAllocMask offset mismatch (a mismatch collides the diff-clear stamp into a register-bank pointer)")
	assert.NotEqualf(t, frameOffsets.RegsLastAllocMask, frameOffsets.RegsSliceBytePtr,
		"RegsLastAllocMask (%d) collides with RegsSliceBytePtr (%d): the 0xFFFF stamp would "+
			"corrupt the slicesByte bank header", frameOffsets.RegsLastAllocMask, frameOffsets.RegsSliceBytePtr)
}
