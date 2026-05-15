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
	"context"
	"errors"
	"io"
	"math"
	"os"
	"time"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/safeconv"
)

// maximumHostWatchdogLifetime is the longest acceptable watchdog lifetime.
const maximumHostWatchdogLifetime = 30 * time.Minute

// monitorHostLifetime terminates a pinned cgroup on host death or bounded expiry.
//
// Takes host (*os.File) which is the host-selected pidfd.
// Takes kill (*os.File) which is the owned cgroup.kill descriptor.
// Takes lifetime (time.Duration) which is the finite watchdog lifetime. Descriptors are
// borrowed and must remain open until this call returns.
//
// Returns observation and termination errors. This primitive needs an independent,
// confined supervisor process before it can protect a production worker.
func monitorHostLifetime(host, kill *os.File, lifetime time.Duration) error {
	if err := validateWatchdogKill(kill); err != nil {
		return err
	}
	observation := waitHostLifetime(host, lifetime)
	written, err := kill.WriteAt([]byte("1"), 0)
	if err == nil && written != 1 {
		err = io.ErrShortWrite
	}
	return errors.Join(observation, err)
}

// validateWatchdogKill verifies the basic type of a host-approved termination handle.
//
// Takes kill (*os.File) which is the exact cgroup.kill file supplied by the trusted group
// owner.
//
// Returns error for missing, non-cgroup or non-write-only descriptors.
func validateWatchdogKill(kill *os.File) error {
	if kill == nil {
		return ErrInvalidLimits
	}
	descriptor := int(kill.Fd())
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(descriptor, &filesystem); err != nil {
		return err
	}
	if filesystem.Type != unix.CGROUP2_SUPER_MAGIC {
		return ErrUnavailable
	}
	flags, err := unix.FcntlInt(kill.Fd(), unix.F_GETFL, 0)
	if err != nil {
		return err
	}
	if flags&unix.O_ACCMODE != unix.O_WRONLY {
		return ErrInvalidLimits
	}
	return nil
}

// waitHostLifetime observes a stable process identity without numeric PID reuse.
//
// Takes host (*os.File) which is the borrowed host pidfd.
// Takes lifetime (time.Duration) which is the finite lifetime capped before polling.
//
// Returns nil on host exit, a deadline error on expiry or a fail-closed poll error.
func waitHostLifetime(host *os.File, lifetime time.Duration) error {
	if host == nil || lifetime <= 0 || lifetime > maximumHostWatchdogLifetime || host.Fd() > math.MaxInt32 {
		return ErrInvalidLimits
	}
	descriptor := int(host.Fd())
	if err := validateWatchdogHost(host); err != nil {
		return err
	}
	return pollWatchdogHost(descriptor, lifetime)
}

// pollWatchdogHost waits for pidfd readiness under an independent monotonic deadline.
//
// Takes descriptor (int) which is the validated pidfd descriptor.
// Takes lifetime (time.Duration) which is the bounded positive lifetime.
//
// Returns host exit, expiry or an unexpected kernel observation without retrying errors.
func pollWatchdogHost(descriptor int, lifetime time.Duration) error {
	deadline := time.Now().Add(lifetime)
	events := []unix.PollFd{{Fd: safeconv.IntToInt32(descriptor), Events: unix.POLLIN, Revents: 0}}
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return context.DeadlineExceeded
		}
		timeout := unix.NsecToTimespec(int64(remaining))
		count, err := unix.Ppoll(events, &timeout, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if count == 0 {
			return context.DeadlineExceeded
		}
		if events[0].Revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
			return errors.New("invalid watchdog host poll state")
		}
		if events[0].Revents&(unix.POLLIN|unix.POLLHUP) != 0 {
			return nil
		}
		return errors.New("unexpected watchdog host poll state")
	}
}
