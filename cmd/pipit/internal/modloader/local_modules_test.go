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
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeLocalModuleFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.27\n\nreplace example.com/other => ./other\n",
		"main.go": `package main

import (
	"fmt"

	"example.com/app/internal/util"
	"example.com/other/greet"
)

func main() { fmt.Println(util.Shout("pipit"), greet.Hello("again")) }
`,
		"internal/util/util.go": `package util

import (
	"strings"

	"example.com/app/lib"
	"example.com/other/greet"
)

func Shout(s string) string { return strings.ToUpper(greet.Hello(s)) + lib.Suffix }
`,
		"lib/lib.go":            "package lib\n\nconst Suffix = \"!\"\n",
		"lib/lib_ignore.go":     "//go:build ignore\n\npackage lib\n\nconst Suffix = \"?\"\n",
		"lib/lib_test.go":       "package lib\n\nconst Suffix = \"#\"\n",
		"other/go.mod":          "module example.com/other\n",
		"other/greet/greet.go":  "package greet\n\nfunc Hello(name string) string { return \"hello \" + name }\n",
		"cmd/unrelated/main.go": "package main\n\nimport _ \"github.com/never/fetched\"\n\nfunc main() {}\n",
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	return root
}

func TestResolveScriptLoadsLocalPackagesWithoutNetwork(t *testing.T) {
	t.Parallel()
	root := writeLocalModuleFixture(t)

	resolution, err := ResolveScript(context.Background(), filepath.Join(root, "main.go"), ScriptResolverOptions{StdlibPackages: map[string]struct{}{"fmt": {}, "strings": {}}})
	require.NoError(t, err)

	require.Equal(t, []string{"example.com/other", "example.com/app"}, resolution.LocalModules, "the replaced module loads before the module that imports it")
	require.Len(t, resolution.Modules, 2)
	require.Empty(t, resolution.Fetched)

	other := resolution.Modules[0]
	require.Equal(t, "example.com/other", other.Path)
	require.Empty(t, other.Version)
	require.Equal(t, []string{"greet"}, other.PackageOrder)

	app := resolution.Modules[1]
	require.Equal(t, "example.com/app", app.Path)
	require.Equal(t, []string{"lib", "internal/util"}, app.PackageOrder, "lib is compiled before the package that imports it")
	require.Equal(t, map[string]string{"lib.go": "package lib\n\nconst Suffix = \"!\"\n"}, app.Packages["lib"], "test and ignored files stay out")
	require.NotContains(t, app.Packages, "cmd/unrelated", "only reachable packages are resolved")
}

func TestResolveScriptFindsAncestorGoMod(t *testing.T) {
	t.Parallel()
	root := writeLocalModuleFixture(t)
	script := filepath.Join(root, "cmd", "tool", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(script), 0o750))
	require.NoError(t, os.WriteFile(script, []byte("package main\n\nimport \"example.com/app/lib\"\n\nfunc main() { _ = lib.Suffix }\n"), 0o600))

	resolution, err := ResolveScript(context.Background(), script, ScriptResolverOptions{})
	require.NoError(t, err)
	require.Equal(t, []string{"example.com/app"}, resolution.LocalModules)
	require.Equal(t, []string{"lib"}, resolution.Modules[0].PackageOrder)
}

func TestResolveScriptLocalPackageRemoteImportNeedsNetwork(t *testing.T) {
	t.Parallel()
	root := writeLocalModuleFixture(t)
	script := filepath.Join(root, "cmd", "unrelated", "main.go")
	require.NoError(t, os.WriteFile(filepath.Join(root, "lib", "remote.go"), []byte("package lib\n\nimport _ \"github.com/never/fetched\"\n"), 0o600))
	require.NoError(t, os.WriteFile(script, []byte("package main\n\nimport \"example.com/app/lib\"\n\nfunc main() { _ = lib.Suffix }\n"), 0o600))

	_, err := ResolveScript(context.Background(), script, ScriptResolverOptions{})
	require.ErrorIs(t, err, ErrNetworkNotAllowed, "a remote import inside a local package still needs --allow-network")
}

