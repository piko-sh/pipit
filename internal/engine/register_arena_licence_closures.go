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
	"sync"
	"sync/atomic"
	"weak"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// closureChunkInitial is the length of a new chunk in an arena that has no history.
	closureChunkInitial = 8

	// closureChunkMin is the least length of a new chunk.
	closureChunkMin = 1

	// closureChunkMax is the greatest length of a new chunk.
	closureChunkMax = 256

	// closureChunkFreeMax bounds each free list a pooled arena keeps between epochs.
	closureChunkFreeMax = 64

	// spawnClosureCacheMax bounds the programs programSpawns remembers.
	spawnClosureCacheMax = 4096
)

// spareClosureChunks is how many spare chunks takeClosureChunk adds to the free list when
// it has to make a new chunk.
const spareClosureChunks = 3

const (
	// closureMaxSites bounds the placement entries an epoch records.
	closureMaxSites = 64
)

var (
	// spawnClosureCache remembers, per program root, whether the program has a go statement.
	spawnClosureCache struct {
		roots map[weak.Pointer[program.CompiledFunction]]bool
		// mu serialises access to the closure metadata cache.
		mu sync.Mutex
	}

	// closureChunkCounters counts closure chunk placements while the licence counters are
	// on.
	closureChunkCounters struct {
		closures, cells, chunks, chunksRevoked atomic.Int64
	}
)

// closureChunkSlabs holds the closure and cell chunks of one arena.
type closureChunkSlabs struct {
	// sites records each placement site and its chunk for settleClosureSites.
	//
	// It holds at most closureMaxSites entries, with a new entry whenever the site or chunk
	// changes.
	sites []closureSite

	// cells holds the epoch's cell chunks in allocation order; the last is current.
	cells [][]program.UpvalueCell

	// freeCells holds cleared cell chunks for the epoch to take before making new ones.
	freeCells [][]program.UpvalueCell

	// closures holds the epoch's closure chunks in allocation order; the last is current.
	closures [][]RuntimeClosure

	// freeClosures holds cleared closure chunks.
	freeClosures [][]RuntimeClosure

	// cellIndex and closureIndex are the next free slots of the current chunks.
	cellIndex, closureIndex int

	// chunk is the length of the next new chunk; zero means closureChunkInitial.
	chunk int

	// madeChunks counts the chunks the epoch took or made.
	madeChunks int
}

// ClosureChunkStats counts closure and cell placements in closure chunks since the
// process started.
type ClosureChunkStats struct {
	// Closures and Cells count the closures and cells placed in closure chunks.
	Closures, Cells int64

	// Chunks counts the chunks taken or made; ChunksRevoked those a release detached.
	Chunks, ChunksRevoked int64
}

var (
	// cellWalkCounters counts closure and cell walks while the licence counters are on.
	cellWalkCounters struct {
		closuresWalked, cellsWalked, wrappersFound, wrappersOpaque, handedOffStores atomic.Int64
	}
)

// wrapperBoundReceiver is the bound receiver of a method value, held by the wrapper's Go
// closure through this small object so the side table can name it weakly.
type wrapperBoundReceiver struct {
	// receiver is the method value's receiver.
	receiver reflect.Value
}

// CellWalkStats counts closure and cell walks since the process started.
type CellWalkStats struct {
	// ClosuresWalked counts closures a licence walk followed into.
	ClosuresWalked int64

	// CellsWalked counts upvalue cells a licence walk read and marked handed off.
	CellsWalked int64

	// WrappersFound counts function values the walk found in the side table.
	WrappersFound int64

	// WrappersOpaque counts function values the walk could not see into.
	WrappersOpaque int64

	// HandedOffStores counts stores into handed-off cells noted as shared-store hand-offs by
	// a VM that shares nothing with goroutines.
	HandedOffStores int64
}

var (
	// closureSiteVetoes counts the closure placement sites vetoed.
	closureSiteVetoes atomic.Int64
)

// closureSite records a placement site and the chunk receiving its allocation.
type closureSite struct {
	// function is the compiled function that owns the placement site.
	function *program.CompiledFunction

	// programCounter identifies the instruction that placed the closure.
	programCounter int

	// base is the address of the placement's backing storage.
	base uintptr
}

// closureChunkStatistics returns the closure chunk counters, which move only while the
// licence counters are on (SetLicenceCountingForTest).
//
// Returns ClosureChunkStats which is a snapshot.
func closureChunkStatistics() ClosureChunkStats {
	return ClosureChunkStats{
		Closures: closureChunkCounters.closures.Load(), Cells: closureChunkCounters.cells.Load(),
		Chunks: closureChunkCounters.chunks.Load(), ChunksRevoked: closureChunkCounters.chunksRevoked.Load(),
	}
}

