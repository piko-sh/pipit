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

func exerciseRecoveryLandlock(t *testing.T) {
	t.Helper()
	directory := os.Getenv("PIPIT_TEST_BROKER_ROOT")
	target := filepath.Join(directory, "file")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := openTestFilesystem(t, directory, Write, FilesystemLimits{})
	_, record := stageRecoveryRemnant(t, backend, "data", "file")
	backend.recovery = []RecoveryRecord{record}
	if err := backend.SealProcess(); err != nil {
		t.Fatal(err)
	}
	if err := backend.beginProtocol(); err != nil {
		t.Fatal(err)
	}
	if err := backend.recover(); err != nil {
		t.Fatal("recovery-only Landlock blocked cleanup:", err)
	}
	if data, err := os.ReadFile(target); !errors.Is(err, os.ErrPermission) || data != nil {
		t.Fatal("recovery role read file content:", err)
	}
	for _, name := range []string{target, filepath.Join(directory, "created")} {
		flags := os.O_WRONLY
		if name != target {
			flags |= os.O_CREATE
		}
		file, err := os.OpenFile(name, flags, 0600)
		if file != nil {
			_ = file.Close()
		}
		if !errors.Is(err, os.ErrPermission) {
			t.Fatal("recovery role opened content for writing:", err)
		}
	}
	file, err := createLinuxStagingFile(backend.roots["data"])
	if file != nil {
		_ = file.Close()
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatal("recovery role created anonymous inode:", err)
	}
}
