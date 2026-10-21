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
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"pipit.sh/pipit/internal/clock"
	"pipit.sh/pipit/internal/engine/program"
)

const (
	// exportGateGuestPatience bounds how long an owner waits for a gate while some reader
	// still runs guest code, which lets the owner in at its next checkpoint or blocking
	// point.
	exportGateGuestPatience = 20 * time.Millisecond

	// exportGateNativePatience bounds how long an owner waits for a gate once every reader
	// left holding it is inside a native call, which may wait for the owner itself.
	exportGateNativePatience = 200 * time.Microsecond

	// exportGateSpins is the number of times an owner yields the processor before it starts
	// to sleep between looks at the readers.
	exportGateSpins = 64

	// exportGateNap is how long an owner sleeps between looks once it stops spinning.
	exportGateNap = 20 * time.Microsecond
)

// gateCover says why a run may leave a gate alone (gateCoverBelow).
type gateCover uint8

const (
	// gateUncovered means the run must take the gate itself.
	gateUncovered gateCover = iota

	// gateCoveredByOwner means the gate's owner is parked below the run on its goroutine,
	// and cannot start a section until the run returns.
	gateCoveredByOwner

	// gateCoveredByReader means a gated run parked below on the goroutine holds the gate,
	// inside the native call that led to this run. This run parks and yields that hold as
	// its own (forEachGatedBelow).
	gateCoveredByReader
)

// nativeCallHold is what enterNativeCall did around a native call, for leaveNativeCall.
type nativeCallHold uint8

const (
	// nativeCallHoldsNothing means the call needed nothing undone.
	nativeCallHoldsNothing nativeCallHold = iota

	// nativeCallReleased means the VM released its interpreter lock and export gates for a
	// call that blocks the host goroutine, without holding the interpreter lock.
	nativeCallReleased

	// nativeCallReleasedLock is nativeCallReleased when the interpreter lock was released.
	nativeCallReleasedLock

	// nativeCallCounted means a gated run counted itself inside the call (noteGatedNative).
	nativeCallCounted
)

// exportGateCounters counts owner sections while the licence counters are on.
var exportGateCounters struct {
	// sections counts owner sections that took their gate.
	sections atomic.Int64

	// waits counts the owner sections that had to wait for readers.
	waits atomic.Int64

	// giveUps counts the owner sections whose patience ran out, so they ran shared.
	giveUps atomic.Int64
}

// ExportGateStats counts owner sections of VMs that exported guest code, since the
// process started, while the licence counters are on (SetLicenceCountingForTest).
type ExportGateStats struct {
	// Sections counts owner sections that took their gate.
	Sections int64

	// Waits counts the sections that waited for a gated run to let them in.
	Waits int64

	// GiveUps counts the sections whose patience ran out, so they ran shared.
	GiveUps int64
}

// exportGate excludes the walks, mark and release of a VM that exported guest code (its
// owner) from that guest code running on other goroutines.
//
// The owner is the writer, and holds the gate only for an owner section
// (enterOwnerSection), which never blocks, calls native code or runs guest code. A gated
// run is a reader. The owner waits for readers only within its patience (awaitReaders)
// and otherwise runs its section shared.
type exportGate struct {
	// clock times the owner's waits (awaitReaders).
	clock clock.Clock

	// wake wakes readers waiting for the owner's section to end. Its lock is mu.
	wake sync.Cond

	// mu guards the readers' wait on wake.
	mu sync.Mutex

	// guestPatience is how long the owner waits while some reader runs guest code
	// (exportGateGuestPatience by default).
	guestPatience time.Duration

	// nativePatience is how long the owner waits once every reader left is inside a native
	// call (exportGateNativePatience by default).
	nativePatience time.Duration

	// writer is true while the owner holds the gate or waits for it.
	writer atomic.Bool

	// readers counts the gated runs holding the gate.
	readers atomic.Int32

	// inNative counts the readers inside a native call, which may wait for the owner.
	inNative atomic.Int32

	// sleepers counts readers sleeping until wake is signalled.
	sleepers atomic.Int32

	// exportedBySpawned is set by a goroutine the owner started, directly or through other
	// goroutines, that handed native code guest code which may reach the owner's memory.
	exportedBySpawned atomic.Bool

	// depth counts the owner's nested sections. Only the owner's goroutine touches it.
	depth int32

	// ownerHeld is true while the owner's section holds the gate. Only the owner's goroutine
	// writes and reads it (licenceShared), as every section runs there.
	ownerHeld bool

	// exportedHere is set once the owner, or a VM parked above it on its goroutine, handed
	// native code guest code that may reach the owner's memory. Written only on the owner's
	// goroutine.
	exportedHere bool
}

