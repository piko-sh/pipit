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

//go:build !safe && !(js && wasm)

package engine

import (
	"reflect"
	"slices"
	"sync"
	"unsafe"
	"weak"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// closureWrapperShardMixShift folds higher bits of a closure's key into its shard index.
const closureWrapperShardMixShift = 10

const (
	// closureWrapperShards is the number of independently locked shards of the side table.
	closureWrapperShards = 64

	// closureWrapperSweepFloor is the shard size below which an insertion never sweeps dead
	// entries.
	closureWrapperSweepFloor = 64

	// flagWrapperMethod is reflect's flagMethod.
	flagWrapperMethod uintptr = 1 << 9
)

var (
	// closureWrappers maps a wrapper's function object to what it captured.
	closureWrappers [closureWrapperShards]closureWrapperShard

	// upvalueCellABIType is an upvalue cell's runtime type, the walk memo's key type.
	upvalueCellABIType = reflectValueABIType(reflect.TypeFor[program.UpvalueCell]())
)

// closureWrapperShard is one shard of closureWrappers, padded to its own cache lines.
type closureWrapperShard struct {
	// entries maps wrapper code pointers to closure metadata.
	entries map[uintptr]closureWrapperEntry

	// mu serialises access to the closure metadata cache.
	mu sync.Mutex

	// sweptSize records the table size at the last removal of expired wrappers.
	sweptSize int

	_ [32]byte
}

// closureWrapperEntry is what one wrapper captured.
//
// Every pointer is weak. The wrapper keeps what it captured alive for as long as it
// lives, and the entry is used only while the wrapper does.
type closureWrapperEntry struct {
	// wrapper names the wrapper's function object (reflect's makeFuncImpl).
	wrapper weak.Pointer[byte]

	// closure is the guest closure a closure wrapper runs, or a nil pointer.
	closure weak.Pointer[RuntimeClosure]

	// bound is the bound receiver of a method value, or a nil pointer.
	bound weak.Pointer[wrapperBoundReceiver]
}

// appendClosureChunkRanges appends a licence range for every closure chunk, free chunks
// included.
//
// Each free list follows its epoch's list in reverse. takeClosureChunk moves a free
// list's last chunk to the end of the epoch's list without publishing, so this order
// leaves that move out of the gathering, and the next publication's window holds only the
// chunks made since the last one (advancePublication).
//
// Takes out ([]licenceSlabRange) which receives the ranges.
// Takes owner (uint64) which is the arena's identity.
//
// Returns []licenceSlabRange which is out extended.
func (a *RegisterArena) appendClosureChunkRanges(out []licenceSlabRange, owner uint64) []licenceSlabRange {
	slabs := &a.closureChunks
	if len(slabs.cells)+len(slabs.freeCells)+len(slabs.closures)+len(slabs.freeClosures) == 0 {
		return out
	}
	class := a.closureChunkClass()
	out = appendLicenceList(out, owner, class, nil, slabs.cells)
	out = appendLicenceListBackwards(out, owner, class, slabs.freeCells)
	out = appendLicenceList(out, owner, class, nil, slabs.closures)
	return appendLicenceListBackwards(out, owner, class, slabs.freeClosures)
}

// visitFunc walks a registered wrapper's closure or bound receiver.
//
// Other function values are opaque, so it revokes the epoch.
//
// Takes value (reflect.Value) which is a non-nil function value.
// Takes depth (int) which is the remaining nesting allowance.
func (w *licenceWalk) visitFunc(value reflect.Value, depth int) {
	if closureWrapperWalksApply(w.arena) {
		if closure, bound, found := closureWrapperLookup(value); found {
			if licenceCounting {
				cellWalkCounters.wrappersFound.Add(1)
			}
			if closure != nil {
				w.visitClosure(closure, depth)
			}
			if bound != nil && bound.receiver.IsValid() {
				w.visit(bound.receiver, true, depth-1)
			}
			return
		}
		if licenceCounting {
			cellWalkCounters.wrappersOpaque.Add(1)
		}
	}
	w.opaque = true
	w.arena.revokeEpoch(epochOpaqueFunction, w.cause)
}

// visitTop is visit for the value a walk starts from.
//
// The closures and cells it records as visited are forgotten when it ends.
//
// Takes value (reflect.Value) which is the value handed off.
// Takes boxed (bool) which is true when value is reached through, or converted to, an
// interface.
// Takes depth (int) which is the nesting allowance.
func (w *licenceWalk) visitTop(value reflect.Value, boxed bool, depth int) {
	state := &w.arena.licenceState
	w.seenBase = len(state.wrapperSeen)
	w.visit(value, boxed, depth)
	clear(state.wrapperSeen[w.seenBase:])
	state.wrapperSeen = state.wrapperSeen[:w.seenBase]
}

// walkSeen reports whether the walk already visited the closure or cell at pointer,
// recording it otherwise, so a closure that captures itself (or cells shared by sibling
// closures) is walked once.
//
// Takes pointer (unsafe.Pointer) which is the object's address.
//
// Returns bool which is true when pointer was seen before.
func (w *licenceWalk) walkSeen(pointer unsafe.Pointer) bool {
	state := &w.arena.licenceState
	if slices.Contains(state.wrapperSeen[w.seenBase:], pointer) {
		return true
	}
	state.wrapperSeen = append(state.wrapperSeen, pointer)
	return false
}

// visitClosure walks a guest closure.
//
// It revokes the closure object and its cell array, which a func-type conversion's copy
// shares with the original, and walks each cell. Every field of RuntimeClosure is either
// a cell or code.
//
// Takes closure (*RuntimeClosure) which is the closure.
// Takes depth (int) which is the remaining nesting allowance.
func (w *licenceWalk) visitClosure(closure *RuntimeClosure, depth int) {
	if closure == nil || w.exhausted {
		return
	}
	if depth <= 0 || w.budget <= 0 {
		w.exhausted = true
		return
	}
	if w.walkSeen(unsafe.Pointer(closure)) {
		return
	}
	w.budget--
	if licenceCounting {
		cellWalkCounters.closuresWalked.Add(1)
	}
	w.revoke(uintptr(unsafe.Pointer(closure)))
	if len(closure.upvalues) != 0 {
		w.revoke(uintptr(unsafe.Pointer(unsafe.SliceData(closure.upvalues))))
	}
	for _, cell := range closure.upvalues {
		if w.exhausted {
			return
		}
		w.visitCell(cell, depth-1)
	}
}

// visitCell walks one upvalue cell.
//
// It revokes the cell's own storage (a closure chunk, for a placed cell), marks the cell
// handed off so every later store into it is a hand-off, and walks what the cell holds.
//
// Takes cell (*program.UpvalueCell) which is the cell.
// Takes depth (int) which is the remaining nesting allowance.
func (w *licenceWalk) visitCell(cell *program.UpvalueCell, depth int) {
	if cell == nil || w.exhausted {
		return
	}
	if depth <= 0 || w.budget <= 0 {
		w.exhausted = true
		return
	}
	pointer := unsafe.Pointer(cell)
	if w.memoCovers(reflect.ValueOf(cell), pointer, upvalueCellABIType, 0, depth) {
		return
	}
	if w.shared {
		w.sharedStop, w.exhausted = true, true
		return
	}
	if w.walkSeen(pointer) {
		return
	}
	defer w.memoNote(pointer, upvalueCellABIType, 0)
	w.budget--
	if licenceCounting {
		cellWalkCounters.cellsWalked.Add(1)
	}
	w.revoke(uintptr(unsafe.Pointer(cell)))
	markCellHandedOff(cell)
	if cell.IsIndirect {
		w.visit(cell.GeneralValue, false, depth-1)
		return
	}
	switch cell.Kind {
	case isa.RegisterGeneral:
		w.visit(cell.GeneralValue, true, depth-1)
	case isa.RegisterString:
		if len(cell.StringValue) != 0 {
			w.revoke(uintptr(unsafe.Pointer(unsafe.StringData(cell.StringValue))))
		}
	case isa.RegisterSliceInt64:
		w.visitSlice(reflect.ValueOf(cell.SliceIntValue), depth-1)
	case isa.RegisterSliceFloat64:
		w.visitSlice(reflect.ValueOf(cell.SliceFloatValue), depth-1)
	case isa.RegisterSliceString:
		w.visitSlice(reflect.ValueOf(cell.SliceStringValue), depth-1)
	case isa.RegisterSliceBool:
		w.visitSlice(reflect.ValueOf(cell.SliceBoolValue), depth-1)
	case isa.RegisterSliceUint64:
		w.visitSlice(reflect.ValueOf(cell.SliceUintValue), depth-1)
	case isa.RegisterSliceByte:
		w.visitSlice(reflect.ValueOf(cell.SliceByteValue), depth-1)
	default:
	}
}

// visitClosureOrCell walks a pointer to a guest closure or to an upvalue cell by its
// fields' meaning rather than reflectively.
//
// Takes value (reflect.Value) which is a non-nil pointer.
// Takes depth (int) which is the remaining nesting allowance.
//
// Returns bool which is true when value was one of the two and is walked.
func (w *licenceWalk) visitClosureOrCell(value reflect.Value, depth int) bool {
	switch value.Type() {
	case runtimeClosurePointerType:
		if !closureWrapperWalksApply(w.arena) {
			return false
		}
		w.visitClosure((*RuntimeClosure)(value.UnsafePointer()), depth)
		return true
	case upvalueCellPointerType:
		if !cellWalksApply(w.arena) {
			return false
		}
		w.visitCell((*program.UpvalueCell)(value.UnsafePointer()), depth)
		return true
	default:
		return false
	}
}

// dropRevokedClosureChunks detaches each revoked chunk at a release whose epoch is not
// revoked.
//
// It leaves the chunk, uncleared, to the collector, and adapts the next chunk length.
//
// Takes tally (*licenceReleaseTally) which counts the outcome.
func (a *RegisterArena) dropRevokedClosureChunks(tally *licenceReleaseTally) {
	slabs := &a.closureChunks
	a.settleClosureSites()
	if a.closureChunkCount() == 0 {
		return
	}
	before := a.closureChunkCount()
	slabs.cells = dropRevoked(a, slabs.cells, tally, nil)
	slabs.freeCells = dropRevoked(a, slabs.freeCells, tally, nil)
	slabs.closures = dropRevoked(a, slabs.closures, tally, nil)
	slabs.freeClosures = dropRevoked(a, slabs.freeClosures, tally, nil)
	if dropped := before - a.closureChunkCount(); dropped != 0 {
		a.adaptClosureChunk(true)
		if licenceCounting {
			closureChunkCounters.chunksRevoked.Add(int64(dropped))
		}

		slabs.cellIndex, slabs.closureIndex = closureChunkCurrentFull(slabs.cells), closureChunkCurrentFull(slabs.closures)
	}
}

// noteSpawnedClosureObject names the closure object a go statement hands to the new
// goroutine, as well as its cells.
//
// A placed closure lies in an arena chunk, which the goroutine reads after the go
// statement.
//
// Takes arena (*RegisterArena) which carries the licence.
// Takes closure (*RuntimeClosure) which is the goroutine's function.
func noteSpawnedClosureObject(arena *RegisterArena, closure *RuntimeClosure) {
	if !arena.licensed || !licenceExactProbes || arena.licenceState.epochRevoked {
		return
	}
	pointer := unsafe.Pointer(closure)
	if !arena.closureChunked(pointer) {
		return
	}
	arena.revokeAddress(uintptr(pointer), handOffSpawn)
	if len(closure.upvalues) != 0 {
		arena.revokeAddress(uintptr(unsafe.Pointer(unsafe.SliceData(closure.upvalues))), handOffSpawn)
	}
}

// closureChunked reports whether p lies in one of the arena's closure chunks.
//
// Takes p (unsafe.Pointer) which is the address.
//
// Returns bool which is true for a placed closure or cell.
func (a *RegisterArena) closureChunked(p unsafe.Pointer) bool {
	_, _, ok := a.closureChunkSlabOf(uintptr(p))
	return ok
}

// closureWrapperShardOf returns the side-table shard of a wrapper's function object.
//
// Takes key (uintptr) which is the object's address.
//
// Returns *closureWrapperShard which is the shard.
func closureWrapperShardOf(key uintptr) *closureWrapperShard {
	return &closureWrappers[(key>>pointerAlignShift^key>>closureWrapperShardMixShift)%closureWrapperShards]
}

// funcValueObject returns the function object a func value points at, or nil for a nil
// function or a method value made by reflect (whose object is not the value's own).
//
// Takes function (reflect.Value) which is a value of kind Func.
//
// Returns unsafe.Pointer which is the object.
func funcValueObject(function reflect.Value) unsafe.Pointer {
	raw := (*unsafeReflectValue)(unsafe.Pointer(&function))
	if raw.flag&flagWrapperMethod != 0 {
		return nil
	}
	pointer := raw.ptr
	if raw.flag&flagIndir != 0 && pointer != nil {
		pointer = *(*unsafe.Pointer)(pointer)
	}
	return pointer
}

// registerClosureWrapper records that wrapper, a reflect.MakeFunc result, runs closure.
//
// Takes wrapper (reflect.Value) which is the wrapper.
// Takes closure (*RuntimeClosure) which it runs.
func registerClosureWrapper(_ *VM, wrapper reflect.Value, closure *RuntimeClosure) {
	if !ClosureCaptureWalksCells() || closure == nil {
		return
	}
	closureWrapperRegister(wrapper, closureWrapperEntry{closure: weak.Make(closure), wrapper: weak.Pointer[byte]{}, bound: weak.Pointer[wrapperBoundReceiver]{}})
}

// registerBoundMethodWrapper records that wrapper, a reflect.MakeFunc result, is a method
// value bound to bound's receiver, in every build with cell walks
// (registerClosureWrapper).
//
// Takes wrapper (reflect.Value) which is the wrapper.
// Takes bound (*wrapperBoundReceiver) which holds the receiver the wrapper's closure
// uses.
func registerBoundMethodWrapper(_ *VM, wrapper reflect.Value, bound *wrapperBoundReceiver) {
	if !ClosureCaptureWalksCells() || bound == nil {
		return
	}
	closureWrapperRegister(wrapper, closureWrapperEntry{bound: weak.Make(bound), wrapper: weak.Pointer[byte]{}, closure: weak.Pointer[RuntimeClosure]{}})
}

// closureWrapperRegister adds entry for wrapper, sweeping the shard's dead entries once
// it has grown to twice its size after the previous sweep.
//
// Takes wrapper (reflect.Value) which is the wrapper.
// Takes entry (closureWrapperEntry) which is what it captured, without its wrapper
// pointer.
//
// Concurrency: Serialises updates and sweeping within the selected wrapper-table shard
// using its mutex.
func closureWrapperRegister(wrapper reflect.Value, entry closureWrapperEntry) {
	object := funcValueObject(wrapper)
	if object == nil {
		return
	}
	entry.wrapper = weak.Make((*byte)(object))
	key := uintptr(object)
	shard := closureWrapperShardOf(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	if shard.entries == nil {
		shard.entries = make(map[uintptr]closureWrapperEntry, closureWrapperSweepFloor)
	}
	shard.entries[key] = entry
	if len(shard.entries) >= max(closureWrapperSweepFloor, 2*shard.sweptSize) {
		for address, held := range shard.entries {
			if held.wrapper.Value() == nil {
				delete(shard.entries, address)
			}
		}
		shard.sweptSize = len(shard.entries)
	}
}

// closureWrapperLookup returns what the live wrapper function captured, when it is a
// registered closure or method-value wrapper.
//
// Takes function (reflect.Value) which is a non-nil value of kind Func.
//
// Returns *RuntimeClosure which is the closure it runs, or nil.
// Returns *wrapperBoundReceiver which holds its bound receiver, or nil.
// Returns bool which is false for a function value the table does not know.
//
// Concurrency: Reads the selected wrapper-table shard under its mutex; weak pointers are
// resolved after releasing it.
func closureWrapperLookup(function reflect.Value) (*RuntimeClosure, *wrapperBoundReceiver, bool) {
	object := funcValueObject(function)
	if object == nil {
		return nil, nil, false
	}
	key := uintptr(object)
	shard := closureWrapperShardOf(key)
	shard.mu.Lock()
	entry, found := shard.entries[key]
	shard.mu.Unlock()
	if !found || entry.wrapper.Value() != (*byte)(object) {
		return nil, nil, false
	}
	closure, bound := entry.closure.Value(), entry.bound.Value()
	if closure == nil && bound == nil {
		return nil, nil, false
	}
	return closure, bound, true
}

// closureChunkBase returns the start of a chunk list's current chunk.
//
// Takes list ([][]T) which is the list.
//
// Returns uintptr which is the start, or zero.
func closureChunkBase[T any](list [][]T) uintptr {
	if len(list) == 0 || len(list[len(list)-1]) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&list[len(list)-1][0]))
}

// closureChunkCurrentFull returns the index that makes the last chunk of list count as
// full, so the next placement takes a fresh chunk rather than writing past a kept one's
// used prefix.
//
// Takes list ([][]T) which is a chunk list.
//
// Returns int which is the length of the last chunk, or zero.
func closureChunkCurrentFull[T any](list [][]T) int {
	if len(list) == 0 {
		return 0
	}
	return len(list[len(list)-1])
}

// markCellHandedOff records that a licence walk named cell.
//
// Takes cell (*program.UpvalueCell) which is the cell.
func markCellHandedOff(cell *program.UpvalueCell) {
	if cell.HandedOff.Load() == 0 {
		cell.HandedOff.Store(1)
	}
}

// closureWrapperWalksApply reports whether closure hand-offs by arena walk the closure's
// cells, finding a wrapper's closure through the side table.
//
// Takes arena (*RegisterArena) which may be nil.
//
// Returns bool which is true when closure hand-offs walk the cells.
func closureWrapperWalksApply(arena *RegisterArena) bool {
	return cellWalksApply(arena)
}
