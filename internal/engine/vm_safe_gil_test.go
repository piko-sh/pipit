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

package engine

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAGatedRunHoldsNoGateWhileItWaitsForTheInterpreterLock(t *testing.T) {
	gate, _ := newTimedExportGate(t)
	vm := gatedRunOf(t, gate)
	vm.Globals = &GlobalStore{}
	vm.Globals.interpreterLock.Lock()
	vm.holdsInterpreterLock = true
	var yielded chan struct{}
	for attempt := 0; ; attempt++ {
		require.Less(t, attempt, 10000, "the run never let the interpreter lock go to a waiting owner")
		yielded = make(chan struct{})
		go func() {
			vm.yieldInterpreterLock()
			close(yielded)
		}()
		if lockWhileTheRunYields(&vm.Globals.interpreterLock, yielded) {
			break
		}
	}
	readers := gate.readers.Load()
	held := gate.enterOwner()
	gate.leaveOwner()
	vm.Globals.interpreterLock.Unlock()
	<-yielded
	require.Zero(t, readers, "the run let its gate go while it waited for the interpreter lock")
	require.True(t, held, "so the owner holding the lock ran its section unshared")
	require.EqualValues(t, 1, gate.readers.Load(), "and the run took its gate back with the lock")
	vm.Globals.interpreterLock.Unlock()
	vm.holdsInterpreterLock = false
	vm.endGatedRun()
}

func lockWhileTheRunYields(lock *sync.Mutex, yielded <-chan struct{}) bool {
	for {
		if lock.TryLock() {
			return true
		}
		select {
		case <-yielded:
			return false
		default:
		}
	}
}
