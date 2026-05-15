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
	"runtime"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/hostmem"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/stablepool"
)

const (
	// initialIntSlabs is the starting capacity for the int64 slab. Typical: 16 registers x
	// 32 call depth = 512.
	initialIntSlabs = 512

	// initialFloatSlabs is the starting capacity for the float64 slab.
	initialFloatSlabs = 128

	// initialStringSlabs is the starting capacity for the string slab. Kept small because
	// strings are GC-visible; sizeArenaFromFunctions grows the slab to match the compiled
	// function's actual needs.
	initialStringSlabs = 32

	// initialGeneralSlabs is the starting capacity for the reflect.Value slab. Kept small
	// because reflect.Value is pointer-rich and scanned on every GC cycle;
	// sizeArenaFromFunctions grows as needed.
	initialGeneralSlabs = 32

	// initialBoolSlabs is the starting capacity for the bool slab.
	initialBoolSlabs = 128

	// initialUintSlabs is the starting capacity for the uint64 slab.
	initialUintSlabs = 128

	// initialComplexSlabs is the starting capacity for the complex128 slab.
	initialComplexSlabs = 64

	// initialSlicesIntSlabs is the starting capacity for the typed []int64 slice-header
	// slab. Each slot is a 24-byte slice header; kept conservative because typed slice usage
	// scales with code that explicitly chooses int-kind slice element types.
	initialSlicesIntSlabs = 32

	// initialSlicesFloatSlabs is the starting capacity for the typed []float64 slice-header
	// slab.
	initialSlicesFloatSlabs = 32

	// initialSlicesStringSlabs is the starting capacity for the typed []string slice-header
	// slab.
	initialSlicesStringSlabs = 32

	// initialSlicesBoolSlabs is the starting capacity for the typed []bool slice-header
	// slab.
	initialSlicesBoolSlabs = 32

	// initialSlicesUintSlabs is the starting capacity for the typed []uint64 slice-header
	// slab.
	initialSlicesUintSlabs = 32

	// initialSlicesByteSlabs is the starting capacity for the typed []byte slice-header
	// slab. Byte iteration is the hot path for parsers / word-count / brainfuck workloads,
	// so the same modest default as the other typed-slice slabs.
	initialSlicesByteSlabs = 32

	// initialUpvalueCellSlabs is the starting capacity for the UpvalueCell slab. Typical:
	// 1-3 upvalues per closure x ~32 call depth.
	initialUpvalueCellSlabs = 64

	// initialUpvalueRefSlabs is the starting capacity for the upvalue reference slab.
	initialUpvalueRefSlabs = 64

	// initialFrameSlabs is the cold-pool starting frame capacity. sizeArenaFromFunctions
	// raises it to the per-program static call depth post-compilation.
	initialFrameSlabs = 64

	// InitialByteSlabSize is the starting capacity for the byte slab used to intern string
	// character data. sizeArenaFromFunctions pre-sizes it post-compilation, so this only
	// governs one-shot programs.
	InitialByteSlabSize = 16384

	// InitialIntBackingSize is the starting capacity for the int64 backing slab used by
	// arena-routed make([]int64, ...) and append-grow on []int64. Smaller than
	// initialIntSlabs because slice backings tend to be one-per-make rather than one-per-
	// register-bank; ensureCapacity grows it post-compilation if the program's static
	// analysis demands more.
	InitialIntBackingSize = 256

	// initialFloatBackingSize is the starting capacity for the float64 backing slab used by
	// arena-routed make([]float64, ...) and append-grow on []float64.
	initialFloatBackingSize = 256

	// initialStringBackingSize is the starting capacity for the string backing slab used by
	// arena-routed make([]string, ...) and append-grow on []string. Kept smaller than the
	// int variant because []string backings hold GC-visible pointers and large
	// over-allocations waste GC scan work.
	initialStringBackingSize = 64

	// initialBoolBackingSize is the starting capacity for the bool backing slab used by
	// arena-routed make([]bool, ...) and append-grow on []bool.
	initialBoolBackingSize = 256

	// initialUintBackingSize is the starting capacity for the uint64 backing slab used by
	// arena-routed make([]uint64, ...) and append-grow on []uint64.
	initialUintBackingSize = 256

	// maxArenaMultiplier is the DoS protection threshold that caps slab growth. If a slab
	// grows beyond initialSize * maxArenaMultiplier, it is shrunk on reset, preventing
	// memory bloat from pathological inputs while allowing legitimate growth to be retained
	// across requests.
	maxArenaMultiplier = 8

	// backingSlabIdleResetsBeforeShrink is the consecutive low-use Reset() calls before an
	// overgrown slab is shrunk back to initial size.
	backingSlabIdleResetsBeforeShrink = 4

	// backingSlabLowUseRatioDenominator gates the idle-reset counter. A slab counts as idle
	// when less than 1/denominator of its capacity was touched.
	backingSlabLowUseRatioDenominator = 4

	// stringBoxBytes is the byte size of one allocated string box slot (a Go string header:
	// pointer plus length).
	stringBoxBytes int64 = 16

	// int64BoxBytes is the byte size of one allocated int64 box slot.
	int64BoxBytes int64 = 8

	// float64BoxBytes is the byte size of one allocated float64 box slot.
	float64BoxBytes int64 = 8

	// uint64BoxBytes is the byte size of one allocated uint64 box slot.
	uint64BoxBytes int64 = 8

	// complex128BoxBytes is the byte size of one allocated complex128 box slot (two
	// float64s).
	complex128BoxBytes int64 = 16

	// arenaSliceHeaderBoxBytes is the byte size of one arenaSliceHeader slot in the
	// slice-header slab.
	arenaSliceHeaderBoxBytes int64 = 24

	// initialBoxSlabCapacity is the starting capacity for the per-type box slabs (string,
	// int64, float64, uint64). Sized so a typical closure-heavy program fills the slab once
	// and never grows.
	initialBoxSlabCapacity = 64

	// initialGenericBytesCapacity is the starting capacity in bytes for the generic byte
	// slab used by arena-backed reflect.Value struct and array storage.
	initialGenericBytesCapacity = 4096

	// initialSliceHeaderCapacity is the slice-header slab capacity. 2048 slots (~48 KiB) fit
	// the pre-warmed pool footprint while absorbing typical short-program needs.
	initialSliceHeaderCapacity = 2048

	// allocMaskInt flags that the int64 register bank is non-empty.
	allocMaskInt uint16 = 1 << 0

	// allocMaskFloat flags that the float64 register bank is non-empty.
	allocMaskFloat uint16 = 1 << 1

	// allocMaskString flags that the string register bank is non-empty.
	allocMaskString uint16 = 1 << 2

	// allocMaskGeneral flags that the reflect.Value register bank is non-empty.
	allocMaskGeneral uint16 = 1 << 3

	// allocMaskBool flags that the bool register bank is non-empty.
	allocMaskBool uint16 = 1 << 4

	// allocMaskUint flags that the uint64 register bank is non-empty.
	allocMaskUint uint16 = 1 << 5

	// allocMaskComplex flags that the complex128 register bank is non-empty.
	allocMaskComplex uint16 = 1 << 6

	// allocMaskSliceInt flags that the []int64 slice-header bank is non-empty.
	allocMaskSliceInt uint16 = 1 << 7

	// allocMaskSliceFloat flags that the []float64 slice-header bank is non-empty.
	allocMaskSliceFloat uint16 = 1 << 8

	// allocMaskSliceString flags that the []string slice-header bank is non-empty.
	allocMaskSliceString uint16 = 1 << 9

	// allocMaskSliceBool flags that the []bool slice-header bank is non-empty.
	allocMaskSliceBool uint16 = 1 << 10

	// allocMaskSliceUint flags that the []uint64 slice-header bank is non-empty.
	allocMaskSliceUint uint16 = 1 << 11

	// allocMaskSliceByte flags that the []byte slice-header bank is non-empty.
	allocMaskSliceByte uint16 = 1 << 12

	// typedSliceBankMask flags banks whose ctx base pointer repairRegisterBasesFromCallers
	// may need to restore on a cross-mask return.
	typedSliceBankMask uint16 = allocMaskSliceInt | allocMaskSliceFloat | allocMaskSliceString |
		allocMaskSliceBool | allocMaskSliceUint | allocMaskSliceByte | allocMaskComplex

	// defaultMaxArenaBytesSmallHostThreshold gates the small-host path. Below this candidate
	// the resolver switches from quarter-RAM to three-quarter-RAM policy.
	defaultMaxArenaBytesSmallHostThreshold uint64 = 2 << 30

	// defaultMaxArenaBytesCeiling is the highest budget the host-aware resolver may select.
	// Acts as a hard upper bound so a 1 TiB server still keeps a single Execute well clear
	// of host OOM territory.
	defaultMaxArenaBytesCeiling uint64 = 16 << 30

	// defaultMaxArenaBytesNormalShift is the right-shift applied to detected host memory on
	// the conservative path (dividing by four).
	defaultMaxArenaBytesNormalShift = 2

	// defaultMaxArenaBytesGenerousNumerator and ...Denominator together express the
	// small-host fallback fraction: 3/4 of detected host memory. A 4 GiB box returns 3 GiB
	// rather than the 1 GiB the normal 1/4 share would yield.
	defaultMaxArenaBytesGenerousNumerator uint64 = 3

	// defaultMaxArenaBytesGenerousDenominator denominator for the 3/4 small-host fallback
	// fraction.
	defaultMaxArenaBytesGenerousDenominator uint64 = 4

	// defaultMaxArenaBytesUndetectedFallback is the budget chosen when the host's total RAM
	// cannot be probed (non-Linux platforms today). A middle-of-the-road figure: large
	// enough for ordinary scripts, small enough not to wipe out a typical workstation.
	defaultMaxArenaBytesUndetectedFallback uint64 = 2 << 30

	// maxRetainedOldSlabsPerType caps retained old-slab references per typed-backing list.
	// Past this threshold the oldest entry is dropped.
	maxRetainedOldSlabsPerType = 16
)

