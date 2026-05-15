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

package sandboxhost

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"pipit.sh/pipit/internal/logging"
)

const (
	// launchRecordCheckpoint is the checkpoint store directory of a launch record.
	launchRecordCheckpoint = "checkpoint"

	// launchRecordApproval is the approval store directory of a launch record.
	launchRecordApproval = "approval"

	// launchRecordImages is the image store directory of a launch record.
	launchRecordImages = "images"

	// launchRecordJournal is the broker journal directory of a filesystem launch record.
	launchRecordJournal = "journal"

	// launchRecordMode is the mode of every directory a launch record creates.
	launchRecordMode = 0o700

	// tenantSlugLength is the number of hexadecimal digest characters naming a tenant.
	tenantSlugLength = 24

	// maximumLaunchRecords bounds the records one recovery enumerates for a tenant.
	maximumLaunchRecords = 64

	// sharedPermissionBits are the group and other permission bits a private directory must
	// not carry.
	sharedPermissionBits = 0o077
)

var (
	// errLaunchRecords reports a tenant directory holding more records than one recovery
	// walks.
	errLaunchRecords = errors.New("isolated launch records exceed the recovery bound")

	// errStateDirectory reports a state directory that is missing, not a directory, or
	// readable by others.
	errStateDirectory = errors.New("isolated state directory must be a private directory")
)

// launchRecord is the private directory one launch owns under StateDirectory/<tenant
// slug>/: its checkpoint and approval stores, its image store and, for a filesystem
// launch, its broker journal. A host that dies leaves the record behind for recovery; a
// clean close removes it.
type launchRecord struct {
	// directory is the record's absolute path.
	directory string

	// journal is true when the record carries a broker journal (a filesystem launch).
	journal bool
}

// checkpointDirectory returns the record's checkpoint store directory.
//
// Returns string which is an absolute path.
func (record launchRecord) checkpointDirectory() string {
	return filepath.Join(record.directory, launchRecordCheckpoint)
}

// approvalDirectory returns the record's approval store directory.
//
// Returns string which is an absolute path.
func (record launchRecord) approvalDirectory() string {
	return filepath.Join(record.directory, launchRecordApproval)
}

// imagesDirectory returns the record's image store directory.
//
// Returns string which is an absolute path.
func (record launchRecord) imagesDirectory() string {
	return filepath.Join(record.directory, launchRecordImages)
}

// journalDirectory returns the record's broker journal directory, or "" for a record
// without one.
//
// Returns string which is an absolute path or empty.
func (record launchRecord) journalDirectory() string {
	if !record.journal {
		return ""
	}
	return filepath.Join(record.directory, launchRecordJournal)
}

// storeMetadata lists the directories the record's image store is bound to, in the order
// the recovery claim reproduces.
//
// Returns []string which names the journal (when present), checkpoint and approval
// directories.
func (record launchRecord) storeMetadata() []string {
	if record.journal {
		return []string{record.journalDirectory(), record.checkpointDirectory(), record.approvalDirectory()}
	}
	return []string{record.checkpointDirectory(), record.approvalDirectory()}
}

// remove deletes the record and everything it created.
//
// Returns error when the directory cannot be removed.
func (record launchRecord) remove() error {
	return os.RemoveAll(record.directory)
}

// checkPrivateDirectory requires an existing directory with no group or other permission
// bits, the shape the state directory must have before anything is recorded under it.
//
// Takes path (string) which is absolute.
//
// Returns error wrapping ErrInvalidIsolatedConfig when the directory does not qualify.
func checkPrivateDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return errors.Join(ErrInvalidIsolatedConfig, errStateDirectory, err)
	}
	if !info.IsDir() || info.Mode().Perm()&sharedPermissionBits != 0 {
		return errors.Join(ErrInvalidIsolatedConfig, errStateDirectory)
	}
	return nil
}

// launchRecordError maps a record failure onto the public sentinels: an unusable state
// directory is a configuration error, anything else means the host cannot isolate.
//
// Takes err (error) which may be nil.
//
// Returns error wrapping the matching sentinel.
func launchRecordError(err error) error {
	if err == nil || errors.Is(err, ErrInvalidIsolatedConfig) {
		return err
	}
	return errors.Join(ErrIsolatedUnavailable, err)
}

// isolatedLoggerContext attaches the host's logger to ctx for one isolated API call. A
// host that configured no logger gets a discarding one, because logging.LoggerFrom()
// falls back to the process default and that would switch audit logging on for a security
// boundary the host never asked to watch.
//
// Takes logger (*slog.Logger) which the host chose, and which may be nil.
//
// Returns context.Context carrying a logger that is never nil.
func isolatedLoggerContext(ctx context.Context, logger *slog.Logger) context.Context {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return logging.ContextWithLogger(ctx, logger)
}

// tenantSlug names a tenant's directory under the state directory: the first characters
// of the tenant's SHA-256 digest, so any tenant string yields a short valid name.
//
// Takes tenant (string) which passed the configuration's tenant validation.
//
// Returns string which is the directory name.
func tenantSlug(tenant string) string {
	digest := sha256.Sum256([]byte(tenant))
	return hex.EncodeToString(digest[:])[:tenantSlugLength]
}

// createLaunchRecord makes a fresh record for a launch under the tenant's directory,
// creating the state and tenant directories privately when they do not exist yet.
//
// Takes stateDirectory (string) which is the host's absolute private state directory.
// Takes tenant (string) which selects the tenant directory.
// Takes journal (bool) which adds the broker journal directory for a filesystem launch.
//
// Returns the record, or an error with nothing left behind.
func createLaunchRecord(stateDirectory, tenant string, journal bool) (launchRecord, error) {
	if err := checkPrivateDirectory(stateDirectory); err != nil {
		return launchRecord{}, err
	}
	tenantDirectory := filepath.Join(stateDirectory, tenantSlug(tenant))
	if err := os.MkdirAll(tenantDirectory, launchRecordMode); err != nil {
		return launchRecord{}, err
	}
	record := launchRecord{directory: filepath.Join(tenantDirectory, rand.Text()), journal: journal}
	if err := os.Mkdir(record.directory, launchRecordMode); err != nil {
		return launchRecord{}, err
	}
	children := []string{record.checkpointDirectory(), record.approvalDirectory(), record.imagesDirectory()}
	if journal {
		children = append(children, record.journalDirectory())
	}
	for _, child := range children {
		if err := os.Mkdir(child, launchRecordMode); err != nil {
			return launchRecord{}, errors.Join(err, record.remove())
		}
	}
	return record, nil
}

// listLaunchRecords enumerates a tenant's records in name order.
//
// Takes stateDirectory (string) which is the host's state directory.
// Takes tenant (string) which selects the tenant directory; a missing directory yields no
// records.
//
// Returns the records, or errLaunchRecords when more than maximumLaunchRecords exist.
func listLaunchRecords(stateDirectory, tenant string) ([]launchRecord, error) {
	if err := checkPrivateDirectory(stateDirectory); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(stateDirectory, tenantSlug(tenant)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	records := make([]launchRecord, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(stateDirectory, tenantSlug(tenant), entry.Name())
		_, journalErr := os.Stat(filepath.Join(directory, launchRecordJournal))
		records = append(records, launchRecord{directory: directory, journal: journalErr == nil})
	}
	if len(records) > maximumLaunchRecords {
		return nil, errLaunchRecords
	}
	slices.SortFunc(records, func(a, b launchRecord) int { return strings.Compare(a.directory, b.directory) })
	return records, nil
}