// closurePlacing reports whether vm places closures and cells in its arena's closure
// chunks.
//
// Takes vm (*VM) which is about to make a closure.
//
// Returns bool which is true when the closure and its cells come from the arena.
func closurePlacing(vm *VM) bool {
	return arenaUsesUnsafeSlabs && licenceExactProbes && vm.Arena != nil && vm.Arena.licensed &&
		!vm.hasGoroutines && !vm.reentrantInterpreterVM && !vm.Arena.heapData &&
		!programSpawns(vm.rootFunction)
}

// programSpawns reports whether the root or any function body contains a GO instruction.
//
// Takes root (*program.CompiledFunction) which is the program's root function, or nil.
//
// Returns bool which is true when the program may spawn goroutines, or root is nil.
//
// Concurrency: Protects the shared result cache with spawnClosureCache.mu; the compiled
// program must remain immutable during inspection.
func programSpawns(root *program.CompiledFunction) bool {
	if root == nil {
		return true
	}
	key := weak.Make(root)
	spawnClosureCache.mu.Lock()
	spawns, known := spawnClosureCache.roots[key]
	spawnClosureCache.mu.Unlock()
	if known {
		return spawns
	}
	spawns = bodySpawns(root.Body)
	for _, function := range root.Functions {
		if spawns {
			break
		}
		if function != nil {
			spawns = bodySpawns(function.Body)
		}
	}
	spawnClosureCache.mu.Lock()
	if spawnClosureCache.roots == nil || len(spawnClosureCache.roots) >= spawnClosureCacheMax {
		spawnClosureCache.roots = make(map[weak.Pointer[program.CompiledFunction]]bool)
	}
	spawnClosureCache.roots[key] = spawns
	spawnClosureCache.mu.Unlock()
	return spawns
}

// bodySpawns reports whether body holds a GO instruction.
//
// Takes body ([]isa.Instruction) which is a function's bytecode.
//
// Returns bool which is true when one is found.
func bodySpawns(body []isa.Instruction) bool {
	for _, instruction := range body {
		if instruction.Op == isa.OpGo {
			return true
		}
	}
	return false
}

// closureChunkClass identifies the licence class used by closure chunks.
//
// It is licenceSlabCell when the arena's walks read cells, as all arenas placing closures
// do. Otherwise it is the upvalue class, whose revocation revokes the whole epoch.
//
// Returns licenceSlabClass which is the class.
func (a *RegisterArena) closureChunkClass() licenceSlabClass {
	if cellWalksApply(a) {
		return licenceSlabCell
	}
	return licenceSlabUpvalue
}

// newChunkCell returns a zeroed cell from the arena's cell chunks.
//
// Returns *program.UpvalueCell which is the cell.
func (a *RegisterArena) newChunkCell() *program.UpvalueCell {
	slabs := &a.closureChunks
	if len(slabs.cells) == 0 || slabs.cellIndex == len(slabs.cells[len(slabs.cells)-1]) {
		takeClosureChunk(a, &slabs.cells, &slabs.freeCells)
		slabs.cellIndex = 0
	}
	cell := &slabs.cells[len(slabs.cells)-1][slabs.cellIndex]
	slabs.cellIndex++
	if licenceCounting {
		closureChunkCounters.cells.Add(1)
	}
	return cell
}

// newChunkClosure returns a zeroed closure from the arena's closure chunks.
//
// Returns *RuntimeClosure which is the closure.
func (a *RegisterArena) newChunkClosure() *RuntimeClosure {
	slabs := &a.closureChunks
	if len(slabs.closures) == 0 || slabs.closureIndex == len(slabs.closures[len(slabs.closures)-1]) {
		takeClosureChunk(a, &slabs.closures, &slabs.freeClosures)
		slabs.closureIndex = 0
	}
	closure := &slabs.closures[len(slabs.closures)-1][slabs.closureIndex]
	slabs.closureIndex++
	if licenceCounting {
		closureChunkCounters.closures.Add(1)
	}
	return closure
}

