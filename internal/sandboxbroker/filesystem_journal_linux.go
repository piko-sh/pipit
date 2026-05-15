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

	"pipit.sh/pipit/internal/sandboxwire"
)

// writeJournalled prepares an anonymous inode and waits for durable host consent. The
// existing operation deadline covers preparation, acknowledgement and commit.
//
// Takes call (*FilesystemCall) which is the admitted write reservation.
// Takes connection (*filesystemConnection) which is the fixed private protocol
// connection.
//
// Returns no publication before an exact, correlated acknowledgement is received.
func (backend *LinuxFilesystem) writeJournalled(call *FilesystemCall, connection *filesystemConnection) (written int, result error) {
	prepared, err := prepareLinuxWrite(backend.roots[call.root()], call.path(), call.request.data, call.identity)
	if prepared != nil {
		written = prepared.written
		defer func() { result = errors.Join(result, prepared.Close()) }()
	}
	if err != nil {
		return written, err
	}
	root := backend.identities[call.root()]
	record := RecoveryRecord{
		Profile: recoveryRecordProfile, Namespace: backend.namespace, Root: call.root(), Path: call.path(), Operation: call.identity,
		RootDevice: root.Device, RootInode: root.Inode, RootMountID: root.MountID,
		ParentDevice: prepared.identity.ParentDevice, ParentInode: prepared.identity.ParentInode,
		Device: prepared.identity.Device, Inode: prepared.identity.Inode, Size: prepared.identity.Size,
	}
	if !validRecoveryRecord(record) {
		return written, ErrInvalidPolicy
	}
	if err := connection.send(sandboxwire.Call, call.identity, record); err != nil {
		return written, err
	}
	acknowledgement, err := connection.receive()
	if err != nil {
		return written, err
	}
	if acknowledgement.Kind != sandboxwire.Reply || acknowledgement.ID != call.identity {
		return written, sandboxwire.ErrProtocol
	}
	if err := acknowledgement.DecodePayload(&struct{}{}); err != nil {
		return written, err
	}
	return written, prepared.commit(backend)
}
