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
	"cmp"
	"errors"
	"io"
	"os"
	"slices"
	"strconv"

	"golang.org/x/sys/unix"
)

// RecoveryRecords re-reads durable intents through the original pinned directory. Failed
// owners stay terminal for writes and previously acknowledged records must remain
// present.
//
// Returns a fresh bounded snapshot or no records on validation or closure failure.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryJournal) RecoveryRecords() (records []RecoveryRecord, result error) {
	if owner == nil {
		return nil, ErrClosed
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed || owner.directory == nil || owner.proc == nil {
		return nil, ErrClosed
	}
	defer func() {
		if result != nil {
			owner.failed = true
			records = nil
		}
	}()
	directory, err := openLinuxBeneath(owner.directory, ".", unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return nil, err
	}
	defer func() { result = errors.Join(result, directory.Close()) }()
	if err := checkJournalDirectory(directory); err != nil {
		return nil, err
	}
	records, err = owner.readRecords(directory)
	if err != nil {
		return nil, err
	}
	if !containsJournalRecords(records, owner.records) {
		return nil, ErrDenied
	}
	return records, nil
}

// load validates the complete bounded journal before exposing any record. Unknown
// entries, aliases and corrupt records fail closed without deletion.
//
// Returns error if more than one broker lifetime's operation budget is stored.
func (owner *LinuxRecoveryJournal) load() error {
	records, err := owner.readRecords(owner.directory)
	if err != nil {
		return err
	}
	owner.records = records
	return nil
}

// readRecords validates a complete journal without modifying cached state.
//
// Takes directory (*os.File) which is a fresh directory cursor referring to the locked
// journal.
//
// Returns no partial records on malformed, excessive or mismatched entries.
func (owner *LinuxRecoveryJournal) readRecords(directory *os.File) ([]RecoveryRecord, error) {
	names, err := directory.Readdirnames(defaultCalls + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(names) > defaultCalls {
		return nil, errLimit
	}
	records := make([]RecoveryRecord, 0, len(names))
	for _, name := range names {
		record, err := owner.readRecord(name)
		if err != nil {
			return nil, err
		}
		expected, err := recoveryRecordName(record)
		if err != nil {
			return nil, err
		}
		if name != expected || record.Namespace != owner.namespace {
			return nil, ErrDenied
		}
		records = append(records, record)
	}
	slices.SortFunc(records, func(first, second RecoveryRecord) int {
		return cmp.Compare(first.Operation, second.Operation)
	})
	for index := 1; index < len(records); index++ {
		if records[index-1].Operation >= records[index].Operation {
			return nil, ErrDenied
		}
	}
	return records, nil
}

// readRecord inspects a path-only handle before reopening that exact regular inode.
// FIFOs, devices, links and oversized files are never opened for content access.
//
// Takes name (string) which is a directory entry from the locked, host-owned journal.
//
// Returns a validated record without exposing partially decoded metadata.
func (owner *LinuxRecoveryJournal) readRecord(name string) (RecoveryRecord, error) {
	encoded, err := owner.readEntry(name, maximumRecoveryRecordBytes)
	if err != nil {
		return RecoveryRecord{}, err
	}
	return decodeRecoveryRecord(encoded)
}

// readEntry pins and bounds an exact private regular inode before reading metadata.
//
// Takes name (string) which is an internally selected entry.
// Takes limit (int64) which is the maximum encoded length.
//
// Returns no partial bytes on unsafe metadata, oversized content or close failure.
func (owner *LinuxRecoveryJournal) readEntry(name string, limit int64) (encoded []byte, result error) {
	pinned, err := openLinuxBeneath(owner.directory, name, unix.O_PATH)
	if err != nil {
		return nil, err
	}
	defer func() {
		result = errors.Join(result, pinned.Close())
		if result != nil {
			encoded = nil
		}
	}()
	var stat unix.Stat_t
	if err := unix.Fstat(int(pinned.Fd()), &stat); err != nil {
		return nil, err
	}
	if !validRecoveryFile(stat) || stat.Size > limit {
		return nil, ErrDenied
	}
	descriptor, err := unix.Openat(int(owner.proc.Fd()), strconv.FormatUint(uint64(pinned.Fd()), 10),
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	source := os.NewFile(uintptr(descriptor), "recovery-record")
	defer func() {
		result = errors.Join(result, source.Close())
		if result != nil {
			encoded = nil
		}
	}()
	var opened unix.Stat_t
	if err := unix.Fstat(descriptor, &opened); err != nil {
		return nil, err
	}
	if !validRecoveryFile(opened) || opened.Dev != stat.Dev || opened.Ino != stat.Ino || opened.Size != stat.Size {
		return nil, ErrDenied
	}
	encoded, err = io.ReadAll(io.LimitReader(source, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(encoded)) != opened.Size {
		return nil, ErrDenied
	}
	return encoded, nil
}

// containsJournalRecords detects loss or alteration of previously accepted intents.
//
// Takes observed ([]RecoveryRecord) which is a fresh snapshot sorted by strictly
// increasing operation ID.
// Takes accepted ([]RecoveryRecord) which is the cached snapshot sorted by strictly
// increasing operation ID.
//
// Returns true only when every cached record is still present without any change.
func containsJournalRecords(observed, accepted []RecoveryRecord) bool {
	position := 0
	for index := range accepted {
		expected := accepted[index]
		for position < len(observed) && observed[position].Operation < expected.Operation {
			position++
		}
		if position == len(observed) || observed[position] != expected {
			return false
		}
		position++
	}
	return true
}

// validRecoveryFile checks private regular-file metadata before content access.
//
// Takes stat (unix.Stat_t) which is descriptor-derived stat information, not
// journal-supplied identity.
//
// Returns false for unsafe ownership, links, permissions, type or size.
func validRecoveryFile(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Mode&stagingPermissionMask == stagingFileMode &&
		int(stat.Uid) == os.Geteuid() && stat.Nlink == 1 &&
		stat.Size > 0 && stat.Size <= maximumRecoveryRecordBytes
}
