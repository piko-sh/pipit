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

//go:build pipit_store_census && !safe && !(js && wasm)

package engine

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"
	"weak"

	"pipit.sh/pipit/internal/isa"
)

const (
	// storeCensusBuilt is true in the pipit_store_census build.
	storeCensusBuilt = true

	// censusProbeDepth bounds the nesting the census probe follows.
	censusProbeDepth = 3

	// censusProbeBudget bounds the values the census probe visits per container.
	censusProbeBudget = 256

	// censusPublishDepth bounds the nesting the published-set walk follows.
	censusPublishDepth = 3

	// censusPublishBudget bounds the values the published-set walk visits per value.
	censusPublishBudget = 512

	// censusBoundary is the operation a violation found outside any instruction is recorded
	// under.
	censusBoundary = "<boundary>"

	// censusClassHeapPublished is the class of heap storage in the published set.
	censusClassHeapPublished = "heap-published"

	// censusClassGlobal is the class of a value stored into a global.
	censusClassGlobal = "global"

	// censusGlobalLogCap bounds the stores one global-store log keeps until the instruction
	// that made them drains them.
	censusGlobalLogCap = 1024

	// censusPublishElementCap bounds the elements of one slice or array the published-set
	// walk visits.
	censusPublishElementCap = 64
)

const (
	// censusUncoveredDepth bounds the containers censusUncovered descends through;
	// interfaces cost none.
	censusUncoveredDepth = 4096

	// censusUncoveredBudget bounds the values censusUncovered visits per probe.
	censusUncoveredBudget = 1 << 20
)

// censusFusedCopyOp names COPY_STRUCT_FIELD_GENERAL in the violations.
const censusFusedCopyOp = "COPY_STRUCT_FIELD_GENERAL"

// censusStringsOverMutBudget bounds the typed []string elements
// censusNoteStringsOverMutBytes reads per instruction.
const censusStringsOverMutBudget = 4096

// censusVMState is the census's view of one VM.
type censusVMState struct {
	// row is the instruction the VM is executing.
	row isa.OpSpec

	// inInstruction is true while row is valid.
	inInstruction bool

	// revoked is true once a revoking hand-off fired during the instruction.
	revoked bool
}

// censusViolationKey groups violations.
type censusViolationKey struct {
	// kind names the violation category.
	kind string

	// op names the bytecode operation responsible for the event.
	op string

	// class identifies the storage category, hand-off cause or value type associated with
	// the violation.
	class string

	// detail distinguishes events within the same census category.
	detail string
}

// censusGlobalLog records the stores into one global store.
type censusGlobalLog struct {
	// recent holds the stored values with their generation, drained by the instruction that
	// made them.
	recent []censusGlobalStore

	// generation counts stores appended to the shared log.
	generation atomic.Int64

	// mu serialises access to the recent global-store records.
	mu sync.Mutex
}

// censusGlobalStore is one store into a global.
type censusGlobalStore struct {
	// value is the reflected value inspected by the census.
	value reflect.Value

	// generation is the log sequence number assigned to this stored value.
	generation int64
}

var (
	// censusStates maps a *VM to its *censusVMState.
	censusStates censusWeakStates[VM, *censusVMState]

	// censusArenaStates maps a *RegisterArena to the state of the VM last seen running on
	// it, so a hand-off is attributed even when the arena records no owner.
	censusArenaStates censusWeakStates[RegisterArena, *censusVMState]

	// censusGlobals maps a *GlobalStore to its *censusGlobalLog.
	censusGlobals censusWeakStates[GlobalStore, *censusGlobalLog]

	// censusMu guards censusViolations.
	censusMu sync.Mutex

	// censusViolations counts violations.
	censusViolations = map[censusViolationKey]int64{}

	// censusPublished holds the addresses of heap storage handed to, or received from,
	// outside the arena's lifetime.
	censusPublished typedCache[uintptr, bool]
)

// censusContainer is one container operand of an instruction.
type censusContainer struct {
	// value is the reflected value inspected by the census.
	value reflect.Value

	// class identifies whether the container storage is arena-owned, covered, local or
	// published.
	class string

	// before records whether the container referenced arena storage before the instruction.
	before bool

	// known records whether the census could classify the inspected value.
	known bool

	// uncoveredBefore is censusUncovered's answer before the instruction, for a published
	// container; uncoveredKnown is false when that probe was truncated.
	uncoveredBefore bool

	// uncoveredKnown records whether the uncovered-storage result is cached.
	uncoveredKnown bool
}

// censusUncoveredWalk tracks the remaining visit allowance and followed pointers for one
// uncovered probe.
type censusUncoveredWalk struct {
	// seen holds the pointers followed, with their types.
	seen map[censusFollowed]struct{}

	// budget is the number of values the probe may still visit.
	budget int
}

// censusFollowed is one pointer a probe followed.
type censusFollowed struct {
	// pointer identifies the storage visited by the census walk.
	pointer unsafe.Pointer

	// typ is the reflected type used to interpret the visited storage.
	typ reflect.Type
}

// censusWeakStates maps the VMs, arenas and global stores the census has seen to its
// state for each, without keeping them alive.
//
// A strong key would hold every VM, arena and global store a process ever ran, with the
// chunks and values they reference, for the life of the process.
type censusWeakStates[K any, V any] struct {
	// entries maps weak.Pointer[K] to V.
	entries sync.Map
}

// load returns the state stored for key.
//
// Takes key (*K) which is the VM, arena or store.
//
// Returns V which is the state, or the zero value when absent.
// Returns bool which is true when a state was stored.
func (c *censusWeakStates[K, V]) load(key *K) (V, bool) {
	raw, found := c.entries.Load(weak.Make(key))
	if !found {
		var zero V
		return zero, false
	}
	return typedCacheValue[V](raw)
}

// loadOrStore returns the state stored for key, storing value first when there is none.
//
// Takes key (*K) which is the VM, arena or store.
// Takes value (V) which is the state to store when none is.
//
// Returns V which is the stored state. An entry of another type is replaced by value.
// Returns bool which is true when a state of type V was already stored.
func (c *censusWeakStates[K, V]) loadOrStore(key *K, value V) (V, bool) {
	weakKey := weak.Make(key)
	raw, loaded := c.entries.LoadOrStore(weakKey, value)
	if !loaded {
		runtime.AddCleanup(key, c.forget, weakKey)
		return value, false
	}
	stored, typed := typedCacheValue[V](raw)
	if !typed {
		c.entries.Store(weakKey, value)
		return value, false
	}
	return stored, true
}

// store sets the state for key.
//
// Takes key (*K) which is the VM, arena or store.
// Takes value (V) which is the state.
func (c *censusWeakStates[K, V]) store(key *K, value V) {
	weakKey := weak.Make(key)
	if _, loaded := c.entries.Swap(weakKey, value); !loaded {
		runtime.AddCleanup(key, c.forget, weakKey)
	}
}

