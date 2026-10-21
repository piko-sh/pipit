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

//go:build pipit_reuse_audit

package engine

import (
	"math"
	"runtime"
	"runtime/debug"
	"slices"
	"sync"
	"sync/atomic"
	"unsafe"
	"weak"
)

const (
	// arenaAuditEnabled gates the weak-reachability reuse audit.
	arenaAuditEnabled = true

	// licencePageCheck checks the licence's page-table answers against the arena's own slab
	// lists.
	licencePageCheck = true

	// auditDefaultMaxPasses caps the forced collections in one audit.
	auditDefaultMaxPasses = 6

	// auditMinSlabBytes is the tiny-allocator guard. Pointer-free objects at or under 16
	// bytes may share an allocation slot with unrelated live objects, so a weak pointer to
	// one may never read nil.
	auditMinSlabBytes = 16

	// slabByte identifies immutable byte slabs in reuse audit records.
	slabByte = "byteSlab"

	// slabGenericBytes identifies generic byte slabs in reuse audit records.
	slabGenericBytes = "genericBytes"

	// slabSliceHeader identifies slice headers in reuse audit records.
	slabSliceHeader = "sliceHeader"

	// slabIntBacking identifies backing arrays of integer slices in reuse audit records.
	slabIntBacking = "intBacking"

	// slabFloatBacking identifies backing arrays of floating point slices in reuse audit
	// records.
	slabFloatBacking = "floatBacking"

	// slabStringBacking identifies backing arrays of string slices in reuse audit records.
	slabStringBacking = "stringBacking"

	// slabBoolBacking identifies backing arrays of boolean slices in reuse audit records.
	slabBoolBacking = "boolBacking"

	// slabUintBacking identifies backing arrays of unsigned integer slices in reuse audit
	// records.
	slabUintBacking = "uintBacking"

	// slabStringBox identifies boxed string values in reuse audit records.
	slabStringBox = "stringBox"

	// slabIntBox identifies boxed integer values in reuse audit records.
	slabIntBox = "intBox"

	// slabFloatBox identifies boxed floating point values in reuse audit records.
	slabFloatBox = "floatBox"

	// slabUintBox identifies boxed unsigned integer values in reuse audit records.
	slabUintBox = "uintBox"

	// slabComplexBox identifies boxed complex values in reuse audit records.
	slabComplexBox = "complexBox"

	// slabUpvalueCell identifies upvalue cell slabs in reuse audit records.
	slabUpvalueCell = "upvalueCell"

	// slabUpvalueRef identifies upvalue reference slabs in reuse audit records.
	slabUpvalueRef = "upvalueRef"

	// slabClosureChunkCells identifies upvalue cells held in closure chunks in reuse audit
	// records.
	slabClosureChunkCells = "closureChunkCells"

	// slabClosureChunks identifies runtime closures held in closure chunks in reuse audit
	// records.
	slabClosureChunks = "closureChunkClosures"

	// slabKRef identifies KRef chunks in reuse audit records.
	slabKRef = "krefChunk"

	// slabMutByte identifies mutable byte slabs in reuse audit records.
	slabMutByte = "mutByteSlab"

	// slabStringRegs identifies string register banks in reuse audit records.
	slabStringRegs = "stringRegs"

	// slabGeneralRegs identifies general register banks in reuse audit records.
	slabGeneralRegs = "generalRegs"

	// slabSlicesInt identifies register banks for integer slices in reuse audit records.
	slabSlicesInt = "slicesIntRegs"

	// slabSlicesFloat identifies register banks for floating point slices in reuse audit
	// records.
	slabSlicesFloat = "slicesFloatRegs"

	// slabSlicesString identifies register banks for string slices in reuse audit records.
	slabSlicesString = "slicesStringRegs"

	// slabSlicesBool identifies register banks for boolean slices in reuse audit records.
	slabSlicesBool = "slicesBoolRegs"

	// slabSlicesUint identifies register banks for unsigned integer slices in reuse audit
	// records.
	slabSlicesUint = "slicesUintRegs"

	// slabSlicesByte identifies register banks for byte slices in reuse audit records.
	slabSlicesByte = "slicesByteRegs"
)

// arenaAudit is embedded in RegisterArena and holds its audit provenance.
type arenaAudit struct {
	// audit is the arena's current provenance.
	audit arenaAuditState
}

// arenaAuditState is the per-arena provenance the audit stamps onto records.
type arenaAuditState struct {
	// vmKind names the kind of VM that acquired the arena.
	vmKind string

	// runID identifies the current acquisition.
	runID uint64
}

