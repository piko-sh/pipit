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

package modloader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceSnapshotFreezesApprovedInputs(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	target := filepath.Join(directory, "main.go")
	original := "package main; func main() {}"
	metadata := "module example.test/main\nrequire example.test/helper v1.2.3\n"
	for name, data := range map[string]string{"main.go": original, "go.mod": metadata} {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(data), 0600))
	}
	axes := []string{"filesystem.read"}
	path, identity, err := InvocationIdentity(target, axes, "main", nil)
	require.NoError(t, err)
	snapshot, err := CaptureInvocation(target, axes, "main", nil)
	require.NoError(t, err)
	require.Equal(t, path, snapshot.Path(), "the captured source matches the approval identity")
	require.Equal(t, identity, snapshot.Identity())
	require.False(t, snapshot.IsDirectory())
	require.NoError(t, os.Remove(target))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "go.mod"), []byte("changed"), 0600))
	captured, err := snapshot.ReadFile(target)
	require.NoError(t, err, "the source is opened once: %q", captured)
	require.Equal(t, original, string(captured))
	captured[0] = 'X'
	again, err := snapshot.ReadFile(target)
	require.NoError(t, err, "the returned byte slice is a copy")
	require.Equal(t, original, string(again))
	module, err := snapshot.ReadFile(filepath.Join(directory, "go.mod"))
	require.NoError(t, err, "module metadata is opened once")
	require.Equal(t, metadata, string(module))
	versions := invocationVersions(directory, snapshot)
	require.Equal(t, "v1.2.3", versions["example.test/helper"], "module resolution uses the approved version metadata")
	resolution, err := ResolveScript(t.Context(), target, ScriptResolverOptions{Snapshot: snapshot})
	require.NoError(t, err, "the resolver uses the captured source: %+v", resolution)
	require.Equal(t, original, resolution.MainSources[target])
	_, err = snapshot.ReadFile(filepath.Join(directory, "..", "outside.go"))
	require.Error(t, err, "a snapshot refuses source outside its captured root")
}

func TestSourceSnapshotDirectoryReturnsIndependentMap(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	for _, name := range []string{"main.go", "helper.go", "helper_test.go", "child/nested.go"} {
		path := filepath.Join(directory, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
		require.NoError(t, os.WriteFile(path, []byte("package main"), 0600))
	}
	snapshot, err := CaptureInvocation(directory, nil, "main", nil)
	require.NoError(t, err)
	require.True(t, snapshot.IsDirectory(), "the directory kind is kept")
	sources := snapshot.DirectorySources()
	require.Len(t, sources, 2, "the selected package")
	require.Equal(t, "package main", sources["helper.go"])
	sources["main.go"] = "changed"
	delete(sources, "helper.go")
	again := snapshot.DirectorySources()
	require.Len(t, again, 2, "the returned map is a copy")
	require.Equal(t, "package main", again["main.go"])
}
