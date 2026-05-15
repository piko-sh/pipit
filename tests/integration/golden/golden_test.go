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
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/debug"
)

const pkasmExtension = ".pkasm"

func snapshotPath(dir, name string) string {
	return filepath.Join(testdataDir, dir, name+pkasmExtension)
}

func checkSnapshot(t *testing.T, path, assembly string) bool {
	t.Helper()
	if updateMode() {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), dirPerm))
		require.NoError(t, os.WriteFile(path, []byte(assembly), filePerm))
		return true
	}
	expected, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		assert.Failf(t, "missing golden snapshot", "%s does not exist; run with %s=1", path, updateEnv)
		return false
	}
	require.NoError(t, err, "reading %s", path)
	return assert.Equal(t, string(expected), assembly, "golden snapshot %s differs", path)
}

func reportDiffering(t *testing.T, kind string, differing []string) {
	t.Helper()
	if len(differing) == 0 {
		return
	}
	t.Errorf("golden: %d %s case(s) differ: %s", len(differing), kind, strings.Join(differing, ", "))
}

func TestGoldenSnippets(t *testing.T) {
	corpus := loadCorpus(t)
	byName := make(map[string]*corpusCase, len(corpus))
	for index := range corpus {
		byName[corpus[index].name] = &corpus[index]
	}

	manifest := readListFile(t, manifestPath())
	require.NotEmpty(t, manifest, "manifest is empty; run TestGoldenCorpusCoversEverySnippet with %s=1", updateEnv)

	var differing []string
	wanted := make(map[string]struct{}, len(manifest))
	for _, name := range manifest {
		entry, ok := byName[name]
		require.True(t, ok, "manifest names unknown snippet %s", name)
		require.NoError(t, entry.err, "manifest snippet %s does not compile", name)
		wanted[name+pkasmExtension] = struct{}{}
		if !checkSnapshot(t, snapshotPath("snippets", name), entry.assembly) {
			differing = append(differing, name)
		}
	}
	reportDiffering(t, "snippet", differing)

	if updateMode() {
		removeStaleSnapshots(t, filepath.Join(testdataDir, "snippets"), wanted)
	}
}

func removeStaleSnapshots(t *testing.T, dir string, wanted map[string]struct{}) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return
	}
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), pkasmExtension) {
			continue
		}
		if _, ok := wanted[entry.Name()]; ok {
			continue
		}
		require.NoError(t, os.Remove(filepath.Join(dir, entry.Name())))
		t.Logf("golden: removed stale snapshot %s", entry.Name())
	}
}

func shapeCases(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join(testdataDir, "shapes")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)

	cases := make(map[string]string)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, entry.Name(), goldenSourceName))
		require.NoError(t, readErr, "reading shape %s", entry.Name())
		cases[entry.Name()] = string(data)
	}
	return cases
}

func TestGoldenShapes(t *testing.T) {
	cases := shapeCases(t)
	if len(cases) == 0 {
		t.Skip("no shape programs under testdata/shapes")
	}

	service := newGoldenService()
	var differing []string
	for _, name := range sortedNames(cases) {
		cfs, err := compileGolden(context.Background(), service, cases[name])
		require.NoError(t, err, "compiling shape %s", name)
		assembly := debug.DisassembleAssembly(cfs)
		if !checkSnapshot(t, snapshotPath("shapes", name), assembly) {
			differing = append(differing, name)
		}
	}
	reportDiffering(t, "shape", differing)
}

func sortedNames(cases map[string]string) []string {
	set := make(map[string]struct{}, len(cases))
	for name := range cases {
		set[name] = struct{}{}
	}
	return slices.Sorted(maps.Keys(set))
}
