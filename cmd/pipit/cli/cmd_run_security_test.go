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
	"pipit.sh/pipit/cmd/pipit/internal/modloader"
)

func TestRunApprovalBinding(t *testing.T) {
	tests := []struct {
		name            string
		changePath      bool
		changeSource    bool
		changeDirectory bool
		wantCode        int
		flags           []string
		arguments       []string
	}{
		{name: "same identity", changePath: false, changeSource: false, wantCode: 0},
		{name: "different script", changePath: true, changeSource: false, wantCode: 1},
		{name: "changed source", changePath: false, changeSource: true, wantCode: 1},
		{name: "changed working directory", changeDirectory: true, wantCode: 1},
		{name: "changed gates", flags: []string{"--gate=disk"}, wantCode: 1},
		{name: "changed entrypoint", flags: []string{"--entrypoint=alternate"}, wantCode: 1},
		{name: "changed arguments", arguments: []string{"changed"}, wantCode: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directory := t.TempDir()
			script := filepath.Join(directory, "main.go")
			source := `package main; import "os"; func main() { os.ReadFile(".") }; func alternate() { os.ReadFile(".") }`
			require.NoError(t, os.WriteFile(script, []byte(source), 0600))
			axes, err := modloader.ParseGateSpec("all")
			require.NoError(t, err)
			path, hash, err := modloader.InvocationIdentity(script, axes, "main", nil)
			require.NoError(t, err)
			if tt.changePath {
				path += ".other"
			}
			if tt.changeSource {
				hash = "sha256:old"
			}
			approvalPath := filepath.Join(directory, "approval.lock")
			store := modloader.NewStore(approvalPath)
			store.SetScript(path, hash)
			store.Upsert(modloader.LockedModule{ApprovedCapabilities: []string{"filesystem.read(.)"}})
			require.NoError(t, store.Save())
			args := make([]string, 0, 4+len(tt.flags)+len(tt.arguments))
			if tt.changeDirectory {
				t.Chdir(t.TempDir())
			}
			args = append(args, "--gate=all", "--autodeny", "--lockfile="+approvalPath)
			args = append(args, tt.flags...)
			args = append(args, script)
			args = append(args, tt.arguments...)
			result := clitest.Run(t.Context(), RunRun, args, "")
			require.Equal(t, tt.wantCode, result.Code, "stderr: %s", result.Stderr)
			if tt.wantCode != 0 {
				require.Contains(t, result.Stderr, "not approved", "the failure is an approval failure")
			}
		})
	}
}

const diskGateProbe = `package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

func attempt(name string, err error) {
	if err == nil {
		fmt.Println(name, "reached outside")
	}
}

func main() {
	outside := os.Args[1]
	switch os.Args[2] {
	case "createtemp":
		file, err := os.CreateTemp(outside, "probe-*")
		if err == nil {
			_ = file.Close()
		}
		attempt("os.CreateTemp", err)
	case "mkdirtemp":
		_, err := os.MkdirTemp(outside, "probe-*")
		attempt("os.MkdirTemp", err)
	case "stat":
		_, err := os.Stat(outside + "/secret.txt")
		attempt("os.Stat", err)
	case "readdir":
		_, err := os.ReadDir(outside)
		attempt("os.ReadDir", err)
	case "readlink":
		_, err := os.Readlink(outside + "/link")
		attempt("os.Readlink", err)
	case "chtimes":
		when := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
		attempt("os.Chtimes", os.Chtimes(outside+"/secret.txt", when, when))
	case "lchown":
		attempt("os.Lchown", os.Lchown(outside+"/secret.txt", os.Getuid(), os.Getgid()))
	case "chdir":
		attempt("os.Chdir", os.Chdir(outside))
	case "walkdir":
		attempt("filepath.WalkDir", filepath.WalkDir(outside, func(string, fs.DirEntry, error) error { return nil }))
	case "rename":
		attempt("os.Rename", os.Rename(outside+"/secret.txt", outside+"/moved.txt"))
	case "symlink":
		attempt("os.Symlink", os.Symlink("/etc/hostname", outside+"/planted"))
	}
}
`

func TestRunDiskGateRefusesUnapprovedFileOperations(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	script := filepath.Join(directory, "main.go")
	require.NoError(t, os.WriteFile(script, []byte(diskGateProbe), 0o600))
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	require.NoError(t, os.WriteFile(secret, []byte("outside"), 0o600))
	require.NoError(t, os.Symlink("/etc/hostname", filepath.Join(outside, "link")))
	before, err := os.Stat(secret)
	require.NoError(t, err)

	for _, operation := range []string{"createtemp", "mkdirtemp", "stat", "readdir", "readlink", "chtimes", "lchown", "chdir", "walkdir", "rename", "symlink"} {
		lockfile := filepath.Join(t.TempDir(), "approval.lock")
		args := []string{"--gate=disk", "--autodeny", "--lockfile=" + lockfile, script, outside, operation}
		result := clitest.Run(t.Context(), RunRun, args, "")
		require.NotEqual(t, 0, result.Code, "%s ran under --gate=disk: stdout %q", operation, result.Stdout)
		require.NotContains(t, result.Stdout, "reached outside", operation)
		require.Regexp(t, `not approved|capability gates cannot confine`, result.Stderr, "%s failed for a reason other than the gate", operation)
	}

	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	require.Len(t, entries, 2, "nothing was created or moved outside")
	after, err := os.Stat(secret)
	require.NoError(t, err)
	require.Equal(t, before.ModTime(), after.ModTime())
}

func TestRunNetworkGateLeavesFileReadsAlone(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	data := filepath.Join(directory, "data.txt")
	require.NoError(t, os.WriteFile(data, []byte("local data"), 0o600))
	script := filepath.Join(directory, "main.go")
	source := "package main\n\nimport \"os\"\n\nfunc run() string {\n\tcontent, err := os.ReadFile(os.Args[1])\n\tif err != nil {\n\t\treturn err.Error()\n\t}\n\treturn string(content)\n}\n\nfunc main() {}\n"
	require.NoError(t, os.WriteFile(script, []byte(source), 0o600))

	lockfile := filepath.Join(directory, "approval.lock")
	result := clitest.Run(t.Context(), RunRun, []string{"--gate=network", "--autodeny", "--lockfile=" + lockfile, "--entrypoint=run", script, data}, "")
	require.Equal(t, 0, result.Code, "stderr: %q", result.Stderr)
	require.Equal(t, "local data", strings.TrimSpace(result.Stdout))
}