// takeClosureChunk appends to list a cleared chunk from free, or a new one of the arena's
// chunk length, which it then publishes in the licence registry and the page table before
// any value can reference it (a chunk moved from the free list is published already).
//
// Takes arena (*RegisterArena) which owns the chunks.
// Takes list (*[][]T) which is the epoch's chunk list.
// Takes free (*[][]T) which is the free list.
func takeClosureChunk[T any](arena *RegisterArena, list, free *[][]T) {
	arena.closureChunks.madeChunks++
	if licenceCounting {
		closureChunkCounters.chunks.Add(1)
	}
	if count := len(*free); count != 0 {
		chunk := (*free)[count-1]
		(*free)[count-1] = nil
		*free = (*free)[:count-1]
		*list = append(*list, chunk)
		return
	}
	length := arena.closureChunks.chunk
	if length == 0 {
		length = closureChunkInitial
	}
	*list = append(*list, make([]T, length))

	for range spareClosureChunks {
		*free = append(*free, make([]T, length))
	}
	arena.publishLicenceSlabs()
}

// closureChunksInUse reports whether the epoch placed anything in the chunks.
//
// Returns bool which is true when some closure or cell was placed.
func (a *RegisterArena) closureChunksInUse() bool {
	return len(a.closureChunks.cells) != 0 || len(a.closureChunks.closures) != 0
}

// closureChunkCount counts the chunks, in use and free.
//
// Returns int which is the count.
func (a *RegisterArena) closureChunkCount() int {
	s := &a.closureChunks
	return len(s.cells) + len(s.freeCells) + len(s.closures) + len(s.freeClosures)
}

// closureChunkSlabOf finds the chunk holding address.
//
// Takes address (uintptr) which is the address.
//
// Returns the chunk's start, its class, and true when address is in a chunk.
func (a *RegisterArena) closureChunkSlabOf(address uintptr) (uintptr, licenceSlabClass, bool) {
	slabs := &a.closureChunks
	if len(slabs.cells)+len(slabs.freeCells)+len(slabs.closures)+len(slabs.freeClosures) == 0 {
		return 0, licenceSlabNone, false
	}
	base, ok := closureChunkListBase(slabs.cells, address)
	if !ok {
		base, ok = closureChunkListBase(slabs.closures, address)
	}
	if !ok {
		base, ok = closureChunkListBase(slabs.freeCells, address)
	}
	if !ok {
		base, ok = closureChunkListBase(slabs.freeClosures, address)
	}
	if !ok {
		return 0, licenceSlabNone, false
	}
	return base, a.closureChunkClass(), true
}

// closureChunkListBase finds the chunk of list holding address.
//
// Takes list ([][]T) which is a chunk list.
// Takes address (uintptr) which is the address.
//
// Returns the chunk's start and true when found.
func closureChunkListBase[T any](list [][]T, address uintptr) (uintptr, bool) {
	for _, chunk := range list {
		if base, ok := licenceSlabBase(chunk, address); ok {
			return base, true
		}
	}
	return 0, false
}

// adaptClosureChunk adjusts the next chunk length at release.
//
// It halves the length after a chunk was revoked and doubles it when the epoch filled
// more than one chunk without revocation.
//
// Takes revoked (bool) which is true when the release detached a chunk.
func (a *RegisterArena) adaptClosureChunk(revoked bool) {
	slabs := &a.closureChunks
	length := slabs.chunk
	if length == 0 {
		length = closureChunkInitial
	}
	switch {
	case revoked:
		length = max(closureChunkMin, length/2)
	case slabs.madeChunks > 1:
		length = min(closureChunkMax, length*2)
	}
	slabs.chunk = length
}

// abandonClosureChunks leaves every chunk to the collector.
//
// The epoch is revoked, or the arena is abandoned.
func (a *RegisterArena) abandonClosureChunks() {
	a.settleClosureSites()
	if a.closureChunkCount() != 0 && licenceCounting {
		closureChunkCounters.chunksRevoked.Add(int64(a.closureChunkCount()))
	}
	chunk := a.closureChunks.chunk
	a.closureChunks = closureChunkSlabs{chunk: chunk, cells: nil, freeCells: nil, closures: nil, freeClosures: nil, cellIndex: 0, closureIndex: 0, madeChunks: 0, sites: nil}
	if chunk != 0 {
		a.adaptClosureChunk(true)
	}
}