// exportGateSet is an immutable set of gates, published through an atomic pointer and
// replaced, never changed, when it grows.
type exportGateSet []*exportGate

// vmExportGates is a VM's part in export gates, including its own gate as an owner, the
// set it publishes for the guest code it exports, and the gates its runs hold as a
// reader. Its fields are promoted into VM.
type vmExportGates struct {
	// exportGate is the gate through which the VM's owner sections exclude guest code it
	// exported that runs on other goroutines. Made when the VM is marked exported or starts
	// its first goroutine, only on its own goroutine (or one it is parked below).
	exportGate *exportGate

	// publishedGates is the set of gates a run of guest code the VM handed to native code
	// must hold. It only grows.
	publishedGates atomic.Pointer[exportGateSet]

	// gatesHeldInline backs gatesHeld for the usual one or two gates, so a fresh callback or
	// bound-method VM takes its gates without allocating.
	gatesHeldInline [2]*exportGate

	// gatesHeld are the runGates the current run took (beginGatedRun), leaving out those a
	// VM parked below on the goroutine covers.
	gatesHeld []*exportGate

	// runGates are the gates this VM's runs hold as a reader, which are those of the
	// wrapper, bound method or gated run that started it.
	runGates exportGateSet

	// spawnGates are the gates of the non-reentrant VMs that started this goroutine VM,
	// directly or through other goroutines, which exporting guest code here marks too.
	spawnGates exportGateSet

	// childSpawnGates caches the spawnGates of the goroutines this VM starts
	// (gatesForChildGoroutines).
	childSpawnGates exportGateSet

	// exportChainGates is the set of gates a run of guest code exported from this VM, or
	// from any VM parked below it on the goroutine, must hold. It is valid for the run whose
	// exportSerial equals exportChainSerial (markGuestCodeExported).
	exportChainGates exportGateSet

	// exportChainSerial is the exportSerial of the run exportChainGates was gathered in.
	exportChainSerial uint64

	// familyExporterCounted is true once the VM is counted in its family's exporters
	// (noteFamilyExporter).
	familyExporterCounted bool

	// gatedDepth counts the nested dispatch entries of a gated run.
	gatedDepth int32

	// gatesParked counts nested parkGates calls. While it is non-zero the run holds none of
	// gatesHeld.
	gatesParked int32

	// gatesLeanBelow is true when the run left a gate to a gated run parked below it on the
	// goroutine (gateCoveredByReader), whose hold it parks and yields with its own.
	gatesLeanBelow bool

	// holdsGates is true from beginGatedRun to endGatedRun when the run holds gates or leans
	// on a hold below.
	holdsGates bool

	// exportShares is true while guest code the VM exported shares its memory.
	exportShares bool
}

// enterOwner starts an owner section, waiting within its patience for the gated runs
// holding the gate to let it go. The caller must call leaveOwner whatever it returns.
//
// Returns bool which is true when the section holds the gate, so the owner may read its
// memory unshared. It returns false when patience ran out and the section must run
// shared.
func (g *exportGate) enterOwner() bool {
	if g.depth > 0 {
		g.depth++
		return g.ownerHeld
	}
	g.depth = 1
	g.writer.Store(true)
	if g.readers.Load() != 0 && !g.awaitReaders() {
		g.writer.Store(false)
		g.wakeReaders()
		if licenceCounting {
			exportGateCounters.giveUps.Add(1)
		}
		return false
	}
	g.ownerHeld = true
	if licenceCounting {
		exportGateCounters.sections.Add(1)
	}
	return true
}

