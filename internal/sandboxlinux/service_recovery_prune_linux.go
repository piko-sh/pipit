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
	"context"
	"errors"
	"io"
	"os"
	"slices"

	"golang.org/x/sys/unix"
)

const (
	// maximumRecoveryCgroups is the upper bound on descendant cgroups in a prune plan.
	maximumRecoveryCgroups = 64

	// maximumRecoveryGroupDepth is the maximum directory nesting depth for pruning.
	maximumRecoveryGroupDepth = 16

	// maximumRecoveryDirectoryEntries is the entry count limit per directory during pruning.
	maximumRecoveryDirectoryEntries = 256
)

// recoveryPruneNode holds one pinned descendant directory in a prune plan.
type recoveryPruneNode struct {
	// parent holds the parent directory handle.
	parent *os.File

	// directory holds the pinned child directory handle.
	directory *os.File

	// name holds the child directory basename.
	name string

	// identity holds the child's verified cgroup identity.
	identity cgroupDirectoryID
}

// Prune removes bounded empty descendants without deleting the original service. A
// complete pinned plan is collected and rechecked before any removal begins.
//
// Takes a cleanup context after successful quiescence of this original service.
//
// Returns success without admitting execution or releasing the service reservation.
//
// Safe for concurrent use by multiple goroutines.
func (owner *ServiceRecoveryClaim) Prune(ctx context.Context) error {
	if owner == nil {
		return errClosed
	}
	if ctx == nil {
		return ErrInvalidLimits
	}
	ctx, cancel := context.WithTimeout(ctx, workerCleanupTimeout)
	defer cancel()
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	if owner.closed || owner.snapshot == nil {
		return errClosed
	}
	if owner.group == nil || !owner.quiesced || owner.process != nil {
		return ErrServiceBusy
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	owner.group.mutex.Lock()
	defer owner.group.mutex.Unlock()
	owner.pending = true
	if err := owner.group.verifyDirectoryEntry(); err != nil {
		return err
	}
	if err := recoveryDirectoryEmpty(owner.group.directory); err != nil {
		return err
	}
	if err := pruneRecoveryDescendants(ctx, owner.group.directory); err != nil {
		return err
	}
	if err := owner.group.verifyDirectoryEntry(); err != nil {
		return err
	}
	owner.pending = false
	return ctx.Err()
}

// pruneRecoveryDescendants pins a bounded tree before removing it from leaves upwards.
//
// Takes directory (*os.File) which is the verified empty original service directory.
//
// Returns closure or removal errors without deleting the service itself.
func pruneRecoveryDescendants(ctx context.Context, directory *os.File) (result error) {
	root, err := openDirectory(int(directory.Fd()), ".")
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, root.Close()) }()
	if _, err := cgroupDirectoryIdentity(root); err != nil {
		return err
	}
	var plan []recoveryPruneNode
	defer func() {
		for _, node := range slices.Backward(plan) {
			result = errors.Join(result, node.directory.Close())
		}
	}()
	if err := collectRecoveryPrunePlan(ctx, root, 0, &plan); err != nil {
		return err
	}
	for _, node := range slices.Backward(plan) {
		if err := removeRecoveryPruneNode(ctx, node); err != nil {
			return err
		}
	}
	return recoveryDirectoryEmpty(root)
}

// collectRecoveryPrunePlan bounds traversal, memory and retained kernel handles.
//
// Takes directory (*os.File) which is the fresh directory cursor.
// Takes depth (int) which is the bounded nesting depth.
// Takes plan (*[]recoveryPruneNode) which accumulates the pinned tree plan.
//
// Returns failure before removal if any entry or directory exceeds the allowed shape.
func collectRecoveryPrunePlan(ctx context.Context, directory *os.File, depth int, plan *[]recoveryPruneNode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parentIdentity, err := cgroupDirectoryIdentity(directory)
	if err != nil {
		return err
	}
	entries, err := directory.ReadDir(maximumRecoveryDirectoryEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(entries) > maximumRecoveryDirectoryEntries {
		return ErrUnavailable
	}
	for _, entry := range entries {
		if err := appendRecoveryPruneChild(ctx, directory, parentIdentity, entry.Name(), depth, plan); err != nil {
			return err
		}
	}
	return nil
}

// appendRecoveryPruneChild verifies one entry entirely relative to its pinned parent.
//
// Takes directory (*os.File) which is the parent directory handle.
// Takes parentIdentity (cgroupDirectoryID) which identifies the parent for mount-crossing
// rejection.
// Takes name (string) which is the child directory basename.
// Takes depth (int) which is the current nesting depth.
// Takes plan (*[]recoveryPruneNode) which accumulates descendants for closure.
//
// Returns with every opened descendant recorded for closure, including on failure.
func appendRecoveryPruneChild(ctx context.Context, directory *os.File, parentIdentity cgroupDirectoryID, name string, depth int, plan *[]recoveryPruneNode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var status unix.Stat_t
	if err := unix.Fstatat(int(directory.Fd()), name, &status, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if status.Mode&unix.S_IFMT == unix.S_IFREG {
		return nil
	}
	if status.Mode&unix.S_IFMT != unix.S_IFDIR || depth >= maximumRecoveryGroupDepth || len(*plan) >= maximumRecoveryCgroups {
		return ErrUnavailable
	}
	child, err := openDirectory(int(directory.Fd()), name)
	if err != nil {
		return err
	}
	identity, err := cgroupDirectoryIdentity(child)
	if err != nil {
		return errors.Join(err, child.Close())
	}
	if identity.Mount != parentIdentity.Mount || identity.Major != parentIdentity.Major || identity.Minor != parentIdentity.Minor {
		return errors.Join(ErrUnavailable, child.Close())
	}
	*plan = append(*plan, recoveryPruneNode{parent: directory, directory: child, name: name, identity: identity})
	return collectRecoveryPrunePlan(ctx, child, depth+1, plan)
}

// removeRecoveryPruneNode refuses replacement entries and populated groups.
//
// Takes node (recoveryPruneNode) which is the pinned original child with its retained
// parent descriptor.
//
// Returns failure without signalling tasks or modifying a different directory.
func removeRecoveryPruneNode(ctx context.Context, node recoveryPruneNode) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := openDirectory(int(node.parent.Fd()), node.name)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, current.Close()) }()
	identity, err := cgroupDirectoryIdentity(current)
	if err != nil {
		return err
	}
	if identity != node.identity {
		return ErrUnavailable
	}
	if err := recoveryDirectoryEmpty(node.directory); err != nil {
		return err
	}
	return unix.Unlinkat(int(node.parent.Fd()), node.name, unix.AT_REMOVEDIR)
}

// recoveryDirectoryEmpty requires the kernel's recursive empty-subtree proof.
//
// Takes directory (*os.File) which is the verified cgroup-v2 directory, never a pathname
// selected by a worker.
//
// Returns busy while any descendant task remains alive.
func recoveryDirectoryEmpty(directory *os.File) error {
	events, err := readControl(directory, "cgroup.events")
	if err != nil {
		return err
	}
	empty, err := parseEmpty(events)
	if err != nil {
		return err
	}
	if !empty {
		return ErrServiceBusy
	}
	return nil
}
