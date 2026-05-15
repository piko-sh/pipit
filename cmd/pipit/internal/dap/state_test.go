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

	"pipit.sh/pipit"
	"pipit.sh/pipit/internal/debug"
)

func TestStopStateAllocatorReservesZero(t *testing.T) {
	stop := newStopState(pipit.DebugEvent{})

	id := stop.allocateContainer(variableContainer{kind: containerKindReflectValue, value: reflect.ValueOf(42)})
	if id == 0 {
		t.Fatalf("allocateContainer returned 0; expected 1+ to leave 0 as DAP's no-children sentinel")
	}
}

func TestStopStateLookupContainer(t *testing.T) {
	stop := newStopState(pipit.DebugEvent{})
	id := stop.allocateContainer(variableContainer{kind: containerKindReflectValue, value: reflect.ValueOf("hello")})

	got, ok := stop.lookupContainer(id)
	if !ok {
		t.Fatalf("lookupContainer(%d) reported missing", id)
	}
	if got.kind != containerKindReflectValue {
		t.Fatalf("kind: got %v, want containerKindReflectValue", got.kind)
	}
	if !got.value.IsValid() || got.value.String() != "hello" {
		t.Fatalf("value: got %v, want \"hello\"", got.value)
	}
}

func TestStopStateLookupContainerRejectsZeroAndOOB(t *testing.T) {
	stop := newStopState(pipit.DebugEvent{})

	if _, ok := stop.lookupContainer(0); ok {
		t.Fatalf("lookupContainer(0) succeeded; DAP reserves 0 for the no-children sentinel")
	}
	if _, ok := stop.lookupContainer(99); ok {
		t.Fatalf("lookupContainer(99) succeeded; expected miss for unallocated id")
	}
	if _, ok := stop.lookupContainer(-1); ok {
		t.Fatalf("lookupContainer(-1) succeeded; expected miss for negative id")
	}
}

func TestStopStateAllocateFrameRoundTrip(t *testing.T) {
	stop := newStopState(pipit.DebugEvent{})

	id := stop.allocateFrame(frameRef{threadID: 7, frameIndex: 3})
	if id == 0 {
		t.Fatalf("allocateFrame returned 0; expected real id")
	}
	if again := stop.allocateFrame(frameRef{threadID: 7, frameIndex: 3}); again != id {
		t.Fatalf("allocateFrame issued %d then %d for the same frame; want a stable id", id, again)
	}
	frame, ok := stop.frameFor(id)
	if !ok {
		t.Fatalf("frameFor(%d) missing", id)
	}
	if frame.threadID != 7 || frame.frameIndex != 3 {
		t.Fatalf("frame: got %+v, want thread 7 frame 3", frame)
	}
	container, ok := stop.lookupContainer(id)
	if !ok || container.kind != containerKindScope || container.scope != debug.ScopeLocals {
		t.Fatalf("a frame id expands to the locals scope; got %+v", container)
	}
}
