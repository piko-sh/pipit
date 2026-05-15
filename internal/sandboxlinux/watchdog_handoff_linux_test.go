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
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestWatchdogHostRejectsPathHandle(t *testing.T) {
	t.Parallel()
	descriptor, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(descriptor)
	path, err := unix.Open(fmt.Sprintf("/proc/self/fd/%d", descriptor), unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(path), "path-only-pidfd")
	defer file.Close()
	if err := validateWatchdogHost(file); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("path-only pidfd accepted: %v", err)
	}
}

func TestWatchdogHandoffRejectsInvalidOwners(t *testing.T) {
	t.Parallel()
	for _, lifetime := range []time.Duration{0, -1, maximumHostWatchdogLifetime + 1} {
		if owner, err := prepareWatchdogHandoff(&Group{}, lifetime); owner != nil || !errors.Is(err, ErrInvalidLimits) {
			t.Fatalf("invalid lifetime acquired authority: %v", err)
		}
	}
	if owner, err := prepareWatchdogHandoff(nil, time.Second); owner != nil || !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("missing group acquired authority: %v", err)
	}
	if owner, err := prepareWatchdogHandoff(&Group{}, time.Second); owner != nil || !errors.Is(err, errClosed) {
		t.Fatalf("unprepared group acquired authority: %v", err)
	}
	for _, files := range [][]*os.File{nil, make([]*os.File, watchdogHandoffFiles), make([]*os.File, watchdogHandoffFiles+1)} {
		if owner, err := adoptWatchdogHandoff(files); owner != nil || err == nil {
			t.Fatalf("invalid handoff acquired authority: %v", err)
		}
	}
}

func TestWatchdogBootstrapOwnerValidation(t *testing.T) {
	t.Parallel()
	var absent *HostWatchdog
	if err := absent.Run(); !errors.Is(err, errClosed) {
		t.Fatal("missing watchdog ran:", err)
	}
	if err := absent.Close(); err != nil {
		t.Fatal(err)
	}
	var empty HostWatchdog
	if err := empty.Run(); !errors.Is(err, errClosed) {
		t.Fatal("unprepared watchdog ran:", err)
	}
	if err := empty.Close(); err != nil {
		t.Fatal(err)
	}
	for _, deadline := range []int64{0, -1} {
		timer, err := prepareWatchdogTimer(deadline)
		if timer != nil || !errors.Is(err, ErrInvalidLimits) {
			t.Fatal("invalid watchdog timer armed:", err)
		}
	}
}

func TestWatchdogPolicyRejectsMutableOrMalformedFiles(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"unsealed", "short", "long", "unknown", "executable"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			flags := unix.MFD_CLOEXEC | unix.MFD_NOEXEC_SEAL
			if mode == "executable" {
				flags = unix.MFD_CLOEXEC | unix.MFD_ALLOW_SEALING
			}
			descriptor, err := unix.MemfdCreate("watchdog-policy-test", flags)
			if err != nil {
				t.Fatal(err)
			}
			file := os.NewFile(uintptr(descriptor), "policy-test")
			defer file.Close()
			data := make([]byte, watchdogPolicyBytes)
			copy(data, watchdogPolicyMagic)
			switch mode {
			case "short":
				data = data[:len(data)-1]
			case "long":
				data = append(data, 0)
			case "unknown":
				data[0] = 'X'
			}
			if _, err := file.WriteAt(data, 0); err != nil {
				t.Fatal(err)
			}
			if mode != "unsealed" {
				if _, err := unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, watchdogRequiredSeals); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := readWatchdogPolicy(file); err == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
}

func TestNativeWatchdogHandoff(t *testing.T) {
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires explicitly delegated cgroup-v2 parent")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	group, err := newGroup(parent, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer group.closeGroup(ctx)
	now, err := watchdogBootTime()
	if err != nil {
		t.Fatal(err)
	}
	deadline := now + int64(5*time.Second)
	handoff, err := prepareWatchdogHandoffAt(group, deadline)
	if err != nil {
		t.Fatal(err)
	}
	defer handoff.Close()
	policy, err := readWatchdogPolicy(handoff.files[0])
	if err != nil || binary.BigEndian.Uint64(policy[watchdogWordBytes:]) != uint64(deadline) {
		t.Fatalf("handoff restarted absolute deadline: %v", err)
	}
	for _, change := range []func() error{
		func() error { _, err := handoff.files[0].WriteAt([]byte{0}, 0); return err },
		func() error { return handoff.files[0].Truncate(0) },
		func() error { return handoff.files[0].Chmod(0o700) },
	} {
		if err := change(); !errors.Is(err, unix.EPERM) {
			t.Fatalf("mutable watchdog policy: %v", err)
		}
	}
	checkWatchdogHandoffSubstitutions(t, ctx, parent, handoff)
	authority, err := adoptWatchdogHandoff(handoff.files)
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	if err := handoff.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validateWatchdogHost(authority.host); err != nil {
		t.Fatal("borrowed host lifetime:", err)
	}
	if err := validateWatchdogKill(authority.kill); err != nil {
		t.Fatal("borrowed kill lifetime:", err)
	}
	for _, file := range []*os.File{authority.host, authority.kill} {
		flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFD, 0)
		if err != nil || flags&unix.FD_CLOEXEC == 0 {
			t.Fatal("adopted authority is inheritable:", err)
		}
	}
}

func checkWatchdogHandoffSubstitutions(t *testing.T, ctx context.Context, parent string, handoff *watchdogHandoff) {
	t.Helper()
	other, err := newGroup(parent, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer other.closeGroup(ctx)
	wrongControl, err := openControl(handoff.files[2], "memory.max", unix.O_WRONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer wrongControl.Close()
	for index, replacement := range map[int]*os.File{0: other.directory, 1: other.directory, 2: other.directory, 3: other.kill} {
		files := slices.Clone(handoff.files)
		files[index] = replacement
		if owner, err := adoptWatchdogHandoff(files); owner != nil || err == nil {
			t.Fatalf("substituted role %d accepted: %v", index, err)
		}
	}
	now, err := watchdogBootTime()
	if err != nil {
		t.Fatal(err)
	}
	for _, kill := range []*os.File{wrongControl, other.kill} {
		files := slices.Clone(handoff.files)
		files[3] = kill
		files[0], err = sealWatchdogPolicy(now+int64(time.Second), files[1:])
		if err != nil {
			t.Fatal(err)
		}
		owner, adoptionErr := adoptWatchdogHandoff(files)
		_ = files[0].Close()
		if owner != nil || adoptionErr == nil {
			t.Fatal("mismatched cgroup control accepted:", adoptionErr)
		}
	}
	for _, deadline := range []int64{now - 1, now + int64(maximumHostWatchdogLifetime) + int64(time.Minute)} {
		files := slices.Clone(handoff.files)
		files[0], err = sealWatchdogPolicy(deadline, files[1:])
		if err != nil {
			t.Fatal(err)
		}
		owner, adoptionErr := adoptWatchdogHandoff(files)
		_ = files[0].Close()
		if owner != nil || adoptionErr == nil {
			t.Fatal("invalid absolute deadline accepted:", adoptionErr)
		}
	}
}
