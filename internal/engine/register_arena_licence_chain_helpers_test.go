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

//go:build !safe && !(js && wasm) && (amd64 || arm64)

package engine

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func runningVMOnArena(arena *RegisterArena, callback bool) *VM {
	vm := &VM{reentrantInterpreterVM: callback}
	if callback {
		vm.debugKind = DebugThreadCallback
	}
	vm.Arena = arena
	arena.setLicenceVM(vm)
	return vm
}

func enterServedChains(t *testing.T, vms ...*VM) func() {
	t.Helper()
	for _, vm := range vms {
		require.True(t, vm.enterServedChain(), "test harness: the served chain must be engaged")
	}
	return func() {
		for _, vm := range slices.Backward(vms) {
			vm.leaveServedChain()
		}
	}
}

func skipWithoutGoroutineTokens(t *testing.T) {
	t.Helper()
	if !goroutineTokens {
		t.Skip("no goroutine identity in this build")
	}
}
