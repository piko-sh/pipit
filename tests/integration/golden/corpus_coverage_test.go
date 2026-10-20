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
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

const manifestHeader = `# Snippets whose full disassembly is snapshotted under testdata/snippets.
# Curated by TestGoldenCorpusCoversEverySnippet: together these cover every opcode and sub-op the
# parity corpus emits. Re-record with PIPIT_GOLDEN_UPDATE=1; new snippets are appended
# only when they add coverage, so existing entries stay stable.
`

func manifestPath() string {
	return filepath.Join(testdataDir, "manifest.txt")
}

func TestGoldenCorpusCoversEverySnippet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping golden coverage in short mode")
	}

	corpus := loadCorpus(t)
	byName := make(map[string]*corpusCase, len(corpus))
	corpusKeys := make(map[string]struct{})
	for index := range corpus {
		byName[corpus[index].name] = &corpus[index]
		for key := range corpus[index].keys {
			corpusKeys[key] = struct{}{}
		}
	}

	manifest := readListFile(t, manifestPath())
	covered := make(map[string]struct{})
	for _, name := range manifest {
		entry, ok := byName[name]
		require.True(t, ok, "manifest names unknown snippet %s", name)
		for key := range entry.keys {
			covered[key] = struct{}{}
		}
	}

	missing := uncoveredKeys(corpusKeys, covered)
	if len(missing) == 0 {
		return
	}

	if !updateMode() {
		require.Failf(t, "golden manifest does not cover the corpus",
			"%d opcode(s) emitted by the corpus appear in no manifest snapshot: %v\n"+
				"run with %s=1 to extend the manifest", len(missing), missing, updateEnv)
		return
	}

	additions := greedyCover(corpus, covered)
	manifest = append(manifest, additions...)
	writeListFile(t, manifestPath(), manifestHeader, manifest)
	t.Logf("golden: manifest extended with %d snippet(s): %v", len(additions), additions)
}

func uncoveredKeys(want, have map[string]struct{}) []string {
	var missing []string
	for key := range want {
		if _, ok := have[key]; !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing
}

func greedyCover(corpus []corpusCase, covered map[string]struct{}) []string {
	var picked []string
	for {
		bestIndex := -1
		bestGain := 0
		for index := range corpus {
			gain := 0
			for key := range corpus[index].keys {
				if _, ok := covered[key]; !ok {
					gain++
				}
			}
			if gain > bestGain || (gain == bestGain && gain > 0 && corpus[index].name < corpus[bestIndex].name) {
				bestIndex = index
				bestGain = gain
			}
		}
		if bestIndex < 0 {
			return picked
		}
		for key := range corpus[bestIndex].keys {
			covered[key] = struct{}{}
		}
		picked = append(picked, corpus[bestIndex].name)
	}
}
