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
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
)

// upvalueCellPointerType is the type of a pointer to an upvalue cell.
var upvalueCellPointerType = reflect.TypeFor[*program.UpvalueCell]()

// markClosureCells is the licensed mark's trace of a guest closure. It marks what the
// closure's cells hold.
//
// Takes arena (*RegisterArena) which owns the slabs being marked.
// Takes state (*gcMarkState) which records the marks.
// Takes closure (*RuntimeClosure) which is the closure. Nil is a no-op.
// Takes depth (int) which is the closure's distance from its root.
func markClosureCells(arena *RegisterArena, state *gcMarkState, closure *RuntimeClosure, depth int) {
	if closure == nil || state.markStopsAt(depth) || !state.firstVisit(unsafe.Pointer(closure), runtimeClosureReflectType, 0) {
		return
	}
	for _, cell := range closure.upvalues {
		markCellContents(arena, state, cell, depth)
	}
}

// markCellContents marks an upvalue cell's contents for a licensed collection.
//
// A shared mark reads only a cell its own VM made and kept (readsCell).
//
// Takes arena (*RegisterArena) which owns the slabs being marked.
// Takes state (*gcMarkState) which records the marks.
// Takes cell (*program.UpvalueCell) which is the cell. Nil is a no-op.
// Takes depth (int) which is the cell's distance from its root.
func markCellContents(arena *RegisterArena, state *gcMarkState, cell *program.UpvalueCell, depth int) {
	if cell == nil || state.markStopsAt(depth) || !state.readsCell(arena, cell) || !state.firstVisit(unsafe.Pointer(cell), upvalueCellPointerType, 0) {
		return
	}
	markString(arena, state, cell.StringValue)
	if cell.GeneralValue.IsValid() {
		state.pushMark(cell.GeneralValue, depth+1)
	}
	markIntSlice(arena, state, cell.SliceIntValue)
	markFloatSlice(arena, state, cell.SliceFloatValue)
	markStringSlice(arena, state, cell.SliceStringValue)
	markBoolSlice(arena, state, cell.SliceBoolValue)
	markUintSlice(arena, state, cell.SliceUintValue)
	markByteSlice(arena, state, cell.SliceByteValue)
}