// ArenaSavePoint records the arena allocation indices at a point in time, enabling call
// frames to restore the arena when popped.
//
//exhaustruct:ignore
type ArenaSavePoint struct {
	arenaIndices
}

// RegisterArena provides arena-based allocation for register banks by pre-allocating
// contiguous slabs and handing out sub-slices via index bumping, with frames recording a
// save point restored on pop so it behaves as a stack allocator pooled via stablepool.
//
//exhaustruct:ignore
type RegisterArena struct {
	// Link is stablepool's intrusive free-list header. MUST be the first field. The pool
	// writes to it when the arena is on the free stack; user code must not touch it.
	stablepool.Link

	// ownerVM is the VM that currently owns this arena. Nil when pooled.
	ownerVM *VM

	// budgetTracker, when non-nil, receives every change to totalAllocatedBytes so the arena
	// budget is enforced across all arenas of one execution (the main goroutine's and every
	// child's) rather than per arena. Attached on acquisition, detached by PutRegisterArena.
	budgetTracker *ResourceTracker

	// genericBytesSlab backs arena copies of pointer-free reflect.Values. Pointer-containing
	// types must fall back to reflect.New because writing pointers into a []byte hides them
	// from the GC.
	genericBytesSlab []byte

	// sliceHeaderSlab pools slice-header storage for reflect.Values. Each slot's Data field
	// is unsafe.Pointer so the GC keeps the backing array reachable.
	sliceHeaderSlab []arenaSliceHeader

	// stringSlab holds the contiguous string register memory.
	stringSlab []string

	// generalSlab holds the contiguous reflect.Value register memory.
	generalSlab []reflect.Value

	// boolSlab holds the contiguous bool register memory.
	boolSlab []bool

	// uintSlab holds the contiguous uint64 register memory.
	uintSlab []uint64

	// complexSlab holds the contiguous complex128 register memory.
	complexSlab []complex128

	// slicesIntSlab holds the contiguous []int64 slice-header storage for the typed
	// slicesInt register bank.
	slicesIntSlab [][]int64

	// slicesFloatSlab holds the contiguous []float64 slice-header storage for the typed
	// slicesFloat register bank.
	slicesFloatSlab [][]float64

	// oldSliceHeaderSlabs retains previous slice-header slabs after growth so reflect.Values
	// whose ptr is an arenaSliceHeader in a retired slab remain valid for the lifetime of
	// the arena. Same shape as oldGenericByteSlabs / oldByteSlabs.
	oldSliceHeaderSlabs [][]arenaSliceHeader

	// slicesBoolSlab holds the contiguous []bool slice-header storage for the typed
	// slicesBool register bank.
	slicesBoolSlab [][]bool

	// slicesUintSlab holds the contiguous []uint64 slice-header storage for the typed
	// slicesUint register bank.
	slicesUintSlab [][]uint64

	// slicesByteSlab holds the contiguous []byte slice-header storage for the typed
	// slicesByte register bank.
	slicesByteSlab [][]byte

	// frameSlab holds the pre-allocated call frame storage.
	frameSlab []CallFrame

	// callInfoBasesSlab holds ASM call-info base pointers parallel to frameSlab.
	callInfoBasesSlab []uintptr

	// floatBoxSlab backs boxed float64-to-general reflect.Values.
	floatBoxSlab []float64

	// byteSlab holds bump-allocated byte storage for interned strings.
	byteSlab []byte

	// oldByteSlabs retains previous byte slabs so existing strings remain valid.
	oldByteSlabs [][]byte

	// intBackingSlab holds bump-allocated int64 element storage used by arena-routed
	// make([]int64, ...) and append-grow handlers. Slices carved out of this slab use the
	// three-index form slab[index:index+n:index+n] so user append past cap triggers our
	// arenaAppendInt helper rather than silently spilling into the next allocation's region.
	intBackingSlab []int64

	// oldIntBackings retains previous intBackingSlab arrays so user slices still pointing
	// into them remain valid after a grow.
	oldIntBackings [][]int64

	// floatBackingSlab holds bump-allocated float64 element storage for arena-routed
	// make([]float64, ...) / append-grow.
	floatBackingSlab []float64

	// oldFloatBackings retains previous floatBackingSlab arrays.
	oldFloatBackings [][]float64

	// stringBackingSlab holds bump-allocated string element storage for arena-routed
	// make([]string, ...) / append-grow.
	stringBackingSlab []string

	// oldStringBackings retains previous stringBackingSlab arrays.
	oldStringBackings [][]string

	// boolBackingSlab holds bump-allocated bool element storage for arena-routed
	// make([]bool, ...) / append-grow.
	boolBackingSlab []bool

	// oldBoolBackings retains previous boolBackingSlab arrays.
	oldBoolBackings [][]bool

	// uintBackingSlab holds bump-allocated uint64 element storage for arena-routed
	// make([]uint64, ...) / append-grow.
	uintBackingSlab []uint64

	// oldUintBackings retains previous uintBackingSlab arrays.
	oldUintBackings [][]uint64

	// upvalueCellSlab holds the contiguous UpvalueCell storage.
	upvalueCellSlab []program.UpvalueCell

	// upvalueReferenceSlab holds the contiguous upvalue reference storage.
	upvalueReferenceSlab []upvalue

	// floatSlab holds the contiguous float64 register memory.
	floatSlab []float64

	// intSlab holds the contiguous int64 register memory.
	intSlab []int64

	// dispatchSavesSlab holds ASM dispatch register saves parallel to frameSlab.
	dispatchSavesSlab []asmDispatchSave

	// uintBoxSlab backs boxed uint64-to-general reflect.Values.
	uintBoxSlab []uint64

	// intBoxSlab backs boxed int64-to-general reflect.Values. Values in [-128,256] hit the
	// runtime cache; values outside that range bump-allocate here.
	intBoxSlab []int64

	// stringBoxSlab backs boxed string-to-general reflect.Values. The slab is a real
	// []string so writes go through Go's normal write barriers.
	stringBoxSlab []string

	// complexBoxSlab backs boxed complex128-to-general reflect.Values.
	complexBoxSlab []complex128

	// oldGenericByteSlabs retains retired generic-byte slabs so reflect.Values whose ptr
	// field points into a grown-away slab remain valid.
	oldGenericByteSlabs [][]byte

	// slicesStringSlab holds the contiguous []string slice-header storage for the typed
	// slicesString register bank.
	slicesStringSlab [][]string

	// freeSliceHeaderChunks holds zeroed chunk-sized slice-header slabs that a MinorGC found
	// dead, ready for growSliceHeaderSlab to reinstall without a fresh make.
	freeSliceHeaderChunks [][]arenaSliceHeader

	// freeGenericByteChunks is the generic-bytes twin of freeSliceHeaderChunks.
	freeGenericByteChunks [][]byte

	// arenaIndices is the contiguous block of fifteen per-frame save/restore indices
	// (intIndex through upvalueReferenceIndex). Embedding it lets SaveInto and Restore copy
	// the entire block in a single struct assignment instead of fifteen sequential stores,
	// and Go's field promotion keeps existing a.IntIndex selectors working unchanged.
	arenaIndices

	// slabRetiredSinceReset accumulates, per slab class, the length of every slab generation
	// retired by a grow* call since the last Reset. Grows abandon the old slab and restart
	// the class's bump index at zero, so index+retired is the class's true allocation volume
	// for the current Execute.
	slabRetiredSinceReset slabUsageSnapshot

	// slabUsageLastReset captures the previous Execute's true allocation volume per slab
	// class, consulted by the hysteretic shrink policy.
	slabUsageLastReset slabUsageSnapshot

	// scalarDemandSinceReset records the largest register-bank capacity requested by
	// pre-sizing or forced by growth during the current execution; it stands in for a
	// per-call peak so the scalar banks can shrink hysteretically without a hot-path max.
	scalarDemandSinceReset scalarBankDemand

	// scalarDemandLastReset is scalarDemandSinceReset captured by the most recent Reset.
	scalarDemandLastReset scalarBankDemand

	// byteIndex tracks the current allocation position in byteSlab. Not saved per-frame;
	// cleared only on arena Reset.
	byteIndex int

	// slabGeneration increments every time one of the ctx-mirrored register slabs
	// (int/float/string/general/bool/uint/slicesByte) is reallocated or replaced.
	// refreshArenaSlabs skips its fourteen stores (a base pointer and a capacity for each of
	// the seven mirrored banks) when the dispatch context has already seen the current
	// generation.
	slabGeneration uint64

	// sliceHeaderIndex tracks the bump position in sliceHeaderSlab. Not saved per-frame;
	// cleared only on arena Reset.
	sliceHeaderIndex int

	// stringBoxIndex tracks the bump position in stringBoxSlab. Not saved per-frame; cleared
	// only on arena Reset.
	stringBoxIndex int

	// intBoxIndex tracks the bump position in intBoxSlab. Not saved per-frame; cleared only
	// on arena Reset.
	intBoxIndex int

	// floatBoxIndex tracks the bump position in floatBoxSlab. Not saved per-frame; cleared
	// only on arena Reset.
	floatBoxIndex int

	// complexBoxIndex tracks the bump position in complexBoxSlab.
	complexBoxIndex int

	// uintBoxIndex tracks the bump position in uintBoxSlab. Not saved per-frame; cleared
	// only on arena Reset.
	uintBoxIndex int

	// floatBackingIndex tracks the current bump position in floatBackingSlab. Not saved
	// per-frame; cleared only on arena Reset.
	floatBackingIndex int

	// boolBackingIndex tracks the current bump position in boolBackingSlab. Not saved
	// per-frame; cleared only on arena Reset.
	boolBackingIndex int

	// MaxArenaBytes is the per-Execute byte budget for arena growth. Zero means use
	// defaultMaxArenaBytes.
	MaxArenaBytes uint64

	// byteSlabPeakIndexLastReset captures the previous iteration's true byteSlab usage,
	// including retired slab generations, for the hysteretic shrink policy.
	byteSlabPeakIndexLastReset int

	// intBackingIndex tracks the current bump position in intBackingSlab. Not saved
	// per-frame; cleared only on arena Reset.
	intBackingIndex int

	// MaxAllocSize caps a single backing allocation's element count. Zero means no limit.
	MaxAllocSize int

	// stringBackingIndex tracks the current bump position in stringBackingSlab. Not saved
	// per-frame; cleared only on arena Reset.
	stringBackingIndex int

	// totalAllocatedBytes tracks the arena's current working-set bytes. When it would exceed
	// MaxArenaBytes, the grow path attempts a MinorGC first; only if it remains above budget
	// does it panic with errArenaBudgetExceeded.
	totalAllocatedBytes uint64

	// genericBytesIndex tracks the bump position in genericBytesSlab. Not saved per-frame;
	// cleared only on arena Reset.
	genericBytesIndex int

	// uintBackingIndex tracks the current bump position in uintBackingSlab. Not saved
	// per-frame; cleared only on arena Reset.
	uintBackingIndex int

	// bytesAllocated tracks the running total of bytes bump-allocated since the last MinorGC
	// or arena reset. Crossing nextGCAt triggers a GC checkpoint.
	bytesAllocated int64

	// nextGCAt is the byte-allocation threshold that triggers the next MinorGC. Zero
	// disables GC for short-lived arenas.
	nextGCAt int64

	// bytesAtLastGC records the bytesAllocated value immediately after the most recent
	// MinorGC compaction. Used to compute growth-since- last-GC and tune nextGCAt.
	bytesAtLastGC int64

	// framesUsed is the high-water mark of frames touched (pushFrame'd) in the current
	// execution. Go-side and assembly inline pushes do not raise it, so Reset() recovers the
	// true peak with frameStackPeak() before it relies on the mark.
	framesUsed int

	// frameStackPeakLastReset is the frame-stack peak of the run the most recent Reset()
	// closed, the demand signal for the frame-stack shrink hysteresis.
	frameStackPeakLastReset int

	// frameStackGrowths counts growFrameStack() calls over the arena's lifetime, a
	// diagnostic for tests that pin how often a run re-climbs the frame stack.
	frameStackGrowths int

	// scalarIdleResets counts consecutive Resets during which each overgrown scalar bank saw
	// little demand.
	scalarIdleResets scalarBankIdleCounters

	// slabIdleResets counts consecutive Reset() calls during which each overgrown slab class
	// stayed below the low-use ratio; a class reaching backingSlabIdleResetsBeforeShrink is
	// shrunk back to its initial capacity.
	slabIdleResets slabIdleCounters

	// byteSlabIdleResetCount is the number of consecutive Reset() calls during which the
	// byteSlab was below the low-use ratio. Reaches backingSlabIdleResetsBeforeShrink to
	// trigger the shrink and is then cleared back to zero.
	byteSlabIdleResetCount uint32

	// frameStackIdleResets counts consecutive Reset() calls during which an overgrown frame
	// stack stayed below the low-use ratio; reaching backingSlabIdleResetsBeforeShrink
	// shrinks the three frame arrays together.
	frameStackIdleResets uint32

	// gcCount is a diagnostic counter of how many MinorGC cycles this arena has performed in
	// its current Execute. Reset on PutRegisterArena.
	gcCount uint32

	// recycleChunks enables zero-and-reuse of dead chunks for this arena. Seeded from
	// ArenaChunkRecyclingEnabled; tests switch it on for a private arena.
	recycleChunks bool

	// disableMinorGC keeps MinorGC() from running for this owner (VMLimits.DisableMinorGC).
	// Diagnostic: bytesAllocated then grows until MaxArenaBytes is hit.
	disableMinorGC bool
}

