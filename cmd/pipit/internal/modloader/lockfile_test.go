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

func TestStoreUpsertAndLookup(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), LockfileName))
	store.Upsert(LockedModule{Path: "example.com/mod", Version: "v1", BundleDigest: "sha256:aaa"})
	got, ok := store.Lookup("example.com/mod", "v1")
	require.True(t, ok, "a lookup after an upsert finds the entry")
	require.Equal(t, "sha256:aaa", got.BundleDigest, "the bundle digest matches")

	store.Upsert(LockedModule{Path: "example.com/mod", Version: "v1", BundleDigest: "sha256:bbb"})
	got, _ = store.Lookup("example.com/mod", "v1")
	require.Equal(t, "sha256:bbb", got.BundleDigest, "an upsert replaces the prior entry")
}

func TestStoreSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), LockfileName)
	store := NewStore(path)
	store.SetScript("hello.go", "sha256:scripthash")
	store.Upsert(LockedModule{
		Path:                 "example.com/mod",
		Version:              "v1",
		BundleDigest:         "sha256:bundle",
		ApprovedCapabilities: []string{"network.dial(*:443)"},
		ApprovedVia:          "interactive",
	})
	require.NoError(t, store.Save())

	reloaded := NewStore(path)
	require.NoError(t, reloaded.Load())
	got, ok := reloaded.Lookup("example.com/mod", "v1")
	require.True(t, ok, "the reloaded store holds the module")
	require.Equal(t, "sha256:bundle", got.BundleDigest, "the bundle digest survives a reload")
	require.Len(t, got.ApprovedCapabilities, 1, "approved capabilities survive a reload")
	require.Equal(t, "network.dial(*:443)", got.ApprovedCapabilities[0])
}

func TestStoreLoadMissingFileIsNoop(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "absent.lock"))
	require.NoError(t, store.Load(), "loading a missing file returns nil")
}

func TestStoreLoadRejectsFutureSchema(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), LockfileName)
	future := `{"schema": 99999, "modules": []}`
	require.NoError(t, os.WriteFile(path, []byte(future), 0o600))
	store := NewStore(path)
	err := store.Load()
	require.Error(t, err, "a future schema is rejected")
}

type mockPrompter struct {
	approve bool
	err     error
	calls   int
}

func (m *mockPrompter) Confirm(_ context.Context, _, _, _ string) (bool, error) {
	m.calls++
	return m.approve, m.err
}

func TestHookPermissiveModeAllowsAll(t *testing.T) {
	t.Parallel()
	hook := NewHook(HookModePermissive, nil, nil)
	require.NoError(t, hook.CheckFileOpen(t.Context(), "", "/etc/passwd", 0, 0))
	require.NoError(t, hook.CheckExec(t.Context(), "", "ls", nil))
}

func TestHookFrozenModeRejectsUnapproved(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), LockfileName))
	hook := NewHook(HookModeFrozen, store, nil)
	err := hook.CheckFileOpen(t.Context(), "example.com/mod", "/etc/passwd", 0, 0)
	require.Error(t, err, "frozen mode rejects an unapproved claim")
}

func TestHookFrozenModeAcceptsApproved(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), LockfileName))
	store.Upsert(LockedModule{
		Path:                 "example.com/mod",
		Version:              "v1",
		ApprovedCapabilities: []string{"filesystem.read(/etc/passwd)"},
	})
	hook := NewHook(HookModeFrozen, store, nil)
	require.NoError(t, hook.CheckFileOpen(t.Context(), "example.com/mod", "/etc/passwd", 0, 0), "frozen mode accepts an approved claim")
}

func TestHookInteractivePromptsAndPersists(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), LockfileName))
	prompter := &mockPrompter{approve: true}
	hook := NewHook(HookModeInteractive, store, prompter)

	require.NoError(t, hook.CheckFileOpen(t.Context(), "example.com/mod", "/tmp/x", 0, 0))
	require.Equal(t, 1, prompter.calls)

	require.NoError(t, hook.CheckFileOpen(t.Context(), "example.com/mod", "/tmp/x", 0, 0), "a second call is accepted")
	require.Equal(t, 1, prompter.calls, "the approval is cached")
}

func TestHookInteractiveDenialPropagates(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), LockfileName))
	prompter := &mockPrompter{approve: false}
	hook := NewHook(HookModeInteractive, store, prompter)
	err := hook.CheckFileOpen(t.Context(), "example.com/mod", "/tmp/x", 0, 0)
	require.Error(t, err, "an operator denial fails")
}

func TestHookInteractivePromptErrorBubbles(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), LockfileName))
	promptErr := errors.New("operator closed stdin")
	prompter := &mockPrompter{err: promptErr}
	hook := NewHook(HookModeInteractive, store, prompter)
	err := hook.CheckFileOpen(t.Context(), "x", "y", 0, 0)
	require.ErrorIs(t, err, promptErr, "a prompt error propagates")
}
