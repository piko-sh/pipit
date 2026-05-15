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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"pipit.sh/pipit/internal/sandboxbroker"
)

func TestServiceRecoveryImageIdentityProfile(t *testing.T) {
	original := serviceRecoveryIdentity{
		Profile: serviceRecoveryImageProfile, Name: serviceGroupNameFor(testTenant), PID: 123, Host: [2]uint64{1, 2},
		Parent: cgroupDirectoryID{Inode: 1, Mount: 1}, Group: cgroupDirectoryID{Inode: 2, Mount: 1},
		Images: sandboxbroker.LinuxImageStoreIdentity{Device: 3, Inode: 4, MountID: 5},
	}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := decodeServiceRecoveryIdentity(encoded); err != nil || decoded != original {
		t.Fatal("image-bound identity rejected:", err)
	}
	for _, mode := range []string{"downgrade", "missing-images", "missing-inode", "missing-mount"} {
		changed := original
		switch mode {
		case "downgrade":
			changed.Profile = serviceRecoveryProfile
		case "missing-images":
			changed.Images = sandboxbroker.LinuxImageStoreIdentity{}
		case "missing-inode":
			changed.Images.Inode = 0
		case "missing-mount":
			changed.Images.MountID = 0
		}
		data, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if decoded, err := decodeServiceRecoveryIdentity(data); err == nil || decoded != (serviceRecoveryIdentity{}) {
			t.Fatal("incomplete image approval accepted:", mode, err)
		}
	}
	original.Profile, original.Images = serviceRecoveryProfile, sandboxbroker.LinuxImageStoreIdentity{}
	legacy, err := json.Marshal(original)
	if err != nil || bytes.Contains(legacy, []byte("Images")) {
		t.Fatal("legacy canonical identity changed:", err)
	}
	if _, err := decodeServiceRecoveryIdentity(legacy); err != nil {
		t.Fatal("legacy identity rejected:", err)
	}
	if _, err := (&Group{}).RecoveryIdentityWithImages(nil); !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("missing explicit image store accepted:", err)
	}
}

func TestRecoveryImageStoreBinding(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := sandboxbroker.OpenLinuxImageStore(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity, err := store.InitialIdentity()
	if err != nil {
		t.Fatal(err)
	}
	owner := &ServiceRecoveryClaim{identity: serviceRecoveryIdentity{Images: identity}}
	if err := owner.checkRecoveryImageStore(nil, nil); !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("image-bound recovery fell back to temporary storage:", err)
	}
	if err := owner.checkRecoveryImageStore(store, nil); err != nil {
		t.Fatal("original store rejected:", err)
	}
	owner.identity.Images.Inode ^= 1
	if err := owner.checkRecoveryImageStore(store, nil); !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("different store accepted:", err)
	}
	owner.identity.Images = sandboxbroker.LinuxImageStoreIdentity{}
	if err := owner.checkRecoveryImageStore(store, nil); !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("unapproved store injected into legacy recovery:", err)
	}
}

func TestRecoveryImagesAdmission(t *testing.T) {
	ctx := context.Background()
	var missing *ServiceRecoveryClaim
	if err := missing.RecoverImages(ctx, "/unused", nil); !errors.Is(err, errClosed) {
		t.Fatal("missing owner accepted:", err)
	}
	owner := &ServiceRecoveryClaim{}
	if err := owner.RecoverImages(nil, "/unused", nil); !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("nil context accepted:", err)
	}
	if err := owner.RecoverImages(ctx, "relative", nil); !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("relative storage accepted:", err)
	}
	for _, mode := range []string{"unquiesced", "process", "recovering", "recovered", "release", "missing-group"} {
		owner := &ServiceRecoveryClaim{
			snapshot: &sandboxbroker.LinuxRecoveryClaim{}, group: &Group{}, quiesced: true,
		}
		switch mode {
		case "unquiesced":
			owner.quiesced = false
		case "process":
			owner.process = &WorkerProcess{}
		case "recovering":
			owner.recovering = true
		case "recovered":
			owner.recovered = true
		case "release":
			owner.releaseStarted = true
		case "missing-group":
			owner.group = nil
		}
		if err := owner.RecoverImages(ctx, "/unused", nil); !errors.Is(err, ErrServiceBusy) {
			t.Fatal("unsafe cleanup admission:", mode, err)
		}
	}
}

func TestRecoveryImageReleaseRetainsLease(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := sandboxbroker.OpenLinuxImageStore(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	owner := &ServiceRecoveryClaim{images: store}
	lease, _, err := store.AcquireDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.closeRecoveryImages(); err == nil || owner.releaseStarted {
		t.Fatal("release passed a borrowed image handle:", err)
	}
	if err := owner.Close(); err == nil || owner.closed {
		t.Fatal("close discarded image ownership:", err)
	}
	if err := store.ReleaseDirectory(lease); err != nil {
		t.Fatal(err)
	}
	if err := owner.closeRecoveryImages(); err != nil || !owner.releaseStarted {
		t.Fatal("drained images could not finish release:", err)
	}
	if err := owner.closeRecoveryImages(); err != nil {
		t.Fatal("image release retry failed:", err)
	}
	owner = &ServiceRecoveryClaim{identity: serviceRecoveryIdentity{Images: sandboxbroker.LinuxImageStoreIdentity{Inode: 1, MountID: 1}}}
	if err := owner.closeRecoveryImages(); !errors.Is(err, ErrServiceBusy) || owner.releaseStarted {
		t.Fatal("image-bound release omitted original storage:", err)
	}
}