// auditRecord is one recorded slab. It holds only a weak pointer and plain numbers, never
// a typed pointer or slice header into the slab.
type auditRecord struct {
	// pointer weakly references the slab's first element.
	pointer weak.Pointer[byte]

	// slabKind names the slab class.
	slabKind string

	// vmKind names the kind of VM that acquired the arena.
	vmKind string

	// function names the running function when recorded, if known.
	function string

	// opcode names the instruction at pc, if known.
	opcode string

	// address is the slab's start address.
	address uintptr

	// ownerID is the arena's slab-registry owner.
	ownerID uint64

	// runID is the arena acquisition the slab belonged to.
	runID uint64

	// id is the record's sequence number.
	id uint64

	// capBytes is the slab's capacity in bytes.
	capBytes int

	// usedBytes is the slab's bump-allocated prefix in bytes.
	usedBytes int

	// programCounter is the program counter of function's frame, or -1.
	programCounter int

	// arenaHeaderRefs counts the arena's own slice-header slots whose Data pointed into the
	// slab when MinorGC recorded it.
	arenaHeaderRefs int

	// gcCount is the arena's MinorGC count when recorded.
	gcCount uint32

	// event is the reuse event.
	event AuditEvent

	// fromGrow is true for retired slabs and free chunks.
	fromGrow bool

	// midRunSeen is true once a mid-run checkpoint found the slab reachable.
	midRunSeen bool

	// rangeOnly is true for a run of clean pages a licensed release kept as a slab
	// (page-granular revocation, register_arena_licence_pages.go).
	rangeOnly bool
}

// auditSite is the cheaply available execution position when a slab is recorded.
type auditSite struct {
	// function is the running function's qualified name.
	function string

	// opcode names the instruction at pc.
	opcode string

	// programCounter is the frame's program counter, or -1 when unknown.
	programCounter int
}

// auditRecorder carries the provenance shared by every slab recorded at one event.
type auditRecorder struct {
	// arena is the arena whose slabs are being recorded.
	arena *RegisterArena

	// site is the execution position.
	site auditSite

	// headerData holds the sorted Data addresses of the arena's slice-header slots, for
	// mid-run records; nil at release, where the header slabs are cleared.
	headerData []uintptr

	// event is the reuse event.
	event AuditEvent
}

// auditLogState is the process-wide record store.
type auditLogState struct {
	// activeRuns holds the run ids acquired and not yet released.
	activeRuns map[uint64]struct{}

	// records holds every unsettled record in id order.
	records []auditRecord

	// counters holds the running totals.
	counters AuditCounters

	// mu guards records and counters.
	mu sync.Mutex

	// runMu serialises audits so a record set is examined by one audit at a time.
	runMu sync.Mutex
}

var (
	// auditLog is the process-wide record store.
	auditLog auditLogState

	// auditNextRecordID numbers records.
	auditNextRecordID atomic.Uint64

	// auditNextRunID numbers arena acquisitions.
	auditNextRunID atomic.Uint64

	// auditMidRunEvery is the mid-run audit interval in GC checkpoints, set by
	// AuditSetMidRunEvery; zero, the default, disables mid-run audits.
	auditMidRunEvery atomic.Uint64

	// auditCheckpointCalls counts GC checkpoints seen by auditMidRunCheckpoint.
	auditCheckpointCalls atomic.Uint64

	// auditLeakSink is the package global the unbarriered-store controls leak arena memory
	// into.
	auditLeakSink struct {
		header unsafe.Pointer
		text   string
		bytes  []byte
	}
)

// AuditReuse runs a release audit over every unsettled record.
//
// Takes opts (AuditOptions) which tunes the pass count and record retention.
//
// Returns []AuditHit which lists the hits in record order.
//
// Concurrency: Serialises audit passes and resets with auditLog.runMu and protects the
// shared records with auditLog.mu.
func AuditReuse(opts AuditOptions) []AuditHit {
	auditLog.runMu.Lock()
	defer auditLog.runMu.Unlock()
	pointers := auditSnapshot()
	reachable, passes, stable := auditCollect(pointers, opts.MaxPasses)
	cleared := auditResolveRanges(reachable)

	auditLog.mu.Lock()
	defer auditLog.mu.Unlock()
	auditLog.counters.ReleaseAudits++
	auditLog.counters.RangeCleared += uint64(len(cleared))
	var hits []AuditHit
	settled := len(pointers)
	pending := auditLog.records[:0:0]
	for position := range settled {
		record := &auditLog.records[position]
		if _, active := auditLog.activeRuns[record.runID]; active && record.event != AuditEventRelease && !opts.IncludeActiveRuns {
			pending = append(pending, *record)
			continue
		}
		switch {
		case cleared[position]:
		case reachable[position] && record.event == AuditEventRelease:
			hits = append(hits, record.hit(AuditReleaseHit, passes, stable))
		case reachable[position]:
			hits = append(hits, record.hit(AuditOutsideReach, passes, stable))
		case record.midRunSeen:
			hits = append(hits, record.hit(AuditInRunOnly, passes, stable))
		}
	}
	if !opts.KeepRecords {
		pending = append(pending, auditLog.records[settled:]...)
		clear(auditLog.records)
		auditLog.records = pending
	}
	auditLog.counters.Pending = uint64(len(auditLog.records))
	auditCountHits(hits)
	return hits
}

