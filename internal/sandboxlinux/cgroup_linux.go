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

// Package sandboxlinux implements Linux resource controls for isolated workers. Resource
// controls alone do not provide filesystem, network or syscall isolation.
package sandboxlinux

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	// defaultMemoryBytes is the default memory limit in bytes.
	defaultMemoryBytes = 256 << 20

	// minimumMemoryBytes is the smallest allowed memory limit.
	minimumMemoryBytes = 1 << 20

	// defaultCPUMilli is the default CPU allowance in millicores.
	defaultCPUMilli = 1000

	// maximumCPUMilli is the largest allowed CPU allowance.
	maximumCPUMilli = 64000

	// minimumCPUMilli is the smallest allowed CPU allowance.
	minimumCPUMilli = 10

	// cpuPeriodMicros is the CFS scheduling period in microseconds.
	cpuPeriodMicros = 100000

	// milliPerCore is the number of millicores per logical CPU core.
	milliPerCore = 1000

	// defaultTasks is the default maximum task count.
	defaultTasks = 64

	// maximumTasks is the largest allowed task count.
	maximumTasks = 4096

	// controlReadLimit is the maximum bytes read from a cgroup control file.
	controlReadLimit = 4096

	// cleanupPollInterval is the polling period when waiting for a group to become empty.
	cleanupPollInterval = 10 * time.Millisecond

	// groupDirectoryMode is the permission mode for new cgroup directories.
	groupDirectoryMode = 0700

	// groupSetupCleanupTimeout is the deadline for cgroup setup rollback.
	groupSetupCleanupTimeout = 5 * time.Second
)

var (
	// ErrUnavailable reports missing native resource enforcement.
	ErrUnavailable = errors.New("linux resource isolation unavailable")

	// ErrInvalidLimits reports an invalid native resource policy.
	ErrInvalidLimits = errors.New("invalid Linux resource limits")

	// errClosed reports a resource group that has already been removed.
	errClosed = errors.New("linux resource group closed")
)

// Limits selects hard cgroup bounds, independently of interpreter accounting. Zero
// selects defaults.
type Limits struct {
	// MemoryBytes holds the hard memory limit, page-aligned.
	MemoryBytes int64

	// CPUMilli holds the CPU allowance in millicores.
	CPUMilli int64

	// Tasks holds the maximum number of threads.
	Tasks int64
}

// Group owns one fresh, private cgroup under a host-delegated parent. This resource
// boundary is not a complete sandbox: the launcher must separately remove filesystem,
// network, namespace and syscall authority from the worker.
type Group struct {
	// parent holds the pinned delegated parent directory.
	parent *os.File

	// directory holds the pinned child cgroup directory.
	directory *os.File

	// kill holds the writable cgroup.kill control handle.
	kill *os.File

	// name holds the child directory basename under the parent.
	name string

	// mutex guards mutable state during start and close.
	mutex sync.Mutex

	// closed is true once cleanup has completed successfully.
	closed bool

	// closing is true once shutdown has begun.
	closing bool

	// started is true once a process has been placed in the group.
	started bool

	// parentOnly is true once the group has delegated to children.
	parentOnly bool
}

// newGroup creates and verifies all required controls before permitting a start.
//
// Takes parentPath (string) which names a host-selected delegated cgroup directory.
// Takes limits (Limits) which select finite resource bounds.
//
// Returns *Group which can start a process inside the prepared cgroup.
// Returns error when any required control cannot be established.
func newGroup(parentPath string, limits Limits) (*Group, error) {
	return newRootGroup(parentPath, limits, "pipit-"+rand.Text())
}

// newRootGroup creates a named group beneath a verified delegated cgroup parent.
//
// Takes parentPath (string) which is the host-selected delegated cgroup parent.
// Takes limits (Limits) which select finite resource bounds.
// Takes name (string) which is the internal basename for the new group.
//
// Returns ownership only for a freshly created group, never an existing directory.
func newRootGroup(parentPath string, limits Limits, name string) (*Group, error) {
	settings, err := resourceSettings(limits)
	if err != nil {
		return nil, err
	}
	parent, err := openDirectory(unix.AT_FDCWD, parentPath)
	if err != nil {
		return nil, fmt.Errorf("%w: opening delegated parent: %w", ErrUnavailable, err)
	}
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(int(parent.Fd()), &filesystem); err != nil || filesystem.Type != unix.CGROUP2_SUPER_MAGIC {
		_ = parent.Close()
		return nil, fmt.Errorf("%w: parent is not cgroup v2", ErrUnavailable)
	}
	return newNamedGroupUnder(parent, settings, name)
}

