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
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"piko.sh/asmgen"

	archamd64 "pipit.sh/pipit/internal/engine/asm/asmgen_arch_amd64"
	archarm64 "pipit.sh/pipit/internal/engine/asm/asmgen_arch_arm64"
)

func TestFileGroupsReturnsAllExpectedGroups(t *testing.T) {
	groups := FileGroups()

	require.Len(t, groups, 26, "expected 26 file groups")

	expectedBaseNames := []string{
		"asm_vm_dispatch_arith",
		"asm_vm_dispatch_cmp",
		"asm_vm_dispatch_string",
		"asm_vm_dispatch_super",
		"asm_vm_dispatch_init",
		"asm_vm_dispatch_struct_field",
		"asm_vm_dispatch_upvalue",
		"asm_vm_dispatch_general",
		"asm_vm_dispatch_inline",
		"asm_vm_dispatch_direct_exits",
		"asm_vm_dispatch_tier1_slice_typed",
		"asm_vm_dispatch_tier1_super_range_check",
		"asm_vm_dispatch_tier1_complex",
		"asm_vm_dispatch_tier1_math",
		"asm_vm_dispatch_tier1_strconv",
		"asm_vm_dispatch_tier1_runtime",
		"asm_vm_dispatch_tier1_move",
		"asm_vm_dispatch_tier1_struct_field_incdec",
		"asm_vm_dispatch_tier1_unary",
		"asm_vm_dispatch_tier1_conversion",
		"asm_vm_dispatch_tier1_math_unary",
		"asm_vm_dispatch_tier1_string",
		"asm_vm_dispatch_tier2_inplace",
		"asm_vm_dispatch_pathb_shims",
		"asm_vm_dispatch_flat_install",
		"asm_vm_dispatch_truncate",
	}

	for i, group := range groups {
		assert.Equal(t, expectedBaseNames[i], group.BaseName, "group %d base name mismatch", i)
	}
}

func TestAllHandlersHaveRequiredFields(t *testing.T) {
	groups := FileGroups()

	for _, group := range groups {
		for i, handler := range group.Handlers {
			t.Run(group.BaseName+"/"+handler.Name, func(t *testing.T) {
				assert.NotEmpty(t, handler.Name, "handler %d in %s has empty Name", i, group.BaseName)

				hasComment := handler.Comment != "" || handler.CommentFunction != nil
				assert.True(t, hasComment, "handler %q in %s has neither Comment nor CommentFunction", handler.Name, group.BaseName)

				assert.NotEmpty(t, handler.FrameSize, "handler %q in %s has empty FrameSize", handler.Name, group.BaseName)
				assert.NotEmpty(t, handler.Flags, "handler %q in %s has empty Flags", handler.Name, group.BaseName)
				assert.NotNil(t, handler.Emit, "handler %q in %s has nil Emit", handler.Name, group.BaseName)
			})
		}
	}
}

func TestHandlerCountPerGroup(t *testing.T) {
	groups := FileGroups()

	expectedCounts := map[string]int{
		"asm_vm_dispatch_arith":                     37,
		"asm_vm_dispatch_cmp":                       21,
		"asm_vm_dispatch_string":                    6,
		"asm_vm_dispatch_super":                     6,
		"asm_vm_dispatch_init":                      10,
		"asm_vm_dispatch_struct_field":              10,
		"asm_vm_dispatch_upvalue":                   1,
		"asm_vm_dispatch_general":                   11,
		"asm_vm_dispatch_inline":                    9,
		"asm_vm_dispatch_direct_exits":              0,
		"asm_vm_dispatch_tier1_slice_typed":         37,
		"asm_vm_dispatch_tier1_super_range_check":   14,
		"asm_vm_dispatch_tier1_complex":             4,
		"asm_vm_dispatch_tier1_math":                5,
		"asm_vm_dispatch_tier1_strconv":             3,
		"asm_vm_dispatch_tier1_runtime":             10,
		"asm_vm_dispatch_tier1_move":                5,
		"asm_vm_dispatch_tier1_struct_field_incdec": 4,
		"asm_vm_dispatch_tier2_inplace":             4,
		"asm_vm_dispatch_tier1_unary":               3,
		"asm_vm_dispatch_tier1_conversion":          4,
		"asm_vm_dispatch_tier1_math_unary":          6,
		"asm_vm_dispatch_tier1_string":              1,
		"asm_vm_dispatch_pathb_shims":               0,
		"asm_vm_dispatch_flat_install":              1,
		"asm_vm_dispatch_truncate":                  1,
	}

	for _, group := range groups {
		expected, exists := expectedCounts[group.BaseName]
		require.True(t, exists, "unexpected group %s", group.BaseName)
		assert.Equal(t, expected, len(group.Handlers), "handler count mismatch for %s", group.BaseName)
	}
}

