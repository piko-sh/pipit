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
