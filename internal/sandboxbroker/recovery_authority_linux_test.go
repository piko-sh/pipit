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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/sandboxwire"
)

const recoveryWritePayload = `{"operation":"fs.write","root":"data","path":"file","data":"bmV3"}`

func TestRecoveryAuthorityRetainsOriginalRoots(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	grants := []LinuxRootGrant{{Name: "data", Path: directory, Rights: Read | Write}}
	bootstrap, err := PrepareLinuxBootstrap(grants, FilesystemLimits{WriteBytes: 3})
	require.NoError(t, err)
	defer bootstrap.Close()
	owner, err := bootstrap.RecoveryAuthority()
	require.NoError(t, err)
	defer owner.Close()
	grants[0].Name, grants[0].Path, grants[0].Rights = "other", t.TempDir(), List
	require.NoError(t, bootstrap.Close())
	moved := filepath.Join(t.TempDir(), "original")
	require.NoError(t, os.Rename(directory, moved))
	require.NoError(t, os.Mkdir(directory, 0700))
	record := testRecoveryAuthorityRecord(t, owner, 1)
	journal := testRecoveryAuthorityJournal(t, owner)
	call, err := owner.admit(brokerMessage(1, recoveryWritePayload))
	require.NoError(t, err)
	require.NoError(t, owner.record(journal, call, record))
	require.NoError(t, call.Finish(3))
	records, err := journal.Records()
	require.NoError(t, err, "authorised record not retained")
	require.Len(t, records, 1)
	require.Equal(t, record, records[0])
	_, err = owner.admit(brokerMessage(2, recoveryWritePayload))
	require.ErrorIs(t, err, errLimit, "sealed write quota changed")
	require.NoError(t, owner.Close())
	require.NoError(t, owner.Close())
	_, err = owner.admit(brokerMessage(3, recoveryWritePayload))
	require.ErrorIs(t, err, ErrClosed, "closed authority admitted operation")
	require.ErrorIs(t, owner.record(journal, call, record), ErrClosed, "closed authority recorded intent")
}

func TestRecoveryAuthorityRejectsMismatchedMetadata(t *testing.T) {
	t.Parallel()
	owner := testRecoveryAuthority(t, Read|Write)
	record := testRecoveryAuthorityRecord(t, owner, 1)
	journal := testRecoveryAuthorityJournal(t, owner)
	call, err := owner.admit(brokerMessage(1, recoveryWritePayload))
	require.NoError(t, err)
	defer call.Finish(0)
	mutations := []func(*RecoveryRecord){
		func(record *RecoveryRecord) { record.Namespace = strings.Repeat("f", 32) },
		func(record *RecoveryRecord) { record.Operation++ },
		func(record *RecoveryRecord) { record.Root = "other" },
		func(record *RecoveryRecord) { record.Path = "other" },
		func(record *RecoveryRecord) { record.Size++ },
		func(record *RecoveryRecord) { record.RootInode++ },
		func(record *RecoveryRecord) { record.RootMountID++ },
		func(record *RecoveryRecord) { record.RootDevice++; record.ParentDevice++; record.Device++ },
	}
	for _, mutate := range mutations {
		changed := record
		mutate(&changed)
		require.ErrorIs(t, owner.record(journal, call, changed), ErrDenied, "mismatched metadata recorded: %v", changed)
	}
	records, err := journal.Records()
	require.NoError(t, err, "invalid metadata reached journal")
	require.Empty(t, records)
	require.NoError(t, owner.record(journal, call, record), "a valid reservation survives a rejection")
	require.Error(t, owner.record(journal, call, record), "duplicate intent recorded")
}

func TestRecoveryAuthorityRejectsForeignAndFinishedCalls(t *testing.T) {
	t.Parallel()
	owner := testRecoveryAuthority(t, Read|Write)
	record := testRecoveryAuthorityRecord(t, owner, 1)
	journal := testRecoveryAuthorityJournal(t, owner)
	foreign := testBudget(t, FilesystemLimits{})
	call, err := foreign.Admit(brokerMessage(1, recoveryWritePayload))
	require.NoError(t, err)
	require.ErrorIs(t, owner.record(journal, call, record), ErrDenied, "foreign reservation admitted")
	require.NoError(t, call.Finish(0))
	call, err = owner.admit(brokerMessage(1, readPayload))
	require.NoError(t, err)
	require.ErrorIs(t, owner.record(journal, call, record), ErrDenied, "read reservation used for write intent")
	require.NoError(t, call.Finish(0))
	call, err = owner.admit(brokerMessage(2, recoveryWritePayload))
	require.NoError(t, err)
	record.Operation = 2
	require.NoError(t, call.Finish(0))
	require.ErrorIs(t, owner.record(journal, call, record), ErrDenied, "finished reservation recorded")
	records, err := journal.Records()
	require.NoError(t, err, "invalid reservation reached storage")
	require.Empty(t, records)
}