// newGroupUnder creates a group beneath an already pinned cgroup-v2 parent.
//
// Takes parent (*os.File) which is the pinned cgroup-v2 parent directory; ownership is
// transferred.
// Takes settings (map[string]string) which holds the validated resource settings.
//
// Returns a group, retaining cleanup ownership when preparation rollback fails.
func newGroupUnder(parent *os.File, settings map[string]string) (*Group, error) {
	return newNamedGroupUnder(parent, settings, "pipit-"+rand.Text())
}

// newNamedGroupUnder atomically reserves a name before configuring resource controls.
//
// Takes parent (*os.File) which is the pinned parent handle; ownership is transferred.
// Takes settings (map[string]string) which holds the validated resource control values.
// Takes name (string) which is the internal basename for the new group.
//
// Returns a fresh owner or an error without inspecting or modifying an existing group.
func newNamedGroupUnder(parent *os.File, settings map[string]string, name string) (*Group, error) {
	if err := unix.Mkdirat(int(parent.Fd()), name, groupDirectoryMode); err != nil {
		_ = parent.Close()
		return nil, fmt.Errorf("%w: creating resource group: %w", ErrUnavailable, err)
	}
	group, err := prepareGroup(parent, name, settings)
	return finishGroupPreparation(group, err)
}

// start creates a process directly in this group using CLONE_INTO_CGROUP.
//
// Caller must hold the group for the duration of the process.
//
// Takes command (*exec.Cmd) which is host-selected and has not yet been started.
//
// Returns error when the group is closed or atomic cgroup placement fails.
//
// Safe for concurrent use by multiple goroutines.
func (group *Group) start(command *exec.Cmd) error {
	group.mutex.Lock()
	defer group.mutex.Unlock()
	if group.closed || group.closing || group.started || group.parentOnly {
		return errClosed
	}
	if command == nil {
		return fmt.Errorf("%w: missing worker command", ErrUnavailable)
	}
	var attributes syscall.SysProcAttr
	if command.SysProcAttr != nil {
		attributes = *command.SysProcAttr
	}
	attributes.UseCgroupFD = true
	attributes.CgroupFD = int(group.directory.Fd())
	command.SysProcAttr = &attributes
	group.started = true
	if err := command.Start(); err != nil {
		return fmt.Errorf("%w: atomic cgroup start: %w", ErrUnavailable, err)
	}
	return nil
}

// closeGroup kills every task in the group and waits for kernel-confirmed emptiness.
//
// Caller must hold the group; the mutex serialises concurrent close attempts.
//
// Returns error when killing, observing emptiness or removing the group fails.
//
// Safe for concurrent use by multiple goroutines.
func (group *Group) closeGroup(ctx context.Context) error {
	group.mutex.Lock()
	defer group.mutex.Unlock()
	if group.closed {
		return nil
	}
	if ctx == nil {
		return errors.New("missing resource cleanup context")
	}
	group.closing = true
	if err := group.prepareCleanup(); err != nil {
		return err
	}
	if err := group.waitEmpty(ctx); err != nil {
		return err
	}
	if err := group.verifyDirectoryEntry(); err != nil {
		return err
	}
	if err := unix.Unlinkat(int(group.parent.Fd()), group.name, unix.AT_REMOVEDIR); err != nil {
		return fmt.Errorf("removing resource group: %w", err)
	}
	group.closed = true
	return group.closeHandles()
}