var (
	// defaultMaxArenaBytes is the resolved per-Execute budget for arena slab growth (the sum
	// of every retained slab's byte capacity at any moment). Computed once at init from the
	// host's detected RAM via the small-host-aware policy implemented by
	// resolveDefaultMaxArenaBytes.
	defaultMaxArenaBytes = resolveDefaultMaxArenaBytes()

	// registerArenaPool is the single pool for arena instances. Uses stablepool so internal
	// slabs survive Go GC cycles and stay warm across collection boundaries.
	registerArenaPool, _ = stablepool.New[RegisterArena](
		initRegisterArenaInPlace,
		nil,
		runtime.NumCPU()*2,
		stablepool.WithGrowth[RegisterArena](runtime.NumCPU()*32),
	)

	// zeroSizeAllocBacking is a shared sentinel for zero-size types and zero-capacity make
	// results. Declared as uint64 for 8-byte alignment.
	zeroSizeAllocBacking uint64

	// zeroSizeAllocPtr is the pointer returned by AllocBytes for zero-size types. Stable for
	// the process lifetime.
	zeroSizeAllocPtr = unsafe.Pointer(&zeroSizeAllocBacking)
)

// NewRegisterArena creates a fresh arena with default slab sizes.
//
// Returns a newly allocated RegisterArena with all slabs at initial capacity.
func NewRegisterArena() *RegisterArena {
	return &RegisterArena{
		recycleChunks:        ArenaChunkRecyclingEnabled,
		intSlab:              make([]int64, initialIntSlabs),
		floatSlab:            make([]float64, initialFloatSlabs),
		stringSlab:           make([]string, initialStringSlabs),
		generalSlab:          make([]reflect.Value, initialGeneralSlabs),
		boolSlab:             make([]bool, initialBoolSlabs),
		uintSlab:             make([]uint64, initialUintSlabs),
		complexSlab:          make([]complex128, initialComplexSlabs),
		slicesIntSlab:        make([][]int64, initialSlicesIntSlabs),
		slicesFloatSlab:      make([][]float64, initialSlicesFloatSlabs),
		slicesStringSlab:     make([][]string, initialSlicesStringSlabs),
		slicesBoolSlab:       make([][]bool, initialSlicesBoolSlabs),
		slicesUintSlab:       make([][]uint64, initialSlicesUintSlabs),
		slicesByteSlab:       make([][]byte, initialSlicesByteSlabs),
		frameSlab:            make([]CallFrame, initialFrameSlabs),
		callInfoBasesSlab:    make([]uintptr, initialFrameSlabs),
		dispatchSavesSlab:    make([]asmDispatchSave, initialFrameSlabs),
		byteSlab:             make([]byte, InitialByteSlabSize),
		intBackingSlab:       make([]int64, InitialIntBackingSize),
		floatBackingSlab:     make([]float64, initialFloatBackingSize),
		stringBackingSlab:    make([]string, initialStringBackingSize),
		boolBackingSlab:      make([]bool, initialBoolBackingSize),
		uintBackingSlab:      make([]uint64, initialUintBackingSize),
		upvalueCellSlab:      make([]program.UpvalueCell, initialUpvalueCellSlabs),
		upvalueReferenceSlab: make([]upvalue, initialUpvalueRefSlabs),
	}
}

// AllocRegisters returns a registers struct backed by sub-slices of the arena's
// contiguous slabs via O(1) index bumping.
//
// Integer and float registers are not zeroed here; the compiler emits explicit
// SubOpLoadZero instructions where zero-initialisation is needed.
//
// Takes numRegs ([NumRegisterKinds]uint32) which is the number of registers to allocate
// for each register kind.
//
// Returns a registers struct with each bank pointing into the arena slabs.
func (a *RegisterArena) AllocRegisters(numRegs [isa.NumRegisterKinds]uint32) Registers {
	counts := program.ArenaCountsFromNumRegs(numRegs)
	if a.needsGrow(counts) {
		a.growSlabs(counts)
	}

	r := Registers{Ints: a.intSlab[a.IntIndex : a.IntIndex+counts.Ints],
		Floats:       a.floatSlab[a.FloatIndex : a.FloatIndex+counts.Floats],
		Strings:      a.stringSlab[a.StringIndex : a.StringIndex+counts.Strings],
		General:      a.generalSlab[a.GeneralIndex : a.GeneralIndex+counts.Generals],
		Bools:        a.boolSlab[a.BoolIndex : a.BoolIndex+counts.Bools],
		Uints:        a.uintSlab[a.UintIndex : a.UintIndex+counts.Uints],
		Complex:      a.complexSlab[a.ComplexIndex : a.ComplexIndex+counts.Complexes],
		SlicesInt:    a.slicesIntSlab[a.SlicesIntIndex : a.SlicesIntIndex+counts.SlicesInts],
		slicesFloat:  a.slicesFloatSlab[a.SlicesFloatIndex : a.SlicesFloatIndex+counts.SlicesFloats],
		slicesString: a.slicesStringSlab[a.SlicesStringIndex : a.SlicesStringIndex+counts.SlicesStrings],
		slicesBool:   a.slicesBoolSlab[a.SlicesBoolIndex : a.SlicesBoolIndex+counts.SlicesBools],
		slicesUint:   a.slicesUintSlab[a.SlicesUintIndex : a.SlicesUintIndex+counts.SlicesUints],
		slicesByte:   a.slicesByteSlab[a.SlicesByteIndex : a.SlicesByteIndex+counts.SlicesBytes], lastAllocMask: 0}

	a.bumpIndices(counts)
	return r
}