// resetClosureChunks clears the chunks the epoch used and moves them to the free lists,
// at Reset after the release detached the revoked ones.
func (a *RegisterArena) resetClosureChunks() {
	clear(a.closureChunks.sites)
	a.closureChunks.sites = a.closureChunks.sites[:0]
	slabs := &a.closureChunks
	if len(slabs.cells) == 0 && len(slabs.closures) == 0 {
		slabs.madeChunks = 0
		return
	}
	if slabs.madeChunks != 0 {
		a.adaptClosureChunk(false)
	}
	for _, chunk := range slabs.cells {
		clear(chunk)
		slabs.freeCells = append(slabs.freeCells, chunk)
	}
	for _, chunk := range slabs.closures {
		clear(chunk)
		slabs.freeClosures = append(slabs.freeClosures, chunk)
	}
	clear(slabs.cells)
	clear(slabs.closures)
	slabs.cells, slabs.closures = slabs.cells[:0], slabs.closures[:0]
	slabs.cellIndex, slabs.closureIndex, slabs.madeChunks = 0, 0, 0
	if len(slabs.freeCells) > closureChunkFreeMax {
		clear(slabs.freeCells[closureChunkFreeMax:])
		slabs.freeCells = slabs.freeCells[:closureChunkFreeMax]
	}
	if len(slabs.freeClosures) > closureChunkFreeMax {
		clear(slabs.freeClosures[closureChunkFreeMax:])
		slabs.freeClosures = slabs.freeClosures[:closureChunkFreeMax]
	}
}

// newClosureFor returns an empty closure over function, from the arena's closure chunks
// when the VM places closures and the site is not vetoed, otherwise from the Go heap.
//
// Takes vm (*VM) which makes the closure.
// Takes function (*program.CompiledFunction) which is its body.
//
// Returns *RuntimeClosure which is the closure, its root set and its cells empty.
func newClosureFor(vm *VM, function *program.CompiledFunction) *RuntimeClosure {
	if closurePlacing(vm) {
		if site, programCounter := vm.closurePlacementSite(); !closureSiteVetoed(site, programCounter) {
			arena := vm.Arena
			closure := arena.newChunkClosure()
			closure.Function, closure.RootFunction = function, vm.rootFunction
			arena.noteClosurePlacementSite(site, programCounter, closureChunkBase(arena.closureChunks.closures))
			return closure
		}
	}
	return &RuntimeClosure{Function: function, RootFunction: vm.rootFunction, inlineCells: [inlineUpvalueCells]*program.UpvalueCell{}, upvalues: nil, namedType: 0}
}

// newCellFor returns a cell, zero but for its maker tag (cellMaker), from the arena's
// closure chunks when the VM places closures and the site is not vetoed, otherwise from
// the Go heap.
//
// Takes vm (*VM) which makes the cell.
//
// Returns *program.UpvalueCell which is the cell.
func newCellFor(vm *VM) *program.UpvalueCell {
	maker := vm.Arena.cellMaker()
	if closurePlacing(vm) {
		if site, pc := vm.closurePlacementSite(); !closureSiteVetoed(site, pc) {
			arena := vm.Arena
			cell := arena.newChunkCell()
			cell.Maker = maker
			arena.noteClosurePlacementSite(site, pc, closureChunkBase(arena.closureChunks.cells))
			return cell
		}
	}
	cell := &program.UpvalueCell{}
	cell.Maker = maker
	return cell
}

// cachedClosureMatches reports whether the closure at the address a call site cached runs
// the cached code.
//
// Takes site (*program.CallSite) which holds the cache.
// Takes closure (*RuntimeClosure) which is the closure at the cached address.
//
// Returns bool which is true when the cache may serve the call.
func cachedClosureMatches(site *program.CallSite, closure *RuntimeClosure) bool {
	return closure.Function == site.CachedClosureCallee && closure.RootFunction == site.CachedClosureRoot
}

// noteDeferredNativeArguments hands off the arguments of a licensed arena's deferred
// native call.
//
// This implements the defer row's identity-kept hand-off, identified by copiesThenRevokes
// on route closure-capture.
//
// Takes arena (*RegisterArena) which is the deferring VM's arena.
// Takes arguments ([]reflect.Value) which are the deferred call's materialised arguments.
func noteDeferredNativeArguments(arena *RegisterArena, arguments []reflect.Value) {
	if arena == nil || !arena.licensed {
		return
	}
	for _, argument := range arguments {
		noteHandOff(arena, handOffIdentityKept, argument)
	}
}

// cellWalkStatistics returns the closure and cell walk counters, which move only while
// the licence counters are on (SetLicenceCountingForTest).
//
// Returns CellWalkStats which is a snapshot.
func cellWalkStatistics() CellWalkStats {
	return CellWalkStats{
		ClosuresWalked:  cellWalkCounters.closuresWalked.Load(),
		CellsWalked:     cellWalkCounters.cellsWalked.Load(),
		WrappersFound:   cellWalkCounters.wrappersFound.Load(),
		WrappersOpaque:  cellWalkCounters.wrappersOpaque.Load(),
		HandedOffStores: cellWalkCounters.handedOffStores.Load(),
	}
}

