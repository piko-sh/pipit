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
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
)

func TestLicenceCheckDefectAbandonsTheVMsArena(t *testing.T) {
	rejected, accepted := &program.CompiledFunction{Name: "rejected"}, &program.CompiledFunction{Name: "accepted"}
	NoteLicenceCheck([]*program.CompiledFunction{rejected}, true)
	NoteLicenceCheck([]*program.CompiledFunction{accepted}, false)

	vm := &VM{Limits: VMLimits{}}
	vm.applyLicenceCheck(accepted)
	require.False(t, vm.Limits.abandonsArenas())
	require.True(t, vm.Limits.licensesArenas())

	vm.applyLicenceCheck(rejected)
	require.True(t, vm.Limits.abandonsArenas(), "a rejected program's VM reuses no arena storage")
	require.True(t, vm.Limits.DisableMinorGC, "nor compacts its arena within the run")
	require.False(t, vm.Limits.licensesArenas(), "abandoning takes precedence over the licence")

	arena := acquireRegisterArena(vm.Limits.abandonsArenas())
	require.True(t, arena.unpooled, "the arena is built fresh, outside the pool")
	releaseRegisterArena(vm.Limits.abandonsArenas(), arena, false)
}

func TestLicenceCheckDefectDoesNotKeepTheProgramAlive(t *testing.T) {
	collected := make(chan struct{})
	func() {
		rejected := &program.CompiledFunction{Name: "rejected"}
		runtime.AddCleanup(rejected, func(done chan struct{}) { close(done) }, collected)
		NoteLicenceCheck([]*program.CompiledFunction{rejected}, true)
	}()

	require.Eventually(t, func() bool {
		runtime.GC()
		select {
		case <-collected:
			return true
		default:
			return false
		}
	}, 5*time.Second, 10*time.Millisecond, "a rejected program is collected once nothing else holds it")
}
