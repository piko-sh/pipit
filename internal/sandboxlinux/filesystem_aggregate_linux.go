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

import "context"

// filesystemAggregateAdmission bounds concurrent filesystem aggregate ownership per host
// process.
var filesystemAggregateAdmission admissionTable

// FilesystemAggregate retains admission until its complete subtree is removed. Close
// children through their owners before closing this aggregate.
type FilesystemAggregate struct {
	// Group holds the owned cgroup for this aggregate.
	Group *Group

	// releaseAdmission frees the process-wide admission slot.
	releaseAdmission func()
}

// NewFilesystemAggregate reserves capacity before creating native resources. One active
// aggregate and one queued constructor are allowed per host process.
//
// Takes parent (string) which is the delegated cgroup parent path.
// Takes limits (Limits) which select finite aggregate resource bounds.
// Takes tenant (string) which identifies the host tenant for admission.
//
// Returns retained ownership on partial failure. Close every non-nil aggregate. A shared
// cgroup slot also coordinates hosts using the same delegated parent.
func NewFilesystemAggregate(ctx context.Context, parent string, limits Limits, tenant string) (*FilesystemAggregate, error) {
	if ctx == nil {
		return nil, ErrInvalidLimits
	}
	if err := validateTenant(tenant); err != nil {
		return nil, err
	}
	release, err := filesystemAggregateAdmission.acquire(ctx, tenant)
	if err != nil {
		return nil, err
	}
	group, err := newServiceGroup(parent, limits, tenant)
	if group == nil {
		release()
		return nil, err
	}
	return &FilesystemAggregate{Group: group, releaseAdmission: release}, err
}

// Close removes the aggregate before releasing its lifecycle admission lease. Failed
// cleanup retains the lease, including when execution has already expired.
//
// Takes a fresh bounded cleanup context after all child owners have been closed.
//
// Returns an error while the aggregate still needs cleanup; retry the same owner.
func (owner *FilesystemAggregate) Close(ctx context.Context) error {
	if owner == nil {
		return nil
	}
	if err := owner.Group.closeGroup(ctx); err != nil {
		return err
	}
	owner.releaseAdmission()
	return nil
}
