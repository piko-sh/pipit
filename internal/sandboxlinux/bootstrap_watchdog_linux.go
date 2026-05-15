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
	"os"
	"sync/atomic"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/safeconv"
)

const (
	// watchdogPolicyFD is the inherited descriptor number where sealed policy starts in a
	// watchdog process.
	watchdogPolicyFD = 4

	// watchdogReadyByte is the single byte sent to signal readiness.
	watchdogReadyByte = 1
)

// HostWatchdog owns sealed observation, termination and absolute deadline authority. It
// must run in a dedicated approved process, never inside an untrusted worker.
type HostWatchdog struct {
	// workerIO holds the sealed IPC transport for the watchdog process.
	workerIO *WorkerIO

	// authority holds the pinned host and kill handles.
	authority *watchdogAuthority

	// timer holds the armed boot-time deadline descriptor.
	timer *os.File

	// used prevents a second Run call.
	used atomic.Bool
}

// Run emits one readiness byte and waits for host death or the immutable deadline.
//
// Returns only after requesting termination, including on readiness or polling failure.
// This is single-use and does not prove descendant reaping or release service admission.
func (watchdog *HostWatchdog) Run() error {
	if watchdog == nil || watchdog.workerIO == nil || watchdog.authority == nil || watchdog.timer == nil ||
		!watchdog.used.CompareAndSwap(false, true) {
		return errClosed
	}
	err := watchdog.ready()
	if err == nil {
		err = watchdog.poll()
	}
	written, killErr := watchdog.authority.kill.WriteAt([]byte("1"), 0)
	if killErr == nil && written != 1 {
		killErr = io.ErrShortWrite
	}
	return errors.Join(err, killErr)
}

// Close releases bootstrap ownership after Run or a terminal setup failure.
//
// Returns joined close errors without reopening paths or granting new authority.
func (watchdog *HostWatchdog) Close() error {
	if watchdog == nil {
		return nil
	}
	var result error
	if watchdog.authority != nil {
		result = errors.Join(result, watchdog.authority.Close())
	}
	if watchdog.timer != nil {
		result = errors.Join(result, watchdog.timer.Close())
		watchdog.timer = nil
	}
	if watchdog.workerIO != nil {
		result = errors.Join(result, watchdog.workerIO.stream.Close())
		watchdog.workerIO = nil
	}
	return result
}

// installFilter removes every operation not required after watchdog readiness.
//
// Returns error unless the reviewed profile synchronises across all threads.
func (watchdog *HostWatchdog) installFilter() error {
	filter, err := watchdogReadyFilter(os.Getpid(), safeconv.IntToUint32(int(watchdog.authority.host.Fd())),
		safeconv.IntToUint32(int(watchdog.authority.kill.Fd())), safeconv.IntToUint32(int(watchdog.timer.Fd())),
		watchdog.workerIO.wakeDescriptors)
	if err != nil {
		return err
	}
	return installWorkerFilter(filter)
}

// ready refuses expired startup and bounds the one-byte readiness write.
//
// Returns error without permitting source admission after any readiness failure.
func (watchdog *HostWatchdog) ready() error {
	now, err := watchdogBootTime()
	if err != nil {
		return err
	}
	if watchdog.authority.deadline <= now {
		return context.DeadlineExceeded
	}
	return unix.Sendto(workerIPCFD, []byte{watchdogReadyByte}, watchdogReadySendFlags, nil)
}

// poll observes host and deadline handles without Go timers or a resettable budget.
//
// Returns host exit, kernel deadline expiry or an observation error.
func (watchdog *HostWatchdog) poll() error {
	events := []unix.PollFd{
		{Fd: safeconv.IntToInt32(int(watchdog.authority.host.Fd())), Events: unix.POLLIN, Revents: 0},
		{Fd: safeconv.IntToInt32(int(watchdog.timer.Fd())), Events: unix.POLLIN, Revents: 0},
	}
	for {
		_, err := unix.Ppoll(events, nil, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		for _, event := range events {
			if event.Revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
				return ErrUnavailable
			}
		}
		if events[1].Revents&(unix.POLLIN|unix.POLLHUP) != 0 {
			return context.DeadlineExceeded
		}
		if events[0].Revents&(unix.POLLIN|unix.POLLHUP) != 0 {
			return nil
		}
		return ErrUnavailable
	}
}

// BootstrapHostWatchdog adopts the fixed handoff and seals a disposable supervisor.
//
// IPC occupies descriptor 3; policy, host, group and kill use 4 to 7. No host procfs
// descriptor is inherited. No readiness is emitted until all inherited authority and
// process restrictions seal.
//
// Returns an owned supervisor, or error after closing all successfully adopted handles.
func BootstrapHostWatchdog() (*HostWatchdog, error) {
	workerIO, err := prepareWorkerIO()
	if err != nil {
		return nil, err
	}
	watchdog := &HostWatchdog{workerIO: workerIO, authority: nil, timer: nil, used: atomic.Bool{}}
	watchdog.authority, err = adoptInheritedWatchdog()
	if err != nil {
		return nil, errors.Join(err, watchdog.Close())
	}
	watchdog.timer, err = prepareWatchdogTimer(watchdog.authority.deadline)
	if err != nil {
		return nil, errors.Join(err, watchdog.Close())
	}
	for _, seal := range []func() error{
		sealWorkerDescriptors, sealWorkerFilesystem, sealWorkerPrivileges, watchdog.installFilter,
	} {
		if err := seal(); err != nil {
			return nil, errors.Join(err, watchdog.Close())
		}
	}
	return watchdog, nil
}

// adoptInheritedWatchdog consumes fixed inherited handles before the descriptor audit.
//
// Returns only independently pinned host and kill authority.
func adoptInheritedWatchdog() (authority *watchdogAuthority, result error) {
	inherited := &watchdogHandoff{files: make([]*os.File, watchdogHandoffFiles)}
	for index := range inherited.files {
		inherited.files[index] = os.NewFile(uintptr(watchdogPolicyFD+index), "watchdog-inherited")
	}
	defer func() {
		result = errors.Join(result, inherited.Close())
		if result != nil && authority != nil {
			result = errors.Join(result, authority.Close())
			authority = nil
		}
	}()
	return adoptWatchdogHandoff(inherited.files)
}

// prepareWatchdogTimer arms an absolute boot-time timer before syscall sealing.
//
// Takes deadline (int64) which is the immutable host-selected deadline, including startup
// and suspended time.
//
// Returns an owned timer that cannot be reset by the sealed supervisor.
func prepareWatchdogTimer(deadline int64) (*os.File, error) {
	if deadline <= 0 {
		return nil, ErrInvalidLimits
	}
	descriptor, err := unix.TimerfdCreate(unix.CLOCK_BOOTTIME, unix.TFD_CLOEXEC|unix.TFD_NONBLOCK)
	if err != nil {
		return nil, err
	}
	timer := os.NewFile(uintptr(descriptor), "watchdog-deadline")
	setting := unix.ItimerSpec{Interval: unix.Timespec{Sec: 0, Nsec: 0}, Value: unix.NsecToTimespec(deadline)}
	if err := unix.TimerfdSettime(descriptor, unix.TFD_TIMER_ABSTIME, &setting, nil); err != nil {
		return nil, errors.Join(err, timer.Close())
	}
	return timer, nil
}