// auditResolveRanges tests each reachable range record (a run of clean pages a licensed
// release kept) by address range.
//
// Takes reachable ([]bool) which is the weak-pointer reachability per record.
//
// Returns map[int]bool which holds the positions cleared.
//
// Concurrency: Requires the caller to hold auditLog.runMu; protects access to the shared
// records with auditLog.mu.
func auditResolveRanges(reachable []bool) map[int]bool {
	var positions []int
	var ranges []auditAddressRange
	auditLog.mu.Lock()
	for position := range reachable {
		record := &auditLog.records[position]
		if reachable[position] && record.rangeOnly {
			positions = append(positions, position)
			ranges = append(ranges, auditAddressRange{start: record.address, end: record.address + uintptr(record.capBytes)})
		}
	}
	auditLog.mu.Unlock()
	cleared := map[int]bool{}
	if len(ranges) == 0 {
		return cleared
	}
	referenced, err := auditRangesReferenced(ranges)
	if err != nil {
		return cleared
	}
	for index, position := range positions {
		if !referenced[index] {
			cleared[position] = true
		}
	}
	return cleared
}

// auditCheckpoint runs a mid-run audit over the chunk-recycle and mark-dead-drop records
// no checkpoint has yet found reachable.
//
// Takes opts (AuditOptions) which tunes the pass count; KeepRecords is ignored.
//
// Returns []AuditHit which lists the new in-run-unrooted hits.
//
// Concurrency: Serialises audit passes with auditLog.runMu and protects records with
// auditLog.mu; the caller must invoke it at a collector safepoint.
func auditCheckpoint(opts AuditOptions) []AuditHit {
	auditLog.runMu.Lock()
	defer auditLog.runMu.Unlock()
	pointers := auditSnapshot()
	reachable, passes, stable := auditCollect(pointers, opts.MaxPasses)

	auditLog.mu.Lock()
	defer auditLog.mu.Unlock()
	auditLog.counters.MidRunChecks++
	var hits []AuditHit
	for position := range pointers {
		record := &auditLog.records[position]
		if record.event != AuditEventRelease && !record.midRunSeen && reachable[position] {
			record.midRunSeen = true
			hits = append(hits, record.hit(AuditInRunUnrooted, passes, stable))
		}
	}
	auditCountHits(hits)
	return hits
}

// AuditSetMidRunEvery sets how often GC checkpoints run a mid-run audit.
//
// Takes every (uint64) which runs an audit at every Nth checkpoint; zero disables them.
//
// Returns uint64 which is the previous setting.
func AuditSetMidRunEvery(every uint64) uint64 {
	return auditMidRunEvery.Swap(every)
}

// AuditStats returns the running totals since the process started.
//
// Returns AuditCounters which is a snapshot of the totals.
//
// Concurrency: Copies the shared counters while holding auditLog.mu.
func AuditStats() AuditCounters {
	auditLog.mu.Lock()
	defer auditLog.mu.Unlock()
	counters := auditLog.counters
	counters.Pending = uint64(len(auditLog.records))
	return counters
}

// auditSnapshot copies the weak pointers of the current records.
//
// Returns the weak pointers in record order.
//
// Concurrency: Requires auditLog.runMu to keep record indices stable and takes
// auditLog.mu while copying weak pointers.
func auditSnapshot() []weak.Pointer[byte] {
	auditLog.mu.Lock()
	defer auditLog.mu.Unlock()
	pointers := make([]weak.Pointer[byte], len(auditLog.records))
	for index := range auditLog.records {
		pointers[index] = auditLog.records[index].pointer
	}
	return pointers
}

// auditCollect forces collections until two consecutive passes report the same reachable
// set, or maxPasses run out.
//
// Takes pointers ([]weak.Pointer[byte]) which are the records to probe.
// Takes maxPasses (int) which caps the collections; zero selects the default.
//
// Returns the per-record reachability, the passes run, and whether two passes agreed.
func auditCollect(pointers []weak.Pointer[byte], maxPasses int) (reachable []bool, passes int, stable bool) {
	if maxPasses <= 0 {
		maxPasses = auditDefaultMaxPasses
	}
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	defer debug.SetMemoryLimit(debug.SetMemoryLimit(math.MaxInt64))
	var previous []bool
	for passes < maxPasses {
		runtime.GC()
		runtime.Gosched()
		passes++
		current := auditProbe(pointers)
		if previous != nil && slicesEqualBool(previous, current) {
			return current, passes, true
		}
		previous = current
	}
	return previous, passes, false
}

// auditProbe reads each weak pointer without keeping its referent.
//
// Takes pointers ([]weak.Pointer[byte]) which are the records to probe.
//
// Returns the per-record reachability.
//
//go:noinline
func auditProbe(pointers []weak.Pointer[byte]) []bool {
	reachable := make([]bool, len(pointers))
	for index := range pointers {
		reachable[index] = pointers[index].Value() != nil
	}
	return reachable
}

// slicesEqualBool reports whether two reachability sets match.
//
// Takes left ([]bool) which is the first reachability set.
// Takes right ([]bool) which is the second reachability set.
//
// Returns bool which is true when they match element for element.
func slicesEqualBool(left, right []bool) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// auditCountHits adds hits to the per-class counters. The caller holds auditLog.mu.
//
// Takes hits ([]AuditHit) which are the hits to count.
func auditCountHits(hits []AuditHit) {
	for index := range hits {
		switch hits[index].Class {
		case AuditReleaseHit:
			auditLog.counters.ReleaseHits++
		case AuditInRunUnrooted:
			auditLog.counters.InRunUnrootedHits++
		case AuditOutsideReach:
			auditLog.counters.OutsideReachHits++
		case AuditInRunOnly:
			auditLog.counters.InRunOnlyHits++
		}
	}
}

