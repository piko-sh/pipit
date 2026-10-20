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
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pipit.sh/pipit/internal/sandboxbroker"
)

func serviceRecoveryBrokerConfig(t *testing.T, parent string) WorkerConfig {
	t.Helper()
	config := WorkerConfig{Tenant: testTenant,
		Executable:         os.Getenv("PIPIT_TEST_FILESYSTEM_BROKER_BINARY"),
		WatchdogExecutable: os.Getenv("PIPIT_TEST_HOST_WATCHDOG_BINARY"),
		CgroupParent:       parent, Lifetime: 5 * time.Second, OutputBytes: 64 << 10,
	}
	image, err := os.ReadFile(config.Executable)
	if err != nil {
		t.Fatal(err)
	}
	config.Digest = sha256.Sum256(image)
	image, err = os.ReadFile(config.WatchdogExecutable)
	if err != nil {
		t.Fatal(err)
	}
	config.WatchdogDigest = sha256.Sum256(image)
	return config
}

func persistOriginalFilesystemRecovery(t *testing.T, storage FilesystemRecoveryStorage, group *Group, config WorkerConfig) *FilesystemBroker {
	t.Helper()
	root := os.Getenv("PIPIT_TEST_RECOVERY_FILESYSTEM_ROOT")
	grants := []sandboxbroker.LinuxRootGrant{{Name: "data", Path: root, Rights: sandboxbroker.Read | sandboxbroker.Write}}
	owner, err := OpenCheckpointedFilesystemBrokerInGroup(context.Background(), config,
		grants, sandboxbroker.FilesystemLimits{}, storage, group)
	if err != nil {
		if owner != nil {
			_ = owner.Close()
		}
		t.Fatal(err)
	}
	result, err := owner.Execute(context.Background(), []byte(`{"operation":"fs.write","root":"data","path":"file","data":"bmV3"}`))
	if err != nil || result.Written != 3 {
		_ = owner.Close()
		t.Fatal("original journalled write failed:", err)
	}
	records, err := owner.recovery.journal.Records()
	if err != nil || len(records) != 1 {
		_ = owner.Close()
		t.Fatal("missing original recovery intent:", err)
	}
	name, err := sandboxbroker.StagingName(records[0].Namespace, records[0].Operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "file"), filepath.Join(root, name)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("published"), 0600); err != nil {
		t.Fatal(err)
	}
	return owner
}

