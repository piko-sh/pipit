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
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestFilesystemBrokerFilter(t *testing.T) {
	filter, err := filesystemBrokerFilter(42, nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		number    uint32
		arguments [6]uint64
		allow     bool
		verdict   uint32
	}{
		{name: "file read", number: unix.SYS_READ, arguments: [6]uint64{9}, allow: true, verdict: 0},
		{name: "file write", number: unix.SYS_WRITE, arguments: [6]uint64{9}, allow: true, verdict: 0},
		{name: "negative descriptor", number: unix.SYS_READ, arguments: [6]uint64{1 << 31}, allow: false, verdict: 0},
		{name: "high descriptor bits", number: unix.SYS_WRITE, arguments: [6]uint64{1<<32 | 9}, allow: false, verdict: 0},
		{name: "proc reopen", number: unix.SYS_OPENAT, arguments: [6]uint64{9, 0, unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK | unix.O_NOCTTY, 0}, allow: true, verdict: 0},
		{name: "open create", number: unix.SYS_OPENAT, arguments: [6]uint64{9, 0, unix.O_CREAT | unix.O_WRONLY, 0600}, allow: false, verdict: 0},
		{name: "cwd open", number: unix.SYS_OPENAT, arguments: [6]uint64{^uint64(99), 0, unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK | unix.O_NOCTTY}, allow: false, verdict: 0},
		{name: "beneath open", number: unix.SYS_OPENAT2, arguments: [6]uint64{9, 0, 0, uint64(unsafe.Sizeof(unix.OpenHow{}))}, allow: true, verdict: 0},
		{name: "unknown open ABI", number: unix.SYS_OPENAT2, arguments: [6]uint64{9, 0, 0, 128}, allow: false, verdict: 0},
		{name: "get flags", number: unix.SYS_FCNTL, arguments: [6]uint64{9, unix.F_GETFL}, allow: true, verdict: 0},
		{name: "duplicate handle", number: unix.SYS_FCNTL, arguments: [6]uint64{9, unix.F_DUPFD_CLOEXEC}, allow: false, verdict: 0},
		{name: "descriptor overwrite", number: unix.SYS_DUP3, arguments: [6]uint64{}, allow: false, verdict: 0},
		{name: "link staging", number: unix.SYS_LINKAT, arguments: [6]uint64{9, 0, 10, 0, unix.AT_SYMLINK_FOLLOW}, allow: true, verdict: 0},
		{name: "link arbitrary inode", number: unix.SYS_LINKAT, arguments: [6]uint64{9, 0, 10, 0, unix.AT_EMPTY_PATH}, allow: false, verdict: 0},
		{name: "rename staging", number: unix.SYS_RENAMEAT2, arguments: [6]uint64{9, 0, 10, 0, 0}, allow: true, verdict: 0},
		{name: "exchange entries", number: unix.SYS_RENAMEAT2, arguments: [6]uint64{9, 0, 10, 0, unix.RENAME_EXCHANGE}, allow: false, verdict: 0},
		{name: "unlink staging", number: unix.SYS_UNLINKAT, arguments: [6]uint64{9, 0, 0}, allow: true, verdict: 0},
		{name: "remove directory", number: unix.SYS_UNLINKAT, arguments: [6]uint64{9, 0, unix.AT_REMOVEDIR}, allow: false, verdict: 0},
		{name: "list", number: unix.SYS_GETDENTS64, arguments: [6]uint64{9}, allow: true, verdict: 0},
		{name: "sync", number: unix.SYS_FSYNC, arguments: [6]uint64{9}, allow: true, verdict: 0},
		{name: "new socket", number: unix.SYS_SOCKET, arguments: [6]uint64{}, allow: false, verdict: 0},
		{name: "receive handles", number: unix.SYS_RECVMSG, arguments: [6]uint64{}, allow: false, verdict: 0},
		{name: "send handles", number: unix.SYS_SENDMSG, arguments: [6]uint64{}, allow: false, verdict: 0},
		{name: "execute", number: unix.SYS_EXECVE, arguments: [6]uint64{}, allow: false, verdict: unix.SECCOMP_RET_KILL_PROCESS},
		{name: "mount", number: unix.SYS_MOUNT, arguments: [6]uint64{}, allow: false, verdict: unix.SECCOMP_RET_KILL_PROCESS},
		{name: "new namespace", number: unix.SYS_UNSHARE, arguments: [6]uint64{}, allow: false, verdict: unix.SECCOMP_RET_KILL_PROCESS},
		{name: "new process", number: unix.SYS_CLONE3, arguments: [6]uint64{}, allow: false, verdict: 0},
		{name: "ptrace", number: unix.SYS_PTRACE, arguments: [6]uint64{}, allow: false, verdict: unix.SECCOMP_RET_KILL_PROCESS},
		{name: "io uring", number: unix.SYS_IO_URING_SETUP, arguments: [6]uint64{}, allow: false, verdict: 0},
		{name: "chmod", number: unix.SYS_FCHMOD, arguments: [6]uint64{}, allow: false, verdict: 0},
		{name: "executable memory", number: unix.SYS_MPROTECT, arguments: [6]uint64{0, 0, unix.PROT_READ | unix.PROT_EXEC}, allow: false, verdict: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expected := uint32(denySyscall)
			if tt.allow {
				expected = unix.SECCOMP_RET_ALLOW
			}
			if tt.verdict != 0 {
				expected = tt.verdict
			}
			if actual := evaluateFilter(t, filter, workerAuditArchitecture, tt.number, tt.arguments); actual != expected {
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
