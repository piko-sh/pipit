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
	"slices"
	"sync/atomic"
)

var (
	// blindScopeCounters counts what the blind-scope rules did, while the licence counters
	// are on (SetLicenceCountingForTest).
	blindScopeCounters struct {
		sharedMemoHits, localBlind, localByteBlind, foreignLocal atomic.Int64
	}
)

// BlindScopeStats counts shared walks settled from the memo, local blind hand-offs and
// skipped foreign revocations since the process started.
type BlindScopeStats struct {
	// SharedMemoHits counts containers a shared walk settled from the memo.
	SharedMemoHits int64

	// LocalBlindHandOffs counts blind hand-offs by a VM serving no native call that bumped
	// no tree counter.
	LocalBlindHandOffs int64

	// LocalByteBlindHandOffs counts byte-only blind hand-offs by such a VM that bumped no
	// tree counter.
	LocalByteBlindHandOffs int64

	// ForeignLocalSkips counts foreign revocations such a VM did not record.
	ForeignLocalSkips int64
}

// licenceBlindCounters counts the walks one goroutine tree skipped.
type licenceBlindCounters struct {
	// all counts skipped walks that might have named any storage.
	all licenceBlindCounter

	// bytes counts skipped walks that might have named only string bytes.
	bytes licenceBlindCounter
}

// licenceBlindCounter is one count of skipped walks, packed with a bit recording whether
// any arena has taken a reading since the count last moved (bit 0; the count is the
// rest).
type licenceBlindCounter struct {
	// word is the count shifted left by one, ORed with the reading bit.
	word atomic.Uint64
}

// blindReading records whether any arena in the tree skipped a walk since this arena's
// previous reading.
type blindReading struct {
	// current is the count of skipped walks that might have named any storage.
	current uint64

	// currentBytes is the count of skipped walks that might have named only byte slabs.
	currentBytes uint64

	// all is true when a skipped walk might have named any of this arena's storage.
	all bool

	// bytes is true when one might have named its byte slabs.
	bytes bool
}

// blindScopeStatistics returns the blind-scope counters, which move only while the
// licence counters are on (SetLicenceCountingForTest).
//
// Returns BlindScopeStats which is a snapshot.
func blindScopeStatistics() BlindScopeStats {
	return BlindScopeStats{
		SharedMemoHits:         blindScopeCounters.sharedMemoHits.Load(),
		LocalBlindHandOffs:     blindScopeCounters.localBlind.Load(),
		LocalByteBlindHandOffs: blindScopeCounters.localByteBlind.Load(),
		ForeignLocalSkips:      blindScopeCounters.foreignLocal.Load(),
	}
}

// servesNoNativeCall reports whether the arena's licence VM is known to serve no native
// call.
//
// Returns bool which is true for such an arena.
func (a *RegisterArena) servesNoNativeCall() bool {
	present, reentrant := a.servingVMReentrant()
	return present && !reentrant
}

// blindLocal reports whether a licensed arena's blind hand-offs stay local because its VM
// serves no native call.
//
// Returns bool which is true when a blind hand-off bumps no tree counter.
func (a *RegisterArena) blindLocal() bool {
	if !a.licensed {
		return false
	}
	return a.servesNoNativeCall()
}

// recordContainerStore notes a store into a Go-heap or host container the arena does not
// own.
//
// Takes cause (HandOffCause) which names the site.
// Takes container (reflect.Value) which is the container stored into.
func (a *RegisterArena) recordContainerStore(cause HandOffCause, container reflect.Value) {
	a.licenceState.containerStore = true
	a.recordHandOff(cause, container, false)
	a.licenceState.containerStore = false
}

// Load returns the count without taking a reading.
//
// Returns uint64 which is the count.
func (c *licenceBlindCounter) Load() uint64 { return c.word.Load() >> 1 }

// Add counts delta skipped walks unconditionally, for tests that simulate them.
//
// Takes delta (uint64) which is the number of walks.
func (c *licenceBlindCounter) Add(delta uint64) { c.word.Add(delta << 1) }

// read returns the count and records that a reading was taken, so the next skipped walk
// is counted.
//
// Returns uint64 which is the count.
func (c *licenceBlindCounter) read() uint64 {
	if word := c.word.Load(); word&1 != 0 {
		return word >> 1
	}
	return c.word.Or(1) >> 1
}

// stale reports whether a reading was taken since the count last moved, so a skipped walk
// must be counted.
//
// Returns bool which is true when the next bump would count.
func (c *licenceBlindCounter) stale() bool { return c.word.Load()&1 != 0 }

