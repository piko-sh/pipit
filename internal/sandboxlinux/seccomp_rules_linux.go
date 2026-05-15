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

import "golang.org/x/sys/unix"

const (
	// allArgumentBits is a mask selecting every bit of a 32-bit argument.
	allArgumentBits = ^uint32(0)

	// futexWaitPrivate is the private futex wait operation constant.
	futexWaitPrivate = 128

	// futexWakePrivate is the private futex wake operation constant.
	futexWakePrivate = 129

	// mmapFlagsArgument is the syscall argument index for mmap flags.
	mmapFlagsArgument = 3
)

// runtimeRules describes the reviewed pure-Go runtime syscall surface.
//
// Takes processID (uint32) which limits asynchronous pre-emption signals to self.
// Takes wakeDescriptors ([]uint32) which are verified runtime eventfd handles.
//
// Returns []syscallRule which excludes process creation, file access and networking.
func runtimeRules(processID uint32, wakeDescriptors []uint32) []syscallRule {
	readDescriptors := append([]uint32{workerIPCFD}, wakeDescriptors...)
	writeDescriptors := append([]uint32{1, 2, workerIPCFD}, wakeDescriptors...)
	rules := []syscallRule{
		{number: unix.SYS_READ, arguments: []argumentRule{{index: 0, mask: allArgumentBits, allowed: readDescriptors}}},
		{number: unix.SYS_WRITE, arguments: []argumentRule{{index: 0, mask: allArgumentBits, allowed: writeDescriptors}}},
		{number: unix.SYS_WRITEV, arguments: []argumentRule{{index: 0, mask: allArgumentBits, allowed: []uint32{1, 2, workerIPCFD}}}},
		{number: unix.SYS_CLONE, arguments: []argumentRule{{index: 0, mask: allArgumentBits, allowed: threadCloneFlags()}}},
		{number: unix.SYS_TGKILL, arguments: []argumentRule{
			{index: 0, mask: allArgumentBits, allowed: []uint32{processID}},
			{index: 2, mask: allArgumentBits, allowed: []uint32{uint32(unix.SIGURG)}},
		}},
		{number: unix.SYS_FUTEX, arguments: []argumentRule{{index: 1, mask: allArgumentBits, allowed: []uint32{futexWaitPrivate, futexWakePrivate}}}},
		{number: unix.SYS_MMAP, arguments: []argumentRule{
			{index: 2, mask: ^uint32(unix.PROT_READ | unix.PROT_WRITE), allowed: []uint32{0}},
			{index: mmapFlagsArgument, mask: ^uint32(unix.MAP_PRIVATE | unix.MAP_ANONYMOUS | unix.MAP_FIXED | unix.MAP_NORESERVE), allowed: []uint32{0}},
			{index: mmapFlagsArgument, mask: unix.MAP_PRIVATE | unix.MAP_SHARED | unix.MAP_ANONYMOUS, allowed: []uint32{unix.MAP_PRIVATE | unix.MAP_ANONYMOUS}},
		}},
		{number: unix.SYS_MPROTECT, arguments: []argumentRule{{index: 2, mask: ^uint32(unix.PROT_READ | unix.PROT_WRITE), allowed: []uint32{0}}}},
		{number: unix.SYS_SCHED_GETAFFINITY, arguments: []argumentRule{{index: 0, mask: allArgumentBits, allowed: []uint32{0, processID}}}},
	}
	for _, number := range []uint32{
		unix.SYS_CLOSE, unix.SYS_MUNMAP, unix.SYS_MADVISE, unix.SYS_MINCORE,
		unix.SYS_BRK, unix.SYS_RT_SIGACTION, unix.SYS_RT_SIGPROCMASK,
		unix.SYS_RT_SIGRETURN, unix.SYS_SIGALTSTACK, unix.SYS_SCHED_YIELD,
		unix.SYS_NANOSLEEP, unix.SYS_CLOCK_GETTIME, unix.SYS_GETPID,
		unix.SYS_GETTID, unix.SYS_EXIT, unix.SYS_EXIT_GROUP, unix.SYS_GETRANDOM,
		unix.SYS_EPOLL_CREATE1, unix.SYS_EPOLL_CTL, unix.SYS_EPOLL_PWAIT,
		unix.SYS_EVENTFD2,
	} {
		rules = append(rules, syscallRule{number: number, arguments: nil})
	}
	return append(rules, architectureRules()...)
}

// killRules names syscalls that terminate the process instead of returning EPERM. These
// are calls a pure-Go worker never issues and only a compromised one would.
//
// Returns []uint32 which are the syscall numbers to terminate on.
func killRules() []uint32 {
	return []uint32{
		unix.SYS_EXECVE, unix.SYS_EXECVEAT, unix.SYS_PTRACE,
		unix.SYS_PROCESS_VM_READV, unix.SYS_PROCESS_VM_WRITEV,
		unix.SYS_MOUNT, unix.SYS_UMOUNT2, unix.SYS_PIVOT_ROOT, unix.SYS_CHROOT,
		unix.SYS_SETNS, unix.SYS_UNSHARE, unix.SYS_BPF, unix.SYS_KEYCTL,
		unix.SYS_KEXEC_LOAD, unix.SYS_INIT_MODULE, unix.SYS_FINIT_MODULE,
		unix.SYS_OPEN_BY_HANDLE_AT,
	}
}
