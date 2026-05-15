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
)

func TestStoreUpsertAndLookup(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), LockfileName))
	store.Upsert(LockedModule{Path: "example.com/mod", Version: "v1", BundleDigest: "sha256:aaa"})
	got, ok := store.Lookup("example.com/mod", "v1")
	if !ok {
		t.Fatalf("Lookup after Upsert returned not-found")
	}
	if got.BundleDigest != "sha256:aaa" {
		t.Fatalf("BundleDigest mismatch: %q", got.BundleDigest)
	}

	store.Upsert(LockedModule{Path: "example.com/mod", Version: "v1", BundleDigest: "sha256:bbb"})
	got, _ = store.Lookup("example.com/mod", "v1")
	if got.BundleDigest != "sha256:bbb" {
		t.Fatalf("Upsert did not replace prior entry")
	}
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
	if err := store.Save(); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	reloaded := NewStore(path)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}
	got, ok := reloaded.Lookup("example.com/mod", "v1")
	if !ok {
		t.Fatalf("reloaded store missing module")
	}
	if got.BundleDigest != "sha256:bundle" {
		t.Fatalf("BundleDigest mismatch after reload: %q", got.BundleDigest)
	}
	if len(got.ApprovedCapabilities) != 1 || got.ApprovedCapabilities[0] != "network.dial(*:443)" {
		t.Fatalf("ApprovedCapabilities lost across reload: %v", got.ApprovedCapabilities)
	}
}

func TestStoreLoadMissingFileIsNoop(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "absent.lock"))
	if err := store.Load(); err != nil {
		t.Fatalf("Load of missing file should return nil, got %v", err)
	}
}

func TestStoreLoadRejectsFutureSchema(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), LockfileName)
	future := `{"schema": 99999, "modules": []}`
	if err := os.WriteFile(path, []byte(future), 0o600); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	store := NewStore(path)
	err := store.Load()
	if err == nil {
		t.Fatalf("expected error for future schema")
	}
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
	if err := hook.CheckFileOpen(context.Background(), "", "/etc/passwd", 0, 0); err != nil {
		t.Fatalf("permissive CheckFileOpen returned %v", err)
	}
	if err := hook.CheckExec(context.Background(), "", "ls", nil); err != nil {
		t.Fatalf("permissive CheckExec returned %v", err)
	}
}

func TestHookFrozenModeRejectsUnapproved(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), LockfileName))
	hook := NewHook(HookModeFrozen, store, nil)
	err := hook.CheckFileOpen(context.Background(), "example.com/mod", "/etc/passwd", 0, 0)
	if err == nil {
		t.Fatalf("frozen mode should reject unapproved claim")
	}
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
	if err := hook.CheckFileOpen(context.Background(), "example.com/mod", "/etc/passwd", 0, 0); err != nil {
		t.Fatalf("frozen mode rejected approved claim: %v", err)
	}
}

func TestHookInteractivePromptsAndPersists(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), LockfileName))
	prompter := &mockPrompter{approve: true}
	hook := NewHook(HookModeInteractive, store, prompter)

	if err := hook.CheckFileOpen(context.Background(), "example.com/mod", "/tmp/x", 0, 0); err != nil {
		t.Fatalf("interactive approval returned %v", err)
	}
	if prompter.calls != 1 {
		t.Fatalf("expected 1 prompter call, got %d", prompter.calls)
	}

	if err := hook.CheckFileOpen(context.Background(), "example.com/mod", "/tmp/x", 0, 0); err != nil {
		t.Fatalf("second call rejected: %v", err)
	}
	if prompter.calls != 1 {
		t.Fatalf("approval not cached; prompter calls = %d", prompter.calls)
	}
}

func TestHookInteractiveDenialPropagates(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), LockfileName))
	prompter := &mockPrompter{approve: false}
	hook := NewHook(HookModeInteractive, store, prompter)
	err := hook.CheckFileOpen(context.Background(), "example.com/mod", "/tmp/x", 0, 0)
	if err == nil {
		t.Fatalf("expected error for operator denial")
	}
}

func TestHookInteractivePromptErrorBubbles(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), LockfileName))
	promptErr := errors.New("operator closed stdin")
	prompter := &mockPrompter{err: promptErr}
	hook := NewHook(HookModeInteractive, store, prompter)
	err := hook.CheckFileOpen(context.Background(), "x", "y", 0, 0)
	if err == nil || !errors.Is(err, promptErr) {
		t.Fatalf("prompt error did not propagate: %v", err)
	}
}