// delete forgets the state for key.
//
// Takes key (*K) which is the VM, arena or store.
func (c *censusWeakStates[K, V]) delete(key *K) {
	c.entries.Delete(weak.Make(key))
}

// forget removes the entry of a collected key, as its cleanup.
//
// Takes weakKey (weak.Pointer[K]) which is the entry's key.
func (c *censusWeakStates[K, V]) forget(weakKey weak.Pointer[K]) {
	c.entries.Delete(weakKey)
}

// censusPlumbing names the functions between a hand-off site and the census.
var censusPlumbing = map[string]bool{
	"noteHandOff": true, "noteBoxedHandOff": true, "noteHandOffString": true, "revokeRemainder": true,
	"recordHandOff": true, "recordHandOffShared": true, "revokeHandOff": true, "censusNoteHandOff": true,
	"noteNativeCallArguments": true, "noteNativeScalarArguments": true, "noteStringCopy": true,
	"noteReflectCopy": true, "noteGoroutineSpawn": true, "noteNativeMethodArguments": true,
	"noteAppendIntoBacking": true, "recordContainerStore": true,
	"censusNoteMemoSettled": true, "memoSettled": true, "memoCovers": true, "revokeTrivially": true,
	"revokeValue": true, "walkHandOff": true, "visit": true, "visitSlice": true, "visitMap": true,
	"visitBacking": true, "visitElements": true,
}

// censusGatingKinds lists violations that fail the store census.
var censusGatingKinds = map[string]bool{
	"undeclared-outside-store":    true,
	"undeclared-cause":            true,
	"uncovered-outside-store":     true,
	"unrevoked-outside-store":     true,
	"fused-copy-arena-reference":  true,
	"copy-builtin-unnoted":        true,
	"string-over-mutable-bytes":   true,
	"uncovered-callback-return":   true,
	"memo-settled-uncovered":      true,
	"served-store-foreign-value":  true,
	"served-store-foreign-target": true,
	"served-store-covered-target": true,

	"local-store-memoised-container":  true,
	"local-store-published-container": true,
}

// censusStateFor returns the census state of vm.
//
// Takes vm (*VM) which is the executing VM.
//
// Returns *censusVMState which is never nil.
func censusStateFor(vm *VM) *censusVMState {
	if state, ok := censusStates.load(vm); ok && state != nil {
		return state
	}
	state, _ := censusStates.loadOrStore(vm, &censusVMState{})
	return state
}

// censusGlobalLogFor returns the store log of globals.
//
// Takes globals (*GlobalStore) which is the store.
//
// Returns *censusGlobalLog which is never nil.
func censusGlobalLogFor(globals *GlobalStore) *censusGlobalLog {
	if log, ok := censusGlobals.load(globals); ok && log != nil {
		return log
	}
	log, _ := censusGlobals.loadOrStore(globals, &censusGlobalLog{})
	return log
}

// censusDispatch runs one instruction under the census.
//
// Takes vm (*VM) which executes it.
// Takes frame (*CallFrame) which is the running frame.
// Takes registers (*Registers) which holds the operands.
// Takes instruction (isa.Instruction) which is the instruction.
//
// Returns OpResult which is the handler's result.
func censusDispatch(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	row, ok := isa.SpecForInstruction(instruction)
	if !ok {
		return flatDispatchSwitch(vm, frame, registers, instruction)
	}
	state := censusStateFor(vm)
	if vm.Arena != nil {
		if current, ok := censusArenaStates.load(vm.Arena); !ok || current != state {
			censusArenaStates.store(vm.Arena, state)
		}
	}
	saved := *state
	state.row, state.inInstruction, state.revoked = row, true, false
	containers := censusContainers(vm.Arena, registers, instruction, row)
	var log *censusGlobalLog
	var generation int64
	if vm.Globals != nil {
		log = censusGlobalLogFor(vm.Globals)
		generation = log.generation.Load()
	}
	var callResults []unsafe.Pointer
	framePointer := vm.FramePointer
	if row.Calls() {
		callResults = censusGeneralStorage(registers)
	}
	result := flatDispatchSwitch(vm, frame, registers, instruction)
	if callResults != nil && vm.FramePointer == framePointer {
		censusPublishCallResults(registers, callResults)
	}
	revoked := state.revoked
	*state = saved
	censusSettle(vm.Arena, row, containers, revoked)
	censusNoteStringsOverMutBytes(vm.Arena, registers, row)
	if log != nil && log.generation.Load() != generation {
		censusSettleGlobals(vm.Arena, row, log, generation, revoked)
	}
	return result
}

// censusGeneralStorage snapshots the storage each general register and typed string slice
// refers to, so the results a native call writes can be told apart afterwards.
//
// Takes registers (*Registers) which holds the banks.
//
// Returns []unsafe.Pointer which holds one address per register.
func censusGeneralStorage(registers *Registers) []unsafe.Pointer {
	storage := make([]unsafe.Pointer, 0, len(registers.General)+len(registers.slicesString))
	for _, value := range registers.General {
		pointer, _ := censusStorage(value)
		storage = append(storage, pointer)
	}
	for _, value := range registers.slicesString {
		storage = append(storage, unsafe.Pointer(unsafe.SliceData(value)))
	}
	return storage
}

// censusPublishCallResults publishes the containers a native call wrote into the general
// or typed string-slice bank.
//
// The host returned them, so the host may still reach them.
//
// Takes registers (*Registers) which holds the banks.
// Takes before ([]unsafe.Pointer) which is the bank's storage before the call.
func censusPublishCallResults(registers *Registers, before []unsafe.Pointer) {
	if len(before) != len(registers.General)+len(registers.slicesString) {
		return
	}
	for index, value := range registers.General {
		if storage, _ := censusStorage(value); storage != nil && storage != before[index] {
			censusPublish(value)
		}
	}
	for index, value := range registers.slicesString {
		if storage := unsafe.Pointer(unsafe.SliceData(value)); storage != nil && storage != before[len(registers.General)+index] {
			censusPublish(reflect.ValueOf(value))
		}
	}
}

// censusContainers collects and probes the container operands the instruction reads.
//
// Takes arena (*RegisterArena) which classifies storage.
// Takes registers (*Registers) which holds the operands.
// Takes instruction (isa.Instruction) which names them.
// Takes row (isa.OpSpec) which describes them.
//
// Returns []censusContainer which holds them.
func censusContainers(arena *RegisterArena, registers *Registers, instruction isa.Instruction, row isa.OpSpec) []censusContainer {
	shape := row.Shape()
	roles := [isa.NumInstructionOperands]isa.OperandRole{shape.A, shape.B, shape.C}
	operands := [isa.NumInstructionOperands]uint8{instruction.A, instruction.B, instruction.C}
	var containers []censusContainer
	for index, role := range roles {
		if !shape.Reads[index] {
			continue
		}
		kind, isRegister := isa.KindForRole(role)
		if !isRegister {
			continue
		}
		var value reflect.Value
		switch kind {
		case isa.RegisterGeneral:
			if int(operands[index]) >= len(registers.General) {
				continue
			}
			value = registers.General[operands[index]]
		case isa.RegisterSliceString:
			if int(operands[index]) >= len(registers.slicesString) {
				continue
			}
			value = reflect.ValueOf(registers.slicesString[operands[index]])
		default:
			continue
		}
		if container, ok := censusClassify(arena, value); ok {
			containers = append(containers, container)
		}
	}
	return containers
}

