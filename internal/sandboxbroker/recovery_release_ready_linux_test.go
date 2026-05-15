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
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestServiceReleaseReadyPersistence(t *testing.T) {
	directories, binding := recoveryClaimFixture(t)
	claim, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	if ready, err := claim.ServiceReleaseReady(); err != nil || ready {
		t.Fatal("fresh claim already ready:", err)
	}
	if err := claim.RecordServiceReleaseReady(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directories[2], serviceReleaseReadyName)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := claim.RecordServiceReleaseReady(); err != nil {
			t.Fatal("idempotent publication failed:", err)
		}
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("receipt replaced on retry:", err)
	}
	if ready, err := claim.ServiceReleaseReady(); err != nil || !ready {
		t.Fatal("published receipt missing:", err)
	}
	if bootstrap, err := claim.PrepareRecovery(nil); !errors.Is(err, ErrDenied) || bootstrap != nil {
		t.Fatal("completed cleanup admitted another helper:", err)
	}
	claim.approval.storage.failed = true
	if err := claim.RecordServiceReleaseReady(); err != nil {
		t.Fatal("uncertain publication could not be synchronised:", err)
	}
	if err := claim.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := claim.ServiceReleaseReady(); !errors.Is(err, ErrClosed) {
		t.Fatal("closed claim exposed readiness:", err)
	}
	if err := claim.RecordServiceReleaseReady(); !errors.Is(err, ErrClosed) {
		t.Fatal("closed claim published readiness:", err)
	}
	reopened, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if ready, err := reopened.ServiceReleaseReady(); err != nil || !ready {
		t.Fatal("reopened claim lost readiness:", err)
	}
}

func TestServiceReleaseReadyRejectsUnsafeRecords(t *testing.T) {
	for _, mode := range []string{"truncated", "oversized", "magic", "digest", "binding", "mode", "symlink", "hardlink", "fifo", "directory", "unexpected", "missing-approval"} {
		t.Run(mode, func(t *testing.T) {
			directories, binding := recoveryClaimFixture(t)
			claim, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
			if err != nil {
				t.Fatal(err)
			}
			if err := claim.RecordServiceReleaseReady(); err != nil {
				t.Fatal(err)
			}
			if err := claim.Close(); err != nil {
				t.Fatal(err)
			}
			corruptReleaseReady(t, directories[2], mode)
			reopened, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
			if reopened != nil {
				_ = reopened.Close()
			}
			if err == nil || reopened != nil {
				t.Fatal("unsafe readiness accepted:", err)
			}
			for _, directory := range directories {
				var storage LinuxRecoveryJournal
				if err := storage.open(directory); err != nil {
					t.Fatal("failed claim retained lease:", err)
				}
				if err := storage.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func corruptReleaseReady(t *testing.T, directory, mode string) {
	t.Helper()
	path := filepath.Join(directory, serviceReleaseReadyName)
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	switch mode {
	case "truncated":
		encoded = encoded[:len(encoded)-1]
	case "oversized":
		encoded = append(encoded, 0)
	case "magic":
		encoded[0] ^= 1
	case "digest":
		encoded[len(serviceReleaseReadyMagic)] ^= 1
	case "binding":
		encoded[len(encoded)-1] ^= 1
	case "mode":
		err = os.Chmod(path, 0644)
	case "symlink", "fifo", "directory":
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		switch mode {
		case "symlink":
			err = os.Symlink(recoveryApprovalName, path)
		case "fifo":
			err = unix.Mkfifo(path, 0600)
		case "directory":
			err = os.Mkdir(path, 0700)
		}
	case "hardlink":
		err = os.Link(path, filepath.Join(t.TempDir(), "alias"))
	case "unexpected":
		err = os.WriteFile(filepath.Join(directory, "unexpected"), []byte("bad"), 0600)
	case "missing-approval":
		err = os.Remove(filepath.Join(directory, recoveryApprovalName))
	}
	if err != nil {
		t.Fatal(err)
	}
	if mode == "truncated" || mode == "oversized" || mode == "magic" || mode == "digest" || mode == "binding" {
		if err := os.WriteFile(path, encoded, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestServiceReleaseReadyRejectsChangedApproval(t *testing.T) {
	directories, binding := recoveryClaimFixture(t)
	claim, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	changed := claim.digest
	changed[0] ^= 1
	encoded, err := encodeRecoveryApproval(changed, binding)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directories[2], recoveryApprovalName), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if err := claim.RecordServiceReleaseReady(); err == nil {
		t.Fatal("changed independent approval accepted")
	}
	if _, err := os.Stat(filepath.Join(directories[2], serviceReleaseReadyName)); !os.IsNotExist(err) {
		t.Fatal("changed approval published a receipt:", err)
	}
}

func TestServiceReleaseReadyRecordFraming(t *testing.T) {
	directories, binding := recoveryClaimFixture(t)
	claim, err := OpenLinuxRecoveryClaim(directories[0], directories[1], directories[2], binding)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	encoded, err := encodeServiceReleaseReady(claim.digest, binding)
	if err != nil || len(encoded) != recoveryApprovalBytes || len(serviceReleaseReadyMagic) != recoveryApprovalHeaderBytes {
		t.Fatal("invalid fixed-size framing:", err)
	}
	approval, err := encodeRecoveryApproval(claim.digest, binding)
	if err != nil || bytes.Equal(encoded, approval) {
		t.Fatal("readiness aliases approval:", err)
	}
	var absent *LinuxRecoveryClaim
	if err := absent.RecordServiceReleaseReady(); !errors.Is(err, ErrClosed) {
		t.Fatal("missing owner published receipt:", err)
	}
	if _, err := absent.ServiceReleaseReady(); !errors.Is(err, ErrClosed) {
		t.Fatal("missing owner read receipt:", err)
	}
}
