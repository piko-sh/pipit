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
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestFilesystemBrokerFilter(t *testing.T) {
	filter, err := filesystemBrokerFilter(42, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		number    uint32
		arguments [6]uint64
		allow     bool
		verdict   uint32
	}{
		{"file read", unix.SYS_READ, [6]uint64{9}, true, 0},
		{"file write", unix.SYS_WRITE, [6]uint64{9}, true, 0},
		{"negative descriptor", unix.SYS_READ, [6]uint64{1 << 31}, false, 0},
		{"high descriptor bits", unix.SYS_WRITE, [6]uint64{1<<32 | 9}, false, 0},
		{"proc reopen", unix.SYS_OPENAT, [6]uint64{9, 0, unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK | unix.O_NOCTTY, 0}, true, 0},
		{"open create", unix.SYS_OPENAT, [6]uint64{9, 0, unix.O_CREAT | unix.O_WRONLY, 0600}, false, 0},
		{"cwd open", unix.SYS_OPENAT, [6]uint64{^uint64(99), 0, unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK | unix.O_NOCTTY}, false, 0},
		{"beneath open", unix.SYS_OPENAT2, [6]uint64{9, 0, 0, uint64(unsafe.Sizeof(unix.OpenHow{}))}, true, 0},
		{"unknown open ABI", unix.SYS_OPENAT2, [6]uint64{9, 0, 0, 128}, false, 0},
		{"get flags", unix.SYS_FCNTL, [6]uint64{9, unix.F_GETFL}, true, 0},
		{"duplicate handle", unix.SYS_FCNTL, [6]uint64{9, unix.F_DUPFD_CLOEXEC}, false, 0},
		{"descriptor overwrite", unix.SYS_DUP3, [6]uint64{}, false, 0},
		{"link staging", unix.SYS_LINKAT, [6]uint64{9, 0, 10, 0, unix.AT_SYMLINK_FOLLOW}, true, 0},
		{"link arbitrary inode", unix.SYS_LINKAT, [6]uint64{9, 0, 10, 0, unix.AT_EMPTY_PATH}, false, 0},
		{"rename staging", unix.SYS_RENAMEAT2, [6]uint64{9, 0, 10, 0, 0}, true, 0},
		{"exchange entries", unix.SYS_RENAMEAT2, [6]uint64{9, 0, 10, 0, unix.RENAME_EXCHANGE}, false, 0},
		{"unlink staging", unix.SYS_UNLINKAT, [6]uint64{9, 0, 0}, true, 0},
		{"remove directory", unix.SYS_UNLINKAT, [6]uint64{9, 0, unix.AT_REMOVEDIR}, false, 0},
		{"list", unix.SYS_GETDENTS64, [6]uint64{9}, true, 0},
		{"sync", unix.SYS_FSYNC, [6]uint64{9}, true, 0},
		{"new socket", unix.SYS_SOCKET, [6]uint64{}, false, 0},
		{"receive handles", unix.SYS_RECVMSG, [6]uint64{}, false, 0},
		{"send handles", unix.SYS_SENDMSG, [6]uint64{}, false, 0},
		{"execute", unix.SYS_EXECVE, [6]uint64{}, false, unix.SECCOMP_RET_KILL_PROCESS},
		{"mount", unix.SYS_MOUNT, [6]uint64{}, false, unix.SECCOMP_RET_KILL_PROCESS},
		{"new namespace", unix.SYS_UNSHARE, [6]uint64{}, false, unix.SECCOMP_RET_KILL_PROCESS},
		{"new process", unix.SYS_CLONE3, [6]uint64{}, false, 0},
		{"ptrace", unix.SYS_PTRACE, [6]uint64{}, false, unix.SECCOMP_RET_KILL_PROCESS},
		{"io uring", unix.SYS_IO_URING_SETUP, [6]uint64{}, false, 0},
		{"chmod", unix.SYS_FCHMOD, [6]uint64{}, false, 0},
		{"executable memory", unix.SYS_MPROTECT, [6]uint64{0, 0, unix.PROT_READ | unix.PROT_EXEC}, false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			expected := uint32(denySyscall)
			if test.allow {
				expected = unix.SECCOMP_RET_ALLOW
			}
			if test.verdict != 0 {
				expected = test.verdict
			}
			if actual := evaluateFilter(t, filter, workerAuditArchitecture, test.number, test.arguments); actual != expected {
				t.Fatalf("verdict %#x, want %#x", actual, expected)
			}
		})
	}
	if verdict := evaluateFilter(t, filter, workerAuditArchitecture^1, unix.SYS_READ, [6]uint64{}); verdict != unix.SECCOMP_RET_KILL_PROCESS {
		t.Fatal("foreign ABI accepted")
	}
	if _, err := filesystemBrokerFilter(0, nil); err == nil {
		t.Fatal("missing identity accepted")
	}
	var workerIO *WorkerIO
	if err := workerIO.installFilesystemBrokerFilter(); err == nil {
		t.Fatal("missing I/O accepted")
	}
}

func TestBrokerFilterDoesNotBroadenWorker(t *testing.T) {
	filter, err := workerFilter(42)
	if err != nil {
		t.Fatal(err)
	}
	for _, number := range []uint32{unix.SYS_OPENAT, unix.SYS_OPENAT2, unix.SYS_GETDENTS64, unix.SYS_LINKAT, unix.SYS_RENAMEAT2, unix.SYS_UNLINKAT, unix.SYS_FSYNC} {
		if actual := evaluateFilter(t, filter, workerAuditArchitecture, number, [6]uint64{}); actual != denySyscall {
			t.Fatalf("worker gained syscall %d", number)
		}
	}
	if actual := evaluateFilter(t, filter, workerAuditArchitecture, unix.SYS_READ, [6]uint64{9}); actual != denySyscall {
		t.Fatal("worker gained arbitrary descriptor reads")
	}
}