// censusStorage returns the address of the storage a container value writes into, or nil
// when it is not a container.
//
// Takes value (reflect.Value) which is the operand.
//
// Returns unsafe.Pointer which is the storage address.
// Returns reflect.Value which is the container, unwrapped from an interface.
func censusStorage(value reflect.Value) (unsafe.Pointer, reflect.Value) {
	for value.IsValid() && value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil, reflect.Value{}
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return nil, value
	}
	switch value.Kind() {
	case reflect.Slice, reflect.Pointer, reflect.Map, reflect.Chan:
		if value.IsNil() {
			return nil, value
		}
		return value.UnsafePointer(), value
	case reflect.Struct, reflect.Array:
		if value.CanAddr() {
			return unsafe.Pointer(value.UnsafeAddr()), value
		}
	default:
	}
	return nil, value
}

// censusClassify classifies and probes one container operand.
//
// Takes arena (*RegisterArena) which classifies storage.
// Takes value (reflect.Value) which is the operand.
//
// Returns censusContainer and true when value is a container.
func censusClassify(arena *RegisterArena, value reflect.Value) (censusContainer, bool) {
	pointer, container := censusStorage(value)
	if pointer == nil {
		return censusContainer{}, false
	}
	if arena != nil && (arena.ownsAnyStorage(pointer) || arena.krefContains(pointer) && !arena.krefMayBeCovered(pointer)) {
		if censusOwnStorageCovered(arena, pointer) {
			uncoveredBefore, uncoveredKnown := censusUncoveredProbe(arena, container)
			return censusContainer{
				value: container, class: "arena-covered", before: false, known: false,
				uncoveredBefore: uncoveredBefore, uncoveredKnown: uncoveredKnown,
			}, true
		}
		return censusContainer{value: container, class: "arena", before: false, known: false, uncoveredBefore: false, uncoveredKnown: false}, true
	}
	class := "heap-local"
	if _, published := censusPublished.load(uintptr(pointer)); published {
		class = censusClassHeapPublished
	}
	references, known := censusProbe(arena, container)
	result := censusContainer{value: container, class: class, before: references, known: known, uncoveredBefore: false, uncoveredKnown: false}
	if class == censusClassHeapPublished {
		result.uncoveredBefore, result.uncoveredKnown = censusUncoveredProbe(arena, container)
	}
	return result, true
}

// censusOwnStorageCovered reports whether pointer, the arena's own storage, lies in a
// data slab or a cell slab the licence has revoked, alone or with the epoch.
//
// Takes arena (*RegisterArena) which owns pointer.
// Takes pointer (unsafe.Pointer) which is the container's storage.
//
// Returns bool which is true for covered own storage.
func censusOwnStorageCovered(arena *RegisterArena, pointer unsafe.Pointer) bool {
	if !arena.licensed {
		return false
	}
	if base, ok := licenceDataSlabOf(arena, uintptr(pointer)); ok {
		return arena.licenceRevokedBase(base)
	}
	if base, class := arena.licenceNonDataSlabOf(uintptr(pointer)); class == licenceSlabCell {
		return arena.licenceRevokedBase(base)
	}
	return false
}

// censusProbe reports whether a container references arena storage, reading a slice up to
// its capacity.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes container (reflect.Value) which is the container.
//
// Returns references (bool) which is true when the container references arena storage.
// Returns known (bool) which is false when the probe cannot determine the answer.
func censusProbe(arena *RegisterArena, container reflect.Value) (references bool, known bool) {
	if arena == nil {
		return false, false
	}
	if container.Kind() == reflect.Slice {
		container = container.Slice(0, container.Cap())
	}
	found, truncated := arena.referencesArenaStorage(container, false, censusProbeDepth, new(censusProbeBudget), arena.licenceShared(0))
	return found, !truncated
}

// censusSettle records the violations of the stores the instruction made into its
// container operands.
//
// Takes arena (*RegisterArena) which classifies storage.
// Takes row (isa.OpSpec) which is the instruction.
// Takes containers ([]censusContainer) which were probed before it ran.
// Takes revoked (bool) which is true when a revoking hand-off fired during it.
//
// Concurrency: Protects violation tallies with censusMu; the caller must own the arena
// and its instruction-attribution state.
func censusSettle(arena *RegisterArena, row isa.OpSpec, containers []censusContainer, revoked bool) {
	mutates, _ := row.MutatesMemory()
	censusMu.Lock()
	defer censusMu.Unlock()
	for _, container := range containers {
		transition := false
		if container.class != "arena" && container.class != "arena-covered" && container.known && !container.before {
			after, known := censusProbe(arena, container.value)
			transition = known && after
		}
		if (container.class == censusClassHeapPublished || container.class == "arena-covered") && mutates && (!container.uncoveredKnown || !container.uncoveredBefore) {
			censusRecordUncovered(arena, row, container.class, container.value, container.value.Type().String(), container.uncoveredKnown)
		}
		if transition {
			censusRecordTransition(row, container.class, revoked, censusArenaLicensed(arena), container.value.Type().String())
		}
	}
}

// censusRecordUncovered probes a published container, or a stored global value, after the
// instruction and records an uncovered-outside-store when it references this arena's
// storage that nothing covers, or a probe-truncated-outside-store when the probe could
// not finish. censusMu must be held.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes row (isa.OpSpec) which is the instruction.
// Takes class (string) which is the target's class.
// Takes value (reflect.Value) which is the container or stored value.
// Takes detail (string) which describes the target.
// Takes beforeKnown (bool) which is false when the container's probe before the
// instruction was truncated, so a reference found now may not be new.
func censusRecordUncovered(arena *RegisterArena, row isa.OpSpec, class string, value reflect.Value, detail string, beforeKnown bool) {
	uncovered, known := censusUncoveredProbe(arena, value)
	switch {
	case !known, uncovered && !beforeKnown:

		censusViolations[censusViolationKey{kind: "probe-truncated-outside-store", op: row.Name, class: class, detail: detail}]++
	case uncovered && class == censusClassGlobal && !arena.licensed:

		censusViolations[censusViolationKey{kind: "global-settled-at-release", op: row.Name, class: class, detail: detail}]++
	case uncovered:
		censusViolations[censusViolationKey{kind: "uncovered-outside-store", op: row.Name, class: class, detail: detail}]++
	default:
	}
}

