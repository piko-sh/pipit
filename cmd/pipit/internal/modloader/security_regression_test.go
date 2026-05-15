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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/stdlib"
)

func TestGatesRejectAuthorityConstructors(t *testing.T) {
	for _, name := range []string{"os.DirFS", "os.OpenRoot", "os.OpenInRoot", "os.Exit", "os.Environ", "os.Clearenv"} {
		t.Run(name, func(t *testing.T) {
			hook := NewHook(HookModeFrozen, NewStore(filepath.Join(t.TempDir(), LockfileName)), nil)
			hook.Gate(allAxes...)
			if err := hook.CheckFunctionCall(context.Background(), "", name, nil); err == nil {
				t.Fatal("call was allowed")
			}
		})
	}
}

func TestCapabilityGateNativeDispatch(t *testing.T) {
	if source := os.Getenv("PIPIT_GATE_TEST_SOURCE"); source != "" {
		hook := NewHook(HookModeFrozen, NewStore(""), nil)
		hook.Gate(allAxes...)
		interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithCapabilityHook(hook))
		_, err := interpreter.EvalFile(context.Background(), source, "main")
		if err == nil || !strings.Contains(err.Error(), "modloader:") {
			t.Fatalf("expected gate denial, got %v", err)
		}
		return
	}
	for _, body := range []string{
		`os.Exit(23)`,
		`os.Environ()`,
		`os.Clearenv()`,
		`os.DirFS("/tmp")`,
		`os.OpenRoot("/tmp")`,
		`os.OpenInRoot("/tmp", "marker")`,
		`exit := os.Exit; exit(23)`,
		`root := os.DirFS; root("/tmp")`,
	} {
		t.Run(body, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCapabilityGateNativeDispatch$")
			command.Env = append(os.Environ(), "PIPIT_GATE_TEST_SOURCE=package main; import \"os\"; func main() { "+body+" }")
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("child failed: %v\n%s", err, output)
			}
		})
	}
}

func TestScriptIdentityTracksSourceAndLocation(t *testing.T) {
	directory := t.TempDir()
	script := filepath.Join(directory, "main.go")
	if err := os.WriteFile(script, []byte("package main"), 0600); err != nil {
		t.Fatal(err)
	}
	path, original, err := ScriptIdentity(script)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(directory, "approval.lock"))
	store.SetScript(path, original)
	store.Upsert(LockedModule{ApprovedCapabilities: []string{"env.read(*)"}})
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	_, unchanged, err := ScriptIdentity(script)
	if err != nil || unchanged != original {
		t.Fatalf("approval file changed source identity: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "helper.go"), []byte("package main; const Value = 1"), 0600); err != nil {
		t.Fatal(err)
	}
	_, changed, err := ScriptIdentity(script)
	if err != nil || changed == original {
		t.Fatalf("source edit did not change identity: %v", err)
	}
	store.SetScript(path, changed)
	if len(store.Snapshot().Modules[0].ApprovedCapabilities) != 0 {
		t.Fatal("changed source retained approvals")
	}
}

func TestLegacyApprovalsAreNotMigrated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.lock")
	if err := os.WriteFile(path, []byte(`{"schema":1,"modules":[{"path":"","approved_capabilities":["env.read(*)"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if len(store.Snapshot().Modules[0].ApprovedCapabilities) != 0 {
		t.Fatal("legacy approval was migrated")
	}
}

func TestScriptIdentityInvalidatesApprovals(t *testing.T) {
	for _, identity := range [][2]string{{"other.go", "sha256:original"}, {"main.go", "sha256:changed"}} {
		store := NewStore(filepath.Join(t.TempDir(), LockfileName))
		store.SetScript("main.go", "sha256:original")
		store.Upsert(LockedModule{Path: "", ApprovedCapabilities: []string{"env.read(*)"}})
		store.SetScript(identity[0], identity[1])
		hook := NewHook(HookModeFrozen, store, nil)
		hook.Gate(AxisEnvRead)
		if err := hook.CheckFunctionCall(context.Background(), "", "os.Environ", nil); err == nil {
			t.Fatal("stale approval accepted")
		}
	}
}

func TestStoreApprovalCopiesAreIndependent(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), LockfileName))
	claims := []string{"env.read(ORIGINAL)"}
	store.Upsert(LockedModule{Path: "", ApprovedCapabilities: claims})
	claims[0] = "env.read(*)"
	snapshot := store.Snapshot()
	snapshot.Modules[0].ApprovedCapabilities[0] = "env.read(*)"
	module, _ := store.Lookup("", "")
	module.ApprovedCapabilities[0] = "env.read(*)"
	actual, _ := store.Lookup("", "")
	if actual.ApprovedCapabilities[0] != "env.read(ORIGINAL)" {
		t.Fatal("approval storage is externally mutable")
	}
}