// bump counts one skipped walk unless no reading was taken since the count last moved,
// clearing the reading bit when it counts.
//
// Returns bool which is true when the count moved.
func (c *licenceBlindCounter) bump() bool {
	for {
		old := c.word.Load()
		if old&1 == 0 {
			return false
		}
		if c.word.CompareAndSwap(old, old&^1+2) {
			return true
		}
	}
}

// licenceBlindSource returns the blind counters of the goroutine tree tracker accounts
// for, or the process-wide ones for an arena acquired without a tracker.
//
// Takes tracker (*ResourceTracker) which may be nil.
//
// Returns *licenceBlindCounters which holds the counters.
func licenceBlindSource(tracker *ResourceTracker) *licenceBlindCounters {
	if tracker == nil {
		return &licenceBlindProcess
	}
	return &tracker.licenceBlind
}

// adoptAtAcquisition applies foreign slab revocations and blind hand-offs recorded by the
// releasing tree after the arena's previous release was sealed.
//
// Takes source (*licenceBlindCounters) which holds the acquiring tree's blind counters.
func (a *RegisterArena) adoptAtAcquisition(source *licenceBlindCounters) {
	state := &a.licenceState
	if a.acquireQuietly(source) {
		return
	}
	if state.blindMarked {
		a.applyBlindHandOffs()
	}
	state.blindSource = source
	state.blindMarked = true
	state.blindMark, state.blindByteMark = a.readBlindTotals()
	a.adoptForeignRevocations()
	if !state.epochRevoked && len(state.revoked) == 0 {
		return
	}
	if licenceCounting {
		a.stats().acquireDetaches.Add(1)
	}
	a.detachRevoked(&licenceReleaseTally{})
	a.clearLicence()
}

// acquireQuietly handles adoptAtAcquisition's common case without the seal's lock.
//
// Takes source (*licenceBlindCounters) which holds the acquiring tree's blind counters.
//
// Returns bool which is true when the acquisition needed nothing more.
func (a *RegisterArena) acquireQuietly(source *licenceBlindCounters) bool {
	state := &a.licenceState
	if !state.blindMarked || state.epochRevoked || len(state.revoked) != 0 || licenceForeignRevokedCount.Load() != 0 {
		return false
	}
	all, bytes := a.readBlindTotals()
	if all != state.blindMark || bytes != state.blindByteMark || licenceForeignRevokedCount.Load() != 0 {
		return false
	}
	state.blindSource = source
	state.blindMark, state.blindByteMark = a.readBlindTotals()
	return true
}

// clearLicence starts a new epoch with nothing revoked.
func (a *RegisterArena) clearLicence() {
	a.flushRevokedEpochNotes()
	state := &a.licenceState
	state.servingKnown = false
	state.revokedLocalBlind = false
	state.coverCoveredKnown = false
	clear(state.revoked)
	a.clearRetiredPages()
	state.memo.reset()
	state.epochRevoked = false
	state.banksRevoked = false
	state.wholeArenaDetached = false
	state.released = false
	state.cellMaker = 0
	state.walkBudget = licenceWalkLeafBudget
	state.epochCause = noHandOffCause
	state.epochReason = 0
	state.releasedRevision = state.revision
}

// licenceShared reports whether another goroutine may read or write what the arena's VM
// holds.
//
// Takes pendingSpawns (int32) which is the number of goroutines the tracker already
// counts that have not been launched yet (one at a go statement's own hand-offs).
//
// Returns bool which is true when the arena must not read through shared memory.
func (a *RegisterArena) licenceShared(pendingSpawns int32) bool {
	if a.licenceAlwaysShared || a.licenceFamilyShared {
		return true
	}
	vm := a.licenceSubjectVM()
	if vm == nil {
		return false
	}
	if vm.exportShares {
		return true
	}
	if !vm.hasGoroutines {
		return false
	}
	if !vm.spawnedGoroutines {
		return true
	}
	tracker := vm.Limits.Tracker
	return tracker == nil || tracker.GoroutineCount.Load() > pendingSpawns
}

// licenceSubjectVM returns the VM whose goroutines decide whether the arena is shared. It
// returns its licence VM, or else its owner.
//
// Returns *VM which is that VM, or nil.
func (a *RegisterArena) licenceSubjectVM() *VM {
	if vm := a.licenceVM; vm != nil {
		return vm
	}
	return a.ownerVM
}