// censusUncoveredProbe runs censusUncovered from the top of value.
//
// Takes arena (*RegisterArena) which owns the candidate storage; nil answers known false.
// Takes value (reflect.Value) which is the container or value.
//
// Returns uncovered (bool) which is true when the value references uncovered arena
// storage.
// Returns known (bool) which is false when the probe is truncated; a nil arena gives a
// known answer of no uncovered storage.
func censusUncoveredProbe(arena *RegisterArena, value reflect.Value) (uncovered bool, known bool) {
	if arena == nil || censusArenaCoversAll(arena) {
		return false, true
	}
	walk := &censusUncoveredWalk{seen: nil, budget: censusUncoveredBudget}
	shared := arena.licenceShared(0)
	if shared && value.Kind() == reflect.Slice && value.CanAddr() {
		value = value.Slice(0, value.Cap())
	}
	found, truncated := censusUncovered(arena, value, censusUncoveredDepth, walk, shared)
	return found, !truncated
}

// censusArenaCoversAll reports whether every arena address is covered so a probe needs no
// walk.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
//
// Returns bool which is true when nothing of the arena is uncovered.
func censusArenaCoversAll(arena *RegisterArena) bool {
	if !arena.licensed {
		return censusAbandons(arena)
	}
	return arena.licenceState.epochRevoked || arena.blindSinceReading()
}

// censusUncovered reports whether value references storage of arena that nothing covers.
// It reads a slice up to its capacity, so storage an append in place reaches is seen.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes value (reflect.Value) which is the value walked.
// Takes depth (int) which is the remaining nesting allowance.
// Takes walk (*censusUncoveredWalk) which holds the remaining value allowance and the
// pointers already followed.
// Takes shared (bool) which is true when another goroutine may write what value reaches.
//
// Returns uncovered (bool) which is true when the probe finds uncovered arena storage.
// Returns truncated (bool) which is true when the probe cannot finish within its bounds
// or safely inspect shared storage.
func censusUncovered(arena *RegisterArena, value reflect.Value, depth int, walk *censusUncoveredWalk, shared bool) (uncovered bool, truncated bool) {
	if !value.IsValid() || typeIsPointerFree(value.Type()) {
		return false, false
	}
	if depth <= 0 || walk.budget <= 0 {
		return false, true
	}
	if shared && value.CanAddr() {
		return censusSharedUncovered(arena, value, depth, walk)
	}
	if shared && value.Kind() == reflect.Map && !value.IsNil() {
		return false, true
	}
	walk.budget--
	switch value.Kind() {
	case reflect.String:
		return censusStringUncovered(arena, value.String()), false
	case reflect.Slice:
		return censusSliceUncovered(arena, value, depth, walk, shared)
	case reflect.Array:
		if typeIsPointerFree(value.Type().Elem()) {
			return false, false
		}
		return censusElementsUncovered(arena, value, depth, walk, shared)
	case reflect.Struct:
		return censusFieldsUncovered(arena, value, depth, walk, shared)
	case reflect.Pointer, reflect.UnsafePointer:
		return censusPointerUncovered(arena, value, depth, walk, shared)
	case reflect.Interface:
		if value.IsNil() {
			return false, false
		}
		return censusUncovered(arena, value.Elem(), depth, walk, shared)
	case reflect.Map:
		return censusMapUncovered(arena, value, depth, walk, shared)
	default:
		return false, false
	}
}

// censusStringUncovered reports whether a string's bytes are uncovered arena storage.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes s (string) which is the string.
//
// Returns bool which is true when the bytes are uncovered arena storage.
func censusStringUncovered(arena *RegisterArena, s string) bool {
	if len(s) == 0 {
		return false
	}
	return censusStorageUncovered(arena, unsafe.Pointer(unsafe.StringData(s)))
}

// censusSliceUncovered is censusUncovered for a slice: its backing array, then its
// elements up to its capacity.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes value (reflect.Value) which is the slice.
// Takes depth (int) which is the remaining nesting allowance.
// Takes walk (*censusUncoveredWalk) which holds the remaining value allowance and the
// pointers already followed.
// Takes shared (bool) which is true when another goroutine may write what value reaches.
//
// Returns uncovered (bool) which is true when the probe finds uncovered arena storage.
// Returns truncated (bool) which is true when the probe cannot finish within its bounds
// or safely inspect shared storage.
func censusSliceUncovered(arena *RegisterArena, value reflect.Value, depth int, walk *censusUncoveredWalk, shared bool) (uncovered bool, truncated bool) {
	if value.IsNil() {
		return false, false
	}
	if censusStorageUncovered(arena, value.UnsafePointer()) {
		return true, false
	}
	if typeIsPointerFree(value.Type().Elem()) {
		return false, false
	}
	return censusElementsUncovered(arena, value.Slice(0, value.Cap()), depth, walk, shared)
}

// censusPointerUncovered is censusUncovered for a pointer or unsafe pointer: its
// pointee's storage, then, for a typed pointer not followed before, the pointee itself.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes value (reflect.Value) which is the pointer.
// Takes depth (int) which is the remaining nesting allowance.
// Takes walk (*censusUncoveredWalk) which holds the remaining value allowance and the
// pointers already followed.
// Takes shared (bool) which is true when another goroutine may write what value reaches.
//
// Returns uncovered (bool) which is true when the probe finds uncovered arena storage.
// Returns truncated (bool) which is true when the probe cannot finish within its bounds
// or safely inspect shared storage.
func censusPointerUncovered(arena *RegisterArena, value reflect.Value, depth int, walk *censusUncoveredWalk, shared bool) (uncovered bool, truncated bool) {
	if value.IsNil() {
		return false, false
	}
	if censusStorageUncovered(arena, value.UnsafePointer()) {
		return true, false
	}
	if value.Kind() == reflect.UnsafePointer || walk.followed(value.UnsafePointer(), value.Type()) {
		return false, false
	}
	return censusUncovered(arena, value.Elem(), depth-1, walk, shared)
}

// censusMapUncovered is censusUncovered over the keys and values of a map.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes value (reflect.Value) which is the map.
// Takes depth (int) which is the remaining nesting allowance.
// Takes walk (*censusUncoveredWalk) which holds the remaining value allowance and the
// pointers already followed.
// Takes shared (bool) which is true when another goroutine may write what value reaches.
//
// Returns uncovered (bool) which is true when the probe finds uncovered arena storage.
// Returns truncated (bool) which is true when the probe cannot finish within its bounds
// or safely inspect shared storage.
func censusMapUncovered(arena *RegisterArena, value reflect.Value, depth int, walk *censusUncoveredWalk, shared bool) (uncovered bool, truncated bool) {
	if value.IsNil() {
		return false, false
	}
	cut := false
	entries := value.MapRange()
	for entries.Next() {
		for _, entry := range [2]reflect.Value{entries.Key(), entries.Value()} {
			found, truncatedEntry := censusUncovered(arena, entry, depth-1, walk, shared)
			if found {
				return true, false
			}
			cut = cut || truncatedEntry
		}
	}
	return false, cut
}