func TestRecoveryAuthorityReadOnlyAndClosedBudget(t *testing.T) {
	t.Parallel()
	readOnly := testRecoveryAuthority(t, Read)
	_, err := readOnly.admit(brokerMessage(1, recoveryWritePayload))
	require.ErrorIs(t, err, ErrDenied, "read grant widened")
	owner := testRecoveryAuthority(t, Write)
	record := testRecoveryAuthorityRecord(t, owner, 1)
	journal := testRecoveryAuthorityJournal(t, owner)
	call, err := owner.admit(brokerMessage(1, recoveryWritePayload))
	require.NoError(t, err)
	defer call.Finish(0)
	_, err = owner.admit(brokerMessage(1, recoveryWritePayload))
	require.ErrorIs(t, err, sandboxwire.ErrProtocol, "replayed request accepted")
	require.ErrorIs(t, owner.record(journal, call, record), ErrDenied, "terminal admission ledger recorded intent")
}

func TestRecoveryAuthorityRejectsSubstitutedBootstrapRoot(t *testing.T) {
	t.Parallel()
	bootstrap := testLinuxBootstrap(t, t.TempDir())
	replacement, err := openLinuxDirectory(t.TempDir())
	require.NoError(t, err)
	original := bootstrap.files[1]
	bootstrap.files[1] = replacement
	require.NoError(t, original.Close())
	owner, err := bootstrap.RecoveryAuthority()
	require.Error(t, err, "substituted root admitted")
	require.Nil(t, owner)
	require.NoError(t, bootstrap.Close())
	owner, err = bootstrap.RecoveryAuthority()
	require.ErrorIs(t, err, ErrClosed, "closed bootstrap admitted")
	require.Nil(t, owner)
	var missing *LinuxBootstrap
	owner, err = missing.RecoveryAuthority()
	require.ErrorIs(t, err, ErrClosed, "nil bootstrap admitted")
	require.Nil(t, owner)
}

func TestRecoveryAuthorityRejectsWrongJournalAndEmptyOwners(t *testing.T) {
	t.Parallel()
	owner := testRecoveryAuthority(t, Write)
	record := testRecoveryAuthorityRecord(t, owner, 1)
	call, err := owner.admit(brokerMessage(1, recoveryWritePayload))
	require.NoError(t, err)
	defer call.Finish(0)
	journal, err := OpenLinuxRecoveryJournal(testRecoveryDirectory(t), strings.Repeat("b", 32))
	require.NoError(t, err)
	defer journal.Close()
	require.Error(t, owner.record(journal, call, record), "wrong journal namespace accepted")
	require.ErrorIs(t, owner.record(nil, call, record), ErrClosed, "nil journal accepted")
	var empty LinuxRecoveryAuthority
	for _, invalid := range []*LinuxRecoveryAuthority{nil, &empty} {
		_, err := invalid.admit(brokerMessage(1, recoveryWritePayload))
		require.ErrorIs(t, err, ErrClosed, "empty authority admitted operation")
		require.ErrorIs(t, invalid.record(journal, call, record), ErrClosed, "empty authority recorded intent")
		require.NoError(t, invalid.Close())
	}
	root := owner.roots["data"].file
	require.NoError(t, owner.Close())
	_, err = root.Stat()
	require.ErrorIs(t, err, os.ErrClosed, "closed authority retained root descriptor")
}

func testRecoveryAuthority(t *testing.T, rights Rights) *LinuxRecoveryAuthority {
	t.Helper()
	bootstrap, err := PrepareLinuxBootstrap([]LinuxRootGrant{{Name: "data", Path: t.TempDir(), Rights: rights}}, FilesystemLimits{})
	require.NoError(t, err)
	defer bootstrap.Close()
	owner, err := bootstrap.RecoveryAuthority()
	require.NoError(t, err)
	t.Cleanup(func() { _ = owner.Close() })
	return owner
}

func testRecoveryAuthorityRecord(t *testing.T, owner *LinuxRecoveryAuthority, operation uint64) RecoveryRecord {
	t.Helper()
	root := owner.roots["data"]
	prepared, err := prepareLinuxWrite(root.file, "file", []byte("new"), operation)
	if prepared != nil {
		t.Cleanup(func() { _ = prepared.Close() })
	}
	require.NoError(t, err)
	return RecoveryRecord{
		Profile: recoveryRecordProfile, Namespace: owner.namespace, Root: "data", Path: "file", Operation: operation,
		RootDevice: root.identity.Device, RootInode: root.identity.Inode, RootMountID: root.identity.MountID,
		ParentDevice: prepared.identity.ParentDevice, ParentInode: prepared.identity.ParentInode,
		Device: prepared.identity.Device, Inode: prepared.identity.Inode, Size: prepared.identity.Size,
	}
}

func testRecoveryAuthorityJournal(t *testing.T, owner *LinuxRecoveryAuthority) *LinuxRecoveryJournal {
	t.Helper()
	journal, err := OpenLinuxRecoveryJournal(testRecoveryDirectory(t), owner.namespace)
	require.NoError(t, err)
	t.Cleanup(func() { _ = journal.Close() })
	return journal
}
