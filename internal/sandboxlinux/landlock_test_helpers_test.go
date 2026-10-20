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
	"fmt"
	"testing"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/sandboxtest"
)

func requireLandlockTSYNC(t *testing.T) {
	t.Helper()
	version, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 || version < 8 {
		sandboxtest.SkipUnlessRequired(t, "PIPIT_REQUIRE_BROKER_LANDLOCK", fmt.Errorf("Landlock TSYNC unavailable: %d: %w", version, errno))
	}
}
