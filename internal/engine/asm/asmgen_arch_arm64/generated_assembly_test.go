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

package asmgen_arch_arm64_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"piko.sh/asmgen"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/asm"
	archarm64 "pipit.sh/pipit/internal/engine/asm/asmgen_arch_arm64"
)

const repositoryRoot = "../../../.."

type generatedFiles struct {
	files map[string][]byte
}

func (g *generatedFiles) WriteFile(path string, data []byte) error {
	g.files[path] = data
	return nil
}

func withoutFileHeader(text string) string {
	lines := strings.Split(text, "\n")
	start := 0
	for start < len(lines) && (strings.HasPrefix(lines[start], "//") || strings.TrimSpace(lines[start]) == "") {
		start++
	}
	return strings.Join(lines[start:], "\n")
}

func TestTheEmittersProduceTheCommittedAssembly(t *testing.T) {
	provided := engine.ProvideAsmHandlerJumpTableEntries()
	entries := make([]archarm64.JumpTableEntry, len(provided))
	for index, entry := range provided {
		entries[index] = archarm64.JumpTableEntry(entry)
	}
	generated := &generatedFiles{files: map[string][]byte{}}
	err := asmgen.GenerateFiles(generated, []asm.BytecodeArchitecturePort{archarm64.New(entries...)}, asm.FileGroups(), nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, generated.files)

	for path, data := range generated.files {
		require.True(t, strings.HasSuffix(path, "_arm64.s"), "only arm64 assembly is generated: %s", path)
		committed, err := os.ReadFile(filepath.Join(repositoryRoot, path))
		require.NoError(t, err, "every generated file is committed")
		require.Equal(t, withoutFileHeader(string(committed)), withoutFileHeader(string(data)),
			"%s: the arm64 emitters produce the committed dispatch assembly", path)
	}
}
