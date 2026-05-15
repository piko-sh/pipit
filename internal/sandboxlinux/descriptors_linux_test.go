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
	"errors"
	"os"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

func TestWorkerDescriptorRoles(t *testing.T) {
	t.Parallel()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if err := checkWorkerOutput(int(writer.Fd())); err != nil {
		t.Fatal(err)
	}
	if err := checkWorkerOutput(int(reader.Fd())); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("readable output descriptor accepted: %v", err)
	}
	if err := checkWorkerIPC(int(writer.Fd())); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("pipe accepted as IPC socket: %v", err)
	}
	if err := checkWorkerProc(int(reader.Fd())); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("pipe accepted as procfs: %v", err)
	}
	proc, err := OpenWorkerProc()
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Close()
	if err := checkWorkerOutput(int(proc.Fd())); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("directory accepted as output: %v", err)
	}
	for _, kind := range []int{unix.SOCK_STREAM, unix.SOCK_DGRAM} {
		pair, err := unix.Socketpair(unix.AF_UNIX, kind|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		err = checkWorkerIPC(pair[0])
		_ = unix.Close(pair[0])
		_ = unix.Close(pair[1])
		if kind == unix.SOCK_STREAM && err != nil {
			t.Fatal(err)
		}
		if kind == unix.SOCK_DGRAM && !errors.Is(err, ErrUnavailable) {
			t.Fatalf("datagram transport accepted: %v", err)
		}
	}
	socket, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(socket)
	if err := checkWorkerIPC(socket); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unconnected transport accepted: %v", err)
	}
}

func TestInheritedDescriptorAuditRetainsRuntimeHandles(t *testing.T) {
	t.Parallel()
	file, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	descriptor := int(file.Fd())
	flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFD, 0)
	if err != nil || flags&unix.FD_CLOEXEC == 0 {
		t.Fatalf("fixture must use a close-on-exec descriptor: %v", err)
	}
	if err := closeInheritedDescriptor(strconv.Itoa(descriptor)); err != nil {
		t.Fatal(err)
	}
	if _, err := unix.FcntlInt(file.Fd(), unix.F_GETFD, 0); err != nil {
		t.Fatalf("runtime-owned descriptor was closed: %v", err)
	}
	for _, invalid := range []string{"", "invalid", "-1", "9223372036854775808"} {
		if err := closeInheritedDescriptor(invalid); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("invalid descriptor name accepted: %q: %v", invalid, err)
		}
	}
}

func TestWorkerDescriptorSealingRejectsHost(t *testing.T) {
	t.Parallel()
	if os.Getpid() == 1 {
		t.Skip("test process is namespace init")
	}
	if err := sealWorkerDescriptors(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("descriptor sealing accepted the host process: %v", err)
	}
}