// censusSharedUncovered is censusUncovered for an addressable value another goroutine may
// write.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes value (reflect.Value) which is the addressable value.
// Takes depth (int) which is the remaining nesting allowance.
// Takes walk (*censusUncoveredWalk) which holds the remaining value allowance and the
// pointers already followed.
//
// Returns uncovered (bool) which is true when the probe finds uncovered arena storage.
// Returns truncated (bool) which is true when the probe cannot finish within its bounds
// or safely inspect shared storage.
func censusSharedUncovered(arena *RegisterArena, value reflect.Value, depth int, walk *censusUncoveredWalk) (uncovered bool, truncated bool) {
	if typeIsPointerFree(value.Type()) {
		return false, false
	}
	walk.budget--
	word := (*unsafe.Pointer)(unsafe.Pointer(value.UnsafeAddr()))
	switch value.Kind() {
	case reflect.String, reflect.UnsafePointer:
		return censusStorageUncovered(arena, atomic.LoadPointer(word)), false
	case reflect.Pointer:
		pointer := atomic.LoadPointer(word)
		if pointer == nil {
			return false, false
		}
		if censusStorageUncovered(arena, pointer) {
			return true, false
		}
		if walk.followed(pointer, value.Type()) {
			return false, false
		}
		return censusUncovered(arena, reflect.NewAt(value.Type().Elem(), pointer).Elem(), depth-1, walk, true)
	case reflect.Struct:
		return censusFieldsUncovered(arena, value, depth, walk, true)
	case reflect.Array:
		return censusElementsUncovered(arena, value, depth, walk, true)
	case reflect.Slice:

		pointer := atomic.LoadPointer(word)
		if pointer == nil {
			return false, false
		}
		if censusStorageUncovered(arena, pointer) {
			return true, false
		}
		return false, true
	default:

		return false, true
	}
}

// censusFieldsUncovered is censusUncovered over the fields of a struct.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes value (reflect.Value) which is the struct.
// Takes depth (int) which is the remaining nesting allowance.
// Takes walk (*censusUncoveredWalk) which holds the remaining value allowance and the
// pointers already followed.
// Takes shared (bool) which is true when another goroutine may write what value reaches.
//
// Returns uncovered (bool) which is true when the probe finds uncovered arena storage.
// Returns truncated (bool) which is true when the probe cannot finish within its bounds
// or safely inspect shared storage.
func censusFieldsUncovered(arena *RegisterArena, value reflect.Value, depth int, walk *censusUncoveredWalk, shared bool) (uncovered bool, truncated bool) {
	if typeIsPointerFree(value.Type()) {
		return false, false
	}
	cut := false
	for index := range value.NumField() {
		found, truncated := censusUncovered(arena, structFieldValue(value, index), depth-1, walk, shared)
		if found {
			return true, false
		}
		cut = cut || truncated
	}
	return false, cut
}

// censusElementsUncovered is censusUncovered over the elements of a slice or array.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes value (reflect.Value) which is the slice or array.
// Takes depth (int) which is the remaining nesting allowance.
// Takes walk (*censusUncoveredWalk) which holds the remaining value allowance and the
// pointers already followed.
// Takes shared (bool) which is true when another goroutine may write what value reaches.
//
// Returns uncovered (bool) which is true when the probe finds uncovered arena storage.
// Returns truncated (bool) which is true when the probe cannot finish within its bounds
// or safely inspect shared storage.
func censusElementsUncovered(arena *RegisterArena, value reflect.Value, depth int, walk *censusUncoveredWalk, shared bool) (uncovered bool, truncated bool) {
	cut := false
	for index := range value.Len() {
		found, truncatedElement := censusUncovered(arena, value.Index(index), depth-1, walk, shared)
		if found {
			return true, false
		}
		cut = cut || truncatedElement
		if walk.budget <= 0 {
			return false, true
		}
	}
	return false, cut
}

// followed records that the probe follows pointer as typ, and reports whether it already
// had.
//
// Takes pointer (unsafe.Pointer) which is the pointer.
// Takes typ (reflect.Type) which is its pointer type.
//
// Returns bool which is true when pointer was followed before.
func (walk *censusUncoveredWalk) followed(pointer unsafe.Pointer, typ reflect.Type) bool {
	key := censusFollowed{pointer: pointer, typ: typ}
	if _, seen := walk.seen[key]; seen {
		return true
	}
	if walk.seen == nil {
		walk.seen = make(map[censusFollowed]struct{})
	}
	walk.seen[key] = struct{}{}
	return false
}

// censusStorageUncovered reports whether an address names reusable arena storage without
// revocation coverage.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes pointer (unsafe.Pointer) which is the address.
//
// Returns bool which is true for uncovered arena storage.
func censusStorageUncovered(arena *RegisterArena, pointer unsafe.Pointer) bool {
	if pointer == nil {
		return false
	}
	if arena.licensed && !licenceRegisteredPage(arena, uintptr(pointer)) {
		return false
	}
	if !arena.ownsPointee(pointer) && !arena.krefContains(pointer) {
		return false
	}
	if !arena.licensed {
		return !censusAbandons(arena)
	}
	if arena.licenceState.epochRevoked || arena.blindSinceReading() {
		return false
	}
	base, found := licenceDataSlabOf(arena, uintptr(pointer))
	if !found {
		return false
	}
	return !arena.licenceRevokedBase(base)
}

// censusSettleGlobals records the violations of the global stores the instruction made.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes row (isa.OpSpec) which is the instruction.
// Takes log (*censusGlobalLog) which holds the stores.
// Takes generation (int64) which is the log's generation before the instruction.
// Takes revoked (bool) which is true when a revoking hand-off fired during it.
//
// Concurrency: Copies recorded global stores under log.mu and updates violation tallies
// under censusMu; arena inspection requires caller ownership.
func censusSettleGlobals(arena *RegisterArena, row isa.OpSpec, log *censusGlobalLog, generation int64, revoked bool) {
	log.mu.Lock()
	var stores []censusGlobalStore
	kept := log.recent[:0]
	for _, store := range log.recent {
		if store.generation > generation {
			stores = append(stores, store)
			continue
		}
		kept = append(kept, store)
	}
	log.recent = kept
	log.mu.Unlock()
	for _, store := range stores {
		references := false
		if arena != nil && store.value.IsValid() {
			found, truncated := arena.referencesArenaStorage(store.value, true, censusProbeDepth, new(censusProbeBudget), arena.licenceShared(0))
			references = found && !truncated
		}
		censusMu.Lock()
		if references {
			censusRecordTransition(row, censusClassGlobal, revoked, censusArenaLicensed(arena), store.value.Kind().String())
		}
		censusRecordUncovered(arena, row, censusClassGlobal, store.value, store.value.Kind().String(), true)
		censusMu.Unlock()
	}
}