// hit converts the record into a reported hit.
//
// Takes class (AuditHitClass) which is the classification.
// Takes passes (int) which is the number of collections the audit ran.
// Takes stable (bool) which is true when two passes agreed.
//
// Returns AuditHit which carries the record's provenance.
func (record *auditRecord) hit(class AuditHitClass, passes int, stable bool) AuditHit {
	return AuditHit{
		SlabKind: record.slabKind, VMKind: record.vmKind, Function: record.function, Opcode: record.opcode,
		RecordID: record.id, OwnerID: record.ownerID, RunID: record.runID, Address: record.address,
		CapBytes: record.capBytes, UsedBytes: record.usedBytes, PC: record.programCounter, Passes: passes,
		GCCount: record.gcCount, Class: class, Event: record.event, FromGrow: record.fromGrow,
		MidRunSeen: record.midRunSeen, Stable: stable, ArenaHeaderRefs: record.arenaHeaderRefs,
	}
}

// auditSiteOf captures the running function and instruction of vm, when it has a frame.
//
// Takes vm (*VM) which may be nil.
//
// Returns auditSite which is empty with pc -1 when nothing is known.
func auditSiteOf(vm *VM) auditSite {
	site := auditSite{function: "", opcode: "", programCounter: -1}
	if vm == nil || vm.FramePointer < 0 || vm.FramePointer >= len(vm.CallStack) {
		return site
	}
	frame := &vm.CallStack[vm.FramePointer]
	if frame.Function == nil {
		return site
	}
	site.function = frame.Function.Name
	site.programCounter = frame.ProgramCounter
	if site.programCounter >= 0 && site.programCounter < len(frame.Function.Body) {
		site.opcode = frame.Function.Body[site.programCounter].Op.String()
	}
	return site
}

// auditRecordSlab records one slab.
//
// Takes recorder (*auditRecorder) which carries the event provenance.
// Takes slab ([]T) which is the slab; only its address is kept, weakly.
// Takes slabKind (string) which names the slab class.
// Takes used (int) which is the bump-allocated prefix in elements.
// Takes fromGrow (bool) which is true for retired slabs and free chunks.
func auditRecordSlab[T any](recorder *auditRecorder, slab []T, slabKind string, used int, fromGrow bool) {
	var zero T
	elementBytes := int(unsafe.Sizeof(zero))
	auditRecordRange(recorder, unsafe.Pointer(unsafe.SliceData(slab)), cap(slab)*elementBytes,
		min(used, cap(slab))*elementBytes, slabKind, fromGrow)
}

// auditRecordRange is auditRecordSlab for a slab given by its first byte and sizes, as a
// KRef chunk (a reflect.Value slab) is.
//
// Takes recorder (*auditRecorder) which carries the event provenance.
// Takes first (unsafe.Pointer) which is the slab's first byte; only its address is kept,
// weakly.
// Takes capBytes (int) which is the slab's capacity in bytes.
// Takes usedBytes (int) which is its bump-allocated prefix in bytes.
// Takes slabKind (string) which names the slab class.
// Takes fromGrow (bool) which is true for retired slabs and free chunks.
//
// Concurrency: Protects the shared record list with auditLog.mu; the caller must hold
// auditLog.runMu and own the recorder state.
func auditRecordRange(recorder *auditRecorder, first unsafe.Pointer, capBytes, usedBytes int, slabKind string, fromGrow bool) {
	auditLog.mu.Lock()
	defer auditLog.mu.Unlock()
	if capBytes == 0 || first == zeroSizeAllocPtr {
		auditLog.counters.SkippedEmpty++
		return
	}
	if capBytes < auditMinSlabBytes {
		auditLog.counters.SkippedSmall++
		return
	}
	data := (*byte)(first)
	start := uintptr(unsafe.Pointer(data))
	arena := recorder.arena
	rangeOnly := false
	if end, kept := arena.keptPageRunAt(start); kept && end == start+uintptr(capBytes) && recorder.event == AuditEventRelease {
		rangeOnly = true
		auditLog.counters.RangeRecorded++
	}
	auditLog.records = append(auditLog.records, auditRecord{
		pointer: weak.Make(data), slabKind: slabKind, vmKind: arena.audit.vmKind,
		function: recorder.site.function, opcode: recorder.site.opcode,
		id: auditNextRecordID.Add(1), ownerID: arena.slabOwner, runID: arena.audit.runID,
		address: start, capBytes: capBytes, arenaHeaderRefs: auditCountInRange(recorder.headerData, start, start+uintptr(capBytes)), usedBytes: usedBytes,
		programCounter: recorder.site.programCounter, gcCount: arena.gcCount, event: recorder.event, fromGrow: fromGrow,
		midRunSeen: false, rangeOnly: rangeOnly,
	})
	auditLog.counters.Recorded++
	switch recorder.event {
	case AuditEventRelease:
		auditLog.counters.RecordedRelease++
	case AuditEventChunkRecycle:
		auditLog.counters.RecordedChunkRecycle++
	case AuditEventMarkDeadDrop:
		auditLog.counters.RecordedMarkDeadDrop++
	default:
	}
}

