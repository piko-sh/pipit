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
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	hashesHeader = `# SHA-256 of the disassembly of every parity snippet, one per line.
# Re-record with PIPIT_GOLDEN_UPDATE=1. A snippet that fails to compile records
# "compile-error" so a change in compilability is visible too.
`
	maxPrintedAssemblyLines = 400
)

func hashesPath() string {
	return filepath.Join(testdataDir, "hashes.txt")
}

func TestGoldenHashes(t *testing.T) {
	corpus := loadCorpus(t)

	if updateMode() {
		lines := make([]string, 0, len(corpus))
		for index := range corpus {
			lines = append(lines, corpus[index].name+"\t"+corpus[index].hash())
		}
		writeListFile(t, hashesPath(), hashesHeader, lines)
		return
	}

	recorded := make(map[string]string)
	for _, line := range readListFile(t, hashesPath()) {
		name, hash, ok := strings.Cut(line, "\t")
		require.True(t, ok, "malformed hashes.txt line %q", line)
		recorded[name] = hash
	}
	require.NotEmpty(t, recorded, "hashes.txt is empty; run with %s=1", updateEnv)

	var changed, added []string
	for index := range corpus {
		entry := &corpus[index]
		want, ok := recorded[entry.name]
		delete(recorded, entry.name)
		if !ok {
			added = append(added, entry.name)
			continue
		}
		if want == entry.hash() {
			continue
		}
		changed = append(changed, entry.name)
		t.Logf("golden: %s changed (recorded %s, now %s)\n%s", entry.name, want, entry.hash(), truncatedListing(entry))
	}

	removed := slices.Sorted(maps.Keys(setOf(recorded)))
	if len(changed)+len(added)+len(removed) == 0 {
		return
	}
	t.Errorf("golden: %d snippet(s) changed, %d added, %d removed\nchanged: %s\nadded: %s\nremoved: %s\n"+
		"run with %s=1 after reviewing the listings above",
		len(changed), len(added), len(removed),
		strings.Join(changed, ", "), strings.Join(added, ", "), strings.Join(removed, ", "), updateEnv)
}

func truncatedListing(entry *corpusCase) string {
	if entry.err != nil {
		return "compile error: " + entry.err.Error()
	}
	lines := strings.Split(entry.assembly, "\n")
	if len(lines) <= maxPrintedAssemblyLines {
		return entry.assembly
	}
	return strings.Join(lines[:maxPrintedAssemblyLines], "\n") +
		fmt.Sprintf("\n... (%d more lines)", len(lines)-maxPrintedAssemblyLines)
}

func setOf(m map[string]string) map[string]struct{} {
	set := make(map[string]struct{}, len(m))
	for key := range m {
		set[key] = struct{}{}
	}
	return set
}
