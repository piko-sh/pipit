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
	"strings"
	"testing"

	"pipit.sh/pipit/internal/sandboxwire"
)

const recoveryWritePayload = `{"operation":"fs.write","root":"data","path":"file","data":"bmV3"}`

func TestRecoveryAuthorityRetainsOriginalRoots(t *testing.T) {
	directory := t.TempDir()
	grants := []LinuxRootGrant{{Name: "data", Path: directory, Rights: Read | Write}}
	bootstrap, err := PrepareLinuxBootstrap(grants, FilesystemLimits{WriteBytes: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	owner, err := bootstrap.RecoveryAuthority()
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	grants[0].Name, grants[0].Path, grants[0].Rights = "other", t.TempDir(), List
	if err := bootstrap.Close(); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(t.TempDir(), "original")
	if err := os.Rename(directory, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	record := testRecoveryAuthorityRecord(t, owner, 1)
	journal := testRecoveryAuthorityJournal(t, owner)
	call, err := owner.admit(brokerMessage(1, recoveryWritePayload))
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.record(journal, call, record); err != nil {
		t.Fatal(err)
	}
	if err := call.Finish(3); err != nil {
		t.Fatal(err)
	}
	records, err := journal.Records()
	if err != nil || len(records) != 1 || records[0] != record {
		t.Fatal("authorised record not retained:", records, err)
	}
	if _, err := owner.admit(brokerMessage(2, recoveryWritePayload)); !errors.Is(err, errLimit) {
		t.Fatal("sealed write quota changed:", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.admit(brokerMessage(3, recoveryWritePayload)); !errors.Is(err, ErrClosed) {
		t.Fatal("closed authority admitted operation:", err)
	}
	if err := owner.record(journal, call, record); !errors.Is(err, ErrClosed) {
		t.Fatal("closed authority recorded intent:", err)
	}
}

func TestRecoveryAuthorityRejectsMismatchedMetadata(t *testing.T) {
	owner := testRecoveryAuthority(t, Read|Write)
	record := testRecoveryAuthorityRecord(t, owner, 1)
	journal := testRecoveryAuthorityJournal(t, owner)
	call, err := owner.admit(brokerMessage(1, recoveryWritePayload))
	if err != nil {
		t.Fatal(err)
	}
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
		if err := owner.record(journal, call, changed); !errors.Is(err, ErrDenied) {
			t.Fatal("mismatched metadata recorded:", changed, err)
		}
	}
	records, err := journal.Records()
	if err != nil || len(records) != 0 {
		t.Fatal("invalid metadata reached journal:", records, err)
	}
	if err := owner.record(journal, call, record); err != nil {
		t.Fatal("valid reservation lost after rejection:", err)
	}
	if err := owner.record(journal, call, record); err == nil {
		t.Fatal("duplicate intent recorded")
	}
}

func TestRecoveryAuthorityRejectsForeignAndFinishedCalls(t *testing.T) {
	owner := testRecoveryAuthority(t, Read|Write)
	record := testRecoveryAuthorityRecord(t, owner, 1)
	journal := testRecoveryAuthorityJournal(t, owner)
	foreign := testBudget(t, FilesystemLimits{})
	call, err := foreign.Admit(brokerMessage(1, recoveryWritePayload))
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.record(journal, call, record); !errors.Is(err, ErrDenied) {
		t.Fatal("foreign reservation admitted:", err)
	}
	if err := call.Finish(0); err != nil {
		t.Fatal(err)
	}
	call, err = owner.admit(brokerMessage(1, readPayload))
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.record(journal, call, record); !errors.Is(err, ErrDenied) {
		t.Fatal("read reservation used for write intent:", err)
	}
	if err := call.Finish(0); err != nil {
		t.Fatal(err)
	}
	call, err = owner.admit(brokerMessage(2, recoveryWritePayload))
	if err != nil {
		t.Fatal(err)
	}
	record.Operation = 2
	if err := call.Finish(0); err != nil {
		t.Fatal(err)
	}
	if err := owner.record(journal, call, record); !errors.Is(err, ErrDenied) {
		t.Fatal("finished reservation recorded:", err)
	}
	records, err := journal.Records()
	if err != nil || len(records) != 0 {
		t.Fatal("invalid reservation reached storage:", records, err)
	}
}

func TestRecoveryAuthorityReadOnlyAndClosedBudget(t *testing.T) {
	readOnly := testRecoveryAuthority(t, Read)
	if _, err := readOnly.admit(brokerMessage(1, recoveryWritePayload)); !errors.Is(err, ErrDenied) {
		t.Fatal("read grant widened:", err)
	}
	owner := testRecoveryAuthority(t, Write)
	record := testRecoveryAuthorityRecord(t, owner, 1)
	journal := testRecoveryAuthorityJournal(t, owner)
	call, err := owner.admit(brokerMessage(1, recoveryWritePayload))
	if err != nil {
		t.Fatal(err)
	}
	defer call.Finish(0)
	if _, err := owner.admit(brokerMessage(1, recoveryWritePayload)); !errors.Is(err, sandboxwire.ErrProtocol) {
		t.Fatal("replayed request accepted:", err)
	}
	if err := owner.record(journal, call, record); !errors.Is(err, ErrDenied) {
		t.Fatal("terminal admission ledger recorded intent:", err)
	}
}

func TestRecoveryAuthorityRejectsSubstitutedBootstrapRoot(t *testing.T) {
	bootstrap := testLinuxBootstrap(t, t.TempDir())
	replacement, err := openLinuxDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	original := bootstrap.files[1]
	bootstrap.files[1] = replacement
	if err := original.Close(); err != nil {
		t.Fatal(err)
	}
	if owner, err := bootstrap.RecoveryAuthority(); err == nil || owner != nil {
		t.Fatal("substituted root admitted")
	}
	if err := bootstrap.Close(); err != nil {
		t.Fatal(err)
	}
	if owner, err := bootstrap.RecoveryAuthority(); !errors.Is(err, ErrClosed) || owner != nil {
		t.Fatal("closed bootstrap admitted")
	}
	var missing *LinuxBootstrap
	if owner, err := missing.RecoveryAuthority(); !errors.Is(err, ErrClosed) || owner != nil {
		t.Fatal("nil bootstrap admitted")
	}
}

func TestRecoveryAuthorityRejectsWrongJournalAndEmptyOwners(t *testing.T) {
	owner := testRecoveryAuthority(t, Write)
	record := testRecoveryAuthorityRecord(t, owner, 1)
	call, err := owner.admit(brokerMessage(1, recoveryWritePayload))
	if err != nil {
		t.Fatal(err)
	}
	defer call.Finish(0)
	journal, err := OpenLinuxRecoveryJournal(testRecoveryDirectory(t), strings.Repeat("b", 32))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if err := owner.record(journal, call, record); err == nil {
		t.Fatal("wrong journal namespace accepted")
	}
	if err := owner.record(nil, call, record); !errors.Is(err, ErrClosed) {
		t.Fatal("nil journal accepted:", err)
	}
	var empty LinuxRecoveryAuthority
	for _, invalid := range []*LinuxRecoveryAuthority{nil, &empty} {
		if _, err := invalid.admit(brokerMessage(1, recoveryWritePayload)); !errors.Is(err, ErrClosed) {
			t.Fatal("empty authority admitted operation:", err)
		}
		if err := invalid.record(journal, call, record); !errors.Is(err, ErrClosed) {
			t.Fatal("empty authority recorded intent:", err)
		}
		if err := invalid.Close(); err != nil {
			t.Fatal(err)
		}
	}
	root := owner.roots["data"].file
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("closed authority retained root descriptor:", err)
	}
}

func testRecoveryAuthority(t *testing.T, rights Rights) *LinuxRecoveryAuthority {
	t.Helper()
	bootstrap, err := PrepareLinuxBootstrap([]LinuxRootGrant{{Name: "data", Path: t.TempDir(), Rights: rights}}, FilesystemLimits{})
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	owner, err := bootstrap.RecoveryAuthority()
	if err != nil {
		t.Fatal(err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	return journal
}
