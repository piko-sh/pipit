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
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"pipit.sh/pipit/internal/sandboxbroker"
)

func TestServiceRecoveryIdentityRejectsUnownedGroup(t *testing.T) {
	for _, group := range []*Group{nil, {}, {closed: true}, {closing: true}, {started: true}, {parentOnly: true}} {
		if data, err := group.RecoveryIdentity(); !errors.Is(err, errClosed) || data != nil {
			t.Fatal("unowned group produced recovery identity:", err)
		}
	}
}

func TestNativeServiceRecoveryIdentity(t *testing.T) {
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires explicitly delegated cgroup-v2 parent")
	}
	group, err := newServiceGroup(parent, Limits{}, testTenant)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer group.closeGroup(ctx)
	encoded, err := group.RecoveryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	again, err := group.RecoveryIdentity()
	if err != nil || !bytes.Equal(encoded, again) {
		t.Fatal("unstable original service identity:", err)
	}
	var identity serviceRecoveryIdentity
	if err := json.Unmarshal(encoded, &identity); err != nil {
		t.Fatal(err)
	}
	if identity.PID != os.Getpid() || identity.Host[1] == 0 || identity.Group.Inode == 0 || identity.Parent.Inode == 0 ||
		identity.Group == identity.Parent || identity.Profile != "linux-service-recovery-v1" {
		t.Fatal("incomplete service identity:", identity)
	}
	assertLiveServiceRecoveryHost(t, encoded)
	child, err := group.newChild(Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer child.closeGroup(ctx)
	if data, err := group.RecoveryIdentity(); !errors.Is(err, errClosed) || data != nil {
		t.Fatal("captured service policy after child admission:", err)
	}
}

func assertLiveServiceRecoveryHost(t *testing.T, metadata []byte) {
	t.Helper()
	storage := checkpointedOwnerStorage(t)
	bootstrap, err := sandboxbroker.PrepareLinuxBootstrap(nil, sandboxbroker.FilesystemLimits{})
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	authority, err := bootstrap.RecoveryAuthority()
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	journal, err := sandboxbroker.OpenLinuxRecoveryJournal(storage.JournalDirectory, bootstrap.StagingNamespace())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	state := &filesystemBrokerRecovery{base: context.Background(), authority: authority, journal: journal}
	owner := &FilesystemBroker{recovery: state}
	defer owner.Close()
	storage.Binding, err = RecoveryHostBinding(context.Background(), [sha256.Size]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	storage.ServiceContext = metadata
	if err := state.persistCheckpoint(storage); err != nil {
		t.Fatal(err)
	}
	approved, err := state.approval.Load(storage.Binding)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := state.checkpoint.Load(approved, storage.Binding)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := sandboxbroker.RecoveryCheckpointContext(checkpoint, approved, storage.Binding)
	if err != nil || !bytes.Equal(captured, metadata) {
		t.Fatal("approved checkpoint lost original service context:", err)
	}
	if err := verifyServiceRecoveryHost(context.Background(), checkpoint, approved, storage.Binding); !errors.Is(err, ErrServiceBusy) {
		t.Fatal("approved live host accepted for restart recovery:", err)
	}
	approved[0] ^= 1
	if err := verifyServiceRecoveryHost(context.Background(), checkpoint, approved, storage.Binding); !errors.Is(err, sandboxbroker.ErrDenied) {
		t.Fatal("host was inspected without independent approval:", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	claim, err := ClaimServiceRecovery(context.Background(), storage)
	if claim != nil {
		_ = claim.Close()
	}
	if !errors.Is(err, ErrServiceBusy) || claim != nil {
		t.Fatal("live host accepted after its metadata leases were released:", err)
	}
	snapshot, err := sandboxbroker.OpenLinuxRecoveryClaim(storage.JournalDirectory, storage.CheckpointDirectory, storage.ApprovalDirectory, storage.Binding)
	if err != nil {
		t.Fatal("failed native claim retained metadata leases:", err)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
}
