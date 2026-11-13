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

package sandboxbroker

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

type checkpointRestoreFixture struct {
	encoded  []byte
	grants   []LinuxRootGrant
	journal  *LinuxRecoveryJournal
	record   RecoveryRecord
	binding  [sha256.Size]byte
	approved [sha256.Size]byte
}

func TestCheckpointRecoveryRejectsUnapprovedIntents(t *testing.T) {
	t.Parallel()
	fixture := prepareCheckpointRestoreFixture(t)
	var original recoveryCheckpoint
	require.NoError(t, json.Unmarshal(fixture.encoded, &original))
	for _, mode := range []string{"namespace", "root", "inode", "mount", "operation", "path", "bytes", "duplicate", "read-only", "call-budget"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			policy := original
			policy.Roots = append([]filesystemBootstrapRoot(nil), original.Roots...)
			record := fixture.record
			switch mode {
			case "namespace":
				record.Namespace = "ffffffffffffffffffffffffffffffff"
			case "root":
				record.Root = "foreign"
			case "inode":
				record.RootInode++
			case "mount":
				record.RootMountID++
			case "operation":
				record.Operation = 0
			case "path":
				record.Path = "../escape"
			case "bytes":
				record.Size = int64(policy.Limits.WriteBytes) + 1
			case "read-only":
				policy.Roots[0].Rights = Read
			case "call-budget":
				policy.Limits.Calls = 0
			}
			records := []RecoveryRecord{record}
			if mode == "duplicate" {
				records = append(records, record)
			}
			_, err := checkpointRecoveryPolicy(policy, records)
			require.Error(t, err, "unapproved intent became cleanup authority")
		})
	}
}

func TestCheckpointRecoveryChecksApprovalBeforeOpeningRoots(t *testing.T) {
	t.Parallel()
	fixture := prepareCheckpointRestoreFixture(t)
	fixture.approved[0] ^= 1
	fixture.grants[0].Path = filepath.Join(t.TempDir(), "missing", "root")
	bootstrap, err := PrepareCheckpointRecovery(fixture.encoded, fixture.approved, fixture.binding, fixture.journal, fixture.grants)
	if bootstrap != nil {
		_ = bootstrap.Close()
	}
	require.Nil(t, bootstrap, "host roots stay closed before independent approval validation")
	require.ErrorIs(t, err, ErrDenied)
}

func prepareCheckpointRestoreFixture(t *testing.T) checkpointRestoreFixture {
	t.Helper()
	grants := []LinuxRootGrant{{Name: "data", Path: testRecoveryDirectory(t), Rights: Read | Write | List}}
	original, err := PrepareLinuxBootstrap(grants, FilesystemLimits{WriteBytes: 3})
	require.NoError(t, err)
	authority, err := original.RecoveryAuthority()
	require.NoError(t, err)
	defer authority.Close()
	require.NoError(t, original.Close())
	directory := testRecoveryDirectory(t)
	journal, err := OpenLinuxRecoveryJournal(directory, authority.namespace)
	require.NoError(t, err)
	defer journal.Close()
	binding := [sha256.Size]byte{1}
	encoded, err := authority.checkpoint(journal, binding)
	require.NoError(t, err)
	record := testRecoveryAuthorityRecord(t, authority, 1)
	call, err := authority.admit(brokerMessage(1, recoveryWritePayload))
	require.NoError(t, err)
	require.NoError(t, authority.record(journal, call, record))
	require.NoError(t, call.Finish(0))
	require.NoError(t, authority.Close())
	require.NoError(t, journal.Close())
	reopened, err := OpenLinuxRecoveryJournal(directory, authority.namespace)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	return checkpointRestoreFixture{encoded: encoded, grants: grants, journal: reopened, record: record,
		binding: binding, approved: sha256.Sum256(encoded)}
}

func TestCheckpointRecoveryRestoresOnlyCleanup(t *testing.T) {
	t.Parallel()
	fixture := prepareCheckpointRestoreFixture(t)
	bootstrap, err := PrepareCheckpointRecovery(fixture.encoded, fixture.approved, fixture.binding, fixture.journal, fixture.grants)
	require.NoError(t, err)
	defer bootstrap.Close()
	require.True(t, bootstrap.RecoveryOnly(), "restored wrong role or namespace")
	require.Equal(t, fixture.record.Namespace, bootstrap.StagingNamespace())
	restored, err := bootstrap.RecoveryAuthority()
	if restored != nil {
		_ = restored.Close()
	}
	require.Nil(t, restored, "recovery does not restore normal execution authority")
	require.ErrorIs(t, err, ErrInvalidPolicy)
	files, err := bootstrap.Files()
	require.NoError(t, err)
	policy, err := readLinuxBootstrap(files[0])
	require.NoError(t, err)
	records, err := decodeBootstrapRecovery(policy)
	require.NoError(t, err, "restored wrong intents")
	require.Len(t, records, 1)
	require.Equal(t, fixture.record, records[0])
	require.Len(t, policy.Roots, 1, "restored authority widened: %v", policy)
	require.Equal(t, Write, policy.Roots[0].Rights)
	require.Equal(t, 3, policy.Limits.WriteBytes)
	require.NoError(t, fixture.journal.Close())
	_, err = linuxBootstrapRootIdentity(files[1])
	require.NoError(t, err, "restored bootstrap lost independent root pin")
}

func TestCheckpointRecoveryRejectsChangedAuthority(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"approval", "binding", "journal", "root", "rights", "name", "missing-root", "extra-root", "closed-journal", "empty-journal"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			fixture := prepareCheckpointRestoreFixture(t)
			switch mode {
			case "approval":
				fixture.approved[0] ^= 1
			case "binding":
				fixture.binding[0] ^= 1
			case "journal":
				other, err := OpenLinuxRecoveryJournal(testRecoveryDirectory(t), fixture.record.Namespace)
				require.NoError(t, err)
				defer other.Close()
				fixture.journal = other
			case "root":
				moved := filepath.Join(t.TempDir(), "original")
				require.NoError(t, os.Rename(fixture.grants[0].Path, moved))
				require.NoError(t, os.Mkdir(fixture.grants[0].Path, 0700))
			case "rights":
				fixture.grants[0].Rights = Read
			case "name":
				fixture.grants[0].Name = "other"
			case "missing-root":
				fixture.grants = nil
			case "extra-root":
				fixture.grants = append(fixture.grants, LinuxRootGrant{Name: "extra", Path: testRecoveryDirectory(t), Rights: Write})
			case "closed-journal":
				require.NoError(t, fixture.journal.Close())
			case "empty-journal":
				fixture.journal.records = nil
				name, err := recoveryRecordName(fixture.record)
				require.NoError(t, err)
				require.NoError(t, os.Remove(filepath.Join("/proc/self/fd", strconv.Itoa(int(fixture.journal.directory.Fd())), name)))
			}
			bootstrap, err := PrepareCheckpointRecovery(fixture.encoded, fixture.approved, fixture.binding, fixture.journal, fixture.grants)
			if bootstrap != nil {
				_ = bootstrap.Close()
			}
			require.Nil(t, bootstrap, "changed authority restored")
			require.Error(t, err)
		})
	}
}
