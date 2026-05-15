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

var textSymbolPattern = regexp.MustCompile(`(?m)^TEXT ·(\w+)\(SB\)`)

func emittedTextSymbols(t *testing.T) map[asmgen.Architecture]map[string]bool {
	t.Helper()

	architectures := []BytecodeArchitecturePort{archamd64.New(), archarm64.New()}
	writer := &memWriter{files: make(map[string][]byte)}
	require.NoError(t, asmgen.GenerateFiles(writer, architectures, FileGroups(), nil, nil))

	symbols := map[asmgen.Architecture]map[string]bool{
		asmgen.ArchitectureAMD64: {},
		asmgen.ArchitectureARM64: {},
	}
	for path, data := range writer.files {
		var architecture asmgen.Architecture
		switch {
		case strings.HasSuffix(path, "_amd64.s"):
			architecture = asmgen.ArchitectureAMD64
		case strings.HasSuffix(path, "_arm64.s"):
			architecture = asmgen.ArchitectureARM64
		default:
			continue
		}
		for _, match := range textSymbolPattern.FindAllStringSubmatch(string(data), -1) {
			symbols[architecture][match[1]] = true
		}
	}
	return symbols
}

func TestGoDeclarationsHaveTextSymbols(t *testing.T) {
	symbols := emittedTextSymbols(t)

	for _, declaration := range goDeclarations() {
		for architecture, defined := range symbols {
			if !declarationTargets(declaration.Architectures, architecture) {
				continue
			}
			assert.True(t, defined[declaration.Name],
				"goDeclarations names %q, which no TEXT symbol defines on %s",
				declaration.Name, architecture)
		}
	}
}

func TestGoDeclarationsNameKnownHandlers(t *testing.T) {
	synopses := handlerSynopsisIndex()

	for _, declaration := range goDeclarations() {
		synopsis, ok := synopses[declaration.Name]
		assert.True(t, ok, "goDeclarations names %q, which no handler definition defines", declaration.Name)
		assert.NotEmpty(t, synopsis, "handler %q has no Comment to document its prototype with", declaration.Name)
	}
}

func TestInitSequenceOrdersSubOpTablesAfterTier1(t *testing.T) {
	tier1Index, subOpIndex := -1, -1
	for i, step := range initSequence() {
		switch step.Call {
		case "installTier1Dispatcher()":
			tier1Index = i
		case "initSubOpJumpTables()":
			subOpIndex = i
		default:
		}
	}

	require.NotEqual(t, -1, tier1Index, "initSequence must install the tier-1 dispatcher")
	require.NotEqual(t, -1, subOpIndex, "initSequence must initialise the sub-op jump tables")
	assert.Greater(t, subOpIndex, tier1Index,
		"initSubOpJumpTables must run after installTier1Dispatcher")
}