// auditRecordList records every slab of a retired-slab or free-chunk list.
//
// Takes recorder (*auditRecorder) which carries the event provenance.
// Takes list ([][]T) which is the list.
// Takes slabKind (string) which names the slab class.
// Takes clearContents (bool) which is true for pointer-bearing classes, whose dropped
// slabs are cleared so they cannot keep other slabs reachable.
func auditRecordList[T any](recorder *auditRecorder, list [][]T, slabKind string, clearContents bool) {
	for _, slab := range list {
		auditRecordSlab(recorder, slab, slabKind, len(slab), true)
		if clearContents {
			clear(slab[:cap(slab)])
		}
	}
}

// auditReplaceSlab records the current slab of a class, optionally clears it, and returns
// a fresh slab of the same length. A nil slab stays nil.
//
// Takes recorder (*auditRecorder) which carries the event provenance.
// Takes slab ([]T) which is the class's current slab.
// Takes slabKind (string) which names the slab class.
// Takes used (int) which is the bump-allocated prefix in elements.
// Takes clearContents (bool) which is true for pointer-bearing classes.
//
// Returns []T which is the fresh slab to install.
func auditReplaceSlab[T any](recorder *auditRecorder, slab []T, slabKind string, used int, clearContents bool) []T {
	if slab == nil {
		return nil
	}
	auditRecordSlab(recorder, slab, slabKind, used, false)
	if clearContents {
		clear(slab[:cap(slab)])
	}
	return make([]T, len(slab))
}

// auditAcquired starts a new audit run on the arena.
//
// Takes vmKind (string) which names the kind of VM acquiring it.
//
// Concurrency: Protects the active-run table with auditLog.mu; the caller must own the
// arena during acquisition.
func (a *arenaAudit) auditAcquired(vmKind string) {
	a.audit.runID = auditNextRunID.Add(1)
	a.audit.vmKind = vmKind
	auditLog.mu.Lock()
	defer auditLog.mu.Unlock()
	if auditLog.activeRuns == nil {
		auditLog.activeRuns = make(map[uint64]struct{})
	}
	auditLog.activeRuns[a.audit.runID] = struct{}{}
}

// auditRunEnded removes the arena's run from the active set.
//
// Concurrency: Protects the active-run table with auditLog.mu; the caller must own the
// arena during release.
func (a *arenaAudit) auditRunEnded() {
	auditLog.mu.Lock()
	defer auditLog.mu.Unlock()
	delete(auditLog.activeRuns, a.audit.runID)
}

// auditRelease records every slab the next owner of the arena would reuse, clears or
// poisons each as Reset would, and installs a fresh slab of the same length in its place.
//
// Concurrency: Holds auditLog.runMu while replacing slabs; the caller must have exclusive
// access to the arena.
func (a *RegisterArena) auditRelease() {
	auditLog.runMu.Lock()
	defer auditLog.runMu.Unlock()
	if arenaParanoidEnabled {
		a.poisonReleasedStringBytes()
		a.poisonReleasedGenericBytes()
	}
	recorder := &auditRecorder{arena: a, site: auditSiteOf(a.ownerVM), headerData: nil, event: AuditEventRelease}
	a.auditReleaseByteSlabs(recorder)
	a.auditReleaseChunkedSlabs(recorder)
	a.auditReleaseBackingSlabs(recorder)
	a.auditReleaseBoxSlabs(recorder)
	a.auditReleaseKRef(recorder)
	a.auditReleaseClosureChunks(recorder)
	a.auditReleaseRegisterBanks(recorder)
	a.auditClearFrameReferences()
	a.auditRunEnded()
}

// auditReleaseByteSlabs records and replaces the string byte slab and its retired
// generations, keeping the arena slab registry current.
//
// Takes recorder (*auditRecorder) which carries the event provenance.
func (a *RegisterArena) auditReleaseByteSlabs(recorder *auditRecorder) {
	auditRecordList(recorder, a.oldByteSlabs, slabByte, false)
	for _, slab := range a.oldByteSlabs {
		forgetArenaSlab(slab)
	}
	clear(a.oldByteSlabs)
	a.oldByteSlabs = a.oldByteSlabs[:0]
	auditRecordSlab(recorder, a.byteSlab, slabByte, a.byteIndex, false)
	if a.byteSlab != nil {
		a.replaceByteSlab(make([]byte, len(a.byteSlab)), false)
	}
	auditRecordList(recorder, a.oldMutByteSlabs, slabMutByte, false)
	clear(a.oldMutByteSlabs)
	a.oldMutByteSlabs = a.oldMutByteSlabs[:0]
	a.mutByteSlab = auditReplaceSlab(recorder, a.mutByteSlab, slabMutByte, a.mutByteIndex, false)
}

