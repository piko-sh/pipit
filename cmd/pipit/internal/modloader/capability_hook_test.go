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

	"github.com/stretchr/testify/require"
)

type recordingPrompter struct {
	claims []string
}

func (p *recordingPrompter) Confirm(_ context.Context, _, axis, scope string) (bool, error) {
	p.claims = append(p.claims, axis+"("+scope+")")
	return true, nil
}

func TestTypedChecksIgnoreAxesThatAreNotGated(t *testing.T) {
	t.Parallel()
	hook := NewHook(HookModeFrozen, NewStore(filepath.Join(t.TempDir(), LockfileName)), nil)
	hook.Gate(AxisNetworkDial, AxisNetworkListen)

	require.NoError(t, hook.CheckFileOpen(t.Context(), "", "/etc/hostname", os.O_RDONLY, 0), "--gate=network leaves file reads alone")
	require.NoError(t, hook.CheckFileWrite(t.Context(), "", "/tmp/x"), "--gate=network leaves file writes alone")
	require.NoError(t, hook.CheckGetenv(t.Context(), "", "HOME"))
	require.NoError(t, hook.CheckSetenv(t.Context(), "", "HOME", "x"))
	require.NoError(t, hook.CheckExec(t.Context(), "", "ls", nil))
	require.NoError(t, hook.CheckSubprocess(t.Context(), "", "ls", nil))
	require.Error(t, hook.CheckNetDial(t.Context(), "", "tcp", "example.com:443"), "the gated axis is still enforced")
	require.Error(t, hook.CheckNetListen(t.Context(), "", "tcp", ":80"), "the gated axis is still enforced")
}

func TestCheckFunctionCallRefusesChdirUnderADiskGate(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), LockfileName))
	store.Upsert(LockedModule{Path: "", ApprovedCapabilities: []string{"filesystem.read(/outside)"}})
	hook := NewHook(HookModeFrozen, store, nil)
	hook.Gate(AxisFilesystemRead, AxisFilesystemWrite)
	err := hook.CheckFunctionCall(t.Context(), "", "os.Chdir", strArgs("/outside"))
	require.ErrorContains(t, err, "working directory")

	ungatedDisk := NewHook(HookModeFrozen, store, nil)
	ungatedDisk.Gate(AxisNetworkDial)
	require.NoError(t, ungatedDisk.CheckFunctionCall(t.Context(), "", "os.Chdir", strArgs("/outside")))
}

func TestCheckFunctionCallPromptsForEachPathOfATwoPathCall(t *testing.T) {
	t.Parallel()
	prompter := &recordingPrompter{}
	hook := NewHook(HookModeInteractive, NewStore(filepath.Join(t.TempDir(), LockfileName)), prompter)
	hook.Gate(AxisFilesystemRead, AxisFilesystemWrite)

	require.NoError(t, hook.CheckFunctionCall(t.Context(), "", "os.Rename", strArgs("/allowed/a", "/outside/b")))
	require.NoError(t, hook.CheckFunctionCall(t.Context(), "", "os.Symlink", strArgs("../etc/hostname", "/allowed/link")))
	require.Equal(t, []string{
		"filesystem.write(/allowed/a)",
		"filesystem.write(/outside/b)",
		"filesystem.write(/allowed/link)",
		"filesystem.read(/etc/hostname)",
	}, prompter.claims)

	require.NoError(t, hook.CheckFileWrite(t.Context(), "", "/outside/b"))
	require.Len(t, prompter.claims, 4, "the typed gate's claim matches the one already approved")
}
