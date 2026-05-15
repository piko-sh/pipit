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
	"reflect"
)

// promoteAddressedValue gives an arena-resident composite its own storage before its
// address is taken.
//
// Copies to the arena when the escape pass says the pointer stays in the frame, or to a
// heap-backed boundary snapshot otherwise.
//
// Takes vm (*VM) which provides the arena.
// Takes frame (*CallFrame) whose function carries the escape annotations; the frame's
// programCounter has already advanced past the isa.OpAddr word.
// Takes v (reflect.Value) which is the arena-resident source value.
//
// Returns the promoted copy.
func promoteAddressedValue(vm *VM, frame *CallFrame, v reflect.Value) reflect.Value {
	if frame.Function.ArenaSafeAllocPCs[frame.ProgramCounter-1] {
		return copyReflectValueArena(vm.Arena, v)
	}
	return vm.snapshotToBoundary(v)
}

// snapshotToBoundary copies v into a fresh addressable slot in the VM's boundary-snapshot
// slab.
//
// Takes v (reflect.Value) which is the value to copy.
//
// Returns reflect.Value which is the addressable heap slot holding the copy.
func (vm *VM) snapshotToBoundary(v reflect.Value) reflect.Value {
	slot := vm.acquireBoundarySnapshot(v.Type())
	slot.Set(v)
	return slot
}