// auditReleaseChunkedSlabs records and replaces the generic-bytes and slice-header slabs,
// their retired generations and their free chunks.
//
// Takes recorder (*auditRecorder) which carries the event provenance.
func (a *RegisterArena) auditReleaseChunkedSlabs(recorder *auditRecorder) {
	auditRecordList(recorder, a.oldGenericByteSlabs, slabGenericBytes, false)
	auditRecordList(recorder, a.freeGenericByteChunks, slabGenericBytes, false)
	clear(a.oldGenericByteSlabs)
	a.oldGenericByteSlabs = a.oldGenericByteSlabs[:0]
	clear(a.freeGenericByteChunks)
	a.freeGenericByteChunks = a.freeGenericByteChunks[:0]
	a.genericBytesSlab = auditReplaceSlab(recorder, a.genericBytesSlab, slabGenericBytes, a.genericBytesIndex, false)

	auditRecordList(recorder, a.oldSliceHeaderSlabs, slabSliceHeader, true)
	auditRecordList(recorder, a.freeSliceHeaderChunks, slabSliceHeader, true)
	clear(a.oldSliceHeaderSlabs)
	a.oldSliceHeaderSlabs = a.oldSliceHeaderSlabs[:0]
	clear(a.freeSliceHeaderChunks)
	a.freeSliceHeaderChunks = a.freeSliceHeaderChunks[:0]
	a.sliceHeaderSlab = auditReplaceSlab(recorder, a.sliceHeaderSlab, slabSliceHeader, a.sliceHeaderIndex, true)
}

// auditReleaseBackingSlabs records and replaces the typed-slice backing slabs, current
// and retired.
//
// Takes recorder (*auditRecorder) which carries the event provenance.
func (a *RegisterArena) auditReleaseBackingSlabs(recorder *auditRecorder) {
	auditRecordList(recorder, a.oldIntBackings, slabIntBacking, false)
	clear(a.oldIntBackings)
	a.oldIntBackings = a.oldIntBackings[:0]
	a.intBackingSlab = auditReplaceSlab(recorder, a.intBackingSlab, slabIntBacking, a.intBackingIndex, false)

	auditRecordList(recorder, a.oldFloatBackings, slabFloatBacking, false)
	clear(a.oldFloatBackings)
	a.oldFloatBackings = a.oldFloatBackings[:0]
	a.floatBackingSlab = auditReplaceSlab(recorder, a.floatBackingSlab, slabFloatBacking, a.floatBackingIndex, false)

	auditRecordList(recorder, a.oldStringBackings, slabStringBacking, true)
	clear(a.oldStringBackings)
	a.oldStringBackings = a.oldStringBackings[:0]
	a.stringBackingSlab = auditReplaceSlab(recorder, a.stringBackingSlab, slabStringBacking, a.stringBackingIndex, true)

	auditRecordList(recorder, a.oldBoolBackings, slabBoolBacking, false)
	clear(a.oldBoolBackings)
	a.oldBoolBackings = a.oldBoolBackings[:0]
	a.boolBackingSlab = auditReplaceSlab(recorder, a.boolBackingSlab, slabBoolBacking, a.boolBackingIndex, false)

	auditRecordList(recorder, a.oldUintBackings, slabUintBacking, false)
	clear(a.oldUintBackings)
	a.oldUintBackings = a.oldUintBackings[:0]
	a.uintBackingSlab = auditReplaceSlab(recorder, a.uintBackingSlab, slabUintBacking, a.uintBackingIndex, false)
}

// auditReleaseBoxSlabs records and replaces the scalar box slabs and the upvalue slabs.
//
// Takes recorder (*auditRecorder) which carries the event provenance.
func (a *RegisterArena) auditReleaseBoxSlabs(recorder *auditRecorder) {
	a.stringBoxSlab = auditReplaceSlab(recorder, a.stringBoxSlab, slabStringBox, a.stringBoxIndex, true)
	a.intBoxSlab = auditReplaceSlab(recorder, a.intBoxSlab, slabIntBox, a.intBoxIndex, false)
	a.floatBoxSlab = auditReplaceSlab(recorder, a.floatBoxSlab, slabFloatBox, a.floatBoxIndex, false)
	a.uintBoxSlab = auditReplaceSlab(recorder, a.uintBoxSlab, slabUintBox, a.uintBoxIndex, false)
	a.complexBoxSlab = auditReplaceSlab(recorder, a.complexBoxSlab, slabComplexBox, a.complexBoxIndex, false)
	a.upvalueCellSlab = auditReplaceSlab(recorder, a.upvalueCellSlab, slabUpvalueCell, a.UpvalueCellIndex, true)
	a.upvalueReferenceSlab = auditReplaceSlab(recorder, a.upvalueReferenceSlab, slabUpvalueRef, a.UpvalueReferenceIndex, true)
}

