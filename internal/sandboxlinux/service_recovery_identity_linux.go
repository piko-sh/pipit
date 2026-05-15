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
	"encoding/json"
	"errors"
	"os"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxbroker"
)

const (
	// serviceRecoveryProfile is the profile name for service recovery without images.
	serviceRecoveryProfile = "linux-service-recovery-v1"

	// serviceRecoveryImageProfile is the profile name for service recovery with image store
	// binding.
	serviceRecoveryImageProfile = "linux-service-recovery-images-v1"
)

// serviceRecoveryIdentity holds the original service ownership metadata for recovery
// binding.
type serviceRecoveryIdentity struct {
	// Profile holds the versioned recovery profile name.
	Profile string

	// Name holds the service group basename.
	Name string

	// Parent holds the delegated parent cgroup identity.
	Parent cgroupDirectoryID

	// Group holds the service cgroup identity.
	Group cgroupDirectoryID

	// Host holds the original host pidfd identity pair.
	Host [2]uint64

	// PID holds the original host process identifier.
	PID int

	// Images holds the optional image store identity.
	Images sandboxbroker.LinuxImageStoreIdentity `json:"Images,omitzero"`
}

// RecoveryIdentity captures original service and host identity before child admission.
// Persist it inside independently approved metadata bound to the current boot.
//
// Returns bounded metadata without changing resource ownership or admitting children.
func (group *Group) RecoveryIdentity() (encoded []byte, result error) {
	var images sandboxbroker.LinuxImageStoreIdentity
	return group.recoveryIdentity(images)
}

// RecoveryIdentityWithImages also captures an exclusively owned empty image store. The
// caller must retain the store and publish approval before any image admission.
//
// Takes store (*sandboxbroker.LinuxImageStore) which is the original private storage.
//
// Returns []byte which is the encoded recovery identity with image-store binding.
// Returns error when identity capture or serialisation fails.
func (group *Group) RecoveryIdentityWithImages(store *sandboxbroker.LinuxImageStore) ([]byte, error) {
	if store == nil {
		return nil, ErrInvalidLimits
	}
	images, err := store.InitialIdentity()
	if err != nil {
		return nil, err
	}
	return group.recoveryIdentity(images)
}

// recoveryIdentity captures original kernel ownership while child admission is closed.
//
// Takes images (sandboxbroker.LinuxImageStoreIdentity) which is either empty or the
// identity of retained original private storage.
//
// Returns canonical bounded metadata for independent approval before launch.
//
// Safe for concurrent use by multiple goroutines.
func (group *Group) recoveryIdentity(images sandboxbroker.LinuxImageStoreIdentity) (encoded []byte, result error) {
	if group == nil {
		return nil, errClosed
	}
	group.mutex.Lock()
	defer group.mutex.Unlock()
	if group.closed || group.closing || group.started || group.parentOnly || !isServiceGroupName(group.name) {
		return nil, errClosed
	}
	if err := group.verifyDirectoryEntry(); err != nil {
		return nil, err
	}
	parent, err := cgroupDirectoryIdentity(group.parent)
	if err != nil {
		return nil, err
	}
	identity, err := cgroupDirectoryIdentity(group.directory)
	if err != nil {
		return nil, err
	}
	descriptor, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		return nil, err
	}
	host := os.NewFile(uintptr(descriptor), "recovery-host")
	defer func() {
		result = errors.Join(result, host.Close())
		if result != nil {
			encoded = nil
		}
	}()
	if err := validateWatchdogHost(host); err != nil {
		return nil, err
	}
	hostIdentity, err := watchdogFileIdentity(host)
	if err != nil || hostIdentity[1] == 0 {
		return nil, errors.Join(ErrUnavailable, err)
	}
	profile := serviceRecoveryProfile
	if images.Inode != 0 {
		profile = serviceRecoveryImageProfile
	}
	return json.Marshal(serviceRecoveryIdentity{
		Profile: profile, Name: group.name, Parent: parent, Group: identity, Host: hostIdentity, PID: os.Getpid(), Images: images,
	})
}
