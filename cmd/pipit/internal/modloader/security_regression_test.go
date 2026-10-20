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
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
	"pipit.sh/pipit/internal/sandboxtest"
	"pipit.sh/pipit/sdk/stdlib"
)

func TestGatesRejectAuthorityConstructors(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"os.DirFS", "os.OpenRoot", "os.OpenInRoot", "os.Exit", "os.Environ", "os.Clearenv"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			hook := NewHook(HookModeFrozen, NewStore(filepath.Join(t.TempDir(), LockfileName)), nil)
			hook.Gate(allAxes...)
			require.Error(t, hook.CheckFunctionCall(t.Context(), "", name, nil))
		})
	}
}

func TestCapabilityGateNativeDispatch(t *testing.T) {
	t.Parallel()
	if source := os.Getenv("PIPIT_GATE_TEST_SOURCE"); source != "" {
		hook := NewHook(HookModeFrozen, NewStore(""), nil)
		hook.Gate(allAxes...)
		interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithCapabilityHook(hook))
		_, err := interpreter.EvalFile(t.Context(), source, "main")
		require.Error(t, err, "the gate denies the call")
		require.Contains(t, err.Error(), "modloader:")
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
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			command := sandboxtest.Command(ctx, "TestCapabilityGateNativeDispatch", "PIPIT_GATE_TEST_SOURCE=package main; import \"os\"; func main() { "+body+" }")
			output, err := command.CombinedOutput()
			require.NoError(t, err, "child output:\n%s", output)
		})
	}
}

func TestScriptIdentityTracksSourceAndLocation(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	script := filepath.Join(directory, "main.go")
	require.NoError(t, os.WriteFile(script, []byte("package main"), 0600))
	path, original, err := ScriptIdentity(script)
	require.NoError(t, err)
	store := NewStore(filepath.Join(directory, "approval.lock"))
	store.SetScript(path, original)
	store.Upsert(LockedModule{ApprovedCapabilities: []string{"env.read(*)"}})
	require.NoError(t, store.Save())
	_, unchanged, err := ScriptIdentity(script)
	require.NoError(t, err, "the approval file does not change the source identity")
	require.Equal(t, original, unchanged)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "helper.go"), []byte("package main; const Value = 1"), 0600))
	_, changed, err := ScriptIdentity(script)
	require.NoError(t, err, "a source edit changes the identity")
	require.NotEqual(t, original, changed)
	store.SetScript(path, changed)
	require.Empty(t, store.Snapshot().Modules[0].ApprovedCapabilities, "changed source drops its approvals")
}

func TestLegacyApprovalsAreNotMigrated(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "legacy.lock")
	require.NoError(t, os.WriteFile(path, []byte(`{"schema":1,"modules":[{"path":"","approved_capabilities":["env.read(*)"]}]}`), 0600))
	store := NewStore(path)
	require.NoError(t, store.Load())
	require.Empty(t, store.Snapshot().Modules[0].ApprovedCapabilities, "a legacy approval is not migrated")
}

func TestScriptIdentityInvalidatesApprovals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		script string
		digest string
	}{
		{name: "another script", script: "other.go", digest: "sha256:original"},
		{name: "changed source", script: "main.go", digest: "sha256:changed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := NewStore(filepath.Join(t.TempDir(), LockfileName))
			store.SetScript("main.go", "sha256:original")
			store.Upsert(LockedModule{Path: "", ApprovedCapabilities: []string{"env.read(*)"}})
			store.SetScript(tt.script, tt.digest)
			hook := NewHook(HookModeFrozen, store, nil)
			hook.Gate(AxisEnvRead)
			require.Error(t, hook.CheckFunctionCall(t.Context(), "", "os.Environ", nil), "a stale approval is refused")
		})
	}
}

func TestStoreApprovalCopiesAreIndependent(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), LockfileName))
	claims := []string{"env.read(ORIGINAL)"}
	store.Upsert(LockedModule{Path: "", ApprovedCapabilities: claims})
	claims[0] = "env.read(*)"
	snapshot := store.Snapshot()
	snapshot.Modules[0].ApprovedCapabilities[0] = "env.read(*)"
	module, _ := store.Lookup("", "")
	module.ApprovedCapabilities[0] = "env.read(*)"
	actual, _ := store.Lookup("", "")
	require.Equal(t, "env.read(ORIGINAL)", actual.ApprovedCapabilities[0], "approval storage is not mutable from outside")
}