// auditReleaseRegisterBanks records and replaces the pointer-bearing register banks.
//
// The scalar banks are not recorded. Frame slots keep slices into them across Reset, so
// they stay reachable from the arena itself.
//
// Takes recorder (*auditRecorder) which carries the event provenance.
func (a *RegisterArena) auditReleaseRegisterBanks(recorder *auditRecorder) {
	a.stringSlab = auditReplaceSlab(recorder, a.stringSlab, slabStringRegs, a.StringIndex, true)
	a.generalSlab = auditReplaceSlab(recorder, a.generalSlab, slabGeneralRegs, a.GeneralIndex, true)
	a.slicesIntSlab = auditReplaceSlab(recorder, a.slicesIntSlab, slabSlicesInt, a.SlicesIntIndex, true)
	a.slicesFloatSlab = auditReplaceSlab(recorder, a.slicesFloatSlab, slabSlicesFloat, a.SlicesFloatIndex, true)
	a.slicesStringSlab = auditReplaceSlab(recorder, a.slicesStringSlab, slabSlicesString, a.SlicesStringIndex, true)
	a.slicesBoolSlab = auditReplaceSlab(recorder, a.slicesBoolSlab, slabSlicesBool, a.SlicesBoolIndex, true)
	a.slicesUintSlab = auditReplaceSlab(recorder, a.slicesUintSlab, slabSlicesUint, a.SlicesUintIndex, true)
	a.slicesByteSlab = auditReplaceSlab(recorder, a.slicesByteSlab, slabSlicesByte, a.SlicesByteIndex, true)
	a.slabGeneration++
}

// auditClearFrameReferences clears, in every frame slot, the register slices and upvalue
// references that point into recorded slabs. Reset clears the same fields up to the frame
// peak; the audit clears every slot so a stale slot above the peak cannot hide a hit.
func (a *RegisterArena) auditClearFrameReferences() {
	for index := range a.frameSlab {
		frame := &a.frameSlab[index]
		frame.Registers.Strings = nil
		frame.Registers.General = nil
		frame.Registers.SlicesInt = nil
		frame.Registers.slicesFloat = nil
		frame.Registers.slicesString = nil
		frame.Registers.slicesBool = nil
		frame.Registers.slicesUint = nil
		frame.Registers.slicesByte = nil
		frame.upvalues = nil
	}
}

// auditCompactPhase records the retired slabs compactPhase is about to drop or recycle.
//
// Takes state (*gcMarkState) which carries the per-slab liveness bitmaps.
//
// Returns func() which releases the audit lock; compactPhase defers it.
//
// Concurrency: Acquires auditLog.runMu and returns its unlock function; the caller must
// own the arena and release the lock after dropping the recorded slabs.
func (a *RegisterArena) auditCompactPhase(state *gcMarkState) func() {
	auditLog.runMu.Lock()
	a.auditRecordDeadSlabs(state)
	return auditLog.runMu.Unlock
}

// auditRecordDeadSlabs records the dead slabs of every retention list.
//
// Takes state (*gcMarkState) which carries the per-slab liveness bitmaps.
func (a *RegisterArena) auditRecordDeadSlabs(state *gcMarkState) {
	site := auditSiteOf(a.ownerVM)
	headerData := a.auditSliceHeaderData()
	drop := &auditRecorder{arena: a, site: site, headerData: headerData, event: AuditEventMarkDeadDrop}
	recycle := &auditRecorder{arena: a, site: site, headerData: headerData, event: AuditEventChunkRecycle}
	auditDeadSlabs(drop, a.oldByteSlabs, state.OldByteSlabLive, slabByte)
	auditDeadSlabs(drop, a.oldMutByteSlabs, state.OldMutByteSlabLive, slabMutByte)
	auditDeadChunks(drop, recycle, a.oldGenericByteSlabs, state.OldGenericBytesLive, slabGenericBytes, genericBytesSlabChunk, a.recycleChunks)
	auditDeadChunks(drop, recycle, a.oldSliceHeaderSlabs, state.OldSliceHeaderLive, slabSliceHeader, sliceHeaderSlabChunk, a.recycleChunks)
	auditDeadSlabs(drop, a.oldIntBackings, state.OldIntBackingLive, slabIntBacking)
	auditDeadSlabs(drop, a.oldFloatBackings, state.OldFloatBackingLive, slabFloatBacking)
	auditDeadSlabs(drop, a.oldStringBackings, state.OldStringBackingLive, slabStringBacking)
	auditDeadSlabs(drop, a.oldBoolBackings, state.OldBoolBackingLive, slabBoolBacking)
	auditDeadSlabs(drop, a.oldUintBackings, state.OldUintBackingLive, slabUintBacking)
}

// auditSliceHeaderData collects the Data address of every non-nil slot in the arena's
// current and retired slice-header slabs.
//
// Returns []uintptr which is sorted.
func (a *RegisterArena) auditSliceHeaderData() []uintptr {
	var data []uintptr
	collect := func(slab []arenaSliceHeader) {
		for index := range slab {
			if slab[index].Data != nil {
				data = append(data, uintptr(slab[index].Data))
			}
		}
	}
	collect(a.sliceHeaderSlab)
	for _, slab := range a.oldSliceHeaderSlabs {
		collect(slab)
	}
	slices.Sort(data)
	return data
}

