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

package dap

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
	"pipit.sh/pipit/internal/debug"
)

func TestStopStateAllocatorReservesZero(t *testing.T) {
	t.Parallel()
	stop := newStopState(pipit.DebugEvent{})

	id := stop.allocateContainer(variableContainer{kind: containerKindReflectValue, value: reflect.ValueOf(42)})
	require.NotEqual(t, 0, id, "allocateContainer never returns 0, which DAP keeps for no children")
}

func TestStopStateLookupContainer(t *testing.T) {
	t.Parallel()
	stop := newStopState(pipit.DebugEvent{})
	id := stop.allocateContainer(variableContainer{kind: containerKindReflectValue, value: reflect.ValueOf("hello")})

	got, ok := stop.lookupContainer(id)
	require.True(t, ok, "lookupContainer(%d) finds the container", id)
	require.Equal(t, containerKindReflectValue, got.kind)
	require.True(t, got.value.IsValid())
	require.Equal(t, "hello", got.value.String())
}

func TestStopStateLookupContainerRejectsZeroAndOOB(t *testing.T) {
	t.Parallel()
	stop := newStopState(pipit.DebugEvent{})

	_, ok := stop.lookupContainer(0)
	require.False(t, ok, "id 0 has no container, because DAP reserves it for no children")
	_, ok = stop.lookupContainer(99)
	require.False(t, ok, "an unallocated id has no container")
	_, ok = stop.lookupContainer(-1)
	require.False(t, ok, "a negative id has no container")
}

func TestStopStateAllocateFrameRoundTrip(t *testing.T) {
	t.Parallel()
	stop := newStopState(pipit.DebugEvent{})

	id := stop.allocateFrame(frameRef{threadID: 7, frameIndex: 3})
	require.NotEqual(t, 0, id, "allocateFrame returns a real id")
	require.Equal(t, id, stop.allocateFrame(frameRef{threadID: 7, frameIndex: 3}), "allocateFrame reuses the id for the same frame")
	frame, ok := stop.frameFor(id)
	require.True(t, ok, "frameFor(%d) finds the frame", id)
	require.Equal(t, uint64(7), frame.threadID, "the frame belongs to thread 7, frame 3")
	require.Equal(t, 3, frame.frameIndex)
	container, ok := stop.lookupContainer(id)
	require.True(t, ok, "a frame id expands to the locals scope: %+v", container)
	require.Equal(t, containerKindScope, container.kind)
	require.Equal(t, debug.ScopeLocals, container.scope)
}