// leaveOwner ends the section enterOwner started, letting waiting readers go on when the
// outermost section ends.
func (g *exportGate) leaveOwner() {
	if g.depth--; g.depth > 0 {
		return
	}
	if !g.ownerHeld {
		return
	}
	g.ownerHeld = false
	g.writer.Store(false)
	g.wakeReaders()
}

// awaitReaders waits, with writer set, for the readers to let the gate go. It waits up to
// guestPatience while some reader runs guest code, and up to nativePatience once every
// remaining reader is inside a native call.
//
// Returns bool which is true when no reader holds the gate.
func (g *exportGate) awaitReaders() bool {
	if licenceCounting {
		exportGateCounters.waits.Add(1)
	}
	start := g.clock.Now()
	for spin := 0; ; spin++ {
		readers := g.readers.Load()
		if readers == 0 {
			return true
		}
		waited := g.clock.Since(start)
		if waited > g.guestPatience || readers <= g.inNative.Load() && waited > g.nativePatience {
			return false
		}
		if spin < exportGateSpins {
			runtime.Gosched()
			continue
		}
		g.clock.Sleep(exportGateNap)
	}
}

// ownerHolds reports whether the owner's section holds the gate. Nil-safe.
//
// Returns bool which is true while it does.
func (g *exportGate) ownerHolds() bool {
	return g != nil && g.ownerHeld
}

// ownerWaiting reports whether the owner holds the gate or waits for it.
//
// Returns bool which is true when a reader should let the gate go.
func (g *exportGate) ownerWaiting() bool {
	return g.writer.Load()
}

// enterReader takes the gate for a gated run, waiting while the owner holds it.
func (g *exportGate) enterReader() {
	for {
		g.readers.Add(1)
		if !g.writer.Load() {
			return
		}
		g.readers.Add(-1)
		g.awaitWriter()
	}
}

// leaveReader lets the gate go after enterReader.
func (g *exportGate) leaveReader() {
	g.readers.Add(-1)
}

// awaitWriter sleeps until the owner clears writer.
//
// Safe for concurrent use. The gate mutex protects the condition wait. A reader's
// cancellation or callback timeout takes effect once it wakes.
func (g *exportGate) awaitWriter() {
	g.mu.Lock()
	g.sleepers.Add(1)
	for g.writer.Load() {
		g.wake.Wait()
	}
	g.sleepers.Add(-1)
	g.mu.Unlock()
}

// wakeReaders wakes the readers sleeping in awaitWriter once writer is clear. It runs
// after the store that clears writer, so a reader that counted itself as a sleeper after
// this load of sleepers reads writer clear and does not sleep.
//
// Safe for concurrent use. The gate mutex protects the broadcast to sleeping readers.
func (g *exportGate) wakeReaders() {
	if g.sleepers.Load() == 0 {
		return
	}
	g.mu.Lock()
	g.wake.Broadcast()
	g.mu.Unlock()
}

// with returns the set together with gates, sharing set when nothing is new.
//
// Takes gates ([]*exportGate) which are the gates to add. Nil entries are ignored.
//
// Returns exportGateSet which is the union.
func (set exportGateSet) with(gates ...*exportGate) exportGateSet {
	grown := set
	for _, gate := range gates {
		if gate == nil || grown.contains(gate) {
			continue
		}
		if len(grown) == len(set) {
			grown = append(make(exportGateSet, 0, len(set)+len(gates)), set...)
		}
		grown = append(grown, gate)
	}
	return grown
}

// contains reports whether the set holds gate.
//
// Takes gate (*exportGate) which is the gate looked for.
//
// Returns bool which is true when it does.
func (set exportGateSet) contains(gate *exportGate) bool {
	return slices.Contains(set, gate)
}

// ensureExportGate returns the VM's own gate, making it on first use. It runs on the VM's
// goroutine, or one the VM is parked below.
//
// Returns *exportGate which is the gate.
func (vm *VM) ensureExportGate() *exportGate {
	if vm.exportGate == nil {
		vm.exportGate = newExportGate()
	}
	return vm.exportGate
}