// auditCountInRange counts the sorted addresses in [start, end).
//
// Takes sorted ([]uintptr) which is the address list.
// Takes start (uintptr) which is the range's inclusive lower address.
// Takes end (uintptr) which is the range's exclusive upper address.
//
// Returns int which is the count.
func auditCountInRange(sorted []uintptr, start, end uintptr) int {
	low, _ := slices.BinarySearch(sorted, start)
	high, _ := slices.BinarySearch(sorted, end)
	return high - low
}

// auditDeadSlabs records each slab of a retention list whose liveness flag is false.
//
// Takes recorder (*auditRecorder) which carries the event provenance.
// Takes slabs ([][]T) which is the retention list.
// Takes live ([]bool) which is the per-slab liveness bitmap.
// Takes slabKind (string) which names the slab class.
func auditDeadSlabs[T any](recorder *auditRecorder, slabs [][]T, live []bool, slabKind string) {
	for index, slab := range slabs {
		if index < len(live) && live[index] {
			continue
		}
		auditRecordSlab(recorder, slab, slabKind, len(slab), true)
	}
}

// auditDeadChunks records each dead slab of a chunked class, as a chunk recycle when
// recycleDeadChunk would have taken it and as a mark-dead drop otherwise; a slab the
// licence has revoked is never recycled.
//
// Takes drop (*auditRecorder) which records mark-dead drops; its arena owns the slabs and
// their licence.
// Takes recycle (*auditRecorder) which records chunk recycles.
// Takes slabs ([][]T) which is the retention list.
// Takes live ([]bool) which is the per-slab liveness bitmap.
// Takes slabKind (string) which names the slab class.
// Takes chunk (int) which is the class's chunk size in elements.
// Takes recycling (bool) which is true when the arena recycles chunks.
func auditDeadChunks[T any](drop, recycle *auditRecorder, slabs [][]T, live []bool, slabKind string, chunk int, recycling bool) {
	arena := drop.arena
	taken := 0
	for index, slab := range slabs {
		if index < len(live) && live[index] {
			continue
		}
		if recycling && cap(slab) == chunk && taken < maxFreeSlabChunks && !licenceRevokedSlab(arena, slab) {
			taken++
			auditRecordSlab(recycle, slab, slabKind, len(slab), true)
			auditClearRecycledChunk(slab[:chunk])
			continue
		}
		auditRecordSlab(drop, slab, slabKind, len(slab), true)
	}
}

// auditClearRecycledChunk clears a chunk as recycleDeadChunk would, or fills a byte chunk
// with the paranoid canary byte in the paranoid build.
//
// Takes slab ([]T) which is the chunk.
func auditClearRecycledChunk[T any](slab []T) {
	const canary = 0xDD
	if bytesSlab, ok := any(slab).([]byte); ok && arenaParanoidEnabled {
		for index := range bytesSlab {
			bytesSlab[index] = canary
		}
		return
	}
	clear(slab)
}

// auditMidRunCheckpoint runs a mid-run audit at every Nth GC checkpoint, N being the
// interval AuditSetMidRunEvery set; it does nothing while the interval is zero.
func auditMidRunCheckpoint() {
	every := auditMidRunEvery.Load()
	if every == 0 {
		return
	}
	if auditCheckpointCalls.Add(1)%every != 0 {
		return
	}
	auditCheckpoint(AuditOptions{MaxPasses: 0, KeepRecords: false, IncludeActiveRuns: false})
}

// auditUnbarrieredStore deliberately stores an arena string, a generic-bytes []byte and a
// slice-header pointer in a package global without materialising them.
//
// Releasing the arena must then produce one release hit for each of the byteSlab,
// genericBytes and sliceHeader classes.
//
// Takes arena (*RegisterArena) which is the arena to leak from.
func auditUnbarrieredStore(arena *RegisterArena) {
	const (
		leakedBytes = 32
		leakedAlign = 8
	)
	buffer := arena.AllocStringBytes(leakedBytes)
	copy(buffer, "pipit reuse audit leaked string")
	auditLeakSink.text = unsafe.String(&buffer[0], len(buffer))
	auditLeakSink.bytes = unsafe.Slice((*byte)(arena.AllocBytes(leakedBytes, leakedAlign)), leakedBytes)
	auditLeakSink.header = unsafe.Pointer(arena.allocSliceHeader())
}

// auditUnbarrieredStoreRetired deliberately leaks pointers into the most recently retired
// generic-bytes and slice-header slabs.
//
// Takes a (*RegisterArena) which is the arena to leak from; both retention lists must be
// non-empty.
func auditUnbarrieredStoreRetired(a *RegisterArena) {
	bytesSlab := a.oldGenericByteSlabs[len(a.oldGenericByteSlabs)-1]
	headerSlab := a.oldSliceHeaderSlabs[len(a.oldSliceHeaderSlabs)-1]
	auditLeakSink.bytes = bytesSlab[:1:1]
	auditLeakSink.header = unsafe.Pointer(&headerSlab[0])
}

// auditClearLeakSink drops every reference the unbarriered-store controls leaked.
func auditClearLeakSink() {
	auditLeakSink.text = ""
	auditLeakSink.bytes = nil
	auditLeakSink.header = nil
}