func exerciseClaimedFilesystemRecovery(t *testing.T, ctx context.Context, claim *ServiceRecoveryClaim, storage FilesystemRecoveryStorage, parent, root, releaseStage string) {
	t.Helper()
	if err := claim.ResumeRelease(ctx, parent); !errors.Is(err, ErrServiceBusy) {
		t.Fatal("missing readiness accepted for final release:", err)
	}
	config := serviceRecoveryBrokerConfig(t, parent)
	grants := []sandboxbroker.LinuxRootGrant{{Name: "data", Path: root, Rights: sandboxbroker.Read | sandboxbroker.Write}}
	invalid := config
	invalid.Digest[0] ^= 1
	if err := claim.RecoverFilesystem(ctx, invalid, grants); err == nil || !claim.recovering || claim.recovered {
		t.Fatal("unapproved recovery image accepted:", err)
	}
	if err := claim.Close(); !errors.Is(err, ErrServiceBusy) {
		t.Fatal("failed recovery released metadata ownership:", err)
	}
	if claim.process != nil {
		if err := claim.Prune(ctx); !errors.Is(err, ErrServiceBusy) {
			t.Fatal("pruning raced a retained recovery process:", err)
		}
		if err := claim.Quiesce(ctx, parent); !errors.Is(err, ErrServiceBusy) {
			t.Fatal("quiescence raced a retained recovery process:", err)
		}
	}
	if err := claim.RecoverFilesystem(ctx, config, grants); err != nil {
		t.Fatal("claimed confined recovery failed:", err)
	}
	if !claim.recovered || claim.recovering || claim.pending || claim.process != nil {
		t.Fatal("successful recovery retained incomplete state")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "file" {
		t.Fatal("restart recovery left staging files:", entries, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "file"))
	if err != nil || string(data) != "published" {
		t.Fatal("restart recovery changed the published destination:", err)
	}
	if err := claim.group.verifyDirectoryEntry(); err != nil {
		t.Fatal("recovery discarded original service ownership:", err)
	}
	exerciseRecoveredServiceRelease(t, ctx, claim, storage, parent, releaseStage)
}

func exerciseRecoveredServiceRelease(t *testing.T, ctx context.Context, claim *ServiceRecoveryClaim, storage FilesystemRecoveryStorage, parent, releaseStage string) {
	t.Helper()
	original := claim.group
	collision := filepath.Join(storage.ApprovalDirectory, "unexpected")
	if err := os.WriteFile(collision, []byte("deny publication"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := claim.Release(ctx); err == nil || !claim.pending || claim.released {
		t.Fatal("unsafe receipt storage allowed release:", err)
	}
	if err := original.verifyDirectoryEntry(); err != nil {
		t.Fatal("failed receipt publication removed original reservation:", err)
	}
	if err := os.Remove(collision); err != nil {
		t.Fatal(err)
	}
	if err := original.kill.Close(); err != nil {
		t.Fatal(err)
	}
	if err := claim.Release(ctx); err == nil || !claim.pending || claim.released {
		t.Fatal("failed release discarded original ownership:", err)
	}
	if err := claim.Close(); !errors.Is(err, ErrServiceBusy) {
		t.Fatal("failed release allowed metadata closure:", err)
	}
	if ready, err := claim.snapshot.ServiceReleaseReady(); err != nil || !ready {
		t.Fatal("service removal attempted before durable readiness:", err)
	}
	if err := claim.Prune(ctx); err != nil || claim.pending {
		t.Fatal("empty service could not be pruned after readiness:", err)
	}
	if err := claim.RecoverFilesystem(ctx, WorkerConfig{Tenant: testTenant}, nil); !errors.Is(err, ErrServiceBusy) || !claim.recovered {
		t.Fatal("release readiness allowed another recovery attempt:", err)
	}
	var err error
	original.kill, err = openControl(original.directory, "cgroup.kill", os.O_WRONLY)
	if err != nil {
		t.Fatal(err)
	}
	if releaseStage != "normal" {
		claim = exerciseReleaseCrash(t, ctx, claim, storage, parent, releaseStage)
		defer claim.Close()
		err = claim.ResumeRelease(ctx, parent)
	} else {
		err = claim.Release(ctx)
	}
	if err != nil {
		t.Fatal("release retry failed:", err)
	}
	exerciseCompletedServiceRelease(t, ctx, claim, storage, parent)
}

func exerciseCompletedServiceRelease(t *testing.T, ctx context.Context, claim *ServiceRecoveryClaim, storage FilesystemRecoveryStorage, parent string) {
	t.Helper()
	if !claim.released || !claim.closed || claim.pending || claim.group != nil {
		t.Fatal("release retained incomplete ownership")
	}
	snapshot, err := sandboxbroker.OpenLinuxRecoveryClaim(storage.JournalDirectory, storage.CheckpointDirectory, storage.ApprovalDirectory, storage.Binding)
	if err != nil {
		t.Fatal("release retained metadata leases or removed records:", err)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	replacement, err := newServiceGroup(parent, Limits{}, testTenant)
	if err != nil {
		t.Fatal("original reservation remains:", err)
	}
	defer func() {
		if err := replacement.closeGroup(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	if err := claim.Release(ctx); err != nil {
		t.Fatal("repeated release failed:", err)
	}
	startRecoveryFixtureSleeper(t, replacement)
	if err := replacement.verifyDirectoryEntry(); err != nil {
		t.Fatal("repeated release changed replacement service:", err)
	}
	reopened, err := ClaimServiceRecovery(ctx, storage)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.ResumeRelease(ctx, parent); err != nil {
		t.Fatal("replacement service prevented completed recovery:", err)
	}
	if err := replacement.verifyDirectoryEntry(); err != nil {
		t.Fatal("resumed release changed replacement service:", err)
	}
	if err := recoveryDirectoryEmpty(replacement.directory); !errors.Is(err, ErrServiceBusy) {
		t.Fatal("resumed release signalled replacement tasks:", err)
	}
}