// SaveInto writes the current allocation indices directly into the destination
// ArenaSavePoint.
//
// Takes sp (*ArenaSavePoint) which is the destination save point to populate; typically
// &CallFrame.arenaSave.
//
//go:nosplit
func (a *RegisterArena) SaveInto(sp *ArenaSavePoint) {
	sp.arenaIndices = a.arenaIndices
}

// Restore rolls the arena back to a previous save point, reclaiming all register slots
// allocated since the save point was taken.
//
// Only GC-visible slabs (string, general, and typed slice-header banks) are zeroed
// because they hold pointers the GC must not retain. Primitive slabs are left dirty
// because the compiler guarantees a register is written before it is read.
//
// Takes sp (ArenaSavePoint) which is the save point to restore to.
func (a *RegisterArena) Restore(sp ArenaSavePoint) {
	if arenaParanoidEnabled {
		a.poisonReleasedPrimitives(&sp)
	}
	if sp.StringIndex < a.StringIndex {
		clear(a.stringSlab[sp.StringIndex:a.StringIndex])
	}
	if sp.GeneralIndex < a.GeneralIndex {
		clear(a.generalSlab[sp.GeneralIndex:a.GeneralIndex])
	}
	if sp.SlicesIntIndex < a.SlicesIntIndex {
		clear(a.slicesIntSlab[sp.SlicesIntIndex:a.SlicesIntIndex])
	}
	if sp.SlicesFloatIndex < a.SlicesFloatIndex {
		clear(a.slicesFloatSlab[sp.SlicesFloatIndex:a.SlicesFloatIndex])
	}
	if sp.SlicesStringIndex < a.SlicesStringIndex {
		clear(a.slicesStringSlab[sp.SlicesStringIndex:a.SlicesStringIndex])
	}
	if sp.SlicesBoolIndex < a.SlicesBoolIndex {
		clear(a.slicesBoolSlab[sp.SlicesBoolIndex:a.SlicesBoolIndex])
	}
	if sp.SlicesUintIndex < a.SlicesUintIndex {
		clear(a.slicesUintSlab[sp.SlicesUintIndex:a.SlicesUintIndex])
	}
	if sp.SlicesByteIndex < a.SlicesByteIndex {
		clear(a.slicesByteSlab[sp.SlicesByteIndex:a.SlicesByteIndex])
	}

	a.arenaIndices = sp.arenaIndices
}

// Reset zeroes the used portions of each slab, resets indices, and shrinks any slabs
// exceeding the DoS protection threshold.
//
// Slabs are not shrunk unless they exceed the threshold, allowing naturally-grown slabs
// to be reused without reallocation.
func (a *RegisterArena) Reset() {
	a.detachBudgetTracker()
	a.byteSlabPeakIndexLastReset = a.byteIndex + a.slabRetiredSinceReset.bytes
	a.slabUsageLastReset = slabUsageSnapshot{genericBytes: a.genericBytesIndex + a.slabRetiredSinceReset.genericBytes,
		sliceHeaders: a.sliceHeaderIndex + a.slabRetiredSinceReset.sliceHeaders,
		stringBoxes:  a.stringBoxIndex + a.slabRetiredSinceReset.stringBoxes,
		intBoxes:     a.intBoxIndex + a.slabRetiredSinceReset.intBoxes,
		floatBoxes:   a.floatBoxIndex + a.slabRetiredSinceReset.floatBoxes,
		uintBoxes:    a.uintBoxIndex + a.slabRetiredSinceReset.uintBoxes,
		complexBoxes: a.complexBoxIndex + a.slabRetiredSinceReset.complexBoxes, bytes: 0}
	a.slabRetiredSinceReset = slabUsageSnapshot{bytes: 0, genericBytes: 0, sliceHeaders: 0, stringBoxes: 0, intBoxes: 0, floatBoxes: 0, uintBoxes: 0, complexBoxes: 0}
	a.scalarDemandLastReset = a.scalarDemandSinceReset
	a.scalarDemandSinceReset = scalarBankDemand{ints: 0, floats: 0, strings: 0, generals: 0, bools: 0, uints: 0, complexes: 0}
	a.clearAllocatedRegisterRanges()
	a.clearBoxSlabsAndReset()
	a.poisonReleasedGenericBytes()
	a.genericBytesIndex = 0
	clear(a.sliceHeaderSlab[:a.sliceHeaderIndex])
	a.sliceHeaderIndex = 0
	a.framesUsed = a.frameStackPeak()
	a.frameStackPeakLastReset = a.framesUsed
	a.resetFrameSlots()
	a.resetAllocationIndices()
	a.dropRetainedOldSlabs()
	a.bytesAllocated = 0
	a.bytesAtLastGC = 0
	a.nextGCAt = 0
	a.gcCount = 0
	a.framesUsed = 0
	a.totalAllocatedBytes = 0
	a.ownerVM = nil
	a.MaxAllocSize = 0
	a.MaxArenaBytes = 0
	a.shrinkOvergrownSlabs()
}

// callInfoBases returns the arena's pre-allocated slab for ASM call-info base pointers,
// parallel to the frame stack.
//
// Returns the callInfoBasesSlab slice.
func (a *RegisterArena) callInfoBases() []uintptr {
	return a.callInfoBasesSlab
}

// ensureCapacity pre-sizes the arena slabs so that AllocRegisters never triggers a grow
// during execution.
//
// Called once after compilation with hints derived from the compiled function table.
//
// Takes counts (typedSlabCounts) which carries the minimum required capacity for every
// slab kind. Bundling avoids a long argument list that grows every time a new typed
// register bank is added.
func (a *RegisterArena) ensureCapacity(counts program.TypedSlabCounts) {
	a.ensureScalarBankCapacity(counts)
	a.ensureSliceBankCapacity(counts)
	a.ensureBackingCapacity(counts)
	a.ensureFrameStackCapacity(counts.FrameStack)
	a.ensureBoxSlabCapacity(counts)
	a.ensureUpvalueCapacity(counts)
}

// allocRegistersInto writes arena-backed sub-slices directly into the target registers
// pointer, avoiding a by-value copy.
//
// Takes r (*Registers) which is the target registers to populate in place.
// Takes numRegs ([NumRegisterKinds]uint32) which is the number of registers to allocate
// for each register kind.
func (a *RegisterArena) allocRegistersInto(r *Registers, numRegs [isa.NumRegisterKinds]uint32) {
	counts := program.ArenaCountsFromNumRegs(numRegs)
	a.allocRegistersIntoFromCounts(r, counts)
}

// allocRegistersIntoCached is the hot-path register-bank allocator.
//
// Consumes the function's precomputed typedSlabCounts so the conversion from
// [NumRegisterKinds]uint32 is paid once per function instead of once per call. The
// nonZeroMask short-circuits banks the callee does not use.
//
// Takes r (*Registers) which is the destination register file to populate.
// Takes counts (typedSlabCounts) which is the precomputed slab request derived from
// callee.numRegisters.
// Takes nonZeroMask (uint16) which is the bitmask of banks the callee actually uses; bits
// map to isa.RegisterKind values and empty banks short-circuit their slice-header write.
func (a *RegisterArena) allocRegistersIntoCached(r *Registers, counts program.TypedSlabCounts, nonZeroMask uint16) {
	if a.needsGrow(counts) {
		a.growSlabs(counts)
	}
	a.assignCachedScalarBanks(r, counts, nonZeroMask)
	a.assignCachedSliceBanks(r, counts, nonZeroMask)
	r.lastAllocMask = nonZeroMask
	a.bumpIndices(counts)
}

// save returns a save point capturing the current allocation indices.
//
// Returns an ArenaSavePoint recording all current slab positions.
func (a *RegisterArena) save() ArenaSavePoint {
	var sp ArenaSavePoint
	a.SaveInto(&sp)
	return sp
}

// ensureScalarBankCapacity grows the scalar register banks to fit counts.
//
// Takes counts (typedSlabCounts) which are the required per-kind counts for
// int/float/string/general/bool/uint/complex banks.
func (a *RegisterArena) ensureScalarBankCapacity(counts program.TypedSlabCounts) {
	a.scalarDemandSinceReset.noteRequest(counts)
	if counts.Ints > len(a.intSlab) {
		a.intSlab = make([]int64, counts.Ints)
		a.slabGeneration++
	}
	if counts.Floats > len(a.floatSlab) {
		a.floatSlab = make([]float64, counts.Floats)
		a.slabGeneration++
	}
	if counts.Strings > len(a.stringSlab) {
		a.stringSlab = make([]string, counts.Strings)
		a.slabGeneration++
	}
	if counts.Generals > len(a.generalSlab) {
		a.generalSlab = make([]reflect.Value, counts.Generals)
		a.slabGeneration++
	}
	if counts.Bools > len(a.boolSlab) {
		a.boolSlab = make([]bool, counts.Bools)
		a.slabGeneration++
	}
	if counts.Uints > len(a.uintSlab) {
		a.uintSlab = make([]uint64, counts.Uints)
		a.slabGeneration++
	}
	if counts.Complexes > len(a.complexSlab) {
		a.complexSlab = make([]complex128, counts.Complexes)
	}
}

// frameStack returns the arena's pre-allocated call frame slab.
//
// Returns the frameSlab slice.
func (a *RegisterArena) frameStack() []CallFrame {
	return a.frameSlab
}

// dispatchSaves returns the arena's pre-allocated slab for ASM dispatch register saves,
// parallel to the frame stack.
//
// Returns the dispatchSavesSlab slice.
func (a *RegisterArena) dispatchSaves() []asmDispatchSave {
	return a.dispatchSavesSlab
}

