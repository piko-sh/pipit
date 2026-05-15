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
	"encoding/binary"
	"testing"

	"golang.org/x/sys/unix"
)

func evaluateFilter(t *testing.T, filter []unix.SockFilter, architecture, number uint32, arguments [6]uint64) uint32 {
	t.Helper()
	var data [64]byte
	binary.LittleEndian.PutUint32(data[:4], number)
	binary.LittleEndian.PutUint32(data[4:8], architecture)
	for index, argument := range arguments {
		binary.LittleEndian.PutUint64(data[16+index*8:], argument)
	}
	var accumulator uint32
	for position := 0; position < len(filter); position++ {
		operation := filter[position]
		switch operation.Code {
		case unix.BPF_LD | unix.BPF_W | unix.BPF_ABS:
			if operation.K > uint32(len(data)-4) {
				t.Fatal("out-of-bounds filter load")
			}
			accumulator = binary.LittleEndian.Uint32(data[operation.K:])
		case unix.BPF_ALU | unix.BPF_AND | unix.BPF_K:
			accumulator &= operation.K
		case unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K:
			if accumulator == operation.K {
				position += int(operation.Jt)
			} else {
				position += int(operation.Jf)
			}
		case unix.BPF_RET | unix.BPF_K:
			return operation.K
		default:
			t.Fatalf("unexpected instruction: %+v", operation)
		}
	}
	t.Fatal("filter ran past its final verdict")
	return 0
}

func TestWorkerFilterPolicy(t *testing.T) {
	t.Parallel()
	filter, err := workerFilter(42)
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
		{name: "ipc read", number: unix.SYS_READ, arguments: [6]uint64{3}, allow: true},
		{name: "stdin read", number: unix.SYS_READ, arguments: [6]uint64{0}, allow: false},
		{name: "stderr write", number: unix.SYS_WRITE, arguments: [6]uint64{2}, allow: true},
		{name: "unknown fd write", number: unix.SYS_WRITE, arguments: [6]uint64{4}, allow: false},
		{name: "high fd bits", number: unix.SYS_WRITE, arguments: [6]uint64{1<<32 | 2}, allow: false},
		{name: "Go thread", number: unix.SYS_CLONE, arguments: [6]uint64{goThreadCloneFlags}, allow: true},
		{name: "fork clone", number: unix.SYS_CLONE, arguments: [6]uint64{uint64(unix.SIGCHLD)}, allow: false},
		{name: "new namespace", number: unix.SYS_CLONE, arguments: [6]uint64{goThreadCloneFlags | unix.CLONE_NEWUSER}, allow: false},
		{name: "clone3", number: unix.SYS_CLONE3, arguments: [6]uint64{}, allow: false},
		{name: "anonymous mapping", number: unix.SYS_MMAP, arguments: [6]uint64{0, 4096, unix.PROT_READ | unix.PROT_WRITE, unix.MAP_PRIVATE | unix.MAP_ANONYMOUS}, allow: true},
		{name: "file mapping", number: unix.SYS_MMAP, arguments: [6]uint64{0, 4096, unix.PROT_READ, unix.MAP_PRIVATE}, allow: false},
		{name: "executable mapping", number: unix.SYS_MMAP, arguments: [6]uint64{0, 4096, unix.PROT_EXEC, unix.MAP_PRIVATE | unix.MAP_ANONYMOUS}, allow: false},
		{name: "hugetlb mapping", number: unix.SYS_MMAP, arguments: [6]uint64{0, 4096, unix.PROT_READ, unix.MAP_PRIVATE | unix.MAP_ANONYMOUS | unix.MAP_HUGETLB}, allow: false},
		{name: "executable protection", number: unix.SYS_MPROTECT, arguments: [6]uint64{0, 4096, unix.PROT_EXEC}, allow: false},
		{name: "self preemption", number: unix.SYS_TGKILL, arguments: [6]uint64{42, 43, uint64(unix.SIGURG)}, allow: true},
		{name: "other process signal", number: unix.SYS_TGKILL, arguments: [6]uint64{41, 43, uint64(unix.SIGURG)}, allow: false},
		{name: "wrong signal", number: unix.SYS_TGKILL, arguments: [6]uint64{42, 43, uint64(unix.SIGKILL)}, allow: false},
		{name: "open", number: unix.SYS_OPENAT, arguments: [6]uint64{}, verdict: denySyscall},
		{name: "future syscall", number: ^uint32(0), arguments: [6]uint64{}, verdict: denySyscall},
		{name: "socket", number: unix.SYS_SOCKET, arguments: [6]uint64{}, allow: false},
		{name: "io uring", number: unix.SYS_IO_URING_SETUP, arguments: [6]uint64{}, allow: false},
		{name: "exec", number: unix.SYS_EXECVE, arguments: [6]uint64{}, verdict: unix.SECCOMP_RET_KILL_PROCESS},
		{name: "execveat", number: unix.SYS_EXECVEAT, arguments: [6]uint64{}, verdict: unix.SECCOMP_RET_KILL_PROCESS},
		{name: "ptrace", number: unix.SYS_PTRACE, arguments: [6]uint64{}, verdict: unix.SECCOMP_RET_KILL_PROCESS},
		{name: "unshare", number: unix.SYS_UNSHARE, arguments: [6]uint64{}, verdict: unix.SECCOMP_RET_KILL_PROCESS},
		{name: "mount", number: unix.SYS_MOUNT, arguments: [6]uint64{}, verdict: unix.SECCOMP_RET_KILL_PROCESS},
	} {
		t.Run(test.name, func(t *testing.T) {
			verdict := evaluateFilter(t, filter, workerAuditArchitecture, test.number, test.arguments)
			want := uint32(denySyscall)
			if test.allow {
				want = unix.SECCOMP_RET_ALLOW
			}
			if test.verdict != 0 {
				want = test.verdict
			}
			if verdict != want {
				t.Fatalf("verdict=%x want=%x", verdict, want)
			}
		})
	}
	if verdict := evaluateFilter(t, filter, 0, unix.SYS_WRITE, [6]uint64{2}); verdict != unix.SECCOMP_RET_KILL_PROCESS {
		t.Fatalf("foreign ABI accepted: %x", verdict)
	}
}

func TestWorkerFilterInvalidArguments(t *testing.T) {
	t.Parallel()
	if _, err := workerFilter(0); err == nil {
		t.Fatal("invalid PID accepted")
	}
	for _, argument := range []argumentRule{
		{index: 6, mask: allArgumentBits, allowed: []uint32{0}},
		{index: 0, mask: allArgumentBits, allowed: nil},
		{index: 0, mask: allArgumentBits, allowed: make([]uint32, 256)},
	} {
		if _, err := ruleBody([]argumentRule{argument}); err == nil {
			t.Fatal("invalid argument rule accepted")
		}
	}
}