func TestArchitectureRestrictedHandlers(t *testing.T) {
	groups := FileGroups()

	handlerMap := make(map[string]asmgen.HandlerDefinition[BytecodeArchitecturePort])
	for _, group := range groups {
		for _, handler := range group.Handlers {
			handlerMap[handler.Name] = handler
		}
	}

	t.Run("initJumpTableSSE41 is amd64-only", func(t *testing.T) {
		handler, exists := handlerMap["initJumpTableSSE41"]
		require.True(t, exists, "initJumpTableSSE41 not found")
		require.Len(t, handler.Architectures, 1)
		assert.Equal(t, asmgen.ArchitectureAMD64, handler.Architectures[0])
	})

}

func TestInlineGoCallRealHandlersHaveNoLocalPointers(t *testing.T) {
	architectures := []BytecodeArchitecturePort{
		archamd64.New(),
		archarm64.New(),
	}
	groups := FileGroups()
	writer := &memWriter{files: make(map[string][]byte)}
	err := asmgen.GenerateFiles(writer, architectures, groups, nil, nil)
	require.NoError(t, err)

	affectedFilenames := map[string]bool{
		"asm_vm_dispatch_tier1_runtime_amd64.s": true,
		"asm_vm_dispatch_tier1_runtime_arm64.s": true,
		"asm_vm_dispatch_tier1_math_amd64.s":    true,
		"asm_vm_dispatch_tier1_math_arm64.s":    true,
		"asm_vm_dispatch_tier1_strconv_amd64.s": true,
		"asm_vm_dispatch_tier1_strconv_arm64.s": true,
	}

	realTextPattern := regexp.MustCompile(`(?m)^TEXT ·(handler\w+)\(SB\)`)

	for path, data := range writer.files {
		basename := path[strings.LastIndex(path, "/")+1:]
		if !affectedFilenames[basename] {
			continue
		}

		content := string(data)
		realHandlerMatches := realTextPattern.FindAllStringSubmatch(content, -1)
		require.NotEmpty(t, realHandlerMatches,
			"%s should contain at least one handler", path)

		for _, match := range realHandlerMatches {
			handlerName := match[1]

			handlerStart := strings.Index(content, match[0])
			require.NotEqual(t, -1, handlerStart, "handler %s start not found", handlerName)

			searchFrom := handlerStart + len(match[0])
			nextText := strings.Index(content[searchFrom:], "\nTEXT ")
			var body string
			if nextText == -1 {
				body = content[searchFrom:]
			} else {
				body = content[searchFrom : searchFrom+nextText]
			}

			if !strings.Contains(body, "CALL") && !strings.Contains(body, " BL ") && !strings.Contains(body, "\tBL\t") {
				continue
			}

			assert.Contains(t, body, "NO_LOCAL_POINTERS",
				"%s: handler %s lacks NO_LOCAL_POINTERS directive - "+
					"runtime will panic 'missing stackmap' on GC scan",
				path, handlerName)
		}
	}
}

func TestInlineGoCallRealHandlersHaveNoSpilledPointers(t *testing.T) {
	architectures := []BytecodeArchitecturePort{
		archamd64.New(),
		archarm64.New(),
	}
	groups := FileGroups()
	writer := &memWriter{files: make(map[string][]byte)}
	err := asmgen.GenerateFiles(writer, architectures, groups, nil, nil)
	require.NoError(t, err)

	affectedFilenames := map[string]bool{
		"asm_vm_dispatch_tier1_runtime_amd64.s": true,
		"asm_vm_dispatch_tier1_runtime_arm64.s": true,
		"asm_vm_dispatch_tier1_math_amd64.s":    true,
		"asm_vm_dispatch_tier1_math_arm64.s":    true,
		"asm_vm_dispatch_tier1_strconv_amd64.s": true,
		"asm_vm_dispatch_tier1_strconv_arm64.s": true,
	}

	realTextPattern := regexp.MustCompile(`(?m)^TEXT ·(handler\w+)\(SB\)`)

	forbiddenAmd64 := regexp.MustCompile(`MOVQ\s+R15,\s+(\d+)\(SP\)`)
	forbiddenArm64 := regexp.MustCompile(`MOVD\s+R19,\s+(\d+)\(RSP\)`)

	for path, data := range writer.files {
		basename := path[strings.LastIndex(path, "/")+1:]
		if !affectedFilenames[basename] {
			continue
		}

		isArm64 := strings.HasSuffix(basename, "_arm64.s")
		pattern := forbiddenAmd64
		register := "R15"
		stackPointer := "SP"
		allowedOffset := "0"
		if isArm64 {
			pattern = forbiddenArm64
			register = "R19"
			stackPointer = "RSP"
			allowedOffset = "8"
		}

		content := string(data)
		realHandlerMatches := realTextPattern.FindAllStringSubmatch(content, -1)
		require.NotEmpty(t, realHandlerMatches, "%s should contain handlers", path)

		for _, match := range realHandlerMatches {
			handlerName := match[1]
			handlerStart := strings.Index(content, match[0])
			searchFrom := handlerStart + len(match[0])
			nextText := strings.Index(content[searchFrom:], "\nTEXT ")
			var body string
			if nextText == -1 {
				body = content[searchFrom:]
			} else {
				body = content[searchFrom : searchFrom+nextText]
			}

			for _, spillMatch := range pattern.FindAllStringSubmatch(body, -1) {
				offset := spillMatch[1]
				if offset == allowedOffset {

					continue
				}
				assert.Failf(t, "handler spill detected",
					"%s: handler %s spills %s to %s+%s - re-introduces "+
						"the pointer-in-locals problem the no-spill restructure "+
						"eliminated. Recover ctx from the abi0 return slot instead.",
					path, handlerName, register, stackPointer, offset)
			}
		}
	}
}

