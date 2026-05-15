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
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"piko.sh/asmgen"

	archamd64 "pipit.sh/pipit/internal/engine/asm/asmgen_arch_amd64"
	archarm64 "pipit.sh/pipit/internal/engine/asm/asmgen_arch_arm64"
)

func TestInstructionParityWithOriginals(t *testing.T) {
	architectures := []BytecodeArchitecturePort{
		archamd64.New(),
		archarm64.New(),
	}

	groups := FileGroups()
	writer := &memWriter{files: make(map[string][]byte)}

	err := asmgen.GenerateFiles(writer, architectures, groups, nil, nil)
	require.NoError(t, err, "generate error")

	originalDir := ".."

	skipRegistryBaseNames := []string{
		"asm_vm_dispatch_direct_exits_",
		"asm_vm_dispatch_pathb_shims_",
	}

	for generatedPath, generatedData := range writer.files {
		filename := filepath.Base(generatedPath)

		if strings.Contains(filename, "asm_vm_dispatch_init_") {

			continue
		}
		skipFile := false
		for _, prefix := range skipRegistryBaseNames {
			if strings.Contains(filename, prefix) {
				skipFile = true
				break
			}
		}
		if skipFile {
			continue
		}

		originalPath := filepath.Join(originalDir, filename)
		originalData, err := os.ReadFile(originalPath)
		if !assert.NoError(t, err, "original file not found for generated file %s", filename) {
			continue
		}

		generatedInstructions := extractInstructions(string(generatedData))
		originalInstructions := extractInstructions(string(originalData))

		generatedTexts := extractTextNames(generatedInstructions)
		originalTexts := extractTextNames(originalInstructions)

		for _, name := range originalTexts {
			assert.True(t, slices.Contains(generatedTexts, name),
				"%s: original TEXT block %q missing from generated output", filename, name)
		}

		for _, name := range generatedTexts {
			assert.True(t, slices.Contains(originalTexts, name),
				"%s: generated output has extra TEXT block %q not present in original", filename, name)
		}

		t.Logf("%s: original=%d instructions, generated=%d instructions, original TEXT blocks=%d, generated TEXT blocks=%d",
			filename, len(originalInstructions), len(generatedInstructions), len(originalTexts), len(generatedTexts))

		if len(originalInstructions) > 0 {
			difference := math.Abs(float64(len(generatedInstructions)-len(originalInstructions))) / float64(len(originalInstructions))
			assert.LessOrEqual(t, difference, 0.20,
				"%s: instruction count differs by %.0f%% (original=%d, generated=%d), exceeds 20%% tolerance",
				filename, difference*100, len(originalInstructions), len(generatedInstructions))
		}
	}
}

func extractInstructions(content string) []string {
	var result []string
	for line := range strings.SplitSeq(content, "\n") {
		stripped := stripInlineComment(line)
		trimmed := strings.TrimSpace(stripped)

		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "//") {
			continue
		}

		result = append(result, stripped)
	}
	return result
}

var (
	inlineCommentPattern = regexp.MustCompile(`\s+//.*$`)
)

func stripInlineComment(line string) string {

	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") {
		return line
	}
	return inlineCommentPattern.ReplaceAllString(line, "")
}

func extractTextNames(lines []string) []string {
	var names []string
	for _, line := range lines {
		if strings.Contains(line, "TEXT") && strings.Contains(line, "(SB)") {

			start := strings.Index(line, "\xc2\xb7")
			if start < 0 {
				continue
			}
			start += 2
			end := strings.Index(line[start:], "(SB)")
			if end < 0 {
				continue
			}
			names = append(names, line[start:start+end])
		}
	}
	return names
}
