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

// workerAuditArchitecture is the arm64 audit architecture constant.
const workerAuditArchitecture = unix.AUDIT_ARCH_AARCH64

// architectureRules supplies arm64-specific additions to the common profile.
//
// Returns []syscallRule which is empty because arm64 needs no extra syscalls.
func architectureRules() []syscallRule {
	return nil
}

// threadCloneFlags lists the exact clone flags used by Go on arm64.
//
// Returns []uint32 which permits only shared-process runtime threads.
func threadCloneFlags() []uint32 {
	return []uint32{goThreadCloneFlags}
}