// exportGuestCode is pinIdentity for an object through which native code can run vm's
// guest code, including a closure wrapper, a method value or method expression, a reflect
// method value, or an interface adapter.
//
// Native code may run such code on another goroutine at any time from now on, so vm
// becomes shared outside its owner sections (markGuestCodeExported), unless vm is
// preparing a call of a native that keeps nothing once it returns (borrowingNativeCall),
// whose objects run only inside the call. It must run on vm's goroutine until vm is
// marked. After that it only reads.
func (vm *VM) exportGuestCode() {
	if vm == nil {
		return
	}
	vm.pinIdentity()
	if vm.guestCodeExported || vm.borrowingNativeCall {
		return
	}
	vm.markGuestCodeExported()
}

// markGuestCodeExported sets guestCodeExported on vm and on every VM parked below it on
// its goroutine, each blocked in the native call that led to the run above it.
func (vm *VM) markGuestCodeExported() {
	var walkedInline [servedChainMax]*VM
	walked := walkedInline[:0]
	var gates exportGateSet
	for current := vm; current != nil; current = current.servedChainRun.below {
		if current.exportChainMarked() {
			gates = current.exportChainGates
			break
		}
		current.guestCodeExported = true
		current.exportShares = true
		current.noteFamilyExporter()
		for _, gate := range current.spawnGates {
			gate.exportedBySpawned.Store(true)
		}
		walked = append(walked, current)
	}
	for _, current := range slices.Backward(walked) {
		if !current.reentrantInterpreterVM {
			gate := current.ensureExportGate()
			gate.exportedHere = true
			gates = gates.with(gate)
		}
		gates = gates.with(current.runGates...)
		gates = gates.with(current.spawnGates...)
		current.noteExportChain(gates)
	}
	vm.publishExportedGates(gates)
}

// exportChainMarked reports whether markGuestCodeExported has marked the VM and every VM
// parked below it in the VM's current run, so exportChainGates holds their gates.
//
// Returns bool which is false for a VM not registered on its goroutine
// (enterServedChain).
func (vm *VM) exportChainMarked() bool {
	serial := vm.servedChainRun.exportSerial
	return serial != 0 && serial == vm.exportChainSerial && vm.servedChainRun.token.Load() != 0
}

// noteExportChain records gates as the set gathered from the VM down in its current run,
// when the VM is registered on its goroutine.
//
// Takes gates (exportGateSet) which is the set.
func (vm *VM) noteExportChain(gates exportGateSet) {
	if vm.servedChainRun.token.Load() == 0 {
		return
	}
	serial := vm.servedChainRun.exportSerial
	if serial == 0 {
		serial = exportMarkSerials.Add(1)
		vm.servedChainRun.exportSerial = serial
	}
	vm.exportChainSerial = serial
	vm.exportChainGates = gates
}

// beginBorrowingNativeCall reports whether a native call about to be prepared keeps
// nothing it is handed once it returns, setting borrowingNativeCall when so. The caller
// clears it (endBorrowingNativeCall) when the call returns.
//
// Takes function (reflect.Value) which is the native callee.
// Takes class (BorrowClass) which is its class when classed is true.
// Takes classed (bool) which is true when the caller has classed it.
//
// Returns bool which is true when the flag was set by this call.
func (vm *VM) beginBorrowingNativeCall(function reflect.Value, class BorrowClass, classed bool) bool {
	if vm.borrowingNativeCall || !vm.Arena.countsHandOffs() {
		return false
	}
	if !classed {
		if class, classed = nativeCallClass(vm.Arena, function); !classed {
			return false
		}
	}
	if class != BorrowBorrows && class != BorrowReadsOnly {
		return false
	}
	vm.borrowingNativeCall = true
	return true
}

// endBorrowingNativeCall clears what beginBorrowingNativeCall set.
func (vm *VM) endBorrowingNativeCall() {
	vm.borrowingNativeCall = false
}

// resetLicencePolicy clears the licence and the flags attachLicencePolicy sets, which a
// pooled arena must not carry into its next owner. Reset calls it after release has
// handled revoked storage, however the arena was acquired.
func (a *RegisterArena) resetLicencePolicy() {
	clear(a.bytePlacementSites)
	a.bytePlacementSites = a.bytePlacementSites[:0]
	a.clearLicence()
	a.licensed = false
	a.licenceAlwaysShared = false
	a.licenceFamilyShared = false
	a.setLicenceVM(nil)
	a.disableMinorGC = false
	a.recycleChunks = ArenaChunkRecyclingEnabled
}