// waitEmpty waits for the kernel's recursive populated flag to become zero.
//
// Returns error when observation fails or the cleanup deadline expires.
func (group *Group) waitEmpty(ctx context.Context) error {
	ticker := time.NewTicker(cleanupPollInterval)
	defer ticker.Stop()
	for {
		events, err := readControl(group.directory, "cgroup.events")
		if err != nil {
			return err
		}
		empty, err := parseEmpty(events)
		if err != nil {
			return err
		}
		if empty {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// resourceSettings validates limits and returns the exact kernel control values.
//
// Takes limits (Limits) which supplies finite bounds or zero-valued defaults.
//
// Returns map[string]string which contains every required resource control.
// Returns error when a requested resource value is invalid.
func resourceSettings(limits Limits) (map[string]string, error) {
	if limits.MemoryBytes == 0 {
		limits.MemoryBytes = defaultMemoryBytes
	}
	if limits.CPUMilli == 0 {
		limits.CPUMilli = defaultCPUMilli
	}
	if limits.Tasks == 0 {
		limits.Tasks = defaultTasks
	}
	if limits.MemoryBytes < minimumMemoryBytes || limits.MemoryBytes%int64(os.Getpagesize()) != 0 {
		return nil, ErrInvalidLimits
	}
	if limits.CPUMilli < minimumCPUMilli || limits.CPUMilli > maximumCPUMilli {
		return nil, ErrInvalidLimits
	}
	if limits.Tasks < 1 || limits.Tasks > maximumTasks {
		return nil, ErrInvalidLimits
	}
	return map[string]string{
		"memory.max":       strconv.FormatInt(limits.MemoryBytes, 10),
		"memory.swap.max":  "0",
		"memory.oom.group": "1",
		"cpu.max":          fmt.Sprintf("%d %d", limits.CPUMilli*cpuPeriodMicros/milliPerCore, cpuPeriodMicros),
		"pids.max":         strconv.FormatInt(limits.Tasks, 10),
	}, nil
}

// prepareGroup opens the new group and verifies each mandatory control.
//
// Takes parent (*os.File) which owns the delegated parent directory.
// Takes name (string) which names the fresh child group.
// Takes settings (map[string]string) which supplies validated control values.
//
// Returns *Group which owns its parent, child and kill handles.
// Returns error when control setup or opening fails.
func prepareGroup(parent *os.File, name string, settings map[string]string) (*Group, error) {
	group := &Group{
		parent: parent, directory: nil, kill: nil, name: name,
		mutex: sync.Mutex{}, closed: false, closing: true, started: false, parentOnly: false,
	}
	directory, err := openDirectory(int(parent.Fd()), name)
	if err != nil {
		return group, fmt.Errorf("%w: opening new group: %w", ErrUnavailable, err)
	}
	group.directory = directory
	for control, value := range settings {
		if err := setControl(directory, control, value); err != nil {
			return group, fmt.Errorf("%w: configuring %s: %w", ErrUnavailable, control, err)
		}
	}
	kill, err := openControl(directory, "cgroup.kill", unix.O_WRONLY)
	if err != nil {
		return group, fmt.Errorf("%w: group-wide termination: %w", ErrUnavailable, err)
	}
	group.kill = kill
	group.closing = false
	return group, nil
}

// openDirectory opens a directory without following its final symlink.
//
// Takes parent (int) which is an owning directory FD or AT_FDCWD.
// Takes path (string) which is selected by the host, not by a worker.
//
// Returns *os.File which owns a close-on-exec directory descriptor.
// Returns error when the directory cannot be opened safely.
func openDirectory(parent int, path string) (*os.File, error) {
	descriptor, err := unix.Openat(parent, path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(descriptor), path), nil
}

// openControl opens one fixed-name kernel control relative to its pinned group.
//
// Takes directory (*os.File) which is an open cgroup directory.
// Takes name (string) which is a fixed kernel control name.
// Takes flags (int) which select read or write access.
//
// Returns *os.File which owns a close-on-exec control descriptor.
// Returns error when the control is unavailable.
func openControl(directory *os.File, name string, flags int) (*os.File, error) {
	descriptor, err := unix.Openat(int(directory.Fd()), name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(descriptor), name), nil
}

// setControl writes and reads back one required kernel-enforced setting.
//
// Takes directory (*os.File) which identifies the fresh group.
// Takes name (string) which is a fixed kernel control name.
// Takes value (string) which is its validated required setting.
//
// Returns error when writing fails or the kernel reports a different setting.
func setControl(directory *os.File, name, value string) error {
	control, err := openControl(directory, name, unix.O_WRONLY)
	if err != nil {
		return err
	}
	written, writeErr := control.WriteString(value)
	closeErr := control.Close()
	if writeErr != nil || closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	if written != len(value) {
		return io.ErrShortWrite
	}
	actual, err := readControl(directory, name)
	if err != nil {
		return err
	}
	if strings.Join(strings.Fields(actual), " ") != value {
		return errors.New("kernel resource control differs from requested value")
	}
	return nil
}

// readControl reads a bounded kernel control file.
//
// Takes directory (*os.File) which identifies the resource group.
// Takes name (string) which is a fixed kernel control name.
//
// Returns string which contains at most the control-file byte limit.
// Returns error when the control cannot be read within that bound.
func readControl(directory *os.File, name string) (string, error) {
	control, err := openControl(directory, name, unix.O_RDONLY)
	if err != nil {
		return "", err
	}
	data, readErr := io.ReadAll(io.LimitReader(control, controlReadLimit+1))
	closeErr := control.Close()
	if readErr != nil || closeErr != nil {
		return "", errors.Join(readErr, closeErr)
	}
	if len(data) > controlReadLimit {
		return "", errors.New("oversized cgroup control")
	}
	return string(data), nil
}

// parseEmpty requires exactly one valid populated event.
//
// Takes events (string) which contains bounded cgroup.events data.
//
// Returns bool which reports kernel-confirmed recursive emptiness.
// Returns error when populated is missing, duplicated or malformed.
func parseEmpty(events string) (bool, error) {
	found := false
	empty := false
	for line := range strings.SplitSeq(events, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "populated" {
			continue
		}
		if found || len(fields) != 2 {
			return false, errors.New("invalid cgroup populated event")
		}
		found = true
		switch fields[1] {
		case "0":
			empty = true
		case "1":
			empty = false
		default:
			return false, errors.New("invalid cgroup population value")
		}
	}
	if !found {
		return false, errors.New("missing cgroup populated event")
	}
	return empty, nil
}
