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
	"sync/atomic"

	"pipit.sh/pipit/internal/engine/program"
)

// upvalueCellMakers issues the tags that name the epoch whose VM made an upvalue cell
// (cellMaker). Zero is never issued.
var upvalueCellMakers atomic.Uint64

// sharedMarkApplies reports whether a licensed collection of the arena may mark while
// another goroutine shares its VM's memory, reading only what the VM holds privately
// (readsPrivately).
//
// It applies to the VM a guest go statement started, which shares memory with other
// goroutines from its first instruction and starts none itself. The epoch must be
// unrevoked, since a revoked epoch recycles nothing. A Go heap object the VM made is not
// read, because the arena keeps no record of which Go allocations its VM made.
//
// Takes vm (*VM) which is the collecting VM.
//
// Returns bool which is true when the mark may run.
func (a *RegisterArena) sharedMarkApplies(vm *VM) bool {
	return vm != nil && vm == a.licenceVM && vm.debugKind == debugThreadGoroutine && !vm.reentrantInterpreterVM &&
		!vm.spawnedGoroutines && !a.licenceState.epochRevoked
}

// cellMaker returns the tag the arena's VM writes into each upvalue cell it makes in the
// epoch, drawing a fresh one from upvalueCellMakers for the epoch's first cell.
//
// Returns uint64 which is the tag, or zero for a nil arena.
func (a *RegisterArena) cellMaker() uint64 {
	if a == nil {
		return 0
	}
	if a.licenceState.cellMaker == 0 {
		a.licenceState.cellMaker = upvalueCellMakers.Add(1)
	}
	return a.licenceState.cellMaker
}

// readsCell reports whether a shared mark may read an upvalue cell.
//
// The arena's VM must have made it in the epoch (its tag is the epoch's cellMaker), no
// walk may have handed it off, and no walk that might have reached it may be pending as a
// blind hand-off. A cell another VM made is skipped, since every store this VM makes into
// it is a hand-off. One this VM made fails the mark closed once handed off or while a
// blind hand-off is pending.
//
// Takes arena (*RegisterArena) which is the collected arena.
// Takes cell (*program.UpvalueCell) which is a non-nil cell.
//
// Returns bool which is true when the mark reads the cell.
func (state *gcMarkState) readsCell(arena *RegisterArena, cell *program.UpvalueCell) bool {
	if !state.shared {
		return true
	}
	if cell.Maker == 0 || cell.Maker != arena.licenceState.cellMaker {
		return false
	}
	if cellHandedOff(cell) || arena.blindSinceReading() {
		state.failClose(failClosedShared)
		return false
	}
	return true
}

// readsPrivately reports whether the mark may read the memory at address.
//
// Takes arena (*RegisterArena) which is the collected arena.
// Takes address (uintptr) which is the memory about to be read.
//
// Returns bool which is true when the mark may read it.
func (state *gcMarkState) readsPrivately(arena *RegisterArena, address uintptr) bool {
	if !state.shared || markOwnsPrivately(arena, address) {
		return true
	}
	state.failClose(failClosedShared)
	return false
}

// readsValuePrivately is readsPrivately for the memory value refers to, when marking
// value reads it (markReadsReferent).
//
// Takes arena (*RegisterArena) which is the collected arena.
// Takes value (reflect.Value) which is the value about to be marked.
//
// Returns bool which is true when the mark may go on.
func (state *gcMarkState) readsValuePrivately(arena *RegisterArena, value reflect.Value) bool {
	if !state.shared || !markReadsReferent(value) {
		return true
	}
	return state.readsPrivately(arena, uintptr(ReflectValuePtr(value)))
}

// markReadsReferent reports whether marking value reads the memory it refers to. It does
// when the value is held there rather than in the reflect.Value itself, and its kind
// holds references the mark follows.
//
// Takes value (reflect.Value) which is a valid value.
//
// Returns bool which is true when the mark reads the referent.
func markReadsReferent(value reflect.Value) bool {
	if !reflectValueIndirect(value) {
		return false
	}
	switch value.Kind() {
	case reflect.Struct, reflect.Array:
		return !typeIsPointerFree(value.Type())
	case reflect.Slice, reflect.String, reflect.Interface, reflect.Pointer, reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return true
	default:
		return false
	}
}