// revokeBlind stands in for a walk skipped because the epoch is already revoked, when
// release abandons every data slab whatever the walk would have named.
//
// Concurrency: Coordinates blind-counter publication with licenceSealMu; the caller must
// own the arena's mutable licence state.
func (a *RegisterArena) revokeBlind() {
	state := &a.licenceState
	if !state.banksRevoked {
		state.banksRevoked = true
		state.revision++
	}
	callback := a.servesNativeCaller()
	if licenceCounting {
		stats := a.stats()
		stats.blindHandOffs.Add(1)
		if callback {
			stats.blindCallbackHandOffs.Add(1)
		}
	}
	if a.blindLocal() {
		if licenceCounting {
			blindScopeCounters.localBlind.Add(1)
		}
		return
	}
	global, chain := a.blindCallbackScope(callback)
	tree := &a.blindCounters().all
	if !tree.stale() && (!global || !licenceBlindCallbacks.all.stale()) && !servedChainStale(chain, false) {
		return
	}
	if !state.foreignLocked {
		licenceSealMu.RLock()
		defer licenceSealMu.RUnlock()
	}
	step := uint64(0)
	if tree.bump() {
		step++
	}
	if global && licenceBlindCallbacks.all.bump() {
		step++
	}
	if len(chain) != 0 {
		bumpServedChain(chain, false)
	}
	a.advanceOwnBlindMark(step, false)
}

// blindCallbackScope chooses where to count a callback arena's blind hand-off in addition
// to its own tree.
//
// Takes callback (bool) which is servesNativeCaller.
//
// Returns bool which is true when the hand-off must be counted process-wide.
// Returns []*RegisterArena which holds the served chain when process-wide counting is
// unnecessary.
func (a *RegisterArena) blindCallbackScope(callback bool) (bool, []*RegisterArena) {
	if !callback {
		return false, nil
	}
	if servedChainEngaged.Load() {
		if chain, ok := a.servedChain(); ok {
			return false, chain
		}
		countChainFallback()
	}
	return true, nil
}

// advanceOwnBlindMark moves the arena's mark past its own increments, since it has
// revoked directly what its skipped walk might have named, unless another arena counted
// meanwhile.
//
// Takes step (uint64) which is the number of increments the arena just made.
// Takes bytes (bool) which selects the byte-only counters.
func (a *RegisterArena) advanceOwnBlindMark(step uint64, bytes bool) {
	state := &a.licenceState
	if step == 0 || !state.blindMarked {
		return
	}
	var total, byteTotal uint64
	if state.epochRevoked && state.banksRevoked && !bytes {
		total, byteTotal = a.blindTotals()
	} else {
		total, byteTotal = a.readBlindTotals()
	}
	switch {
	case !bytes && state.blindMark == total-step:
		state.blindMark = total
	case bytes && state.blindByteMark == byteTotal-step:
		state.blindByteMark = byteTotal
	}
}

// servesNativeCaller reports whether the arena belongs to a callback or bound-method VM
// running guest code for a native call.
//
// Returns bool which is true for such an arena.
func (a *RegisterArena) servesNativeCaller() bool {
	present, reentrant := a.servingVMReentrant()
	return present && reentrant
}

// servingVMReentrant reports whether the arena serves a VM (its licence VM, or else its
// owner) and whether that VM is a callback or bound-method VM, from the cache in the
// licence state (servingKnown).
//
// Returns serving (bool) which is true when there is a serving VM.
// Returns reentrant (bool) which is that VM's reentrantInterpreterVM.
func (a *RegisterArena) servingVMReentrant() (serving, reentrant bool) {
	state := &a.licenceState
	if !state.servingKnown {
		vm := a.licenceVM
		if vm == nil {
			vm = a.ownerVM
		}
		state.servingKnown, state.servingPresent, state.servingReentrant = true, vm != nil, vm != nil && vm.reentrantInterpreterVM
	}
	return state.servingPresent, state.servingReentrant
}

// setLicenceVM sets the VM the licence attributes the arena's hand-offs to, and empties
// the serving-VM cache and revokedLocalBlind, which it feeds.
//
// Takes vm (*VM) which is the VM, or nil.
func (a *RegisterArena) setLicenceVM(vm *VM) {
	a.licenceVM = vm
	a.licenceState.servingKnown = false
	a.licenceState.revokedLocalBlind = false
	a.refreshFamilyShared()
}

// setOwnerVM sets the VM that owns the arena, and empties the serving-VM cache and
// revokedLocalBlind, which it feeds.
//
// Takes vm (*VM) which is the VM, or nil.
func (a *RegisterArena) setOwnerVM(vm *VM) {
	a.ownerVM = vm
	a.licenceState.servingKnown = false
	a.licenceState.revokedLocalBlind = false
	a.refreshFamilyShared()
}

