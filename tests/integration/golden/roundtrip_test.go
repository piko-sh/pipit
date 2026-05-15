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

//go:build integration

package golden_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/adapters"
	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/rootfs"
)

const instructionLinePrefixLen = 4

func stripInstructionComments(listing string) string {
	lines := strings.Split(listing, "\n")
	for index, line := range lines {
		if !isInstructionLine(line) {
			continue
		}
		if instruction, _, found := strings.Cut(line, "    ; "); found {
			lines[index] = strings.TrimRight(instruction, " ")
		}
	}
	return strings.Join(lines, "\n")
}

func isInstructionLine(line string) bool {
	if len(line) < instructionLinePrefixLen {
		return false
	}
	for _, r := range line[:instructionLinePrefixLen] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func TestGoldenRoundTrip(t *testing.T) {
	corpus := loadCorpus(t)
	byName := make(map[string]*corpusCase, len(corpus))
	for index := range corpus {
		byName[corpus[index].name] = &corpus[index]
	}

	programs := make(map[string]string)
	for _, name := range readListFile(t, manifestPath()) {
		entry, ok := byName[name]
		require.True(t, ok, "manifest names unknown snippet %s", name)
		programs["snippet/"+name] = entry.source
	}
	for name, source := range shapeCases(t) {
		programs["shape/"+name] = source
	}
	require.NotEmpty(t, programs, "nothing to round-trip; record the manifest first")

	service := newGoldenService(app.WithBytecodeStore(adapters.NewBytecodeStore(rootfs.NewMemoryStore())))
	ctx := context.Background()
	for _, name := range sortedNames(programs) {
		cfs, err := compileGolden(ctx, service, programs[name])
		require.NoError(t, err, "compiling %s", name)
		before := stripInstructionComments(debug.DisassembleAssembly(cfs))

		require.NoError(t, service.SaveCompiled(ctx, name, cfs), "saving %s", name)
		loaded, loadErr := service.LoadCompiled(ctx, name)
		require.NoError(t, loadErr, "loading %s", name)

		after := stripInstructionComments(debug.DisassembleAssembly(loaded))
		require.Equal(t, before, after, "round-trip changed %s", name)
	}
}
