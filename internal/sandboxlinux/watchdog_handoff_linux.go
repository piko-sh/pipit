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
	"time"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/safeconv"
)

const (
	// watchdogPolicyMagic is the fixed header identifying a valid watchdog policy buffer.
	watchdogPolicyMagic = "PIPITWD1"

	// watchdogPolicyBytes is the total byte length of a sealed watchdog policy.
	watchdogPolicyBytes = 64

	// watchdogWordBytes is the byte width of one policy word.
	watchdogWordBytes = 8

	// watchdogIdentityOffset is the byte offset where descriptor identities begin in a
	// sealed policy.
	watchdogIdentityOffset = 16

	// watchdogHandoffFiles is the number of descriptors in a complete watchdog handoff.
	watchdogHandoffFiles = 4

	// watchdogKillIndex is the descriptor position of the cgroup kill handle within a
	// handoff.
	watchdogKillIndex = 3

	// watchdogExecutableBits is the permission mask rejecting executable policy descriptors.
	watchdogExecutableBits = 0o111

	// watchdogRequiredSeals is the combined seal set required on a valid watchdog policy
	// descriptor.
	watchdogRequiredSeals = unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_SEAL | unix.F_SEAL_EXEC
)

// watchdogHandoff holds owned descriptors prepared for a watchdog launch.
type watchdogHandoff struct {
	// files holds the ordered policy, host, group and kill descriptors.
	files []*os.File
}

// Close releases every owned handoff descriptor without changing cgroup authority.
//
// Returns joined closure errors and clears ownership for subsequent calls.
func (handoff *watchdogHandoff) Close() error {
	if handoff == nil {
		return nil
	}
	var result error
	for index, file := range handoff.files {
		if file != nil {
			result = errors.Join(result, file.Close())
			handoff.files[index] = nil
		}
	}
	return result
}

// prepareWatchdogHandoff captures current-host and group authority before launch.
//
// Takes group (*Group) which is the retained group.
// Takes lifetime (time.Duration) which is the finite lifetime measured from preparation.
//
// Returns sealed policy followed by owned host, group and kill descriptors.
func prepareWatchdogHandoff(group *Group, lifetime time.Duration) (*watchdogHandoff, error) {
	if group == nil || lifetime <= 0 || lifetime > maximumHostWatchdogLifetime {
		return nil, ErrInvalidLimits
	}
	now, err := watchdogBootTime()
	if err != nil || now > math.MaxInt64-int64(lifetime) {
		return nil, errors.Join(ErrInvalidLimits, err)
	}
	return prepareWatchdogHandoffAt(group, now+int64(lifetime))
}

// prepareWatchdogHandoffAt preserves a deadline captured before queued preparation.
//
// Takes group (*Group) which is the retained group.
// Takes deadline (int64) which is the absolute, finite boot-time deadline.
//
// Returns sealed authority without restarting the caller's remaining budget.
//
// Safe for concurrent use by multiple goroutines.
func prepareWatchdogHandoffAt(group *Group, deadline int64) (*watchdogHandoff, error) {
	if group == nil || deadline <= 0 {
		return nil, ErrInvalidLimits
	}
	now, err := watchdogBootTime()
	if err != nil {
		return nil, err
	}
	if deadline <= now || deadline-now > int64(maximumHostWatchdogLifetime) {
		return nil, ErrInvalidLimits
	}
	group.mutex.Lock()
	defer group.mutex.Unlock()
	if group.closed || group.closing || group.directory == nil || group.kill == nil {
		return nil, errClosed
	}
	descriptor, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		return nil, err
	}
	handoff := &watchdogHandoff{files: make([]*os.File, watchdogHandoffFiles)}
	handoff.files[1] = os.NewFile(uintptr(descriptor), "watchdog-host")
	for index, source := range []*os.File{group.directory, group.kill} {
		handoff.files[index+2], err = duplicateWatchdogFile(source)
		if err != nil {
			return nil, errors.Join(err, handoff.Close())
		}
	}
	handoff.files[0], err = sealWatchdogPolicy(deadline, handoff.files[1:])
	if err != nil {
		return nil, errors.Join(err, handoff.Close())
	}
	return handoff, nil
}

// duplicateWatchdogFile isolates descriptor lifetime using close-on-exec copies.
//
// Takes source (*os.File) which is the borrowed host-approved handle.
//
// Returns a new owned descriptor or an error.
func duplicateWatchdogFile(source *os.File) (*os.File, error) {
	if source == nil {
		return nil, ErrInvalidLimits
	}
	descriptor, err := unix.FcntlInt(source.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(descriptor), "watchdog-authority"), nil
}

// sealWatchdogPolicy seals exact inode identities and an absolute boot-time deadline.
//
// Takes deadline (int64) which is the absolute boot-time deadline.
// Takes authority ([]*os.File) which holds the ordered host, group and kill handles.
//
// Returns a fixed-size non-executable immutable policy descriptor.
func sealWatchdogPolicy(deadline int64, authority []*os.File) (*os.File, error) {
	if deadline <= 0 || len(authority) != watchdogHandoffFiles-1 {
		return nil, ErrInvalidLimits
	}
	encoded := make([]byte, watchdogPolicyBytes)
	copy(encoded, watchdogPolicyMagic)
	binary.BigEndian.PutUint64(encoded[watchdogWordBytes:], safeconv.Int64ToUint64(deadline))
	for index, file := range authority {
		identity, err := watchdogFileIdentity(file)
		if err != nil {
			return nil, err
		}
		offset := watchdogIdentityOffset + index*2*watchdogWordBytes
		binary.BigEndian.PutUint64(encoded[offset:], identity[0])
		binary.BigEndian.PutUint64(encoded[offset+watchdogWordBytes:], identity[1])
	}
	descriptor, err := unix.MemfdCreate("pipit-watchdog-policy", unix.MFD_CLOEXEC|unix.MFD_NOEXEC_SEAL)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(descriptor), "watchdog-policy")
	if _, err := file.WriteAt(encoded, 0); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if _, err := unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, watchdogRequiredSeals); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

// watchdogFileIdentity reads the actual descriptor identity rather than a pathname.
//
// Takes file (*os.File) which is the borrowed handle.
//
// Returns its kernel device and inode numbers.
func watchdogFileIdentity(file *os.File) ([2]uint64, error) {
	if file == nil {
		return [2]uint64{}, ErrInvalidLimits
	}
	var status unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &status); err != nil {
		return [2]uint64{}, err
	}
	return [2]uint64{status.Dev, status.Ino}, nil
}

// watchdogBootTime reads the shared clock used for immutable launch deadlines.
//
// Returns checked nanoseconds since boot, including suspended time.
func watchdogBootTime() (int64, error) {
	var stamp unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_BOOTTIME, &stamp); err != nil {
		return 0, err
	}
	if stamp.Sec < 0 || stamp.Sec >= math.MaxInt64/int64(time.Second) {
		return 0, ErrInvalidLimits
	}
	return stamp.Nano(), nil
}
