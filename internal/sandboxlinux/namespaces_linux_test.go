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

package sandboxlinux

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestWorkerAttributes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	attributes, err := WorkerAttributes(root)
	if err != nil {
		t.Fatal(err)
	}
	const required = unix.CLONE_NEWUSER | unix.CLONE_NEWPID | unix.CLONE_NEWNET |
		unix.CLONE_NEWIPC | unix.CLONE_NEWUTS | unix.CLONE_NEWCGROUP
	if attributes.Cloneflags != required || attributes.Unshareflags != unix.CLONE_NEWNS || attributes.Chroot != root {
		t.Fatalf("incomplete namespace configuration: %+v", attributes)
	}
	if !attributes.Setsid || attributes.GidMappingsEnableSetgroups || len(attributes.AmbientCaps) != 0 {
		t.Fatalf("unexpected inherited authority: %+v", attributes)
	}
	if len(attributes.UidMappings) != 1 || len(attributes.GidMappings) != 1 ||
		attributes.UidMappings[0].ContainerID != 0 || attributes.UidMappings[0].HostID != os.Geteuid() ||
		attributes.UidMappings[0].Size != 1 || attributes.GidMappings[0].ContainerID != 0 ||
		attributes.GidMappings[0].HostID != os.Getegid() || attributes.GidMappings[0].Size != 1 {
		t.Fatalf("unexpected identity mappings: %+v", attributes)
	}
	attributes.UidMappings[0].HostID = -1
	fresh, err := WorkerAttributes(root)
	if err != nil || fresh.UidMappings[0].HostID != os.Geteuid() {
		t.Fatalf("namespace configuration shares mutable mappings: %v", err)
	}
}

func TestWorkerAttributesRejectUnsafeRoot(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	private := filepath.Join(parent, "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(private, link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(parent, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	public := filepath.Join(parent, "public")
	if err := os.Mkdir(public, 0755); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"", "/", ".", "relative", private + "/..", link, file, public, filepath.Join(parent, "missing")} {
		t.Run(root, func(t *testing.T) {
			if _, err := WorkerAttributes(root); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("unsafe root %q accepted: %v", root, err)
			}
		})
	}
}

func TestSealWorkerFilesystemRejectHostProcess(t *testing.T) {
	t.Parallel()
	if os.Getpid() == 1 {
		t.Skip("the test process itself is namespace init")
	}
	if err := sealWorkerFilesystem(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("filesystem sealing accepted a host process: %v", err)
	}
}

func TestWorkerRootContents(t *testing.T) {
	t.Parallel()
	for _, variant := range []string{"regular", "empty", "extra", "directory", "symlink"} {
		t.Run(variant, func(t *testing.T) {
			root := t.TempDir()
			worker := filepath.Join(root, "worker")
			var err error
			switch variant {
			case "regular", "extra":
				err = os.WriteFile(worker, nil, 0500)
			case "directory":
				err = os.Mkdir(worker, 0700)
			case "symlink":
				err = os.Symlink("/etc/passwd", worker)
			case "empty":
			}
			if err != nil {
				t.Fatal(err)
			}
			if variant == "extra" {
				if err := os.WriteFile(filepath.Join(root, "unexpected"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			directory, err := os.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			defer directory.Close()
			err = checkWorkerRootContents(directory)
			if variant == "regular" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("unsafe root contents accepted: %v", err)
			}
		})
	}
}