// blindTotals reads the sum of the arena's tree counters, process-wide callback counters
// and own served-chain counters in chainBlind.
//
// Returns all (uint64) which is the sum for skipped walks of every kind.
// Returns bytes (uint64) which is the sum for byte-only skipped walks.
func (a *RegisterArena) blindTotals() (all uint64, bytes uint64) {
	counters, chain := a.blindCounters(), &a.licenceState.chainBlind
	all = counters.all.Load() + licenceBlindCallbacks.all.Load() + chain.all.Load()
	bytes = counters.bytes.Load() + licenceBlindCallbacks.bytes.Load() + chain.bytes.Load()
	return all, bytes
}

// readBlindTotals is blindTotals taken as a reading that a mark will be set from.
//
// Returns all (uint64) which is the sum for skipped walks of every kind.
// Returns bytes (uint64) which is the sum for byte-only skipped walks.
func (a *RegisterArena) readBlindTotals() (all uint64, bytes uint64) {
	counters, chain := a.blindCounters(), &a.licenceState.chainBlind
	all = counters.all.read() + licenceBlindCallbacks.all.read() + chain.all.read()
	bytes = counters.bytes.read() + licenceBlindCallbacks.bytes.read() + chain.bytes.read()
	return all, bytes
}

// blindCounters returns the blind counters of the arena's goroutine tree.
//
// Returns *licenceBlindCounters which is never nil.
func (a *RegisterArena) blindCounters() *licenceBlindCounters {
	if source := a.licenceState.blindSource; source != nil {
		return source
	}
	return &licenceBlindProcess
}

// revokeByteSlabs revokes the current and retired byte slabs.
func (a *RegisterArena) revokeByteSlabs() {
	a.revokeByteSlabsFrom(true)
}

// lockForeign takes licenceForeignRevokedMu, and licenceSealMu exclusively, for the
// arena's goroutine.
func (a *RegisterArena) lockForeign() {
	licenceForeignRevokedMu.Lock()
	licenceSealMu.Lock()
	a.licenceState.foreignLocked = true
}

// unlockForeign releases the locks lockForeign took.
//
// Concurrency: Requires the caller to hold the locks acquired by lockForeign and have
// exclusive access to the arena's licence state.
func (a *RegisterArena) unlockForeign() {
	a.licenceState.foreignLocked = false
	licenceSealMu.Unlock()
	licenceForeignRevokedMu.Unlock()
}

// readBlindHandOffs reads the tree's blind counters against the arena's mark without
// moving the mark.
//
// Returns blindReading which is the reading.
func (a *RegisterArena) readBlindHandOffs() blindReading {
	state := &a.licenceState
	current, currentBytes := a.readBlindTotals()
	reading := blindReading{current: current, currentBytes: currentBytes, all: false, bytes: false}
	reading.all = !state.blindMarked || reading.current != state.blindMark
	reading.bytes = !state.blindMarked || reading.currentBytes != state.blindByteMark
	return reading
}

// commitBlindReading starts the next blind interval at an applied reading.
//
// Takes reading (blindReading) which has been applied.
//
// Panics with the injected abort error when the blind-reading window site fires in a
// pipit_testhooks build.
func (a *RegisterArena) commitBlindReading(reading blindReading) {
	if abortInjectBuilt && abortInjectFires(abortSiteBlindWindow) {
		panic(abortInjectedError(abortSiteBlindWindow))
	}
	state := &a.licenceState
	state.blindMarked = true
	state.blindMark, state.blindByteMark = reading.current, reading.currentBytes
}

// applyBlindHandOffs revokes what the tree's skipped walks since the previous reading
// might have named. A revoked epoch already abandons every data slab, but not the
// register banks unless a hand-off referenced one, so a skipped walk that might have
// named any storage still revokes them.
//
// Returns bool which is true when anything was skipped.
func (a *RegisterArena) applyBlindHandOffs() bool {
	reading := a.readBlindHandOffs()
	switch {
	case a.licenceState.epochRevoked:
		if reading.all {
			a.revokeEpoch(epochRegisterBank, noHandOffCause)
		}
	case reading.all:
		a.revokeForeignReachable()
	case reading.bytes:
		a.revokeByteSlabs()
	}
	a.commitBlindReading(reading)
	return reading.all || reading.bytes
}

// revokeForeignReachable revokes every slab of every class and the register banks,
// covering everything a foreign revocation could name.
func (a *RegisterArena) revokeForeignReachable() {
	a.revokeEpoch(epochBlindForeign, noHandOffCause)
	a.revokeEpoch(epochRegisterBank, noHandOffCause)
}