// censusRecordTransition records the violations of one store that left arena references
// in a container that held none before it (a transition). censusMu must be held.
//
// Takes row (isa.OpSpec) which is the instruction.
// Takes class (string) which is the target's class.
// Takes revoked (bool) which is true when a revoking hand-off fired.
// Takes licensed (bool) which is true when the storing arena is licensed, where an
// unrevoked outside store is a gating violation.
// Takes detail (string) which describes the target.
func censusRecordTransition(row isa.OpSpec, class string, revoked bool, licensed bool, detail string) {
	if class != censusClassHeapPublished && class != censusClassGlobal {
		if !censusRowHandsOff(row) {
			censusViolations[censusViolationKey{kind: "undeclared-local-store", op: row.Name, class: class, detail: detail}]++
		}
		if !revoked {
			censusViolations[censusViolationKey{kind: "unrevoked-local-store", op: row.Name, class: class, detail: detail}]++
		}
		return
	}
	if !censusRowHandsOff(row) {
		censusViolations[censusViolationKey{kind: "undeclared-outside-store", op: row.Name, class: class, detail: detail}]++
	}
	if !revoked {
		kind := "unrevoked-outside-store"
		if !licensed {
			kind = "unrevoked-outside-store-without-licence"
		}
		censusViolations[censusViolationKey{kind: kind, op: row.Name, class: class, detail: detail}]++
	}
}

// censusArenaLicensed reports whether arena is licensed, where a store that hands arena
// storage outside must be revoked at the store.
//
// Takes arena (*RegisterArena) which may be nil.
//
// Returns bool which is true for a licensed arena.
func censusArenaLicensed(arena *RegisterArena) bool {
	return arena != nil && arena.licensed
}

// censusAbandons reports whether an arena without a licence is one of the abandon
// fallback's, which is dropped at release and reuses none of its storage.
//
// Takes arena (*RegisterArena) which is not licensed.
//
// Returns bool which is true when nothing the arena holds is reused.
func censusAbandons(arena *RegisterArena) bool {
	return arena.unpooled && !arena.heapData
}

// censusNoteFusedCopy asserts the invariant the fused field copy rests on.
//
// Takes arena (*RegisterArena) which classifies storage; may be nil.
// Takes destinationBase (unsafe.Pointer) which is the destination struct's storage.
// Takes fieldType (reflect.Type) which is the field's type.
// Takes fieldPointer (unsafe.Pointer) which is the destination field after the copy.
//
// Concurrency: Updates violation tallies under censusMu; the caller must own the
// inspected arena and keep the source and destination storage valid.
func censusNoteFusedCopy(arena *RegisterArena, destinationBase unsafe.Pointer, fieldType reflect.Type, fieldPointer unsafe.Pointer) {
	if arena == nil || typeIsPointerFree(fieldType) || arena.ownsAnyStorage(destinationBase) {
		return
	}
	field := unsafeNewAt(reflectValueABIType(fieldType), fieldPointer, fieldType.Kind())
	references, _ := arena.referencesArenaStorage(field, false, censusProbeDepth, new(censusProbeBudget), arena.licenceShared(0))
	if !references {
		return
	}
	class := "heap-local"
	if _, published := censusPublished.load(uintptr(destinationBase)); published {
		class = censusClassHeapPublished
	}
	uncovered, known := censusUncoveredProbe(arena, field)
	kind := "fused-copy-covered-reference"
	switch {
	case !known:
		kind = "fused-copy-unknown"
	case uncovered:
		kind = "fused-copy-arena-reference"
	}
	censusMu.Lock()
	censusViolations[censusViolationKey{kind: kind, op: censusFusedCopyOp, class: class, detail: fieldType.String()}]++
	censusMu.Unlock()
}

// censusRowHandsOff reports whether a row declares a copy, revoke or borrow entry.
//
// Takes row (isa.OpSpec) which is the instruction.
//
// Returns bool which is true when it does.
func censusRowHandsOff(row isa.OpSpec) bool {
	return row.HasHandOffKind(isa.HandOffCopy) || row.HasHandOffKind(isa.HandOffRevoke) || row.HasHandOffKind(isa.HandOffBorrow)
}

// censusEnterBoundary suspends instruction attribution on the state of the VM last seen
// running on a, so the hand-offs a boundary root makes (the panic capture) are attributed
// to the boundary, as the hand-off table attributes them, and are not checked against the
// row of the instruction that raised the panic.
//
// Takes arena (*RegisterArena) which is the arena about to hand off.
//
// Returns state (*censusVMState) which is the suspended state, nil when there is none.
// Returns was (bool) which is the state's inInstruction before the suspension.
func censusEnterBoundary(arena *RegisterArena) (state *censusVMState, was bool) {
	if arena == nil {
		return nil, false
	}
	state, ok := censusArenaStates.load(arena)
	if !ok || state == nil {
		return nil, false
	}
	was = state.inInstruction
	state.inInstruction = false
	return state, was
}

// censusLeaveBoundary restores the attribution censusEnterBoundary suspended.
//
// Takes state (*censusVMState) which censusEnterBoundary returned; may be nil.
// Takes was (bool) which censusEnterBoundary returned.
func censusLeaveBoundary(state *censusVMState, was bool) {
	if state != nil {
		state.inInstruction = was
	}
}

// censusNoteHandOff records the storage published by a hand-off.
//
// Takes arena (*RegisterArena) which is the arena handing off.
// Takes cause (HandOffCause) which is the cause.
// Takes value (reflect.Value) which is the value handed off.
//
// Concurrency: Updates violation tallies under censusMu; the caller must own the arena's
// instruction-attribution state.
func censusNoteHandOff(arena *RegisterArena, cause HandOffCause, value reflect.Value) {
	if state, ok := censusArenaStates.load(arena); ok && state != nil && state.inInstruction {
		if cause.Revokes() {
			state.revoked = true
		}
		if !state.row.DeclaredCauses()[cause] {
			site := censusSiteOfCaller()
			censusMu.Lock()
			censusViolations[censusViolationKey{kind: "undeclared-cause", op: state.row.Name, class: cause.String(), detail: site}]++
			censusMu.Unlock()
		}
	}
	censusPublish(value)
}

// censusSiteOfCaller names the first frame outside the hand-off plumbing.
//
// Returns string which is "file:line(function)".
func censusSiteOfCaller() string {
	var pcs [24]uintptr
	frames := runtime.CallersFrames(pcs[:runtime.Callers(3, pcs[:])])
	for {
		frame, more := frames.Next()
		name := frame.Function[strings.LastIndexByte(frame.Function, '/')+1:]
		base := name[strings.LastIndexByte(name, '.')+1:]
		if !censusPlumbing[base] {
			file := frame.File[strings.LastIndexByte(frame.File, '/')+1:]
			return fmt.Sprintf("%s:%d(%s)", file, frame.Line, name)
		}
		if !more {
			return "unknown"
		}
	}
}

