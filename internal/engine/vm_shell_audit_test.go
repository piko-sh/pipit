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

//go:build pipit_reuse_audit

package engine

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuditVMShellsCountsTheShellsStillReachable(t *testing.T) {
	AuditVMShells()
	held := &VM{callbackVMs: &callbackVMCache{}}
	cache := held.callbackVMs
	auditRecordVMShell(held)
	require.Nil(t, held.callbackVMs, "the audit takes the cache from the abandoned shell")
	auditRecordVMShell(&VM{})

	recorded, reachable := AuditVMShells()
	require.Equal(t, 2, recorded)
	require.Equal(t, 2, reachable, "the held shell and its cache are reachable; the dropped one is not")
	runtime.KeepAlive(held)
	runtime.KeepAlive(cache)

	recorded, reachable = AuditVMShells()
	require.Zero(t, recorded, "the audit forgets what it reported")
	require.Zero(t, reachable)
}
