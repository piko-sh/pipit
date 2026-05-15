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

//go:build linux && (amd64 || arm64)

package sandboxlinux

import (
	"encoding/binary"
	"errors"
	"math"
	"os"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/safeconv"
)

// watchdogAuthority holds adopted host observation and cgroup termination handles after
// handoff validation.
type watchdogAuthority struct {
	// host holds the observed host process descriptor.
	host *os.File

	// kill holds the cgroup termination control handle.
	kill *os.File

	// deadline holds the absolute boot-time termination deadline.
	deadline int64
}

// Close releases adopted observation and termination authority.
//
// Returns joined close errors without retaining reusable descriptor numbers.
func (authority *watchdogAuthority) Close() error {
	if authority == nil {
		return nil
	}
	var result error
	if authority.host != nil {
		result = errors.Join(result, authority.host.Close())
		authority.host = nil
	}
	if authority.kill != nil {
		result = errors.Join(result, authority.kill.Close())
		authority.kill = nil
	}
	return result
}

// adoptWatchdogHandoff validates immutable policy against independently owned copies.
//
// Takes files ([]*os.File) which holds the borrowed descriptors ordered as policy, host
// pidfd, cgroup directory and kill.
//
// Returns only host observation and exact cgroup.kill authority, without a directory.
func adoptWatchdogHandoff(files []*os.File) (authority *watchdogAuthority, result error) {
	if len(files) != watchdogHandoffFiles {
		return nil, ErrInvalidLimits
	}
	copied := &watchdogHandoff{files: make([]*os.File, len(files))}
	defer func() {
		result = errors.Join(result, copied.Close())
		if result != nil && authority != nil {
			result = errors.Join(result, authority.Close())
			authority = nil
		}
	}()
	for index, file := range files {
		owned, err := duplicateWatchdogFile(file)
		if err != nil {
			return nil, err
		}
		copied.files[index] = owned
	}
	policy, err := readWatchdogPolicy(copied.files[0])
	if err != nil {
		return nil, err
	}
	if err := validateWatchdogIdentities(policy, copied.files[1:]); err != nil {
		return nil, err
	}
	if err := validateWatchdogHost(copied.files[1]); err != nil {
		return nil, err
	}
	if err := validateWatchdogKillBinding(copied.files[2], copied.files[watchdogKillIndex]); err != nil {
		return nil, err
	}
	deadline := binary.BigEndian.Uint64(policy[watchdogWordBytes:])
	now, err := watchdogBootTime()
	if err != nil {
		return nil, err
	}
	if deadline > math.MaxInt64 || deadline <= safeconv.Int64ToUint64(now) ||
		deadline-safeconv.Int64ToUint64(now) > uint64(maximumHostWatchdogLifetime) {
		return nil, ErrInvalidLimits
	}
	authority = &watchdogAuthority{host: copied.files[1], kill: copied.files[watchdogKillIndex], deadline: safeconv.Uint64ToInt64(deadline)}
	copied.files[1], copied.files[watchdogKillIndex] = nil, nil
	return authority, nil
}

// validateWatchdogIdentities compares ordered descriptors with sealed kernel identities.
//
// Takes policy ([watchdogPolicyBytes]byte) which is the complete immutable policy.
// Takes files ([]*os.File) which holds the borrowed host, group and kill handles.
//
// Returns error for any missing, substituted or unreadable descriptor.
func validateWatchdogIdentities(policy [watchdogPolicyBytes]byte, files []*os.File) error {
	if len(files) != watchdogHandoffFiles-1 {
		return ErrInvalidLimits
	}
	for index, file := range files {
		identity, err := watchdogFileIdentity(file)
		if err != nil {
			return err
		}
		offset := watchdogIdentityOffset + index*2*watchdogWordBytes
		if identity != [2]uint64{binary.BigEndian.Uint64(policy[offset:]), binary.BigEndian.Uint64(policy[offset+watchdogWordBytes:])} {
			return ErrInvalidLimits
		}
	}
	return nil
}

// readWatchdogPolicy accepts only the fixed-size, non-executable sealed policy.
//
// Takes file (*os.File) which is the independently pinned policy descriptor.
//
// Returns complete bytes or an error before any authority is adopted.
func readWatchdogPolicy(file *os.File) ([watchdogPolicyBytes]byte, error) {
	var encoded [watchdogPolicyBytes]byte
	seals, err := unix.FcntlInt(file.Fd(), unix.F_GET_SEALS, 0)
	if err != nil || seals&watchdogRequiredSeals != watchdogRequiredSeals {
		return encoded, errors.Join(ErrInvalidLimits, err)
	}
	info, err := file.Stat()
	if err != nil {
		return encoded, err
	}
	if !info.Mode().IsRegular() || info.Size() != watchdogPolicyBytes || info.Mode().Perm()&watchdogExecutableBits != 0 {
		return encoded, ErrInvalidLimits
	}
	if _, err := file.ReadAt(encoded[:], 0); err != nil {
		return [watchdogPolicyBytes]byte{}, err
	}
	if string(encoded[:watchdogWordBytes]) != watchdogPolicyMagic {
		return [watchdogPolicyBytes]byte{}, ErrInvalidLimits
	}
	return encoded, nil
}

// validateWatchdogKillBinding proves that termination names the granted group's control.
//
// Takes directory (*os.File) which is the independently copied cgroup directory handle.
// Takes kill (*os.File) which is the independently copied kill handle whose policy
// identity matches.
//
// Returns error for substitution, foreign filesystems or any other cgroup control.
func validateWatchdogKillBinding(directory, kill *os.File) (result error) {
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(int(directory.Fd()), &filesystem); err != nil {
		return err
	}
	info, err := directory.Stat()
	if err != nil {
		return err
	}
	if filesystem.Type != unix.CGROUP2_SUPER_MAGIC || !info.IsDir() {
		return ErrUnavailable
	}
	if err := validateWatchdogKill(kill); err != nil {
		return err
	}
	expected, err := openControl(directory, "cgroup.kill", unix.O_WRONLY)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, expected.Close()) }()
	actualIdentity, err := watchdogFileIdentity(kill)
	if err != nil {
		return err
	}
	expectedIdentity, err := watchdogFileIdentity(expected)
	if err != nil {
		return err
	}
	if actualIdentity != expectedIdentity {
		return ErrInvalidLimits
	}
	return nil
}

// validateWatchdogHost checks a stable kernel process handle without signalling.
//
// Takes host (*os.File) which is the independently copied host pidfd.
//
// Returns error on older kernels or a descriptor of any other filesystem type.
func validateWatchdogHost(host *os.File) error {
	if host == nil {
		return ErrInvalidLimits
	}
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(int(host.Fd()), &filesystem); err != nil {
		return err
	}
	if filesystem.Type != unix.PID_FS_MAGIC {
		return ErrUnavailable
	}
	flags, err := unix.FcntlInt(host.Fd(), unix.F_GETFL, 0)
	if err != nil {
		return err
	}
	if flags&unix.O_PATH != 0 {
		return ErrInvalidLimits
	}
	return nil
}