func TestAffectedFilesIncludeFuncdataHeader(t *testing.T) {
	architectures := []BytecodeArchitecturePort{
		archamd64.New(),
		archarm64.New(),
	}
	groups := FileGroups()
	writer := &memWriter{files: make(map[string][]byte)}
	err := asmgen.GenerateFiles(writer, architectures, groups, nil, nil)
	require.NoError(t, err)

	affectedFilenames := map[string]bool{
		"asm_vm_dispatch_tier1_runtime_amd64.s": true,
		"asm_vm_dispatch_tier1_runtime_arm64.s": true,
		"asm_vm_dispatch_tier1_math_amd64.s":    true,
		"asm_vm_dispatch_tier1_math_arm64.s":    true,
		"asm_vm_dispatch_tier1_strconv_amd64.s": true,
		"asm_vm_dispatch_tier1_strconv_arm64.s": true,
	}

	for path, data := range writer.files {
		basename := path[strings.LastIndex(path, "/")+1:]
		if !affectedFilenames[basename] {
			continue
		}
		assert.Contains(t, string(data), `#include "funcdata.h"`,
			"%s must include funcdata.h for NO_LOCAL_POINTERS macro", path)
	}
}

func TestHeaderFilesReturnsThreeHeaders(t *testing.T) {
	headers := HeaderFiles(testOffsetsForHeaderFiles())

	require.Len(t, headers, 3, "expected 3 header files")

	expectedNames := []string{
		"asm_dispatch_offsets.h",
		"asm_dispatch_amd64.h",
		"asm_dispatch_arm64.h",
	}

	for i, header := range headers {
		assert.Equal(t, expectedNames[i], header.Name, "header %d name mismatch", i)
	}
}

func TestHandlersThatAddToProgramCounterEmitBackEdgePoll(t *testing.T) {
	architectures := []BytecodeArchitecturePort{
		archamd64.New(),
		archarm64.New(),
	}
	groups := FileGroups()
	writer := &memWriter{files: make(map[string][]byte)}
	err := asmgen.GenerateFiles(writer, architectures, groups, nil, nil)
	require.NoError(t, err)

	textPattern := regexp.MustCompile(`(?m)^TEXT ·(handler\w+)\(SB\)`)
	pcRelativeAddPatterns := map[string]*regexp.Regexp{
		"_amd64.s": regexp.MustCompile(`(?m)^\s*ADDQ\s+[A-Z]{2}, R14\s*$`),
		"_arm64.s": regexp.MustCompile(`(?m)^\s*ADD\s+R\d+, R20, R20\s*$`),
	}

	checked := 0
	for path, data := range writer.files {
		var addPattern *regexp.Regexp
		for suffix, pattern := range pcRelativeAddPatterns {
			if strings.HasSuffix(path, suffix) {
				addPattern = pattern
			}
		}
		if addPattern == nil {
			continue
		}
		content := string(data)
		matches := textPattern.FindAllStringSubmatchIndex(content, -1)
		for i, match := range matches {
			bodyEnd := len(content)
			if i+1 < len(matches) {
				bodyEnd = matches[i+1][0]
			}
			body := content[match[1]:bodyEnd]
			if !addPattern.MatchString(body) {
				continue
			}
			checked++
			assert.Contains(t, body, "CTX_POLL_BUDGET",
				"%s: handler %s adds a register to the program counter without polling the back-edge budget",
				path, content[match[2]:match[3]])
		}
	}
	require.Positive(t, checked, "no handler with a register-relative program counter update was found")
}