// exportedGates returns the gates published for wrappers and adapters made on the VM,
// which are those a gated run of guest code the VM handed to native code must hold.
//
// Returns exportGateSet which is the set, nil when the VM exported nothing.
func (vm *VM) exportedGates() exportGateSet {
	if set := vm.publishedGates.Load(); set != nil {
		return *set
	}
	return nil
}

// publishExportedGates adds gates to the VM's published set.
//
// Takes gates (exportGateSet) which are the gates to add.
func (vm *VM) publishExportedGates(gates exportGateSet) {
	current := vm.exportedGates()
	grown := current.with(gates...)
	if len(grown) != len(current) {
		vm.publishedGates.Store(&grown)
	}
}

// goroutinesShare reports whether goroutines the VM runs with may read or write what it
// holds, because it runs for native code (a callback VM) or goroutines it started are
// still counted by its tracker.
//
// Takes pendingSpawns (int32) which is the number of goroutines the tracker already
// counts that have not been launched yet.
//
// Returns bool which is true when they may.
func (vm *VM) goroutinesShare(pendingSpawns int32) bool {
	if !vm.hasGoroutines {
		return false
	}
	if !vm.spawnedGoroutines {
		return true
	}
	tracker := vm.Limits.Tracker
	return tracker == nil || tracker.GoroutineCount.Load() > pendingSpawns
}

// enterOwnerSection starts an owner section for a walk, collection or release that would
// run shared only because guest code the arena's VM exported may be running.
//
// Takes shared (bool) which is the sharing the caller computed (licenceShared).
//
// Returns bool which is the sharing the section must use.
// Returns *VM which is the owner the caller must end the section on (leaveOwnerSection),
// or nil when no section was started.
func (a *RegisterArena) enterOwnerSection(shared bool) (bool, *VM) {
	if !a.licensed || a.licenceAlwaysShared {
		return shared, nil
	}
	vm := a.licenceSubjectVM()
	if vm == nil || vm.exportGate == nil {
		return shared, nil
	}
	gate := vm.exportGate
	switch {
	case gate.ownerHeld:
		return shared, nil
	case !shared:
		if !gate.exportedBySpawned.Load() {
			return false, nil
		}
		vm.exportShares, gate.exportedHere = true, true
	case !vm.exportShares || vm.goroutinesShare(0):
		return shared, nil
	}
	if !gate.enterOwner() {
		gate.leaveOwner()
		return true, nil
	}
	vm.exportShares = false
	return a.licenceShared(0), vm
}

// leaveOwnerSection ends the owner section enterOwnerSection started on the VM, so its
// exported guest code shares its memory again.
func (vm *VM) leaveOwnerSection() {
	vm.exportGate.leaveOwner()
	vm.exportShares = true
}

// mayRunGated reports whether a dispatch entry on the VM may need beginGatedRun, as a
// nested entry of a gated run, a VM with gates to hold, or a reentrant VM that exported
// guest code may.
//
// Returns bool which is false for every other VM, which pays only this test.
func (vm *VM) mayRunGated() bool {
	return vm.gatedDepth != 0 || len(vm.runGates) != 0 || vm.guestCodeExported && vm.reentrantInterpreterVM
}

// beginGatedRun takes the gates a run of guest code on the VM must hold, at the outermost
// dispatch entry. Nested entries only count.
//
// Returns bool which is true when the caller must call endGatedRun.
func (vm *VM) beginGatedRun() bool {
	if vm.gatedDepth > 0 {
		vm.gatedDepth++
		return true
	}
	if vm.guestCodeExported && vm.reentrantInterpreterVM {
		vm.markGuestCodeExported()
	}
	if len(vm.runGates) == 0 {
		return false
	}
	vm.gatedDepth = 1
	vm.gatesParked = 0
	vm.gatesLeanBelow = false
	held := vm.gatesHeld[:0]
	if held == nil {
		held = vm.gatesHeldInline[:0]
	}
	for _, gate := range vm.runGates {
		switch vm.gateCoverBelow(gate) {
		case gateCoveredByOwner:
		case gateCoveredByReader:
			vm.gatesLeanBelow = true
		case gateUncovered:
			gate.enterReader()
			held = append(held, gate)
		}
	}
	vm.gatesHeld = held
	vm.holdsGates = len(held) != 0 || vm.gatesLeanBelow
	return true
}