// ClosureCaptureWalksCells reports whether this build's closure hand-offs walk captured
// cells.
//
// Builds with exact walks over unsafe slabs do so, and their closure-capture route
// revokes the reached storage instead of the epoch.
//
// Returns bool which is true when closure hand-offs walk the cells.
func ClosureCaptureWalksCells() bool {
	return licenceExactProbes && arenaUsesUnsafeSlabs
}

// cellWalksApply reports whether a licensed arena's walks read upvalue cells and mark
// them handed off.
//
// This requires an exact build.
//
// Takes arena (*RegisterArena) which may be nil.
//
// Returns bool which is true when walks read cells and mark them handed off.
func cellWalksApply(arena *RegisterArena) bool {
	return licenceExactProbes && arenaUsesUnsafeSlabs && arena != nil && arena.licensed
}

// cellHandedOff reports whether a licence walk has named cell.
//
// Every store into it is then a hand-off.
//
// Takes cell (*program.UpvalueCell) which is the cell stored into.
//
// Returns bool which is true for a handed-off cell.
func cellHandedOff(cell *program.UpvalueCell) bool {
	return cell.HandedOff.Load() != 0
}

// cellStoreIsHandOff reports whether a store into cell by a VM on arena must be noted as
// a shared-store hand-off.
//
// The VM shares memory with goroutines, or a walk named the cell.
//
// Takes vm (*VM) which is the storing VM, possibly nil.
// Takes arena (*RegisterArena) which is its arena.
// Takes cell (*program.UpvalueCell) which is stored into.
//
// Returns bool which is true when the store is a hand-off.
func cellStoreIsHandOff(vm *VM, arena *RegisterArena, cell *program.UpvalueCell) bool {
	if vm == nil || !arena.countsHandOffs() {
		return false
	}
	if vm.hasGoroutines {
		return true
	}
	if !cellHandedOff(cell) {
		return false
	}
	if licenceCounting {
		cellWalkCounters.handedOffStores.Add(1)
	}
	return true
}

// closurePlacementSite returns the current frame's function and program counter,
// identifying the MAKE_CLOSURE that is creating a closure or cell.
//
// Returns *program.CompiledFunction which is the function, or nil.
// Returns int which is the program counter.
func (vm *VM) closurePlacementSite() (*program.CompiledFunction, int) {
	if vm.FramePointer < 0 || vm.FramePointer >= len(vm.CallStack) {
		return nil, 0
	}
	frame := &vm.CallStack[vm.FramePointer]
	return frame.Function, frame.ProgramCounter
}

// closureSiteVetoed reports whether a release vetoed the site's closure and cell
// placements.
//
// Takes function (*program.CompiledFunction) which is the site's function, or nil.
// Takes pc (int) which is its program counter.
//
// Returns bool which is true when the site must not place.
func closureSiteVetoed(function *program.CompiledFunction, pc int) bool {
	return function != nil && function.ArenaPlacementVetoed(pc)
}

// noteClosurePlacementSite records a placement.
//
// Takes function (*program.CompiledFunction) which is the site's function, or nil.
// Takes programCounter (int) which is its program counter.
// Takes base (uintptr) which is the chunk's start.
func (a *RegisterArena) noteClosurePlacementSite(function *program.CompiledFunction, programCounter int, base uintptr) {
	slabs := &a.closureChunks
	if function == nil || base == 0 {
		return
	}
	if n := len(slabs.sites); n != 0 && slabs.sites[n-1] == (closureSite{function: function, programCounter: programCounter, base: base}) {
		return
	}
	if len(slabs.sites) < closureMaxSites {
		slabs.sites = append(slabs.sites, closureSite{function: function, programCounter: programCounter, base: base})
	}
}

// settleClosureSites vetoes, at a licensed release before the closure chunks are dropped
// or abandoned, the site of every recorded placement whose chunk was revoked, or of every
// recorded placement when the epoch was, and forgets them.
func (a *RegisterArena) settleClosureSites() {
	slabs := &a.closureChunks
	if len(slabs.sites) == 0 {
		return
	}
	epoch := a.licenceState.epochRevoked
	for _, site := range slabs.sites {
		if !epoch {
			if _, revoked := a.licenceState.revoked[site.base]; !revoked {
				continue
			}
		}
		if !site.function.ArenaPlacementVetoed(site.programCounter) {
			site.function.VetoArenaPlacement(site.programCounter)
			if licenceCounting {
				closureSiteVetoes.Add(1)
			}
		}
	}
	clear(slabs.sites)
	slabs.sites = slabs.sites[:0]
}
