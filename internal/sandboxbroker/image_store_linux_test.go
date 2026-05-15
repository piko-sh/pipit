//go:build linux && (amd64 || arm64)

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

package sandboxbroker

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxImageStoreOwnership(t *testing.T) {
	directory := testRecoveryDirectory(t)
	grant := testRecoveryDirectory(t)
	metadata := []string{testRecoveryDirectory(t), testRecoveryDirectory(t), testRecoveryDirectory(t)}
	store, err := OpenLinuxImageStore(directory, []LinuxRootGrant{{Name: "data", Path: grant, Rights: Read | Write}}, metadata)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity, err := store.Identity()
	if err != nil || identity.Inode == 0 || identity.MountID == 0 {
		t.Fatal("missing original image-store identity:", err)
	}
	if initial, err := store.InitialIdentity(); err != nil || initial != identity {
		t.Fatal("empty store could not capture initial identity:", err)
	}
	if err := store.ValidatePolicy([]LinuxRootGrant{{Name: "data", Path: grant, Rights: Read}}, metadata); err != nil {
		t.Fatal("original exclusion policy rejected:", err)
	}
	if err := store.ValidatePolicy([]LinuxRootGrant{{Name: "other", Path: t.TempDir(), Rights: Read}}, metadata); err == nil {
		t.Fatal("uncovered grant accepted")
	}
	if err := store.ValidatePolicy(nil, []string{t.TempDir()}); err == nil {
		t.Fatal("uncovered metadata accepted")
	}
	competing, err := OpenLinuxImageStore(directory, nil, nil)
	if competing != nil {
		_ = competing.Close()
	}
	if err == nil || competing != nil {
		t.Fatal("image store lease was not exclusive:", err)
	}
	leases := make([]*os.File, 0, maximumImageStoreLeases)
	for range maximumImageStoreLeases {
		file, path, err := store.AcquireDirectory()
		if err != nil || path != directory {
			t.Fatal("private directory acquisition failed:", err)
		}
		leases = append(leases, file)
	}
	if file, _, err := store.AcquireDirectory(); file != nil || !errors.Is(err, errLimit) {
		t.Fatal("image lease bound bypassed:", err)
	}
	if _, err := store.InitialIdentity(); err == nil {
		t.Fatal("active staging accepted as initial image identity")
	}
	if err := store.Close(); err == nil {
		t.Fatal("live images lost storage ownership")
	}
	if err := store.ReleaseDirectory(nil); !errors.Is(err, ErrDenied) {
		t.Fatal("foreign image lease accepted:", err)
	}
	for _, file := range leases {
		if err := store.ReleaseDirectory(file); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ReleaseDirectory(leases[0]); !errors.Is(err, ErrDenied) {
		t.Fatal("returned lease accepted twice:", err)
	}
	unexpected := filepath.Join(directory, "unexpected")
	if err := os.WriteFile(unexpected, []byte("retain"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err == nil {
		t.Fatal("non-empty storage was released")
	}
	if _, err := store.InitialIdentity(); err == nil {
		t.Fatal("non-empty storage accepted as initial image identity")
	}
	if data, err := os.ReadFile(unexpected); err != nil || string(data) != "retain" {
		t.Fatal("unexpected storage entry changed:", err)
	}
	if err := os.Remove(unexpected); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Identity(); !errors.Is(err, ErrClosed) {
		t.Fatal("closed store exposed identity:", err)
	}
	if file, _, err := store.AcquireDirectory(); file != nil || !errors.Is(err, ErrClosed) {
		t.Fatal("closed store granted staging:", err)
	}
	reopened, err := OpenLinuxImageStore(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if current, err := reopened.Identity(); err != nil || current != identity {
		t.Fatal("storage identity changed on reopen:", err)
	}
}

func TestLinuxImageStoreRejectsOverlap(t *testing.T) {
	for _, mode := range []string{"same-grant", "grant-ancestor", "grant-descendant", "same-metadata", "metadata-ancestor", "metadata-descendant"} {
		t.Run(mode, func(t *testing.T) {
			ancestor := testRecoveryDirectory(t)
			descendant := filepath.Join(ancestor, "nested")
			if err := os.Mkdir(descendant, 0700); err != nil {
				t.Fatal(err)
			}
			directory, excluded := descendant, ancestor
			switch mode {
			case "same-grant", "same-metadata":
				excluded = directory
			case "grant-descendant", "metadata-descendant":
				directory, excluded = ancestor, descendant
			}
			var grants []LinuxRootGrant
			var metadata []string
			switch mode {
			case "same-grant", "grant-ancestor", "grant-descendant":
				grants = []LinuxRootGrant{{Name: "data", Path: excluded, Rights: Read}}
			default:
				metadata = []string{excluded}
			}
			store, err := OpenLinuxImageStore(directory, grants, metadata)
			if store != nil {
				_ = store.Close()
			}
			if err == nil || store != nil {
				t.Fatal("overlapping image storage accepted:", err)
			}
			var probe LinuxRecoveryJournal
			if err := probe.open(directory); err != nil {
				t.Fatal("failed image claim retained its lease:", err)
			}
			if err := probe.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLinuxImageStoreRejectsChangedPaths(t *testing.T) {
	for _, change := range []string{"storage", "grant", "mode"} {
		t.Run(change, func(t *testing.T) {
			directory, grant := testRecoveryDirectory(t), testRecoveryDirectory(t)
			store, err := OpenLinuxImageStore(directory, []LinuxRootGrant{{Name: "data", Path: grant, Rights: Read}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			target := directory
			if change == "grant" {
				target = grant
			}
			original := target + ".original"
			if change == "mode" {
				if err := os.Chmod(target, 0755); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Rename(target, original); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if file, _, err := store.AcquireDirectory(); file != nil || err == nil {
				t.Fatal("changed storage policy admitted staging:", err)
			}
			if change == "mode" {
				if err := os.Chmod(target, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(original, target); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.Close(); err != nil {
				t.Fatal("restored storage could not close:", err)
			}
		})
	}
}