// endGatedRun lets go the gates beginGatedRun took, at the outermost dispatch exit.
func (vm *VM) endGatedRun() {
	if vm.gatedDepth--; vm.gatedDepth > 0 {
		return
	}
	if vm.gatesParked == 0 {
		for _, gate := range vm.gatesHeld {
			gate.leaveReader()
		}
	} else if vm.gatesLeanBelow {
		vm.forEachGatedBelow((*VM).unparkGates)
	}
	clear(vm.gatesHeld)
	vm.gatesHeld = vm.gatesHeld[:0]
	vm.gatesParked = 0
	vm.gatesLeanBelow = false
	vm.holdsGates = false
}

// gateCoverBelow reports whether a VM parked below the VM on its goroutine covers gate
// for the VM's run.
//
// Takes gate (*exportGate) which is a gate the run would hold.
//
// Returns gateCover which says whether and how the gate is covered.
func (vm *VM) gateCoverBelow(gate *exportGate) gateCover {
	if vm.servedChainRun.token.Load() == 0 {
		return gateUncovered
	}
	below := vm.servedChainRun.below
	for range servedChainMax {
		if below == nil {
			return gateUncovered
		}
		if below.exportGate == gate {
			return gateCoveredByOwner
		}
		if below.gatesParked == 0 && (exportGateSet(below.gatesHeld).contains(gate) || below.gatesLeanBelow) {
			return gateCoveredByReader
		}
		below = below.servedChainRun.below
	}
	return gateUncovered
}

// forEachGatedBelow calls each on every gated run parked below the VM on its goroutine
// whose holds the VM's run leans on. Those runs wait inside the native calls that led to
// this run, on this goroutine, so while this run blocks or yields nothing on the
// goroutine writes guest memory.
//
// Takes each (func(*VM)) which is parkGates, unparkGates or yieldExportGates.
func (vm *VM) forEachGatedBelow(each func(*VM)) {
	below := vm.servedChainRun.below
	for range servedChainMax {
		if below == nil {
			return
		}
		if below.holdsGates {
			each(below)
			return
		}
		below = below.servedChainRun.below
	}
}

// parkGates lets go the gates a gated run holds, and those it leans on below, before it
// blocks (a channel operation, a sync wait, a sleep), where nothing writes guest memory
// but the runtime's sync state.
func (vm *VM) parkGates() {
	if !vm.holdsGates {
		return
	}
	if vm.gatesParked++; vm.gatesParked > 1 {
		return
	}
	for _, gate := range vm.gatesHeld {
		gate.leaveReader()
	}
	if vm.gatesLeanBelow {
		vm.forEachGatedBelow((*VM).parkGates)
	}
}

// unparkGates takes back the gates parkGates let go, once the block is over.
func (vm *VM) unparkGates() {
	if vm.gatesParked == 0 {
		return
	}
	if vm.gatesParked--; vm.gatesParked > 0 {
		return
	}
	if vm.gatesLeanBelow {
		vm.forEachGatedBelow((*VM).unparkGates)
	}
	for _, gate := range vm.gatesHeld {
		gate.enterReader()
	}
}

// yieldExportGates lets an owner waiting for a gate the run holds, or leans on below,
// have it at a checkpoint between instructions, and takes it back after the owner's
// section.
func (vm *VM) yieldExportGates() {
	if vm.gatesParked != 0 {
		return
	}
	for _, gate := range vm.gatesHeld {
		if gate.ownerWaiting() {
			gate.leaveReader()
			gate.enterReader()
		}
	}
	if vm.gatesLeanBelow {
		vm.forEachGatedBelow((*VM).yieldExportGates)
	}
}

