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
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
)

func TestACallbackRunPastTheLimitReportsTheDepthReached(t *testing.T) {
	type enteredRun struct {
		vm *VM

		place callbackRunPlace
	}
	var entered []enteredRun
	t.Cleanup(func() {
		for _, run := range slices.Backward(entered) {
			run.vm.leaveCallbackRun(run.place)
		}
	})
	var err error
	for range 1 << 10 {
		vm := newGCTestVM()
		vm.Limits.MaxCallDepth = 16
		vm.FramePointer = -1
		recovered := func() (recovered any) {
			defer func() { recovered = recover() }()
			entered = append(entered, enteredRun{vm: vm, place: vm.enterCallbackRun()})
			vm.FramePointer = 0
			return nil
		}()
		if recovered != nil {
			var isError bool
			err, isError = recovered.(error)
			require.True(t, isError, "the run panicked with %v", recovered)
			break
		}
	}
	require.ErrorIs(t, err, fault.ErrStackOverflow)
	require.Regexp(t, `call depth \d+ exceeded limit 16 `, err.Error(), "the depth is the one the run would have started at")
}

func TestANestedCallbackRunCountsTheFramesOfTheRunsBelow(t *testing.T) {
	if currentGoroutineToken() == 0 {
		t.Skip("this build cannot tell goroutines apart, so runs are counted together")
	}
	below := newGCTestVM()
	below.Limits.MaxCallDepth = 100
	belowPlace := below.enterCallbackRun()
	defer below.leaveCallbackRun(belowPlace)
	below.FramePointer = 2

	nested := newGCTestVM()
	nested.Limits.MaxCallDepth = 100
	nestedPlace := nested.enterCallbackRun()
	defer nested.leaveCallbackRun(nestedPlace)
	require.Equal(t, 3, nested.callbackDepthBase, "three live frames below, and the nested run's own frame counts from there")
}
