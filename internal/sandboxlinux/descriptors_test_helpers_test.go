//go:build linux && (amd64 || arm64) && !race

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
	"os/exec"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

func configureDescriptorFixture(t *testing.T, command *exec.Cmd) {
	t.Helper()
	proc, err := OpenWorkerProc()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = proc.Close() })
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	parent := os.NewFile(uintptr(pair[0]), "parent-ipc")
	child := os.NewFile(uintptr(pair[1]), "worker-ipc")
	t.Cleanup(func() {
		_ = parent.Close()
		_ = child.Close()
	})
	secret, err := os.CreateTemp(t.TempDir(), "host-secret")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secret.Close() })
	high, err := unix.FcntlInt(secret.Fd(), unix.F_DUPFD, 512)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(high) })
	command.ExtraFiles = []*os.File{child, proc, secret}
	command.Env = append(command.Env, "PIPIT_INHERITED_HIGH_FD="+strconv.Itoa(high))
}

func checkNativeWorkerDescriptors(t *testing.T) {
	t.Helper()
	high, err := strconv.Atoi(os.Getenv("PIPIT_INHERITED_HIGH_FD"))
	if err != nil || high < 512 {
		t.Fatalf("missing inherited high descriptor: %v", err)
	}
	for _, descriptor := range []int{5, high} {
		flags, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0)
		if err != nil || flags&unix.FD_CLOEXEC != 0 {
			t.Fatalf("fixture did not inherit descriptor %d: %v", descriptor, err)
		}
	}
	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &unix.Rlimit{Cur: 64, Max: 64}); err != nil {
		t.Fatal(err)
	}
	if err := sealWorkerDescriptors(); err != nil {
		t.Fatal(err)
	}
	for _, descriptor := range []int{0, workerProcFD, 5, high} {
		if _, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0); !errors.Is(err, unix.EBADF) {
			t.Fatalf("descriptor %d retained host authority: %v", descriptor, err)
		}
	}
	if _, err := unix.Write(workerIPCFD, []byte("bootstrap IPC retained")); err != nil {
		t.Fatalf("private IPC was lost: %v", err)
	}
}