// allocUpvalueCells bump-allocates n UpvalueCell slots from the arena.
//
// Takes n (int) which is the number of upvalue cells to allocate.
//
// Returns a zeroed slice of UpvalueCell of length n.
func (a *RegisterArena) allocUpvalueCells(n int) []program.UpvalueCell {
	if a.UpvalueCellIndex+n > len(a.upvalueCellSlab) {
		a.growUpvalueCellSlab(a.UpvalueCellIndex + n)
	}
	start := a.UpvalueCellIndex
	a.UpvalueCellIndex += n
	cells := a.upvalueCellSlab[start:a.UpvalueCellIndex]
	clear(cells)
	return cells
}

// allocUpvalueRefs bump-allocates n upvalue slots from the arena.
//
// Takes n (int) which is the number of upvalue references to allocate.
//
// Returns a zeroed slice of upvalue of length n.
func (a *RegisterArena) allocUpvalueRefs(n int) []upvalue {
	if a.UpvalueReferenceIndex+n > len(a.upvalueReferenceSlab) {
		a.growUpvalueRefSlab(a.UpvalueReferenceIndex + n)
	}
	start := a.UpvalueReferenceIndex
	a.UpvalueReferenceIndex += n
	refs := a.upvalueReferenceSlab[start:a.UpvalueReferenceIndex]
	clear(refs)
	return refs
}

// ensureFrameStackCapacity grows the parallel frame slabs (frameSlab + callInfoBasesSlab
// + dispatchSavesSlab) when the compile-time depth estimate exceeds the current capacity.
// Idempotent and grow-only; calling with zero or a value already covered is a no-op so
// multiple Execute calls amortise correctly.
//
// Takes minDepth (int) which is the minimum frame-stack depth the arena must support
// without paying a growCallStack allocation.
func (a *RegisterArena) ensureFrameStackCapacity(minDepth int) {
	if minDepth <= len(a.frameSlab) {
		return
	}
	a.growFrameStack(minDepth)
}

// ensureBoxSlabCapacity grows each of the five per-kind box slabs (int / uint / float /
// string / complex) when the supplied counts exceed the current capacity. Each box slab
// is independent so a workload that packs only one kind does not over-provision the other
// four.
//
// Takes counts (typedSlabCounts) which carries the per-kind box slot budget computed by
// sizeArenaFromFunctions.
func (a *RegisterArena) ensureBoxSlabCapacity(counts program.TypedSlabCounts) {
	if counts.IntBoxes > len(a.intBoxSlab) {
		a.intBoxSlab = make([]int64, counts.IntBoxes)
		a.intBoxIndex = 0
	}
	if counts.UintBoxes > len(a.uintBoxSlab) {
		a.uintBoxSlab = make([]uint64, counts.UintBoxes)
		a.uintBoxIndex = 0
	}
	if counts.FloatBoxes > len(a.floatBoxSlab) {
		a.floatBoxSlab = make([]float64, counts.FloatBoxes)
		a.floatBoxIndex = 0
	}
	if counts.StringBoxes > len(a.stringBoxSlab) {
		a.stringBoxSlab = make([]string, counts.StringBoxes)
		a.stringBoxIndex = 0
	}
	if counts.ComplexBoxes > len(a.complexBoxSlab) {
		a.complexBoxSlab = make([]complex128, counts.ComplexBoxes)
		a.complexBoxIndex = 0
	}
}

// ensureUpvalueCapacity grows the upvalue cell and reference slabs when the supplied
// counts exceed the current capacity. Sized from the per-program isa.OpMakeClosure
// descriptor count.
//
// Takes counts (typedSlabCounts) which carries the budget for both upvalue slabs.
func (a *RegisterArena) ensureUpvalueCapacity(counts program.TypedSlabCounts) {
	if counts.UpvalueCells > len(a.upvalueCellSlab) {
		a.upvalueCellSlab = make([]program.UpvalueCell, counts.UpvalueCells)
	}
	if counts.UpvalueReferences > len(a.upvalueReferenceSlab) {
		a.upvalueReferenceSlab = make([]upvalue, counts.UpvalueReferences)
	}
}

// clearAllocatedRegisterRanges zeroes the in-use prefix of every register and upvalue
// slab so the next allocation hands out freshly zeroed sub-slices.
func (a *RegisterArena) clearAllocatedRegisterRanges() {
	clear(a.intSlab[:a.IntIndex])
	clear(a.floatSlab[:a.FloatIndex])
	clear(a.stringSlab[:a.StringIndex])
	clear(a.generalSlab[:a.GeneralIndex])
	clear(a.boolSlab[:a.BoolIndex])
	clear(a.uintSlab[:a.UintIndex])
	clear(a.complexSlab[:a.ComplexIndex])
	clear(a.slicesIntSlab[:a.SlicesIntIndex])
	clear(a.slicesFloatSlab[:a.SlicesFloatIndex])
	clear(a.slicesStringSlab[:a.SlicesStringIndex])
	clear(a.slicesBoolSlab[:a.SlicesBoolIndex])
	clear(a.slicesUintSlab[:a.SlicesUintIndex])
	clear(a.slicesByteSlab[:a.SlicesByteIndex])
	clear(a.upvalueCellSlab[:a.UpvalueCellIndex])
	clear(a.upvalueReferenceSlab[:a.UpvalueReferenceIndex])
}

// clearBoxSlabsAndReset zeroes the four per-type box slabs (string, int, float, uint) and
// resets their bump indices.
func (a *RegisterArena) clearBoxSlabsAndReset() {
	clear(a.stringBoxSlab[:a.stringBoxIndex])
	a.stringBoxIndex = 0
	clear(a.intBoxSlab[:a.intBoxIndex])
	a.intBoxIndex = 0
	clear(a.floatBoxSlab[:a.floatBoxIndex])
	a.floatBoxIndex = 0
	clear(a.uintBoxSlab[:a.uintBoxIndex])
	a.uintBoxIndex = 0
	clear(a.complexBoxSlab[:a.complexBoxIndex])
	a.complexBoxIndex = 0
}

// resetFrameSlots zeroes per-frame register slices, function pointers, and shared-cell
// references. Iterates only [0:framesUsed]; frames past the high-water mark are
// guaranteed nil.
func (a *RegisterArena) resetFrameSlots() {
	for i := range a.framesUsed {
		f := &a.frameSlab[i]
		f.Registers.Strings = nil
		f.Registers.General = nil
		f.Registers.SlicesInt = nil
		f.Registers.slicesFloat = nil
		f.Registers.slicesString = nil
		f.Registers.slicesBool = nil
		f.Registers.slicesUint = nil
		f.Registers.slicesByte = nil
		f.Function = nil
		releaseSharedCellMap(f.sharedCells)
		f.sharedCells = nil
		f.upvalues = nil
		f.returnDestination = nil
	}
}

// resetAllocationIndices zeroes every per-slab bump index so the next AllocRegisters call
// hands out sub-slices starting at position 0 in each slab.
func (a *RegisterArena) resetAllocationIndices() {
	a.IntIndex = 0
	a.FloatIndex = 0
	a.StringIndex = 0
	a.GeneralIndex = 0
	a.BoolIndex = 0
	a.UintIndex = 0
	a.ComplexIndex = 0
	a.SlicesIntIndex = 0
	a.SlicesFloatIndex = 0
	a.SlicesStringIndex = 0
	a.SlicesBoolIndex = 0
	a.SlicesUintIndex = 0
	a.SlicesByteIndex = 0
	a.byteIndex = 0
	a.intBackingIndex = 0
	a.floatBackingIndex = 0
	a.stringBackingIndex = 0
	a.boolBackingIndex = 0
	a.uintBackingIndex = 0
	a.UpvalueCellIndex = 0
	a.UpvalueReferenceIndex = 0
}

// dropRetainedOldSlabs truncates the retention lists for retired slabs. Each list is
// cleared before truncating so GC can reclaim the dropped slabs once any remaining user
// references are themselves collected.
func (a *RegisterArena) dropRetainedOldSlabs() {
	a.dropFreeChunks()
	a.oldByteSlabs = a.oldByteSlabs[:0]
	clear(a.oldGenericByteSlabs)
	a.oldGenericByteSlabs = a.oldGenericByteSlabs[:0]
	clear(a.oldSliceHeaderSlabs)
	a.oldSliceHeaderSlabs = a.oldSliceHeaderSlabs[:0]
	clear(a.oldIntBackings)
	a.oldIntBackings = a.oldIntBackings[:0]
	clear(a.oldFloatBackings)
	a.oldFloatBackings = a.oldFloatBackings[:0]
	clear(a.oldStringBackings)
	a.oldStringBackings = a.oldStringBackings[:0]
	clear(a.oldBoolBackings)
	a.oldBoolBackings = a.oldBoolBackings[:0]
	clear(a.oldUintBackings)
	a.oldUintBackings = a.oldUintBackings[:0]
}

// allocRegistersIntoFromCounts is the fallback used by allocRegistersInto when the caller
// has not precomputed counts. Same shape as allocRegistersIntoCached but derives the
// non-zero mask from the counts struct directly, paying one extra load per bank.
//
// Takes r (*Registers) which is the destination register file to populate.
// Takes counts (typedSlabCounts) which is the per-bank request size.
func (a *RegisterArena) allocRegistersIntoFromCounts(r *Registers, counts program.TypedSlabCounts) {
	if a.needsGrow(counts) {
		a.growSlabs(counts)
	}
	r.Ints = a.intSlab[a.IntIndex : a.IntIndex+counts.Ints]
	r.General = a.generalSlab[a.GeneralIndex : a.GeneralIndex+counts.Generals]
	r.Uints = a.uintSlab[a.UintIndex : a.UintIndex+counts.Uints]
	a.assignScalarBanksFromCounts(r, counts)
	a.assignSliceBanksFromCounts(r, counts)
	a.bumpIndices(counts)
}

