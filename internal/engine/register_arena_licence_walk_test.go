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
	"testing"
	"unsafe"
)

func TestUnregisteredOwnStorageIsAnInvariantFailure(t *testing.T) {
	if !licencePageCheck {
		t.Skip("the page table is checked in the reuse audit build")
	}
	arena := &RegisterArena{byteSlab: make([]byte, 1<<20)}
	inside := uintptr(unsafe.Pointer(&arena.byteSlab[1<<19]))
	if slabPagesMayHold(inside) {
		t.Skip("another arena's slab shares the page")
	}

	requireInvariantPanic(t, func() { licenceRegisteredPage(arena, inside) })
}
