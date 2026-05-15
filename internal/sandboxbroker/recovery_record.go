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
	"bytes"
	"encoding/json"

	"pipit.sh/pipit/internal/sandboxwire"
)

const (
	// recoveryRecordProfile is the sealed schema version for recovery intent records.
	recoveryRecordProfile = "filesystem-recovery-v1"

	// maximumRecoveryRecordBytes is the upper bound on a single encoded record.
	maximumRecoveryRecordBytes = 32 << 10

	// recoveryRecordFields is the exact number of top-level JSON fields in a valid record.
	recoveryRecordFields = 13
)

// RecoveryRecord describes one prepared publication, not permission to remove it.
// Decoding a record never authorises filesystem access.
type RecoveryRecord struct {
	// Profile identifies the recovery record schema version.
	Profile string `json:"profile"`

	// Namespace is the original sealed staging namespace.
	Namespace string `json:"namespace"`

	// Root is the opaque Root identifier from the host grant.
	Root string `json:"root"`

	// Path is the portable relative path of the published file.
	Path string `json:"path"`

	// Operation is the admitted broker call identifier.
	Operation uint64 `json:"operation"`

	// RootDevice is the kernel device number of the grant root.
	RootDevice uint64 `json:"root_device"`

	// RootInode is the kernel inode number of the grant root.
	RootInode uint64 `json:"root_inode"`

	// RootMountID is the kernel mount identifier of the grant root.
	RootMountID uint64 `json:"root_mount_id"`

	// ParentDevice is the kernel device number of the parent.
	ParentDevice uint64 `json:"parent_device"`

	// ParentInode is the kernel inode number of the parent.
	ParentInode uint64 `json:"parent_inode"`

	// Device is the kernel Device number of the staged inode.
	Device uint64 `json:"device"`

	// Inode is the kernel Inode number of the staged Inode.
	Inode uint64 `json:"inode"`

	// Size is the byte length of the staged write payload.
	Size int64 `json:"size"`
}

// encodeRecoveryRecord validates a single bounded intent before serialisation.
//
// Takes record (RecoveryRecord) which is metadata captured before publication, with no
// host pathname.
//
// Returns encoded metadata only, without writing or synchronising a journal.
func encodeRecoveryRecord(record RecoveryRecord) ([]byte, error) {
	if !validRecoveryRecord(record) {
		return nil, ErrInvalidPolicy
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maximumRecoveryRecordBytes {
		return nil, ErrInvalidPolicy
	}
	return encoded, nil
}

// decodeRecoveryRecord rejects ambiguous, incomplete or oversized journal metadata.
//
// Takes encoded ([]byte) which is one record from a separately bounded and privately
// owned journal.
//
// Returns no partial record on corruption; valid metadata is not deletion authority.
func decodeRecoveryRecord(encoded []byte) (RecoveryRecord, error) {
	if len(encoded) == 0 || len(encoded) > maximumRecoveryRecordBytes {
		return RecoveryRecord{}, ErrInvalidPolicy
	}
	message := sandboxwire.Message{Kind: sandboxwire.Result, ID: 0, Payload: encoded}
	var record RecoveryRecord
	if err := message.DecodePayload(&record); err != nil {
		return RecoveryRecord{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return RecoveryRecord{}, err
	}
	if len(fields) != recoveryRecordFields {
		return RecoveryRecord{}, ErrInvalidPolicy
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return RecoveryRecord{}, ErrInvalidPolicy
		}
	}
	if !validRecoveryRecord(record) {
		return RecoveryRecord{}, ErrInvalidPolicy
	}
	return record, nil
}

// validRecoveryRecord checks representation and same-filesystem identity bounds.
//
// Takes record (RecoveryRecord) which is untrusted metadata without consulting or
// widening any host grant.
//
// Returns false for paths or identities that cannot describe a prepared write.
func validRecoveryRecord(record RecoveryRecord) bool {
	return record.Profile == recoveryRecordProfile && validStagingNamespace(record.Namespace) &&
		validRootName(record.Root) && validRelativePath(record.Path, false) &&
		record.Operation != 0 && record.RootInode != 0 && record.RootMountID != 0 &&
		record.ParentInode != 0 && record.Inode != 0 &&
		record.RootDevice == record.ParentDevice && record.ParentDevice == record.Device &&
		record.Size >= 0 && record.Size <= maximumFileBytes
}
