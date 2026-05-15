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
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pipit.sh/pipit/cmd/pipit/internal/clitest"
	"pipit.sh/pipit/cmd/pipit/internal/modloader"
)

func TestRunApprovalBinding(t *testing.T) {
	for _, test := range []struct {
		name                     string
		changePath, changeSource bool
		changeDirectory          bool
		wantCode                 int
		flags                    []string
		arguments                []string
	}{
		{name: "same identity", changePath: false, changeSource: false, wantCode: 0},
		{name: "different script", changePath: true, changeSource: false, wantCode: 1},
		{name: "changed source", changePath: false, changeSource: true, wantCode: 1},
		{name: "changed working directory", changeDirectory: true, wantCode: 1},
		{name: "changed gates", flags: []string{"--gate=disk"}, wantCode: 1},
		{name: "changed entrypoint", flags: []string{"--entrypoint=alternate"}, wantCode: 1},
		{name: "changed arguments", arguments: []string{"changed"}, wantCode: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			script := filepath.Join(directory, "main.go")
			source := `package main; import "os"; func main() { os.ReadFile(".") }; func alternate() { os.ReadFile(".") }`
			if err := os.WriteFile(script, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			axes, err := modloader.ParseGateSpec("all")
			if err != nil {
				t.Fatal(err)
			}
			path, hash, err := modloader.InvocationIdentity(script, axes, "main", nil)
			if err != nil {
				t.Fatal(err)
			}
			if test.changePath {
				path += ".other"
			}
			if test.changeSource {
				hash = "sha256:old"
			}
			approvalPath := filepath.Join(directory, "approval.lock")
			store := modloader.NewStore(approvalPath)
			store.SetScript(path, hash)
			store.Upsert(modloader.LockedModule{ApprovedCapabilities: []string{"filesystem.read(.)"}})
			if err := store.Save(); err != nil {
				t.Fatal(err)
			}
			args := make([]string, 0, 4+len(test.flags)+len(test.arguments))
			if test.changeDirectory {
				t.Chdir(t.TempDir())
			}
			args = append(args, "--gate=all", "--autodeny", "--lockfile="+approvalPath)
			args = append(args, test.flags...)
			args = append(args, script)
			args = append(args, test.arguments...)
			result := clitest.Run(context.Background(), RunRun, args, "")
			if result.Code != test.wantCode {
				t.Fatalf("exit=%d stderr=%s", result.Code, result.Stderr)
			}
			if test.wantCode != 0 && !strings.Contains(result.Stderr, "not approved") {
				t.Fatalf("wrong failure: %s", result.Stderr)
			}
		})
	}
}
