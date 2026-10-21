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

//go:build linux && (amd64 || arm64)

package sandboxlinux

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"pipit.sh/pipit/internal/sandboxbroker"
)

func TestServiceRecoveryPruneRequiresQuiescence(t *testing.T) {
	var absent *ServiceRecoveryClaim
	if err := absent.Prune(t.Context()); !errors.Is(err, errClosed) {
		t.Fatal("missing claim accepted:", err)
	}
	owner := &ServiceRecoveryClaim{snapshot: &sandboxbroker.LinuxRecoveryClaim{}}
	defer owner.Close()
	if err := owner.Prune(nil); !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("missing context accepted:", err)
	}
	if err := owner.Prune(t.Context()); !errors.Is(err, ErrServiceBusy) {
		t.Fatal("pruned before quiescence:", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Prune(t.Context()); !errors.Is(err, errClosed) {
		t.Fatal("closed claim accepted:", err)
	}
}

func TestRecoveryPruneRejectsOrdinaryDirectory(t *testing.T) {
	path := t.TempDir()
	if err := os.Mkdir(filepath.Join(path, "untouched"), 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := pruneRecoveryDescendants(t.Context(), root); !errors.Is(err, ErrUnavailable) {
		t.Fatal("ordinary filesystem accepted for pruning:", err)
	}
	if _, err := os.Stat(filepath.Join(path, "untouched")); err != nil {
		t.Fatal("ordinary directory was removed:", err)
	}
}
