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

func TestRunRunHelloFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "hello.go")
	const source = `package main

import "fmt"

func main() {
	fmt.Println("Hello, Pipit!")
}
`
	require.NoError(t, os.WriteFile(file, []byte(source), 0o644))

	result := clitest.Run(t.Context(), RunRun, []string{file}, "")
	require.Equal(t, 0, result.Code, "stderr: %q", result.Stderr)

}

func TestRunRunMissingFile(t *testing.T) {
	t.Parallel()

	result := clitest.Run(t.Context(), RunRun, []string{"/nope/does/not/exist.go"}, "")
	require.NotEqual(t, 0, result.Code, "the command fails")
}

func TestRunRunCacheFlagAcceptsAllModes(t *testing.T) {
	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "hello.go")
	const source = `package main

import "fmt"

func main() { fmt.Println("ok") }
`
	require.NoError(t, os.WriteFile(file, []byte(source), 0o644))

	pathSpec := filepath.Join(t.TempDir(), "custom-cache")

	tests := []struct {
		name string
		spec string
	}{
		{name: "off", spec: "off"},
		{name: "on", spec: "on"},
		{name: "home", spec: "home"},
		{name: "gopath", spec: "gopath"},
		{name: "absolute path", spec: pathSpec},
		{name: "relative path", spec: "./" + filepath.Base(t.TempDir())},
		{name: "home-relative path", spec: "~/.pipit-cache-flag-test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := clitest.Run(t.Context(), RunRun, []string{"--cache=" + tt.spec, file}, "")
			require.Equal(t, 0, result.Code, "stderr: %q", result.Stderr)
		})
	}
}

func TestRunRunCacheFlagRejectsBareWord(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "hello.go")
	require.NoError(t, os.WriteFile(file, []byte("package main\nfunc main(){}\n"), 0o644))
	result := clitest.Run(t.Context(), RunRun,
		[]string{"--cache=localdir", file}, "")
	require.NotEqual(t, 0, result.Code, "an invalid cache value fails the command")
	require.Contains(t, result.Stderr, "localdir", "stderr mentions the bad value")
}

func TestRunDirectorySkipsFilesTheInterpreterExcludes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	files := map[string]string{
		"main.go":   "package main\n\nfunc run() string { return content() }\n",
		"interp.go": "//go:build interp\n\npackage main\n\nfunc content() string { return \"interpreted\" }\n",
		"native.go": "//go:build !interp\n\npackage main\n\nimport \"example.com/never/fetched\"\n\nfunc content() string { return fetched.Value }\n",
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o600))
	}
	result := clitest.Run(t.Context(), RunRun, []string{"-entrypoint", "run", root}, "")
	require.Equal(t, 0, result.Code, "stderr: %q", result.Stderr)
	require.Equal(t, "interpreted", strings.TrimSpace(result.Stdout))
}
