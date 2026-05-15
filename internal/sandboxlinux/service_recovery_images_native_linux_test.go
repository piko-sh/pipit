//go:build linux && (amd64 || arm64) && !race

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
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pipit.sh/pipit/internal/sandboxbroker"
)

func TestNativeCheckpointImageStoreBinding(t *testing.T) {
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires explicitly delegated cgroup-v2 parent")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	group, err := newServiceGroup(parent, Limits{}, testTenant)
	if err != nil {
		t.Fatal(err)
	}
	defer group.closeGroup(ctx)
	storage := checkpointedOwnerStorage(t)
	storage.Binding, err = RecoveryHostBinding(ctx, [sha256.Size]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	paths := []string{storage.JournalDirectory, storage.CheckpointDirectory, storage.ApprovalDirectory}
	store, err := sandboxbroker.OpenLinuxImageStore(directory, nil, paths)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	metadata, err := group.RecoveryIdentityWithImages(store)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := store.InitialIdentity()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := decodeServiceRecoveryIdentity(metadata)
	if err != nil || identity.Images != expected || identity.Profile != serviceRecoveryImageProfile {
		t.Fatal("image-store identity was not captured:", err)
	}
	storage.ServiceContext = metadata
	config := serviceRecoveryBrokerConfig(t, parent)
	config.ImageStore = store
	rejectCheckpointImageChanges(t, ctx, group, config, storage, identity)
	owner, err := OpenCheckpointedFilesystemBrokerInGroup(ctx, config, nil, sandboxbroker.FilesystemLimits{}, storage, group)
	if owner != nil {
		defer owner.Close()
	}
	if err != nil {
		t.Fatal("approved image-bound broker could not start:", err)
	}
	if owner.process.image.store != store || owner.process.watchdog.image.store != store {
		t.Fatal("broker or watchdog did not use approved original image storage")
	}
	approved, err := owner.recovery.approval.Load(storage.Binding)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := owner.recovery.checkpoint.Load(approved, storage.Binding)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := sandboxbroker.RecoveryCheckpointContext(checkpoint, approved, storage.Binding)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeServiceRecoveryIdentity(captured)
	if err != nil || decoded != identity {
		t.Fatal("durable checkpoint lost original image-store identity:", err)
	}
	if _, err := group.RecoveryIdentityWithImages(store); err == nil {
		t.Fatal("image identity captured after child admission")
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal("broker cleanup retained image storage:", err)
	}
	exerciseQuiescedImageRecovery(t, ctx, group, storage, identity, directory, config)
}

func exerciseQuiescedImageRecovery(t *testing.T, ctx context.Context, group *Group, storage FilesystemRecoveryStorage, identity serviceRecoveryIdentity, directory string, config WorkerConfig) {
	t.Helper()
	snapshot, err := sandboxbroker.OpenLinuxRecoveryClaim(storage.JournalDirectory, storage.CheckpointDirectory, storage.ApprovalDirectory, storage.Binding)
	if err != nil {
		t.Fatal(err)
	}
	claim := &ServiceRecoveryClaim{
		snapshot: snapshot, group: group, identity: identity, quiesced: true,
		metadata: []string{storage.JournalDirectory, storage.CheckpointDirectory, storage.ApprovalDirectory},
	}
	defer claim.Close()
	orphan := filepath.Join(directory, "pipit-worker-AAAAAAAAAAAAAAAAAAAAAAAAAA")
	if err := os.Mkdir(orphan, 0700); err != nil {
		t.Fatal(err)
	}
	unexpected := filepath.Join(orphan, "unexpected")
	if err := os.WriteFile(unexpected, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := claim.RecoverImages(ctx, directory, nil); err == nil || !claim.pending || claim.images == nil {
		t.Fatal("unexpected orphan contents were not retained:", err)
	}
	if err := claim.Close(); !errors.Is(err, ErrServiceBusy) {
		t.Fatal("failed cleanup released its claim:", err)
	}
	if err := claim.RecoverImages(ctx, t.TempDir(), nil); !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("cleanup retry retargeted storage:", err)
	}
	if err := os.Remove(unexpected); err != nil {
		t.Fatal(err)
	}
	if err := claim.RecoverImages(ctx, directory, nil); err != nil {
		t.Fatal("orphan cleanup retry failed:", err)
	}
	if entries, err := os.ReadDir(directory); err != nil || len(entries) != 0 {
		t.Fatal("orphan image remained:", entries, err)
	}
	config.ImageStore = nil
	if err := claim.RecoverFilesystem(ctx, config, nil); err != nil {
		t.Fatal("recovery did not select retained storage:", err)
	}
	if err := claim.Release(ctx); err != nil {
		t.Fatal("image-bound release failed:", err)
	}
	if !claim.releaseStarted || !claim.released {
		t.Fatal("image-bound release did not finish")
	}
	if err := claim.RecoverImages(ctx, directory, nil); !errors.Is(err, errClosed) {
		t.Fatal("completed release admitted new image cleanup:", err)
	}
}

func rejectCheckpointImageChanges(t *testing.T, ctx context.Context, group *Group, config WorkerConfig, storage FilesystemRecoveryStorage, identity serviceRecoveryIdentity) {
	t.Helper()
	for _, mode := range []string{"missing-context", "changed-store", "missing-store", "legacy-context", "uncovered-grant"} {
		selected, metadata, changed := config, storage, identity
		var grants []sandboxbroker.LinuxRootGrant
		switch mode {
		case "missing-context":
			metadata.ServiceContext = nil
		case "changed-store", "legacy-context":
			if mode == "changed-store" {
				changed.Images.Inode ^= 1
			} else {
				changed.Profile, changed.Images = serviceRecoveryProfile, sandboxbroker.LinuxImageStoreIdentity{}
			}
			encoded, err := json.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			metadata.ServiceContext = encoded
		case "missing-store":
			selected.ImageStore = nil
		case "uncovered-grant":
			grants = []sandboxbroker.LinuxRootGrant{{Name: "other", Path: t.TempDir(), Rights: sandboxbroker.Read}}
		}
		owner, err := OpenCheckpointedFilesystemBrokerInGroup(ctx, selected, grants, sandboxbroker.FilesystemLimits{}, metadata, group)
		if owner != nil {
			_ = owner.Close()
		}
		if err == nil || owner != nil {
			t.Fatal("mismatched checkpoint image policy admitted:", mode, err)
		}
	}
	for _, directory := range []string{storage.JournalDirectory, storage.CheckpointDirectory, storage.ApprovalDirectory} {
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 0 {
			t.Fatal("rejected image policy published recovery metadata:", err)
		}
	}
}