// noteGatedNative counts a gated run in or out of a native call on the gates it holds, so
// an owner waiting for them knows the run may be waiting for it. A run that has parked
// its gates holds none of them, so it counts nothing.
//
// Takes delta (int32) which is 1 entering the call and -1 leaving it.
//
// Returns bool which is true when the run was counted, so the caller counts it out again.
func (vm *VM) noteGatedNative(delta int32) bool {
	if vm.gatesParked != 0 {
		return false
	}
	for _, gate := range vm.gatesHeld {
		gate.inNative.Add(delta)
	}
	return true
}

// gatesForChildGoroutines returns the spawnGates of a goroutine the VM starts, which are
// its own and the VM's own gate, unless the VM is reentrant or a goroutine VM, whose
// memory is shared for as long as it runs. Computed once per VM, on its own goroutine.
//
// Returns exportGateSet which is the set.
func (vm *VM) gatesForChildGoroutines() exportGateSet {
	if vm.childSpawnGates == nil {
		vm.childSpawnGates = vm.spawnGates
		if !vm.reentrantInterpreterVM && vm.debugKind != debugThreadGoroutine {
			vm.childSpawnGates = vm.spawnGates.with(vm.ensureExportGate())
		}
		if vm.childSpawnGates == nil {
			vm.childSpawnGates = exportGateSet{}
		}
	}
	return vm.childSpawnGates
}

// enterNativeCall prepares the VM for a native call at site.
//
// Takes site (*program.CallSite) which is the call site.
//
// Returns nativeCallHold which the caller passes to leaveNativeCall when the call
// returns, unless it is nativeCallHoldsNothing.
func (vm *VM) enterNativeCall(site *program.CallSite) nativeCallHold {
	if !site.BlocksHostGoroutine && !vm.holdsGates {
		return nativeCallHoldsNothing
	}
	return vm.enterNativeCallSlow(site)
}

// enterNativeCallSlow is enterNativeCall for a call that blocks the host goroutine or a
// VM that holds export gates.
//
// Takes site (*program.CallSite) which is the call site.
//
// Returns nativeCallHold which says what was done.
func (vm *VM) enterNativeCallSlow(site *program.CallSite) nativeCallHold {
	switch {
	case vm.releasesAroundBlock(site):
		if vm.releaseAroundBlock() {
			return nativeCallReleasedLock
		}
		return nativeCallReleased
	case vm.holdsGates && vm.noteGatedNative(1):
		return nativeCallCounted
	default:
		return nativeCallHoldsNothing
	}
}

// leaveNativeCall undoes what enterNativeCall did.
//
// Takes hold (nativeCallHold) which enterNativeCall returned.
func (vm *VM) leaveNativeCall(hold nativeCallHold) {
	switch hold {
	case nativeCallReleased, nativeCallReleasedLock:
		vm.reacquireAfterBlock(hold == nativeCallReleasedLock)
	case nativeCallCounted:
		vm.noteGatedNative(-1)
	case nativeCallHoldsNothing:
	}
}

// exportGateStatistics returns the export gate counters.
//
// Returns ExportGateStats which is a snapshot.
func exportGateStatistics() ExportGateStats {
	return ExportGateStats{
		Sections: exportGateCounters.sections.Load(),
		Waits:    exportGateCounters.waits.Load(),
		GiveUps:  exportGateCounters.giveUps.Load(),
	}
}

// newExportGate returns an exportGate with nothing held.
//
// Returns *exportGate which is the gate.
func newExportGate() *exportGate {
	gate := &exportGate{
		clock:             clock.WallClock,
		wake:              sync.Cond{},
		mu:                sync.Mutex{},
		guestPatience:     exportGateGuestPatience,
		nativePatience:    exportGateNativePatience,
		writer:            atomic.Bool{},
		readers:           atomic.Int32{},
		inNative:          atomic.Int32{},
		sleepers:          atomic.Int32{},
		exportedBySpawned: atomic.Bool{},
		depth:             0,
		ownerHeld:         false,
		exportedHere:      false,
	}
	gate.wake.L = &gate.mu
	return gate
}
