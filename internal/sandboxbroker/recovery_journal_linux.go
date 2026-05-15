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
	"slices"
	"strconv"
	"sync"

	"golang.org/x/sys/unix"
)

// recoveryDirectoryMode is the required permission mask for private journal directories.
const recoveryDirectoryMode = 0700

// LinuxRecoveryJournal owns a private, locked directory of durable write intents. Its
// directory must be on trusted local storage, outside every script grant.
type LinuxRecoveryJournal struct {
	// directory is the locked private storage descriptor.
	directory *os.File

	// proc is the pinned /proc/self/fd directory for linking.
	proc *os.File

	// namespace is the sealed broker namespace for this journal.
	namespace string

	// records holds the loaded and appended recovery intents.
	records []RecoveryRecord

	// mutex guards all mutable journal state.
	mutex sync.Mutex

	// failed is true after an uncertain storage error.
	failed bool

	// closed is true after Close releases the directory lock.
	closed bool
}

// Records returns a detached snapshot without interpreting it as recovery authority.
//
// Returns an error after closure or an uncertain storage failure.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryJournal) Records() ([]RecoveryRecord, error) {
	if owner == nil {
		return nil, ErrClosed
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed || owner.failed {
		return nil, ErrClosed
	}
	return slices.Clone(owner.records), nil
}

// Close releases the directory lock and owned descriptors without deleting records. The
// owner must not be closed until publication and recovery users have stopped.
//
// Returns joined descriptor errors; repeated closure is harmless.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryJournal) Close() error {
	if owner == nil {
		return nil
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed {
		return nil
	}
	owner.closed = true
	var result error
	if owner.proc != nil {
		result = owner.proc.Close()
	}
	if owner.directory != nil {
		result = errors.Join(result, unix.Flock(int(owner.directory.Fd()), unix.LOCK_UN), owner.directory.Close())
	}
	return result
}

// appendRecord persists a new intent before the caller may acknowledge publication.
// Records are never overwritten, removed or refunded, and IDs must increase.
//
// Takes record (RecoveryRecord) which is validated metadata already checked against the
// original host write grant.
//
// Returns success only after both the file and its directory are synchronised.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryJournal) appendRecord(record RecoveryRecord) error {
	if owner == nil {
		return ErrClosed
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed || owner.failed {
		return ErrClosed
	}
	if record.Namespace != owner.namespace || len(owner.records) >= defaultCalls {
		return ErrInvalidPolicy
	}
	if len(owner.records) != 0 && record.Operation <= owner.records[len(owner.records)-1].Operation {
		return ErrInvalidPolicy
	}
	encoded, err := encodeRecoveryRecord(record)
	if err != nil {
		return err
	}
	if err := owner.persist(record, encoded); err != nil {
		owner.failed = true
		return err
	}
	owner.records = append(owner.records, record)
	return nil
}

// open pins the private directory, acquires a non-blocking lease and pins procfs.
//
// Takes name (string) which is an absolute host-approved directory path.
//
// Returns error without modifying directory permissions or replacing entries.
func (owner *LinuxRecoveryJournal) open(name string) error {
	pinned, err := openRecoveryJournalDirectory(name)
	if err != nil {
		return err
	}
	owner.directory, err = openLinuxBeneath(pinned, ".", unix.O_RDONLY|unix.O_DIRECTORY)
	if err := errors.Join(err, pinned.Close()); err != nil {
		return err
	}
	if err := checkJournalDirectory(owner.directory); err != nil {
		return err
	}
	if err := unix.Flock(int(owner.directory.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return err
	}
	owner.proc, err = openLinuxDirectory("/proc/" + strconv.Itoa(os.Getpid()) + "/fd")
	if err != nil {
		return err
	}
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(int(owner.proc.Fd()), &filesystem); err != nil {
		return err
	}
	if filesystem.Type != unix.PROC_SUPER_MAGIC {
		return ErrDenied
	}
	return nil
}

// persist links only a fully written anonymous inode under its unique record name.
//
// Takes record (RecoveryRecord) which is a valid record.
// Takes encoded ([]byte) which is its bounded encoded representation.
//
// Returns error without deleting a colliding name or rolling back a linked intent.
func (owner *LinuxRecoveryJournal) persist(record RecoveryRecord, encoded []byte) (result error) {
	name, err := recoveryRecordName(record)
	if err != nil {
		return err
	}
	return owner.persistEntry(name, encoded)
}

// persistEntry publishes complete private metadata without replacing an existing inode.
//
// Takes name (string) which is an internally selected basename.
// Takes encoded ([]byte) which contains bounded validated encoded bytes.
//
// Returns success only after both the anonymous file and containing directory sync.
func (owner *LinuxRecoveryJournal) persistEntry(name string, encoded []byte) (result error) {
	temporary, err := createLinuxStagingFile(owner.directory)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, temporary.Close()) }()
	if err := temporary.Chmod(stagingFileMode); err != nil {
		return err
	}
	if _, err := temporary.Write(encoded); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := unix.Linkat(int(owner.proc.Fd()), strconv.FormatUint(uint64(temporary.Fd()), 10),
		int(owner.directory.Fd()), name, unix.AT_SYMLINK_FOLLOW); err != nil {
		return err
	}
	return owner.directory.Sync()
}

// OpenLinuxRecoveryJournal validates and locks an existing private directory. Unsupported
// anonymous files or locking fail closed without a weaker fallback.
//
// Takes name (string) which is a host-approved absolute path.
// Takes namespace (string) which is the original sealed broker namespace.
//
// Returns validated existing intents and exclusive ownership, or an error. No journal
// content is used to select host grant paths or delete script files.
func OpenLinuxRecoveryJournal(name, namespace string) (*LinuxRecoveryJournal, error) {
	if !filepath.IsAbs(name) || !validStagingNamespace(namespace) {
		return nil, ErrInvalidPolicy
	}
	owner := &LinuxRecoveryJournal{
		directory: nil, proc: nil, namespace: namespace, records: nil,
		mutex: sync.Mutex{}, failed: false, closed: false,
	}
	if err := owner.open(name); err != nil {
		return nil, errors.Join(err, owner.Close())
	}
	if err := owner.load(); err != nil {
		return nil, errors.Join(err, owner.Close())
	}
	return owner, nil
}

// checkJournalDirectory requires private ownership of the pinned storage directory.
//
// Takes directory (*os.File) which is a descriptor already opened without following
// symlinks.
//
// Returns error for changed ownership, type, permission or special mode bits.
func checkJournalDirectory(directory *os.File) error {
	var stat unix.Stat_t
	if err := unix.Fstat(int(directory.Fd()), &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&stagingPermissionMask != recoveryDirectoryMode ||
		int(stat.Uid) != os.Geteuid() {
		return ErrDenied
	}
	return nil
}

// recoveryRecordName derives a unique basename from validated operation metadata.
//
// Takes record (RecoveryRecord) which is a record in the directory's original broker
// namespace.
//
// Returns a bounded basename rather than a script-supplied host path.
func recoveryRecordName(record RecoveryRecord) (string, error) {
	name, err := StagingName(record.Namespace, record.Operation)
	if err != nil {
		return "", err
	}
	return name + ".json", nil
}