// ensureSliceBankCapacity grows the typed slice-header banks to fit counts.
//
// Takes counts (typedSlabCounts) which are the required per-kind counts for
// int/float/string/bool/uint/byte slice-header banks.
func (a *RegisterArena) ensureSliceBankCapacity(counts program.TypedSlabCounts) {
	if counts.SlicesInts > len(a.slicesIntSlab) {
		a.slicesIntSlab = make([][]int64, counts.SlicesInts)
	}
	if counts.SlicesFloats > len(a.slicesFloatSlab) {
		a.slicesFloatSlab = make([][]float64, counts.SlicesFloats)
	}
	if counts.SlicesStrings > len(a.slicesStringSlab) {
		a.slicesStringSlab = make([][]string, counts.SlicesStrings)
	}
	if counts.SlicesBools > len(a.slicesBoolSlab) {
		a.slicesBoolSlab = make([][]bool, counts.SlicesBools)
	}
	if counts.SlicesUints > len(a.slicesUintSlab) {
		a.slicesUintSlab = make([][]uint64, counts.SlicesUints)
	}
	if counts.SlicesBytes > len(a.slicesByteSlab) {
		a.slicesByteSlab = make([][]byte, counts.SlicesBytes)
		a.slabGeneration++
	}
}

// ensureBackingCapacity grows the byte slab and per-type backing slabs used by
// arena-routed make and append-grow.
//
// Takes counts (typedSlabCounts) which are the required per-kind backing capacities.
func (a *RegisterArena) ensureBackingCapacity(counts program.TypedSlabCounts) {
	if counts.Bytes > len(a.byteSlab) {
		a.byteSlab = make([]byte, counts.Bytes)
	}
	if counts.IntBacking > len(a.intBackingSlab) {
		a.intBackingSlab = make([]int64, counts.IntBacking)
	}
	if counts.FloatBacking > len(a.floatBackingSlab) {
		a.floatBackingSlab = make([]float64, counts.FloatBacking)
	}
	if counts.StringBacking > len(a.stringBackingSlab) {
		a.stringBackingSlab = make([]string, counts.StringBacking)
	}
	if counts.BoolBacking > len(a.boolBackingSlab) {
		a.boolBackingSlab = make([]bool, counts.BoolBacking)
	}
	if counts.UintBacking > len(a.uintBackingSlab) {
		a.uintBackingSlab = make([]uint64, counts.UintBacking)
	}
	if counts.GenericBytes > len(a.genericBytesSlab) {
		a.genericBytesSlab = make([]byte, counts.GenericBytes)
	}
	if counts.SliceHeaders > len(a.sliceHeaderSlab) {
		a.sliceHeaderSlab = make([]arenaSliceHeader, counts.SliceHeaders)
	}
}

// assignCachedScalarBanks writes the scalar bank slice headers onto r using the
// precomputed mask.
//
// Banks whose bit in nonZeroMask is clear are reset to nil.
//
// Takes r (*Registers) which is the register set receiving the assignments.
// Takes counts (typedSlabCounts) which are the per-kind slot counts to expose on r.
// Takes nonZeroMask (uint16) which is the bitmask selecting which scalar banks receive
// non-nil slices.
func (a *RegisterArena) assignCachedScalarBanks(r *Registers, counts program.TypedSlabCounts, nonZeroMask uint16) {
	if nonZeroMask&allocMaskInt != 0 {
		r.Ints = a.intSlab[a.IntIndex : a.IntIndex+counts.Ints]
	} else if r.lastAllocMask&allocMaskInt != 0 {
		r.Ints = nil
	}
	if nonZeroMask&allocMaskGeneral != 0 {
		r.General = a.generalSlab[a.GeneralIndex : a.GeneralIndex+counts.Generals]
	} else if r.lastAllocMask&allocMaskGeneral != 0 {
		r.General = nil
	}
	if nonZeroMask&allocMaskUint != 0 {
		r.Uints = a.uintSlab[a.UintIndex : a.UintIndex+counts.Uints]
	} else if r.lastAllocMask&allocMaskUint != 0 {
		r.Uints = nil
	}
	if nonZeroMask&allocMaskFloat != 0 {
		r.Floats = a.floatSlab[a.FloatIndex : a.FloatIndex+counts.Floats]
	} else if r.lastAllocMask&allocMaskFloat != 0 {
		r.Floats = nil
	}
	if nonZeroMask&allocMaskString != 0 {
		r.Strings = a.stringSlab[a.StringIndex : a.StringIndex+counts.Strings]
	} else if r.lastAllocMask&allocMaskString != 0 {
		r.Strings = nil
	}
	if nonZeroMask&allocMaskBool != 0 {
		r.Bools = a.boolSlab[a.BoolIndex : a.BoolIndex+counts.Bools]
	} else if r.lastAllocMask&allocMaskBool != 0 {
		r.Bools = nil
	}
	if nonZeroMask&allocMaskComplex != 0 {
		r.Complex = a.complexSlab[a.ComplexIndex : a.ComplexIndex+counts.Complexes]
	} else if r.lastAllocMask&allocMaskComplex != 0 {
		r.Complex = nil
	}
}

// assignCachedSliceBanks writes the typed-slice bank slice headers onto r using the
// precomputed mask.
//
// Banks whose bit in nonZeroMask is clear are left untouched; every reader of another
// frame's banks is gated on that frame's own nonZeroBankMask.
//
// Takes r (*Registers) which is the register set receiving the assignments.
// Takes counts (typedSlabCounts) which are the per-kind slice-header counts to expose on
// r.
// Takes nonZeroMask (uint16) which is the bitmask selecting which typed-slice banks
// receive non-nil slices.
func (a *RegisterArena) assignCachedSliceBanks(r *Registers, counts program.TypedSlabCounts, nonZeroMask uint16) {
	if nonZeroMask&allocMaskSliceInt != 0 {
		r.SlicesInt = a.slicesIntSlab[a.SlicesIntIndex : a.SlicesIntIndex+counts.SlicesInts]
	} else if r.lastAllocMask&allocMaskSliceInt != 0 {
		r.SlicesInt = nil
	}
	if nonZeroMask&allocMaskSliceFloat != 0 {
		r.slicesFloat = a.slicesFloatSlab[a.SlicesFloatIndex : a.SlicesFloatIndex+counts.SlicesFloats]
	} else if r.lastAllocMask&allocMaskSliceFloat != 0 {
		r.slicesFloat = nil
	}
	if nonZeroMask&allocMaskSliceString != 0 {
		r.slicesString = a.slicesStringSlab[a.SlicesStringIndex : a.SlicesStringIndex+counts.SlicesStrings]
	} else if r.lastAllocMask&allocMaskSliceString != 0 {
		r.slicesString = nil
	}
	if nonZeroMask&allocMaskSliceBool != 0 {
		r.slicesBool = a.slicesBoolSlab[a.SlicesBoolIndex : a.SlicesBoolIndex+counts.SlicesBools]
	} else if r.lastAllocMask&allocMaskSliceBool != 0 {
		r.slicesBool = nil
	}
	if nonZeroMask&allocMaskSliceUint != 0 {
		r.slicesUint = a.slicesUintSlab[a.SlicesUintIndex : a.SlicesUintIndex+counts.SlicesUints]
	} else if r.lastAllocMask&allocMaskSliceUint != 0 {
		r.slicesUint = nil
	}
	if nonZeroMask&allocMaskSliceByte != 0 {
		r.slicesByte = a.slicesByteSlab[a.SlicesByteIndex : a.SlicesByteIndex+counts.SlicesBytes]
	} else if r.lastAllocMask&allocMaskSliceByte != 0 {
		r.slicesByte = nil
	}
}

// assignScalarBanksFromCounts writes the optional scalar register banks onto r derived
// from counts.
//
// Banks with a zero count receive nil slices.
//
// Takes r (*Registers) which is the register set receiving the assignments.
// Takes counts (typedSlabCounts) which are the per-kind slot counts
// (float/string/bool/complex banks).
func (a *RegisterArena) assignScalarBanksFromCounts(r *Registers, counts program.TypedSlabCounts) {
	if counts.Floats > 0 {
		r.Floats = a.floatSlab[a.FloatIndex : a.FloatIndex+counts.Floats]
	} else {
		r.Floats = nil
	}
	if counts.Strings > 0 {
		r.Strings = a.stringSlab[a.StringIndex : a.StringIndex+counts.Strings]
	} else {
		r.Strings = nil
	}
	if counts.Bools > 0 {
		r.Bools = a.boolSlab[a.BoolIndex : a.BoolIndex+counts.Bools]
	} else {
		r.Bools = nil
	}
	if counts.Complexes > 0 {
		r.Complex = a.complexSlab[a.ComplexIndex : a.ComplexIndex+counts.Complexes]
	} else {
		r.Complex = nil
	}
}

// assignSliceBanksFromCounts writes the typed-slice register banks onto r derived from
// counts.
//
// Banks with a zero count receive nil slices.
//
// Takes r (*Registers) which is the register set receiving the assignments.
// Takes counts (typedSlabCounts) which are the per-kind slice-header counts
// (int/float/string/bool/uint/byte banks).
func (a *RegisterArena) assignSliceBanksFromCounts(r *Registers, counts program.TypedSlabCounts) {
	if counts.SlicesInts > 0 {
		r.SlicesInt = a.slicesIntSlab[a.SlicesIntIndex : a.SlicesIntIndex+counts.SlicesInts]
	} else {
		r.SlicesInt = nil
	}
	if counts.SlicesFloats > 0 {
		r.slicesFloat = a.slicesFloatSlab[a.SlicesFloatIndex : a.SlicesFloatIndex+counts.SlicesFloats]
	} else {
		r.slicesFloat = nil
	}
	if counts.SlicesStrings > 0 {
		r.slicesString = a.slicesStringSlab[a.SlicesStringIndex : a.SlicesStringIndex+counts.SlicesStrings]
	} else {
		r.slicesString = nil
	}
	if counts.SlicesBools > 0 {
		r.slicesBool = a.slicesBoolSlab[a.SlicesBoolIndex : a.SlicesBoolIndex+counts.SlicesBools]
	} else {
		r.slicesBool = nil
	}
	if counts.SlicesUints > 0 {
		r.slicesUint = a.slicesUintSlab[a.SlicesUintIndex : a.SlicesUintIndex+counts.SlicesUints]
	} else {
		r.slicesUint = nil
	}
	if counts.SlicesBytes > 0 {
		r.slicesByte = a.slicesByteSlab[a.SlicesByteIndex : a.SlicesByteIndex+counts.SlicesBytes]
	} else {
		r.slicesByte = nil
	}
}