func TestResolveScriptReportsMissingLocalPackage(t *testing.T) {
	t.Parallel()
	root := writeLocalModuleFixture(t)
	script := filepath.Join(root, "missing.go")
	require.NoError(t, os.WriteFile(script, []byte("package main\n\nimport _ \"example.com/app/nowhere\"\n\nfunc main() {}\n"), 0o600))

	_, err := ResolveScript(context.Background(), script, ScriptResolverOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "example.com/app/nowhere")
}

func TestResolveDirectoryLoadsLocalPackages(t *testing.T) {
	t.Parallel()
	root := writeLocalModuleFixture(t)
	source, err := os.ReadFile(filepath.Join(root, "main.go"))
	require.NoError(t, err)

	resolution, err := ResolveDirectory(context.Background(), root, map[string]string{"main.go": string(source)}, ScriptResolverOptions{})
	require.NoError(t, err)
	require.Equal(t, []string{"example.com/other", "example.com/app"}, resolution.LocalModules)
	require.Equal(t, string(source), resolution.MainSources["main.go"])
}

func TestImportsNeedResolutionRecognisesLocalModules(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module app\n"), 0o600))
	stdlib := map[string]struct{}{"fmt": {}}

	require.True(t, ImportsNeedResolution(root, []string{"fmt", "app/util"}, stdlib, nil), "a dotless local module path is not stdlib")
	require.False(t, ImportsNeedResolution(root, []string{"fmt"}, stdlib, nil))
	require.True(t, ImportsNeedResolution(t.TempDir(), []string{"github.com/x/y"}, stdlib, nil), "remote imports resolve with or without a go.mod")
	require.False(t, ImportsNeedResolution(t.TempDir(), []string{"app/util"}, stdlib, nil), "without a go.mod an unknown path is left to the compiler")
}

func TestResolveScriptSnapshotServesLocalPackagesFromCapture(t *testing.T) {
	t.Parallel()
	root := writeLocalModuleFixture(t)
	script := filepath.Join(root, "main.go")
	snapshot, err := CaptureInvocation(script, nil, "main", nil)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(root, "lib", "lib.go"), []byte("package lib\n\nconst Suffix = \"changed\"\n"), 0o600))

	resolution, err := ResolveScript(context.Background(), script, ScriptResolverOptions{Snapshot: snapshot})
	require.NoError(t, err)
	require.Equal(t, []string{"example.com/other", "example.com/app"}, resolution.LocalModules)
	require.Equal(t, "package lib\n\nconst Suffix = \"!\"\n", resolution.Modules[1].Packages["lib"]["lib.go"], "the approved bytes win over the live tree")
}

func TestResolveScriptSnapshotRefusesReplaceOutsideRoot(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	root := filepath.Join(parent, "app")
	require.NoError(t, os.MkdirAll(filepath.Join(parent, "sibling"), 0o750))
	require.NoError(t, os.MkdirAll(root, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(parent, "sibling", "s.go"), []byte("package sibling\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\nreplace example.com/sibling => ../sibling\n"), 0o600))
	script := filepath.Join(root, "main.go")
	require.NoError(t, os.WriteFile(script, []byte("package main\n\nimport _ \"example.com/sibling\"\n\nfunc main() {}\n"), 0o600))

	snapshot, err := CaptureInvocation(script, nil, "main", nil)
	require.NoError(t, err)
	_, err = ResolveScript(context.Background(), script, ScriptResolverOptions{Snapshot: snapshot})
	require.Error(t, err, "a gated run must not read outside the approved root")
	require.True(t, errors.Is(err, os.ErrNotExist) || err != nil)
	require.Contains(t, err.Error(), "approved source snapshot")

	live, err := ResolveScript(context.Background(), script, ScriptResolverOptions{})
	require.NoError(t, err, "the trusted path may follow the replace")
	require.Equal(t, []string{"example.com/sibling"}, live.LocalModules)
}
