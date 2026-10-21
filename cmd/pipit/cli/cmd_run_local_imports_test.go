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

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/cmd/pipit/internal/clitest"
)

func writeLocalModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/app\n\nreplace example.com/greeting => ./greeting\n",
		"main.go": `package main

import (
	"fmt"

	"example.com/app/internal/util"
)

func main() { fmt.Println(run()) }

// run is the entrypoint the tests invoke: its result reaches the command's stdout
// writer, whereas a script's own prints go to the process stdout.
func run() string { return util.Greet("pipit") }
`,
		"internal/util/util.go": `package util

import (
	"strings"

	"example.com/greeting"
)

func Greet(name string) string { return strings.ToUpper(greeting.Hello(name)) }
`,
		"greeting/greeting.go": "package greeting\n\nfunc Hello(name string) string { return \"hello, \" + name }\n",
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	return root
}

func TestRunScriptImportsLocalModulePackages(t *testing.T) {
	t.Parallel()
	root := writeLocalModule(t)
	result := clitest.Run(t.Context(), RunRun, []string{"-entrypoint", "run", filepath.Join(root, "main.go")}, "")
	require.Equal(t, 0, result.Code, "stderr: %q", result.Stderr)
	require.Equal(t, "HELLO, PIPIT", strings.TrimSpace(result.Stdout), "stdout: %q", result.Stdout)
	require.Contains(t, result.Stderr, "local module(s): example.com/greeting, example.com/app")
}

func TestRunDirectoryImportsLocalModulePackages(t *testing.T) {
	t.Parallel()
	root := writeLocalModule(t)
	result := clitest.Run(t.Context(), RunRun, []string{"-entrypoint", "run", root}, "")
	require.Equal(t, 0, result.Code, "stderr: %q", result.Stderr)
	require.Equal(t, "HELLO, PIPIT", strings.TrimSpace(result.Stdout), "stdout: %q", result.Stdout)
}

func TestRunScriptMissingLocalPackageFails(t *testing.T) {
	t.Parallel()
	root := writeLocalModule(t)
	script := filepath.Join(root, "broken.go")
	require.NoError(t, os.WriteFile(script, []byte("package main\n\nimport _ \"example.com/app/nowhere\"\n\nfunc main() {}\n"), 0o600))
	result := clitest.Run(t.Context(), RunRun, []string{script}, "")
	require.NotEqual(t, 0, result.Code, "stdout: %q", result.Stdout)
	require.Contains(t, result.Stderr, "example.com/app/nowhere")
}

func TestRunRefusesALocalImportThatLeavesTheModule(t *testing.T) {
	t.Parallel()
	root := writeLocalModule(t)
	outside := t.TempDir()
	outsidePackage := filepath.Join(outside, "pkg")
	require.NoError(t, os.MkdirAll(outsidePackage, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(outsidePackage, "pkg.go"), []byte("package pkg\n\nconst Leaked = \"outside-module-source\"\n"), 0o600))
	script := filepath.Join(root, "escape.go")
	source := "package main\n\nimport \"example.com/app/../" + filepath.Base(outside) + "/pkg\"\n\nfunc run() string { return pkg.Leaked }\n\nfunc main() {}\n"
	require.NoError(t, os.WriteFile(script, []byte(source), 0o600))

	result := clitest.Run(t.Context(), RunRun, []string{"-entrypoint", "run", script}, "")
	require.NotEqual(t, 0, result.Code, "an ungated run compiled source from outside the module")
	require.NotContains(t, result.Stdout, "outside-module-source")
	require.Contains(t, result.Stderr, "does not name a package inside its module")
}