// needsGrow reports whether at least one slab is too small to satisfy the requested
// counts at its current allocation index.
//
// Takes counts (typedSlabCounts) which is the per-bank request size.
//
// Returns true when any slab requires growth before AllocRegisters can hand out a
// sub-slice.
func (a *RegisterArena) needsGrow(counts program.TypedSlabCounts) bool {
	return a.IntIndex+counts.Ints > len(a.intSlab) ||
		a.FloatIndex+counts.Floats > len(a.floatSlab) ||
		a.StringIndex+counts.Strings > len(a.stringSlab) ||
		a.GeneralIndex+counts.Generals > len(a.generalSlab) ||
		a.BoolIndex+counts.Bools > len(a.boolSlab) ||
		a.UintIndex+counts.Uints > len(a.uintSlab) ||
		a.ComplexIndex+counts.Complexes > len(a.complexSlab) ||
		a.SlicesIntIndex+counts.SlicesInts > len(a.slicesIntSlab) ||
		a.SlicesFloatIndex+counts.SlicesFloats > len(a.slicesFloatSlab) ||
		a.SlicesStringIndex+counts.SlicesStrings > len(a.slicesStringSlab) ||
		a.SlicesBoolIndex+counts.SlicesBools > len(a.slicesBoolSlab) ||
		a.SlicesUintIndex+counts.SlicesUints > len(a.slicesUintSlab) ||
		a.SlicesByteIndex+counts.SlicesBytes > len(a.slicesByteSlab)
}

// bumpIndices advances the arena's per-bank allocation indices by the per-bank request
// size after sub-slices have been handed out.
//
// Takes counts (typedSlabCounts) which is the per-bank request size just satisfied.
func (a *RegisterArena) bumpIndices(counts program.TypedSlabCounts) {
	a.IntIndex += counts.Ints
	a.FloatIndex += counts.Floats
	a.StringIndex += counts.Strings
	a.GeneralIndex += counts.Generals
	a.BoolIndex += counts.Bools
	a.UintIndex += counts.Uints
	a.ComplexIndex += counts.Complexes
	a.SlicesIntIndex += counts.SlicesInts
	a.SlicesFloatIndex += counts.SlicesFloats
	a.SlicesStringIndex += counts.SlicesStrings
	a.SlicesBoolIndex += counts.SlicesBools
	a.SlicesUintIndex += counts.SlicesUints
	a.SlicesByteIndex += counts.SlicesBytes
}

// shrinkOvergrownSlabs replaces any slab that has grown beyond the DoS protection
// threshold with a fresh default-sized allocation.
func (a *RegisterArena) shrinkOvergrownSlabs() {
	a.shrinkOvergrownScalarSlabs()
	a.shrinkOvergrownSliceSlabs()
	a.shrinkOvergrownFrameSlabs()
	a.shrinkOvergrownBackingSlabs()
}

// shrinkOvergrownSliceSlabs replaces typed-slice banks (int/float/ string/bool/uint/byte)
// that grew past the threshold with fresh default-sized allocations.
func (a *RegisterArena) shrinkOvergrownSliceSlabs() {
	if len(a.slicesIntSlab) > initialSlicesIntSlabs*maxArenaMultiplier {
		a.slicesIntSlab = make([][]int64, initialSlicesIntSlabs)
	}
	if len(a.slicesFloatSlab) > initialSlicesFloatSlabs*maxArenaMultiplier {
		a.slicesFloatSlab = make([][]float64, initialSlicesFloatSlabs)
	}
	if len(a.slicesStringSlab) > initialSlicesStringSlabs*maxArenaMultiplier {
		a.slicesStringSlab = make([][]string, initialSlicesStringSlabs)
	}
	if len(a.slicesBoolSlab) > initialSlicesBoolSlabs*maxArenaMultiplier {
		a.slicesBoolSlab = make([][]bool, initialSlicesBoolSlabs)
	}
	if len(a.slicesUintSlab) > initialSlicesUintSlabs*maxArenaMultiplier {
		a.slicesUintSlab = make([][]uint64, initialSlicesUintSlabs)
	}
	if len(a.slicesByteSlab) > initialSlicesByteSlabs*maxArenaMultiplier {
		a.slicesByteSlab = make([][]byte, initialSlicesByteSlabs)
		a.slabGeneration++
	}
}

// shrinkOvergrownFrameSlabs replaces frame-adjacent slabs (frames, call-info bases,
// dispatch saves, upvalue cells and refs) that grew past the threshold.
func (a *RegisterArena) shrinkOvergrownFrameSlabs() {
	a.shrinkFrameStackWithHysteresis()
	if len(a.upvalueCellSlab) > initialUpvalueCellSlabs*maxArenaMultiplier {
		a.upvalueCellSlab = make([]program.UpvalueCell, initialUpvalueCellSlabs)
	}
	if len(a.upvalueReferenceSlab) > initialUpvalueRefSlabs*maxArenaMultiplier {
		a.upvalueReferenceSlab = make([]upvalue, initialUpvalueRefSlabs)
	}
}

// shrinkOvergrownBackingSlabs replaces the byte slab and the typed backing slabs (used by
// arena-routed make() and append-grow) that grew past the threshold.
func (a *RegisterArena) shrinkOvergrownBackingSlabs() {
	a.byteSlab = shrinkSlabWithHysteresis(a.byteSlab, InitialByteSlabSize, a.byteSlabPeakIndexLastReset, &a.byteSlabIdleResetCount)
	if len(a.intBackingSlab) > InitialIntBackingSize*maxArenaMultiplier {
		a.intBackingSlab = make([]int64, InitialIntBackingSize)
	}
	if len(a.floatBackingSlab) > initialFloatBackingSize*maxArenaMultiplier {
		a.floatBackingSlab = make([]float64, initialFloatBackingSize)
	}
	if len(a.stringBackingSlab) > initialStringBackingSize*maxArenaMultiplier {
		a.stringBackingSlab = make([]string, initialStringBackingSize)
	}
	if len(a.boolBackingSlab) > initialBoolBackingSize*maxArenaMultiplier {
		a.boolBackingSlab = make([]bool, initialBoolBackingSize)
	}
	if len(a.uintBackingSlab) > initialUintBackingSize*maxArenaMultiplier {
		a.uintBackingSlab = make([]uint64, initialUintBackingSize)
	}
	a.shrinkOvergrownBoxSlabs()
	a.genericBytesSlab = shrinkSlabWithHysteresis(a.genericBytesSlab, initialGenericBytesCapacity, a.slabUsageLastReset.genericBytes, &a.slabIdleResets.genericBytes)
	a.sliceHeaderSlab = shrinkSlabWithHysteresis(a.sliceHeaderSlab, initialSliceHeaderCapacity, a.slabUsageLastReset.sliceHeaders, &a.slabIdleResets.sliceHeaders)
}

// shrinkOvergrownBoxSlabs shrinks any per-type box slab that has grown past its DoS
// threshold and then sat idle across several Resets.
func (a *RegisterArena) shrinkOvergrownBoxSlabs() {
	a.stringBoxSlab = shrinkSlabWithHysteresis(a.stringBoxSlab, initialBoxSlabCapacity, a.slabUsageLastReset.stringBoxes, &a.slabIdleResets.stringBoxes)
	a.intBoxSlab = shrinkSlabWithHysteresis(a.intBoxSlab, initialBoxSlabCapacity, a.slabUsageLastReset.intBoxes, &a.slabIdleResets.intBoxes)
	a.floatBoxSlab = shrinkSlabWithHysteresis(a.floatBoxSlab, initialBoxSlabCapacity, a.slabUsageLastReset.floatBoxes, &a.slabIdleResets.floatBoxes)
	a.uintBoxSlab = shrinkSlabWithHysteresis(a.uintBoxSlab, initialBoxSlabCapacity, a.slabUsageLastReset.uintBoxes, &a.slabIdleResets.uintBoxes)
	a.complexBoxSlab = shrinkSlabWithHysteresis(a.complexBoxSlab, initialBoxSlabCapacity, a.slabUsageLastReset.complexBoxes, &a.slabIdleResets.complexBoxes)
}

// holdsEscapableData reports whether the arena allocated anything a value outside it
// could still reference: string bytes, generic bytes (pointer-free cells), slice headers,
// typed backing arrays, scalar boxes or upvalue cells. Register banks are excluded; a
// frame's registers are copied out before the frame is released.
//
// Returns bool which is true when any data slab was used since the last reset.
func (a *RegisterArena) holdsEscapableData() bool {
	return a.byteIndex != 0 || len(a.oldByteSlabs) != 0 ||
		a.slabRetiredSinceReset != noSlabUsage ||
		a.genericBytesIndex != 0 || a.sliceHeaderIndex != 0 || len(a.oldSliceHeaderSlabs) != 0 ||
		a.intBackingIndex != 0 || a.floatBackingIndex != 0 || a.stringBackingIndex != 0 ||
		a.boolBackingIndex != 0 || a.uintBackingIndex != 0 ||
		a.stringBoxIndex != 0 || a.intBoxIndex != 0 || a.floatBoxIndex != 0 ||
		a.uintBoxIndex != 0 || a.complexBoxIndex != 0 ||
		a.UpvalueCellIndex != 0 || a.UpvalueReferenceIndex != 0
}