// censusNoteGlobalStore records a store into a global.
//
// Takes globals (*GlobalStore) which is the store.
// Takes value (reflect.Value) which is the stored value.
//
// Concurrency: Appends to the shared global-store log under its mutex and advances its
// generation atomically.
func censusNoteGlobalStore(globals *GlobalStore, value reflect.Value) {
	log := censusGlobalLogFor(globals)
	log.mu.Lock()
	generation := log.generation.Add(1)
	if len(log.recent) < censusGlobalLogCap {
		log.recent = append(log.recent, censusGlobalStore{value: value, generation: generation})
	}
	log.mu.Unlock()
	censusPublish(value)
}

// censusNoteCallbackReturn probes the results a callback or bound-method run hands its
// native caller (the callback-return boundary) once they are materialised, and records an
// uncovered-callback-return when one still references this arena's storage that nothing
// covers, or a probe-truncated-callback-return when the probe could not finish.
//
// The results are published. The host holds them after the arena is released.
//
// Takes arena (*RegisterArena) which ran the guest code.
// Takes results ([]any) which are the materialised results.
//
// Concurrency: Updates violation tallies under censusMu; the caller must own the arena
// while probing its returned values.
func censusNoteCallbackReturn(arena *RegisterArena, results []any) {
	for _, result := range results {
		if result == nil {
			continue
		}
		value := reflect.ValueOf(result)
		uncovered, known := censusUncoveredProbe(arena, value)
		kind := ""
		switch {
		case !known:
			kind = "probe-truncated-callback-return"
		case uncovered:
			kind = "uncovered-callback-return"
		default:
		}
		if kind != "" {
			censusMu.Lock()
			censusViolations[censusViolationKey{kind: kind, op: censusBoundary, class: "callback-return", detail: value.Type().String()}]++
			censusMu.Unlock()
		}
		censusPublish(value)
	}
}

// censusNoteMemoSettled probes a container settled by revokeTrivially or encountered at a
// memoCovers stopping point.
//
// Takes arena (*RegisterArena) which walks.
// Takes value (reflect.Value) which is the container.
//
// Concurrency: Updates violation tallies under censusMu; the caller must own the arena's
// memo and instruction-attribution state.
func censusNoteMemoSettled(arena *RegisterArena, value reflect.Value) {
	if !value.IsValid() {
		return
	}
	uncovered, known := censusUncoveredProbe(arena, value)
	kind := ""
	switch {
	case !known:
		kind = "probe-truncated-memo-settled"
	case uncovered:
		kind = "memo-settled-uncovered"
	default:
		return
	}
	op := censusBoundary
	if state, ok := censusArenaStates.load(arena); ok && state != nil && state.inInstruction {
		op = state.row.Name
	}
	site := censusSiteOfCaller()
	censusMu.Lock()
	censusViolations[censusViolationKey{kind: kind, op: op, class: value.Type().String(), detail: site}]++
	censusMu.Unlock()
}

// censusNotePublished records values the host handed the guest.
//
// Takes values ([]reflect.Value) which are the values.
func censusNotePublished(values []reflect.Value) {
	for _, value := range values {
		censusPublish(value)
	}
}

// censusNoteMadeMap clears publication history for a newly allocated map.
//
// Takes made (reflect.Value) which is the new map.
func censusNoteMadeMap(made reflect.Value) {
	if made.Kind() == reflect.Map && !made.IsNil() {
		censusPublished.delete(uintptr(made.UnsafePointer()))
	}
}

// censusNoteLocalContainerStore checks a store the licence checker granted as a store
// into a map only the running function can reach.
//
// Takes arena (*RegisterArena) which ran the store.
// Takes container (reflect.Value) which is the map stored into.
//
// Safe for concurrent use. censusMu guards violation tallies. The caller must own the
// arena.
func censusNoteLocalContainerStore(arena *RegisterArena, container reflect.Value) {
	if container.Kind() != reflect.Map || container.IsNil() {
		return
	}
	pointer := container.UnsafePointer()
	memoised := arena.licenceState.memo.peek(pointer, reflectValueABIType(container.Type()), 0)
	_, published := censusPublished.load(uintptr(pointer))
	if !memoised && !published {
		return
	}
	censusMu.Lock()
	defer censusMu.Unlock()
	if memoised {
		censusViolations[censusViolationKey{
			kind: "local-store-memoised-container", op: "SET_MAP", class: "heap-local", detail: container.Type().String(),
		}]++
	}
	if published {
		censusViolations[censusViolationKey{
			kind: "local-store-published-container", op: "SET_MAP", class: censusClassHeapPublished, detail: container.Type().String(),
		}]++
	}
}

// censusPublish adds the heap storage v reaches to the published set.
//
// Takes v (reflect.Value) which is the value.
func censusPublish(v reflect.Value) {
	censusPublishWalk(v, censusPublishDepth, new(censusPublishBudget))
}

// censusPublishWalk is censusPublish's recursive walk.
//
// Takes value (reflect.Value) which is the value.
// Takes depth (int) which is the remaining nesting allowance.
// Takes budget (*int) which is the remaining leaf allowance.
func censusPublishWalk(value reflect.Value, depth int, budget *int) {
	if !value.IsValid() || depth <= 0 || *budget <= 0 {
		return
	}
	*budget--
	if pointer, container := censusStorage(value); pointer != nil {
		censusPublished.store(uintptr(pointer), true)
		value = container
	}
	switch value.Kind() {
	case reflect.Interface:
		if !value.IsNil() {
			censusPublishWalk(value.Elem(), depth, budget)
		}
	case reflect.Pointer:
		if !value.IsNil() {
			censusPublishWalk(value.Elem(), depth-1, budget)
		}
	case reflect.Slice, reflect.Array:
		censusPublishElements(value, depth, budget)
	case reflect.Struct:
		censusPublishFields(value, depth, budget)
	default:
	}
}

// censusPublishElements is censusPublishWalk over the first elements of a slice or array.
//
// Takes value (reflect.Value) which is the slice or array.
// Takes depth (int) which is the remaining nesting allowance.
// Takes budget (*int) which is the remaining leaf allowance.
func censusPublishElements(value reflect.Value, depth int, budget *int) {
	if typeIsPointerFree(value.Type().Elem()) {
		return
	}
	for index := range min(value.Len(), censusPublishElementCap) {
		censusPublishWalk(value.Index(index), depth-1, budget)
	}
}

// censusPublishFields is censusPublishWalk over the fields of a struct.
//
// Takes value (reflect.Value) which is the struct.
// Takes depth (int) which is the remaining nesting allowance.
// Takes budget (*int) which is the remaining leaf allowance.
func censusPublishFields(value reflect.Value, depth int, budget *int) {
	if typeIsPointerFree(value.Type()) {
		return
	}
	for index := range value.NumField() {
		censusPublishWalk(structFieldValue(value, index), depth-1, budget)
	}
}

