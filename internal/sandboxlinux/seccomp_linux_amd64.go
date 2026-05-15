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
	// workerAuditArchitecture is the amd64 audit architecture constant.
	workerAuditArchitecture = unix.AUDIT_ARCH_X86_64

	// archSetFS is the ARCH_SET_FS subcommand for arch_prctl on amd64.
	archSetFS = 0x1002
)

// architectureRules supplies amd64 runtime operations absent from generic Linux.
//
// Returns []syscallRule which permits Go thread TLS and legacy epoll waiting.
func architectureRules() []syscallRule {
	return []syscallRule{
		{number: unix.SYS_ARCH_PRCTL, arguments: []argumentRule{{index: 0, mask: allArgumentBits, allowed: []uint32{archSetFS}}}},
		{number: unix.SYS_EPOLL_WAIT, arguments: nil},
	}
}

// threadCloneFlags lists exact clone flag combinations used by Go on amd64.
//
// Returns []uint32 which permits shared-process threads with optional TLS setup.
func threadCloneFlags() []uint32 {
	return []uint32{goThreadCloneFlags, goThreadCloneFlags | unix.CLONE_SETTLS}
}