// arenaIndices is the contiguous block of arena allocation indices saved and restored per
// frame. Field order is load-bearing: byte offsets must match the ASM hard-coded
// CF_ARENA_SAVE references in asm_dispatch_offsets.h.
type arenaIndices struct {
	// IntIndex stores the allocation index for the int64 slab.
	IntIndex int

	// FloatIndex stores the allocation index for the float64 slab.
	FloatIndex int

	// StringIndex stores the allocation index for the string slab.
	StringIndex int

	// GeneralIndex stores the allocation index for the reflect.Value slab.
	GeneralIndex int

	// BoolIndex stores the allocation index for the bool slab.
	BoolIndex int

	// UintIndex stores the allocation index for the uint64 slab.
	UintIndex int

	// ComplexIndex stores the allocation index for the complex128 slab.
	ComplexIndex int

	// SlicesIntIndex stores the allocation index for the typed []int64 slice-header slab.
	SlicesIntIndex int

	// SlicesFloatIndex stores the allocation index for the typed []float64 slice-header
	// slab.
	SlicesFloatIndex int

	// SlicesStringIndex stores the allocation index for the typed []string slice-header
	// slab.
	SlicesStringIndex int

	// SlicesBoolIndex stores the allocation index for the typed []bool slice-header slab.
	SlicesBoolIndex int

	// SlicesUintIndex stores the allocation index for the typed []uint64 slice-header slab.
	SlicesUintIndex int

	// SlicesByteIndex stores the allocation index for the typed []byte slice-header slab.
	SlicesByteIndex int

	// UpvalueCellIndex stores the allocation index for the UpvalueCell slab.
	UpvalueCellIndex int

	// UpvalueReferenceIndex stores the allocation index for the upvalue reference slab.
	UpvalueReferenceIndex int
}

// arenaSliceHeader mirrors Go's runtime/internal slice header so the arena slab can back
// reflect.Value slice headers. Data is typed as unsafe.Pointer (not uintptr) so the GC
// keeps the backing array of any in-use header reachable as long as the slab itself is
// reachable.
type arenaSliceHeader struct {
	// Data is the slice's backing-array pointer; typed as unsafe.Pointer so the GC traces
	// through it.
	Data unsafe.Pointer

	// Len is the slice's logical length.
	Len int

	// Cap is the slice's capacity in elements.
	Cap int
}

// slabUsageSnapshot records per-class slab allocation volume in each class's native
// units: bytes for the byte-based slabs, slots for the header and box slabs.
type slabUsageSnapshot struct {
	// bytes is the byteSlab volume (string/byte data).
	bytes int

	// genericBytes is the genericBytesSlab volume (pointer-free struct storage).
	genericBytes int

	// sliceHeaders is the sliceHeaderSlab volume in header slots.
	sliceHeaders int

	// stringBoxes is the string box-slab volume in box slots.
	stringBoxes int

	// intBoxes is the int64 box-slab volume in box slots.
	intBoxes int

	// floatBoxes is the float64 box-slab volume in box slots.
	floatBoxes int

	// uintBoxes is the uint64 box-slab volume in box slots.
	uintBoxes int

	// complexBoxes is the complex128 box-slab volume in box slots.
	complexBoxes int
}

// noSlabUsage is the snapshot of an arena that has allocated nothing since its reset.
var noSlabUsage slabUsageSnapshot

// slabIdleCounters holds the consecutive-idle-Reset counters that drive hysteretic slab
// shrinking, one per slab class that participates (byteSlab keeps its historical
// standalone counter).
type slabIdleCounters struct {
	// genericBytes counts idle resets for the generic-byte slab.
	genericBytes uint32

	// sliceHeaders counts idle resets for the slice-header slab.
	sliceHeaders uint32

	// stringBoxes counts idle resets for the string box slab.
	stringBoxes uint32

	// intBoxes counts idle resets for the int64 box slab.
	intBoxes uint32

	// floatBoxes counts idle resets for the float64 box slab.
	floatBoxes uint32

	// uintBoxes counts idle resets for the uint64 box slab.
	uintBoxes uint32

	// complexBoxes counts idle resets for the complex128 box slab.
	complexBoxes uint32
}

// GetRegisterArena retrieves a RegisterArena from the pool.
//
// Returns a reset RegisterArena ready for use. Allocates a fresh arena via
// initRegisterArenaInPlace if the pool is exhausted past its growth ceiling.
func GetRegisterArena() *RegisterArena {
	return registerArenaPool.MustGet()
}

// PutRegisterArena returns a RegisterArena to the pool after resetting.
//
// Takes a (*RegisterArena) which is the arena to return to the pool.
func PutRegisterArena(a *RegisterArena) {
	if a == nil {
		return
	}
	a.detachBudgetTracker()
	a.Reset()
	registerArenaPool.Put(a)
}

// shrinkSlabWithHysteresis returns a fresh default-capacity slab when the current one is
// overgrown and idle.
//
// A slab that has grown past initial * maxArenaMultiplier and remained mostly idle for
// backingSlabIdleResetsBeforeShrink consecutive Resets is replaced. An Execute that
// touches more than len/backingSlabLowUseRatioDenominator of the slab counts as hot and
// clears the idle streak.
//
// Takes slab ([]T) which is the slab to consider.
// Takes initial (int) which is its default capacity.
// Takes usedLastExecute (int) which is the previous Execute's allocation volume in the
// slab's native units.
// Takes idleResets (*uint32) which is the class's consecutive idle counter.
//
// Returns the shrunk slab, or slab unchanged.
func shrinkSlabWithHysteresis[T any](slab []T, initial int, usedLastExecute int, idleResets *uint32) []T {
	if len(slab) <= initial*maxArenaMultiplier {
		*idleResets = 0
		return slab
	}
	if usedLastExecute > len(slab)/backingSlabLowUseRatioDenominator {
		*idleResets = 0
		return slab
	}
	*idleResets++
	if *idleResets < backingSlabIdleResetsBeforeShrink {
		return slab
	}
	*idleResets = 0
	return make([]T, initial)
}

// resolveDefaultMaxArenaBytes returns the host-aware arena budget.
//
// When detectHostMemoryBytes cannot probe, it falls back to
// defaultMaxArenaBytesUndetectedFallback when detection fails. Otherwise picks a
// conservative fraction of host RAM, clamped by defaultMaxArenaBytesCeiling.
//
// Returns the resolved per-Execute byte budget.
func resolveDefaultMaxArenaBytes() uint64 {
	hostBytes := hostmem.DetectHostMemoryBytes()
	if hostBytes == 0 {
		return defaultMaxArenaBytesUndetectedFallback
	}
	if cgroupBytes := hostmem.DetectCgroupMemoryLimitBytes(); cgroupBytes > 0 && cgroupBytes < hostBytes {
		hostBytes = cgroupBytes
	}
	candidate := hostBytes >> defaultMaxArenaBytesNormalShift
	if candidate < defaultMaxArenaBytesSmallHostThreshold {
		candidate = hostBytes / defaultMaxArenaBytesGenerousDenominator * defaultMaxArenaBytesGenerousNumerator
	}
	if candidate > defaultMaxArenaBytesCeiling {
		return defaultMaxArenaBytesCeiling
	}
	return candidate
}

// initRegisterArenaInPlace populates an existing RegisterArena slot with fresh slabs.
//
// Takes a (*RegisterArena) which is the arena slot to initialise; must be non-nil.
func initRegisterArenaInPlace(a *RegisterArena) {
	a.recycleChunks = ArenaChunkRecyclingEnabled
	a.intSlab = make([]int64, initialIntSlabs)
	a.slabGeneration++
	a.floatSlab = make([]float64, initialFloatSlabs)
	a.slabGeneration++
	a.stringSlab = make([]string, initialStringSlabs)
	a.slabGeneration++
	a.generalSlab = make([]reflect.Value, initialGeneralSlabs)
	a.slabGeneration++
	a.boolSlab = make([]bool, initialBoolSlabs)
	a.slabGeneration++
	a.uintSlab = make([]uint64, initialUintSlabs)
	a.slabGeneration++
	a.complexSlab = make([]complex128, initialComplexSlabs)
	a.slicesIntSlab = make([][]int64, initialSlicesIntSlabs)
	a.slicesFloatSlab = make([][]float64, initialSlicesFloatSlabs)
	a.slicesStringSlab = make([][]string, initialSlicesStringSlabs)
	a.slicesBoolSlab = make([][]bool, initialSlicesBoolSlabs)
	a.slicesUintSlab = make([][]uint64, initialSlicesUintSlabs)
	a.slicesByteSlab = make([][]byte, initialSlicesByteSlabs)
	a.slabGeneration++
	a.frameSlab = make([]CallFrame, initialFrameSlabs)
	a.callInfoBasesSlab = make([]uintptr, initialFrameSlabs)
	a.dispatchSavesSlab = make([]asmDispatchSave, initialFrameSlabs)
	a.byteSlab = make([]byte, InitialByteSlabSize)
	a.sliceHeaderSlab = make([]arenaSliceHeader, initialSliceHeaderCapacity)
	a.intBackingSlab = make([]int64, InitialIntBackingSize)
	a.floatBackingSlab = make([]float64, initialFloatBackingSize)
	a.stringBackingSlab = make([]string, initialStringBackingSize)
	a.boolBackingSlab = make([]bool, initialBoolBackingSize)
	a.uintBackingSlab = make([]uint64, initialUintBackingSize)
	a.upvalueCellSlab = make([]program.UpvalueCell, initialUpvalueCellSlabs)
	a.upvalueReferenceSlab = make([]upvalue, initialUpvalueRefSlabs)
}
