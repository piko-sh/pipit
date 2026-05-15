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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"piko.sh/asmgen"

	archamd64 "pipit.sh/pipit/internal/engine/asm/asmgen_arch_amd64"
	archarm64 "pipit.sh/pipit/internal/engine/asm/asmgen_arch_arm64"
)

func TestGenerateProducesNonEmptyOutput(t *testing.T) {
	architectures := []BytecodeArchitecturePort{
		archamd64.New(),
		archarm64.New(),
	}

	groups := FileGroups()
	writer := &memWriter{files: make(map[string][]byte)}

	err := asmgen.GenerateFiles(writer, architectures, groups, nil, nil)
	require.NoError(t, err, "generate error")

	require.NotEmpty(t, writer.files, "no files generated")

	for path, data := range writer.files {
		assert.NotEmpty(t, data, "empty file: %s", path)
	}
}

func TestEachFileGroupGeneratesForBothArchitectures(t *testing.T) {
	architectures := []BytecodeArchitecturePort{
		archamd64.New(),
		archarm64.New(),
	}

	groups := FileGroups()
	writer := &memWriter{files: make(map[string][]byte)}

	err := asmgen.GenerateFiles(writer, architectures, groups, nil, nil)
	require.NoError(t, err, "generate error")

	for _, group := range groups {
		for _, arch := range architectures {
			filename := group.BaseName + "_" + string(arch.Arch()) + ".s"
			found := false
			for path := range writer.files {
				if strings.HasSuffix(path, filename) {
					found = true
					break
				}
			}
			assert.True(t, found, "missing generated file: %s", filename)
		}
	}
}

func TestGeneratedFilesContainTextDirectives(t *testing.T) {
	architectures := []BytecodeArchitecturePort{
		archamd64.New(),
		archarm64.New(),
	}

	groups := FileGroups()
	writer := &memWriter{files: make(map[string][]byte)}

	err := asmgen.GenerateFiles(writer, architectures, groups, nil, nil)
	require.NoError(t, err, "generate error")

	registryDependentBaseNames := map[string]bool{
		"asm_vm_dispatch_direct_exits": true,
		"asm_vm_dispatch_pathb_shims":  true,
	}
	skipFile := func(path string) bool {
		for baseName := range registryDependentBaseNames {
			if strings.Contains(path, baseName) {
				return true
			}
		}
		return false
	}

	for path, data := range writer.files {
		content := string(data)
		assert.Contains(t, content, "//go:build",
			"file %s contains no build constraint", path)
		assert.Contains(t, content, "#include",
			"file %s contains no #include directives", path)
		if skipFile(path) {
			continue
		}
		assert.Contains(t, content, "TEXT",
			"file %s contains no TEXT directives", path)
	}
}

func TestGeneratedArithmeticFileContainsExpectedHandlers(t *testing.T) {
	architectures := []BytecodeArchitecturePort{archamd64.New()}
	groups := FileGroups()
	writer := &memWriter{files: make(map[string][]byte)}

	err := asmgen.GenerateFiles(writer, architectures, groups, nil, nil)
	require.NoError(t, err, "generate error")

	var arithContent string
	for path, data := range writer.files {
		if strings.Contains(path, "vm_dispatch_arith_amd64") {
			arithContent = string(data)
			break
		}
	}

	require.NotEmpty(t, arithContent, "no vm_dispatch_arith_amd64.s generated")

	expectedHandlers := []string{
		"handlerAddInt", "handlerSubInt", "handlerMulInt",
		"handlerDivInt", "handlerRemInt",
		"handlerBitAnd", "handlerBitOr", "handlerBitXor",
		"handlerBitAndNot",
		"handlerShiftLeft", "handlerShiftRight",
		"handlerAddFloat", "handlerSubFloat",
		"handlerMulFloat", "handlerDivFloat",
	}

	for _, handler := range expectedHandlers {
		assert.Contains(t, arithContent, handler,
			"missing handler in arith file: %s", handler)
	}
}

func TestGeneratedComparisonFileContainsExpectedHandlers(t *testing.T) {
	architectures := []BytecodeArchitecturePort{archamd64.New()}
	groups := FileGroups()
	writer := &memWriter{files: make(map[string][]byte)}

	err := asmgen.GenerateFiles(writer, architectures, groups, nil, nil)
	require.NoError(t, err, "generate error")

	var cmpContent string
	for path, data := range writer.files {
		if strings.Contains(path, "vm_dispatch_cmp_amd64") {
			cmpContent = string(data)
			break
		}
	}

	require.NotEmpty(t, cmpContent, "no vm_dispatch_cmp_amd64.s generated")

	expectedHandlers := []string{
		"handlerEqInt", "handlerNeInt", "handlerLtInt",
		"handlerLeInt", "handlerGtInt", "handlerGeInt",
		"handlerEqFloat", "handlerNeFloat",
		"handlerJump",
		"handlerJumpIfTrue", "handlerJumpIfFalse",
	}

	for _, handler := range expectedHandlers {
		assert.Contains(t, cmpContent, handler,
			"missing handler in cmp file: %s", handler)
	}
}

func TestGenerateOutputSample(t *testing.T) {

	architectures := []BytecodeArchitecturePort{archamd64.New()}
	groups := FileGroups()
	writer := &memWriter{files: make(map[string][]byte)}

	err := asmgen.GenerateFiles(writer, architectures, groups, nil, nil)
	require.NoError(t, err, "generate error")

	for path, data := range writer.files {
		if strings.Contains(path, "vm_dispatch_arith_amd64") {
			lines := strings.Split(string(data), "\n")
			limit := min(50, len(lines))
			t.Logf("=== %s (first %d lines) ===", path, limit)
			for i := range limit {
				t.Logf("%3d: %s", i+1, lines[i])
			}
			break
		}
	}

	arm64Archs := []BytecodeArchitecturePort{archarm64.New()}
	arm64Writer := &memWriter{files: make(map[string][]byte)}
	err = asmgen.GenerateFiles(arm64Writer, arm64Archs, groups, nil, nil)
	require.NoError(t, err, "arm64 generate error")

	for path, data := range arm64Writer.files {
		if strings.Contains(path, "vm_dispatch_arith_arm64") {
			lines := strings.Split(string(data), "\n")
			limit := min(50, len(lines))
			t.Logf("=== %s (first %d lines) ===", path, limit)
			for i := range limit {
				t.Logf("%3d: %s", i+1, lines[i])
			}
			break
		}
	}
}

func TestHeaderFilesGenerate(t *testing.T) {
	architectures := []BytecodeArchitecturePort{
		archamd64.New(),
		archarm64.New(),
	}

	headers := HeaderFiles(testOffsetsForHeaderFiles())
	writer := &memWriter{files: make(map[string][]byte)}

	err := asmgen.GenerateFiles(writer, architectures, nil, headers, nil)
	require.NoError(t, err, "generate error")

	expectedFiles := []string{"dispatch_offsets.h", "dispatch_amd64.h", "dispatch_arm64.h"}
	for _, name := range expectedFiles {
		found := false
		for path, data := range writer.files {
			if strings.HasSuffix(path, name) {
				found = true
				assert.NotEmpty(t, data, "header %s is empty", name)
				content := string(data)
				assert.Contains(t, content, "#define",
					"header %s contains no #define directives", name)
				t.Logf("%s: %d bytes", name, len(data))
				break
			}
		}
		assert.True(t, found, "missing header file: %s", name)
	}
}

type memWriter struct {
	files map[string][]byte
}

func (w *memWriter) WriteFile(path string, data []byte) error {
	w.files[path] = data
	return nil
}