// StoreCensusViolations returns a snapshot of census violation tallies.
//
// Returns map[string]int64 which is a snapshot.
//
// Concurrency: Copies the shared violation tallies while holding censusMu.
func StoreCensusViolations() map[string]int64 {
	censusMu.Lock()
	defer censusMu.Unlock()
	snapshot := make(map[string]int64, len(censusViolations))
	for key, count := range censusViolations {
		snapshot[fmt.Sprintf("%s\t%s\t%s\t%s", key.kind, key.op, key.class, key.detail)] += count
	}
	return snapshot
}

// StoreCensusGates reports whether a violation kind fails the census (censusGatingKinds).
//
// Takes kind (string) which is the violation's kind, the first field of a
// StoreCensusViolations key.
//
// Returns bool which is true for a gating kind.
func StoreCensusGates(kind string) bool {
	return censusGatingKinds[kind]
}

// censusNoteBuiltinCopy checks one copy builtin after reflect.Copy and before its note.
//
// Takes arena (*RegisterArena) which ran the copy; may be nil.
// Takes destination (reflect.Value) which is the destination slice.
// Takes count (int) which is the number of elements copied.
//
// Concurrency: Updates violation tallies under censusMu; the caller must own the
// inspected arena and coordinate access to the destination.
func censusNoteBuiltinCopy(arena *RegisterArena, destination reflect.Value, count int) {
	if arena == nil || arena.countsHandOffs() || censusAbandons(arena) || count == 0 || typeIsPointerFree(destination.Type().Elem()) {
		return
	}
	if backing := destination.UnsafePointer(); arena.ownsAnyStorage(backing) || arena.krefContains(backing) {
		return
	}
	if references, _ := arena.referencesArenaStorage(destination.Slice(0, count), true, censusProbeDepth, new(censusProbeBudget), false); !references {
		return
	}
	censusMu.Lock()
	defer censusMu.Unlock()
	censusViolations[censusViolationKey{kind: "copy-builtin-unnoted", op: "COPY_GENERAL", class: "outside", detail: destination.Type().String()}]++
}

// censusNoteStringsOverMutBytes records a string-over-mutable-bytes violation for every
// string the frame's registers hold whose bytes lie in the arena's mutable byte slab
// (register_arena_licence_strings.go).
//
// Takes arena (*RegisterArena) which ran the instruction; nil records nothing.
// Takes registers (*Registers) which holds the frame's banks.
// Takes row (isa.OpSpec) which is the instruction.
//
// Concurrency: Updates violation tallies under censusMu; the caller must own the arena
// and register banks being inspected.
func censusNoteStringsOverMutBytes(arena *RegisterArena, registers *Registers, row isa.OpSpec) {
	if arena == nil || registers == nil || (arena.mutByteSlab == nil && len(arena.oldMutByteSlabs) == 0) {
		return
	}
	found := censusRegisterStringsOverMutBytes(arena, registers) + censusSliceStringsOverMutBytes(arena, registers)
	if found == 0 {
		return
	}
	censusMu.Lock()
	defer censusMu.Unlock()
	censusViolations[censusViolationKey{kind: "string-over-mutable-bytes", op: row.Name, class: "register", detail: "string"}] += int64(found)
}

// censusRegisterStringsOverMutBytes counts the strings in the string and general
// registers whose bytes lie in the arena's mutable byte slab.
//
// Takes arena (*RegisterArena) which ran the instruction.
// Takes registers (*Registers) which holds the frame's banks.
//
// Returns int which is the count.
func censusRegisterStringsOverMutBytes(arena *RegisterArena, registers *Registers) int {
	found := 0
	for _, s := range registers.Strings {
		if arena.stringOverMutBytes(s) {
			found++
		}
	}
	for _, v := range registers.General {
		if v.IsValid() && v.Kind() == reflect.String && arena.stringOverMutBytes(v.String()) {
			found++
		}
	}
	return found
}

// censusSliceStringsOverMutBytes counts, within censusStringsOverMutBudget elements, the
// strings in the typed []string registers whose bytes lie in the arena's mutable byte
// slab.
//
// Takes arena (*RegisterArena) which ran the instruction.
// Takes registers (*Registers) which holds the frame's banks.
//
// Returns int which is the count.
func censusSliceStringsOverMutBytes(arena *RegisterArena, registers *Registers) int {
	found := 0
	budget := censusStringsOverMutBudget
	for _, slice := range registers.slicesString {
		for _, text := range slice {
			if budget--; budget < 0 {
				return found
			}
			if arena.stringOverMutBytes(text) {
				found++
			}
		}
	}
	return found
}

// censusNoteServedStore checks a served store (register_arena_licence_callbacks.go)
// against the licence registry rather than the arena's own slab lists.
//
// Takes callback (*RegisterArena) which is the storing callback arena.
// Takes served (*RegisterArena) which is the arena callback serves.
// Takes destination (unsafe.Pointer) which is the destination.
// Takes text (string) which is the string stored.
//
// Concurrency: Updates violation tallies under censusMu; the caller must follow the
// served-store ownership protocol for both arenas.
func censusNoteServedStore(callback, served *RegisterArena, destination unsafe.Pointer, text string) {
	kind := ""
	value, valueFound := licenceSlabContaining(uintptr(unsafe.Pointer(unsafe.StringData(text))))
	target, targetFound := licenceSlabContaining(uintptr(destination))
	switch {
	case !valueFound || value.owner != served.slabOwner:
		kind = "served-store-foreign-value"
	case !targetFound || target.owner != served.slabOwner || target.class != licenceSlabData:
		kind = "served-store-foreign-target"
	case served.licenceCoveredAt(destination):
		kind = "served-store-covered-target"
	default:
		return
	}
	op := censusBoundary
	if state, ok := censusArenaStates.load(callback); ok && state != nil && state.inInstruction {
		op = state.row.Name
	}
	censusMu.Lock()
	censusViolations[censusViolationKey{kind: kind, op: op, class: "string", detail: censusSiteOfCaller()}]++
	censusMu.Unlock()
}

// krefMayBeCovered checks KRef revocation coverage for instrumentation without changing
// the licence.
//
// Takes pointer (unsafe.Pointer) which is an address in one of the arena's KRef chunks.
//
// Returns bool which is true when the chunk is covered.
//
// Concurrency: Reads pending foreign revocations under licenceForeignRevokedMu; the
// caller must own the arena's remaining licence state.
func (a *RegisterArena) krefMayBeCovered(pointer unsafe.Pointer) bool {
	chunk, found := a.krefFind(uintptr(pointer))
	if !found {
		return false
	}
	state := &a.licenceState
	if a.licenceRevokedBase(chunk.base) {
		return true
	}
	if !state.blindMarked {
		return true
	}
	if all, _ := a.blindTotals(); all != state.blindMark {
		return true
	}
	if licenceForeignRevokedCount.Load() == 0 {
		return false
	}
	if !state.foreignLocked {
		licenceForeignRevokedMu.Lock()
		defer licenceForeignRevokedMu.Unlock()
	}
	_, pending := licenceForeignRevoked.lookup(licenceForeignKey{start: chunk.base, owner: a.slabOwner})
	return pending
}
