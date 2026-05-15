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
	"sync"

	"pipit.sh/pipit/internal/sandboxwire"
)

// LinuxRecoveryAuthority retains the exact roots and budget of one sealed bootstrap.
// Close only after all publication and supervised recovery work has finished.
type LinuxRecoveryAuthority struct {
	// roots maps each named root to its retained handle and identity.
	roots map[string]linuxRecoveryRoot

	// budget is the original sealed admission ledger.
	budget *FilesystemBudget

	// namespace is the sealed staging namespace.
	namespace string

	// mutex guards all mutable authority state.
	mutex sync.Mutex

	// closed is true after Close releases all handles.
	closed bool
}

// Close releases independent root pins and permanently closes admission.
//
// Returns joined descriptor errors; existing reservations still need Finish.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryAuthority) Close() error {
	if owner == nil {
		return nil
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed {
		return nil
	}
	owner.closed = true
	owner.budget.Close()
	var result error
	for _, root := range owner.roots {
		result = errors.Join(result, root.file.Close())
	}
	return result
}

// admit reserves an operation against the original sealed grants and quotas.
//
// Takes message (sandboxwire.Message) which is an ordered filesystem call envelope from
// the host client.
//
// Returns a reservation that must be finished exactly once, including on failure.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryAuthority) admit(message sandboxwire.Message) (*FilesystemCall, error) {
	if owner == nil {
		return nil, ErrClosed
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed {
		return nil, ErrClosed
	}
	return owner.budget.Admit(message)
}

// record binds prepared metadata to a live write reservation before journalling. It
// checks namespace, operation, root, path, exact write size and pinned root identity.
//
// Takes journal (*LinuxRecoveryJournal) which is the private host intent storage.
// Takes call (*FilesystemCall) which is a reservation from this owner.
// Takes record (RecoveryRecord) which is prepared metadata from the broker.
//
// Returns success only after a matching intent is durably stored. It does not acknowledge
// the broker or grant permission to remove a staging entry.
//
// Safe for concurrent use by multiple goroutines.
func (owner *LinuxRecoveryAuthority) record(journal *LinuxRecoveryJournal, call *FilesystemCall, record RecoveryRecord) error {
	if owner == nil {
		return ErrClosed
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed || owner.budget == nil {
		return ErrClosed
	}
	owner.budget.mutex.Lock()
	defer owner.budget.mutex.Unlock()
	if !owner.matches(call, record) {
		return ErrDenied
	}
	return journal.appendRecord(record)
}

// matches validates a live reservation while the owner and budget locks are held.
//
// Takes call (*FilesystemCall) which is the immutable host reservation.
// Takes record (RecoveryRecord) which is untrusted prepared metadata.
//
// Returns false without interpreting metadata as new authority.
func (owner *LinuxRecoveryAuthority) matches(call *FilesystemCall, record RecoveryRecord) bool {
	if call == nil || call.budget != owner.budget || call.finished == nil || owner.budget.closed {
		return false
	}
	if *call.finished || call.Operation() != WriteFile || call.identity != record.Operation ||
		call.root() != record.Root || call.path() != record.Path || int64(call.limit()) != record.Size ||
		record.Namespace != owner.namespace || !validRecoveryRecord(record) {
		return false
	}
	root, exists := owner.roots[record.Root]
	return exists && root.identity.Rights&Write != 0 &&
		record.RootDevice == root.identity.Device && record.RootInode == root.identity.Inode &&
		record.RootMountID == root.identity.MountID
}

// linuxRecoveryRoot holds one retained grant handle and its kernel identity.
type linuxRecoveryRoot struct {
	// file is the independently duplicated root descriptor.
	file *os.File

	// identity holds the kernel identity from the sealed bootstrap.
	identity filesystemBootstrapRoot
}

// RecoveryAuthority copies sealed authority and retains independent root handles. It
// survives closure of the bootstrap owner.
//
// Returns a new bounded host admission ledger, or an error after partial cleanup.
//
// Safe for concurrent use by multiple goroutines.
func (bootstrap *LinuxBootstrap) RecoveryAuthority() (*LinuxRecoveryAuthority, error) {
	if bootstrap == nil {
		return nil, ErrClosed
	}
	bootstrap.mutex.Lock()
	defer bootstrap.mutex.Unlock()
	if bootstrap.closed || len(bootstrap.files) == 0 {
		return nil, ErrClosed
	}
	policy, err := readLinuxBootstrap(bootstrap.files[0])
	if err != nil {
		return nil, err
	}
	if policy.Profile != filesystemBootstrapProfile || policy.Namespace != bootstrap.namespace ||
		len(policy.Roots) != len(bootstrap.files)-1 {
		return nil, ErrInvalidPolicy
	}
	grants := make([]RootGrant, len(policy.Roots))
	for index, root := range policy.Roots {
		grants[index] = RootGrant{Name: root.Name, Rights: root.Rights}
	}
	budget, err := NewFilesystemBudget(grants, *policy.Limits)
	if err != nil {
		return nil, err
	}
	owner := &LinuxRecoveryAuthority{
		roots: make(map[string]linuxRecoveryRoot, len(policy.Roots)), budget: budget,
		namespace: policy.Namespace, mutex: sync.Mutex{}, closed: false,
	}
	for index, approved := range policy.Roots {
		root, err := duplicateLinuxBootstrapFile(bootstrap.files[index+1])
		if err != nil {
			return nil, errors.Join(err, owner.Close())
		}
		owner.roots[approved.Name] = linuxRecoveryRoot{file: root, identity: approved}
		identity, err := linuxBootstrapRootIdentity(root)
		if err != nil {
			return nil, errors.Join(err, owner.Close())
		}
		identity.Name, identity.Rights = approved.Name, approved.Rights
		if identity != approved {
			return nil, errors.Join(ErrDenied, owner.Close())
		}
	}
	return owner, nil
}
