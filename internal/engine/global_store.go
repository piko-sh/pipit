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
	"strings"
	"sync"
	"sync/atomic"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab/typemodel"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// externalMethodsInitialCapacity is the starting size for the lazy-allocated
	// externalMethods map. Sized for typical small method tables; the map grows naturally
	// past this.
	externalMethodsInitialCapacity = 8
)

// GlobalStore holds package-level variables shared across function invocations within a
// package and across goroutines. Access is synchronised, with a lock-free fast path when
// no goroutine has been launched.
type GlobalStore struct {
	// interpretedChannels holds the addresses of channels made by interpreted code, for the
	// deadlock check in deadlock.go; guarded by channelsMu.
	interpretedChannels map[uintptr]struct{}

	// goroutinePanic captures the first unrecovered panic from any spawned goroutine running
	// on this store. Parent VMs check this in their run loop and surface it as an evaluation
	// error so the goroutine panic propagates instead of being silently swallowed.
	goroutinePanic atomic.Pointer[GoroutinePanicInfo]

	// goroutinePanicSignal is closed exactly once by the panic-CAS winner. Shared-mode
	// blocking operations select on it so a parked goroutine wakes when a sibling panics
	// instead of deadlocking.
	goroutinePanicSignal chan struct{}

	// externalMethods registers methods from packages compiled in separate
	// Service.CompileProgram calls so cross-package interface-adapter lookups can locate
	// them. Keyed by "TypeName.MethodName", guarded by externalMethodsMu.
	externalMethods map[string]externalMethodEntry

	// userNamedInterfaces records source-level identity for user-declared named interface
	// types, keyed by qualified name ("pkg.Iface"). Guarded by externalMethodsMu.
	userNamedInterfaces map[string]*typemodel.Type

	// callStack hands out the synthesised program counters runtime.Caller reports.
	callStack *callStackRegistry

	// uints holds all package-level uint64 global variables.
	uints []uint64

	// general holds all package-level reflect.Value global variables.
	general []reflect.Value

	// bools holds all package-level bool global variables.
	bools []bool

	// Strings holds all package-level string global variables.
	Strings []string

	// complexes holds all package-level complex128 global variables.
	complexes []complex128

	// floats holds all package-level float64 global variables.
	floats []float64

	// ints holds all package-level int64 global variables.
	ints []int64

	// mu guards concurrent access to all fields when shared is true.
	mu sync.RWMutex

	// externalMethodsMu guards externalMethods and userNamedInterfaces against concurrent
	// registration and lookup.
	externalMethodsMu sync.RWMutex

	// interpreterLock serialises interpreted bytecode execution across every VM that shares
	// this store, active only in safe mode. Ensures at most one goroutine runs bytecode at a
	// time so multi-word writes to shared state cannot tear.
	interpreterLock sync.Mutex

	// channelsMu guards interpretedChannels.
	channelsMu sync.Mutex

	// shared toggles the locked vs fast-path mode for this store. False until any VM
	// launches its first goroutine, toggled true once and never reset.
	shared atomic.Bool

	// dispatchDepth counts active pipit recover-boundary frames. MakeFunc closure wrappers
	// consult it before re-panicking and swallow the panic when zero to protect the host
	// process.
	dispatchDepth atomic.Int32

	// adapterDepth counts the registered interface adapters currently running an interpreted
	// method body on this store, so re-entrant native code cannot recurse through adapters
	// without bound.
	adapterDepth atomic.Int32

	// nextGoroutineID numbers goroutine VMs for stack dumps; the root VM is 1.
	nextGoroutineID atomic.Uint64
}

// NewGlobalStore creates an empty global variable store.
//
// Returns a newly allocated GlobalStore with no variables.
func NewGlobalStore() *GlobalStore {
	return &GlobalStore{
		interpretedChannels:  nil,
		channelsMu:           sync.Mutex{},
		goroutinePanicSignal: make(chan struct{}),
		goroutinePanic:       atomic.Pointer[GoroutinePanicInfo]{},
		adapterDepth:         atomic.Int32{},
		externalMethods:      nil,
		userNamedInterfaces:  nil,
		uints:                nil,
		general:              nil,
		bools:                nil,
		Strings:              nil,
		complexes:            nil,
		floats:               nil,
		ints:                 nil,
		mu:                   sync.RWMutex{},
		externalMethodsMu:    sync.RWMutex{},
		interpreterLock:      sync.Mutex{},
		shared:               atomic.Bool{},
		dispatchDepth:        atomic.Int32{},
		nextGoroutineID:      atomic.Uint64{},
		callStack:            newCallStackRegistry(),
	}
}

// Lengths returns the current bank lengths as a SlotAllocation.
//
// Used at Service.PackageModule entry and exit to compute how many slots a single compile
// reserved (the delta).
//
// Returns SlotAllocation which holds the per-kind slot counts.
//
// Concurrency: safe to call from any goroutine; takes the shared lock when
// GlobalStore.shared is set.
func (g *GlobalStore) Lengths() program.SlotAllocation {
	if g.shared.Load() {
		g.mu.RLock()
		defer g.mu.RUnlock()
	}
	return program.SlotAllocation{
		safeconv.MustIntToUint16(len(g.ints)),
		safeconv.MustIntToUint16(len(g.floats)),
		safeconv.MustIntToUint16(len(g.Strings)),
		safeconv.MustIntToUint16(len(g.general)),
		safeconv.MustIntToUint16(len(g.bools)),
		safeconv.MustIntToUint16(len(g.uints)),
		safeconv.MustIntToUint16(len(g.complexes)),
	}
}

// ReserveSlots grows each bank and returns the pre-grow base offsets.
//
// Grows each global-store bank by the corresponding count in alloc and returns the
// per-kind base offsets the reservation occupied. Used at Service.LoadModule time to
// place a bundle's slots in a non-overlapping region of the target Service's GlobalStore;
// the returned bases are stored on every CompiledFunction in the loaded bundle so the
// VM's global-access handlers shift the encoded operand by the right amount.
//
// Takes alloc (SlotAllocation) which is the per-kind slot count to add.
//
// Returns SlotAllocation which is the pre-grow lengths (base indices).
//
// Concurrency: takes g.mu in write mode when GlobalStore.shared is set; otherwise mutates
// banks directly on the single-threaded fast path.
func (g *GlobalStore) ReserveSlots(alloc program.SlotAllocation) program.SlotAllocation {
	if g.shared.Load() {
		g.mu.Lock()
		defer g.mu.Unlock()
	}
	bases := program.SlotAllocation{
		safeconv.MustIntToUint16(len(g.ints)),
		safeconv.MustIntToUint16(len(g.floats)),
		safeconv.MustIntToUint16(len(g.Strings)),
		safeconv.MustIntToUint16(len(g.general)),
		safeconv.MustIntToUint16(len(g.bools)),
		safeconv.MustIntToUint16(len(g.uints)),
		safeconv.MustIntToUint16(len(g.complexes)),
	}
	if n := int(alloc[isa.RegisterInt]); n > 0 {
		g.ints = append(g.ints, make([]int64, n)...)
	}
	if n := int(alloc[isa.RegisterFloat]); n > 0 {
		g.floats = append(g.floats, make([]float64, n)...)
	}
	if n := int(alloc[isa.RegisterString]); n > 0 {
		g.Strings = append(g.Strings, make([]string, n)...)
	}
	if n := int(alloc[isa.RegisterGeneral]); n > 0 {
		g.general = append(g.general, make([]reflect.Value, n)...)
	}
	if n := int(alloc[isa.RegisterBool]); n > 0 {
		g.bools = append(g.bools, make([]bool, n)...)
	}
	if n := int(alloc[isa.RegisterUint]); n > 0 {
		g.uints = append(g.uints, make([]uint64, n)...)
	}
	if n := int(alloc[isa.RegisterComplex]); n > 0 {
		g.complexes = append(g.complexes, make([]complex128, n)...)
	}
	return bases
}

// AllocInt allocates a new int64 global variable and returns its index.
//
// Takes initial (int64) which is the starting value for the variable.
//
// Returns the index of the newly allocated variable.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) AllocInt(initial int64) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	index := len(g.ints)
	g.ints = append(g.ints, initial)
	return index
}

// AllocFloat allocates a new float64 global variable and returns its index.
//
// Takes initial (float64) which is the starting value for the variable.
//
// Returns the index of the newly allocated variable.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) AllocFloat(initial float64) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	index := len(g.floats)
	g.floats = append(g.floats, initial)
	return index
}

// AllocString allocates a new string global variable and returns its index.
//
// Takes initial (string) which is the starting value for the variable.
//
// Returns the index of the newly allocated variable.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) AllocString(initial string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	index := len(g.Strings)
	g.Strings = append(g.Strings, initial)
	return index
}

// AllocGeneral allocates a new reflect.Value global variable and returns its index.
//
// Takes initial (reflect.Value) which is the starting value for the variable.
//
// Returns the index of the newly allocated variable.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) AllocGeneral(initial reflect.Value) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	index := len(g.general)
	g.general = append(g.general, initial)
	return index
}

// GetInt reads an int64 global variable.
//
// Takes index (int) which is the variable to read.
//
// Returns the current value of the variable.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) GetInt(index int) int64 {
	if !g.shared.Load() {
		return g.ints[index]
	}
	g.mu.RLock()
	v := g.ints[index]
	g.mu.RUnlock()
	return v
}

// SetInt writes an int64 global variable.
//
// Takes index (int) which is the variable to write.
// Takes v (int64) which is the new value.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) SetInt(index int, v int64) {
	if !g.shared.Load() {
		g.ints[index] = v
		return
	}
	g.mu.Lock()
	g.ints[index] = v
	g.mu.Unlock()
}

// GetFloat reads a float64 global variable.
//
// Takes index (int) which is the variable to read.
//
// Returns the current value of the variable.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) GetFloat(index int) float64 {
	if !g.shared.Load() {
		return g.floats[index]
	}
	g.mu.RLock()
	v := g.floats[index]
	g.mu.RUnlock()
	return v
}

// SetFloat writes a float64 global variable.
//
// Takes index (int) which is the variable to write.
// Takes v (float64) which is the new value.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) SetFloat(index int, v float64) {
	if !g.shared.Load() {
		g.floats[index] = v
		return
	}
	g.mu.Lock()
	g.floats[index] = v
	g.mu.Unlock()
}

// GetString reads a string global variable.
//
// Takes index (int) which is the variable to read.
//
// Returns the current value of the variable.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) GetString(index int) string {
	if !g.shared.Load() {
		return g.Strings[index]
	}
	g.mu.RLock()
	v := g.Strings[index]
	g.mu.RUnlock()
	return v
}

// SetString writes a string global variable.
//
// Takes index (int) which is the variable to write.
// Takes v (string) which is the new value.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) SetString(index int, v string) {
	if !g.shared.Load() {
		g.Strings[index] = v
		return
	}
	g.mu.Lock()
	g.Strings[index] = v
	g.mu.Unlock()
}

// GetGeneral reads a reflect.Value global variable.
//
// Takes index (int) which is the variable to read.
//
// Returns the current value of the variable.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) GetGeneral(index int) reflect.Value {
	if !g.shared.Load() {
		return g.general[index]
	}
	g.mu.RLock()
	v := g.general[index]
	g.mu.RUnlock()
	return v
}

// SetGeneral writes a reflect.Value global variable.
//
// Takes index (int) which is the variable to write.
// Takes v (reflect.Value) which is the new value.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) SetGeneral(index int, v reflect.Value) {
	if !g.shared.Load() {
		g.general[index] = v
		return
	}
	g.mu.Lock()
	g.general[index] = v
	g.mu.Unlock()
}

// AllocBool allocates a new bool global variable and returns its index.
//
// Takes initial (bool) which is the starting value for the variable.
//
// Returns the index of the newly allocated variable.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) AllocBool(initial bool) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	index := len(g.bools)
	g.bools = append(g.bools, initial)
	return index
}

// GetBool reads a bool global variable.
//
// Takes index (int) which is the variable to read.
//
// Returns the current value of the variable.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) GetBool(index int) bool {
	if !g.shared.Load() {
		return g.bools[index]
	}
	g.mu.RLock()
	v := g.bools[index]
	g.mu.RUnlock()
	return v
}

// SetBool writes a bool global variable.
//
// Takes index (int) which is the variable to write.
// Takes v (bool) which is the new value.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) SetBool(index int, v bool) {
	if !g.shared.Load() {
		g.bools[index] = v
		return
	}
	g.mu.Lock()
	g.bools[index] = v
	g.mu.Unlock()
}

// AllocUint allocates a new uint64 global variable and returns its index.
//
// Takes initial (uint64) which is the starting value for the variable.
//
// Returns the index of the newly allocated variable.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) AllocUint(initial uint64) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	index := len(g.uints)
	g.uints = append(g.uints, initial)
	return index
}

// GetUint reads a uint64 global variable.
//
// Takes index (int) which is the variable to read.
//
// Returns the current value of the variable.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) GetUint(index int) uint64 {
	if !g.shared.Load() {
		return g.uints[index]
	}
	g.mu.RLock()
	v := g.uints[index]
	g.mu.RUnlock()
	return v
}

// SetUint writes a uint64 global variable.
//
// Takes index (int) which is the variable to write.
// Takes v (uint64) which is the new value.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) SetUint(index int, v uint64) {
	if !g.shared.Load() {
		g.uints[index] = v
		return
	}
	g.mu.Lock()
	g.uints[index] = v
	g.mu.Unlock()
}

// AllocComplex allocates a new complex128 global variable and returns its index.
//
// Takes initial (complex128) which is the starting value for the variable.
//
// Returns the index of the newly allocated variable.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) AllocComplex(initial complex128) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	index := len(g.complexes)
	g.complexes = append(g.complexes, initial)
	return index
}

// RegisterExternalMethod publishes a method entry for cross-package use.
//
// Publishes a single (typeName, methodName) -> (rootFunction, methodIndex) entry so
// adapters in OTHER CompileProgram batches can dispatch to the method body. Called from
// Service.bridgePackageExports for each entry in the just-compiled package's methodTable.
// Re-registration overrides the previous owner, matching Go's last-write-wins semantics
// if two packages declare a same-named type with the same method (rare outside test
// fixtures).
//
// Takes key (string) which is "TypeName.MethodName".
// Takes rootFunction (*CompiledFunction) which owns the method body.
// Takes methodIndex (uint16) which is the function's slot in rootFunction.functions.
//
// Concurrency: takes g.externalMethodsMu in write mode.
func (g *GlobalStore) RegisterExternalMethod(key string, rootFunction *program.CompiledFunction, methodIndex uint16) {
	if g == nil || rootFunction == nil || key == "" {
		return
	}
	g.externalMethodsMu.Lock()
	defer g.externalMethodsMu.Unlock()
	if g.externalMethods == nil {
		g.externalMethods = make(map[string]externalMethodEntry, externalMethodsInitialCapacity)
	}
	g.externalMethods[key] = externalMethodEntry{
		rootFunction: rootFunction,
		methodIndex:  methodIndex,
	}
}

// RegisterUserNamedInterface records the symtab.Type identity for a named interface
// declared in user code.
//
// Keyed by qualifiedName ("pkg.Iface"). Called from compiler_types.convertNamedType for
// every named type whose underlying is *types.Interface and which is NOT in
// wellKnownNamedInterfaceRegistry (those have canonical native reflect.Types and do not
// need the side-channel). Re-registration overrides; last-write-wins matches the
// externalMethods bridge.
//
// Takes qualifiedName (string) which is "pkg.Iface".
// Takes pipit (*symtab.Type) which is the populated symtab.Type identity.
//
// Concurrency: takes g.externalMethodsMu in write mode.
func (g *GlobalStore) RegisterUserNamedInterface(qualifiedName string, pipit *typemodel.Type) {
	if g == nil || qualifiedName == "" || pipit == nil {
		return
	}
	g.externalMethodsMu.Lock()
	defer g.externalMethodsMu.Unlock()
	if g.userNamedInterfaces == nil {
		g.userNamedInterfaces = make(map[string]*typemodel.Type, externalMethodsInitialCapacity)
	}
	g.userNamedInterfaces[qualifiedName] = pipit
}

// SnapshotVarAs reads a global slot and wraps it in a reflect.Value, converting numeric
// banks back to the caller's declared Go type. Returns a zero value and false when the
// slot is out of range.
//
// Takes slot (GlobalVariableInfo) which addresses the value.
// Takes targetType (reflect.Type) which is the var's declared Go type.
//
// Returns reflect.Value which is the wrapped value on success.
// Returns bool which reports whether the snapshot succeeded.
func (g *GlobalStore) SnapshotVarAs(slot program.GlobalVariableInfo, targetType reflect.Type) (reflect.Value, bool) {
	if targetType == nil {
		return reflect.Value{}, false
	}
	if slot.IsIndirect {
		return g.snapshotIndirectVarAs(slot, targetType)
	}
	if !g.indexInBank(slot) {
		return reflect.Value{}, false
	}
	switch slot.Kind {
	case isa.RegisterInt:
		return reflect.ValueOf(g.GetInt(slot.Index)).Convert(targetType), true
	case isa.RegisterFloat:
		return reflect.ValueOf(g.GetFloat(slot.Index)).Convert(targetType), true
	case isa.RegisterString:
		return reflect.ValueOf(g.GetString(slot.Index)).Convert(targetType), true
	case isa.RegisterGeneral:
		return snapshotGeneralSlotAs(g.GetGeneral(slot.Index), targetType), true
	case isa.RegisterBool:
		return reflect.ValueOf(g.GetBool(slot.Index)), true
	case isa.RegisterUint:
		return reflect.ValueOf(g.GetUint(slot.Index)).Convert(targetType), true
	case isa.RegisterComplex:
		return reflect.ValueOf(g.getComplex(slot.Index)).Convert(targetType), true
	default:
	}
	return reflect.Value{}, false
}

// SnapshotVar reads a package-level variable as the raw value its bank holds, for the
// debugger's globals view. An indirect slot is followed to its pointee.
//
// Takes slot (program.GlobalVariableInfo) which addresses the value.
//
// Returns reflect.Value which is the value.
// Returns bool which is false when the slot is out of range or an indirect cell is empty.
func (g *GlobalStore) SnapshotVar(slot program.GlobalVariableInfo) (reflect.Value, bool) {
	if slot.IsIndirect {
		cellSlot := program.GlobalVariableInfo{Index: slot.Index, Kind: isa.RegisterGeneral, IsIndirect: false}
		if !g.indexInBank(cellSlot) {
			return reflect.Value{}, false
		}
		cell := g.GetGeneral(slot.Index)
		if !cell.IsValid() || cell.Kind() != reflect.Pointer || cell.IsNil() {
			return reflect.Value{}, false
		}
		return cell.Elem(), true
	}
	if !g.indexInBank(slot) {
		return reflect.Value{}, false
	}
	switch slot.Kind {
	case isa.RegisterInt:
		return reflect.ValueOf(g.GetInt(slot.Index)), true
	case isa.RegisterFloat:
		return reflect.ValueOf(g.GetFloat(slot.Index)), true
	case isa.RegisterString:
		return reflect.ValueOf(g.GetString(slot.Index)), true
	case isa.RegisterGeneral:
		return g.GetGeneral(slot.Index), true
	case isa.RegisterBool:
		return reflect.ValueOf(g.GetBool(slot.Index)), true
	case isa.RegisterUint:
		return reflect.ValueOf(g.GetUint(slot.Index)), true
	case isa.RegisterComplex:
		return reflect.ValueOf(g.getComplex(slot.Index)), true
	default:
		return reflect.Value{}, false
	}
}

// Reset clears all global variables and returns the store to single-threaded fast-path
// mode. Callers must ensure no concurrent users remain when Reset is called.
func (g *GlobalStore) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ints = g.ints[:0]
	g.floats = g.floats[:0]
	g.Strings = g.Strings[:0]
	g.general = g.general[:0]
	g.bools = g.bools[:0]
	g.uints = g.uints[:0]
	g.complexes = g.complexes[:0]
	g.shared.Store(false)
	g.goroutinePanic.Store(nil)
	g.goroutinePanicSignal = make(chan struct{})
}

// materialise replaces globals that the callbacks identify as arena-backed with
// heap-backed copies so the store never holds dangling pointers into a reset arena.
//
// Takes ownsString (func(string) bool) which reports whether a string's bytes live in an
// arena slab.
// Takes materialise (func(reflect.Value) reflect.Value) which returns a heap copy of an
// arena-backed value, or the value itself when it is already heap-backed.
//
// Concurrency: holds g.mu for the entire scan.
func (g *GlobalStore) materialise(ownsString func(string) bool, materialise func(reflect.Value) reflect.Value) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, s := range g.Strings {
		if ownsString(s) {
			g.Strings[i] = strings.Clone(s)
		}
	}
	for i, v := range g.general {
		if !v.IsValid() {
			continue
		}
		materialised := materialise(v)
		if materialised != v {
			g.general[i] = materialised
		}
	}
}

// markShared transitions the store into shared mode.
func (g *GlobalStore) markShared() {
	g.shared.Store(true)
}

// getComplex reads a complex128 global variable.
//
// Takes index (int) which is the variable to read.
//
// Returns the current value of the variable.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) getComplex(index int) complex128 {
	if !g.shared.Load() {
		return g.complexes[index]
	}
	g.mu.RLock()
	v := g.complexes[index]
	g.mu.RUnlock()
	return v
}

// setComplex writes a complex128 global variable.
//
// Takes index (int) which is the variable to write.
// Takes v (complex128) which is the new value.
//
// Safe for concurrent use by multiple goroutines.
func (g *GlobalStore) setComplex(index int, v complex128) {
	if !g.shared.Load() {
		g.complexes[index] = v
		return
	}
	g.mu.Lock()
	g.complexes[index] = v
	g.mu.Unlock()
}

// lookupExternalMethod returns the cross-package entry matching key.
//
// Called by adapter builders after a local vm.RootFunction.methodTable miss.
//
// Takes key (string) which is "TypeName.MethodName".
//
// Returns externalMethodEntry which is the registered entry on hit.
// Returns bool which reports whether a matching entry was found.
//
// Concurrency: takes g.externalMethodsMu in read mode.
func (g *GlobalStore) lookupExternalMethod(key string) (externalMethodEntry, bool) {
	if g == nil {
		return externalMethodEntry{}, false
	}
	g.externalMethodsMu.RLock()
	defer g.externalMethodsMu.RUnlock()
	entry, ok := g.externalMethods[key]
	return entry, ok
}

// lookupUserNamedInterface returns the symtab.Type identity for a name.
//
// Called by the reflect.TypeOf intercept to decide whether to wrap a *interface{} result.
//
// Takes qualifiedName (string) which is "pkg.Iface" or just "Iface".
//
// Returns *symtab.Type which is the registered identity on hit.
// Returns bool which reports whether an entry was found.
//
// Concurrency: takes g.externalMethodsMu in read mode.
func (g *GlobalStore) lookupUserNamedInterface(qualifiedName string) (*typemodel.Type, bool) {
	if g == nil || qualifiedName == "" {
		return nil, false
	}
	g.externalMethodsMu.RLock()
	defer g.externalMethodsMu.RUnlock()
	pipit, ok := g.userNamedInterfaces[qualifiedName]
	return pipit, ok
}

// indexInBank reports whether slot.Index is in range for slot.Kind.
//
// Read-only. Callers that need the value follow up with the bank-specific getter so the
// existing goroutine-safety fast path is reused.
//
// Takes slot (GlobalVariableInfo) which addresses the bank and index.
//
// Returns bool which reports whether the index is in range.
//
// Concurrency: takes g.mu in read mode while inspecting bank lengths.
func (g *GlobalStore) indexInBank(slot program.GlobalVariableInfo) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	switch slot.Kind {
	case isa.RegisterInt:
		return slot.Index >= 0 && slot.Index < len(g.ints)
	case isa.RegisterFloat:
		return slot.Index >= 0 && slot.Index < len(g.floats)
	case isa.RegisterString:
		return slot.Index >= 0 && slot.Index < len(g.Strings)
	case isa.RegisterGeneral:
		return slot.Index >= 0 && slot.Index < len(g.general)
	case isa.RegisterBool:
		return slot.Index >= 0 && slot.Index < len(g.bools)
	case isa.RegisterUint:
		return slot.Index >= 0 && slot.Index < len(g.uints)
	case isa.RegisterComplex:
		return slot.Index >= 0 && slot.Index < len(g.complexes)
	default:
	}
	return false
}

// recordGoroutinePanic stores the first panic captured from a spawned goroutine and wakes
// any sibling parked on a blocking channel or select operation. The compare-and-swap
// guarantees a single winner, so goroutinePanicSignal is closed exactly once.
//
// Takes value (any) which is the panic value as it was thrown.
// Takes stack (string) which is the captured stack trace, or empty when unavailable.
func (g *GlobalStore) recordGoroutinePanic(value any, stack string) {
	if g.goroutinePanic.CompareAndSwap(nil, &GoroutinePanicInfo{value: value, stack: stack}) {
		close(g.goroutinePanicSignal)
	}
}

// goroutinePanicWakeChan returns the channel a blocking operation should select on to
// observe a sibling goroutine's panic, or nil when the store has never launched a
// goroutine (so no panic can occur and the caller keeps its lock-free fast path).
//
// Returns the panic-signal channel when the store is shared, nil otherwise.
func (g *GlobalStore) goroutinePanicWakeChan() <-chan struct{} {
	if !g.shared.Load() {
		return nil
	}
	return g.goroutinePanicSignal
}

// snapshotIndirectVarAs reads an indirect package variable through the pointer cell its
// general-bank slot holds (see program.GlobalVariableInfo.IsIndirect).
//
// Takes slot (program.GlobalVariableInfo) which names the cell's general slot.
// Takes targetType (reflect.Type) which is the variable's declared type.
//
// Returns the pointee converted to targetType and true, or false when the slot is out of
// range or the cell is unset.
func (g *GlobalStore) snapshotIndirectVarAs(slot program.GlobalVariableInfo, targetType reflect.Type) (reflect.Value, bool) {
	cellSlot := program.GlobalVariableInfo{Index: slot.Index, Kind: isa.RegisterGeneral, IsIndirect: false}
	if !g.indexInBank(cellSlot) {
		return reflect.Value{}, false
	}
	cell := g.GetGeneral(slot.Index)
	if !cell.IsValid() || cell.Kind() != reflect.Pointer || cell.IsNil() {
		return reflect.Value{}, false
	}
	pointee := cell.Elem()
	if pointee.Type() != targetType && pointee.Type().ConvertibleTo(targetType) {
		return pointee.Convert(targetType), true
	}
	return snapshotGeneralSlotAs(pointee, targetType), true
}

// GoroutinePanicInfo describes a panic captured from a spawned goroutine that the parent
// VM has not yet observed.
type GoroutinePanicInfo struct {
	// value is the panic value as it was thrown.
	value any

	// stack is the captured stack trace from the panicking goroutine.
	stack string
}

// Value returns the panic value as it was thrown.
//
// Returns any which is the panic value.
func (g *GoroutinePanicInfo) Value() any { return g.value }

// externalMethodEntry locates a method body across CompileProgram batches.
type externalMethodEntry struct {
	// rootFunction is the source package's compiled root, used by boundMethodVM to pick the
	// right functions slice.
	rootFunction *program.CompiledFunction

	// methodIndex is the method's position in rootFunction.functions.
	methodIndex uint16
}

// snapshotGeneralSlotAs coerces a general-bank value into targetType.
//
// Allocated-but-never-written slots return a typed zero so downstream reflect operations
// have a valid Value to work with.
//
// Takes value (reflect.Value) which is the stored general-bank value.
// Takes targetType (reflect.Type) which is the destination Go type.
//
// Returns reflect.Value which is the coerced value.
func snapshotGeneralSlotAs(value reflect.Value, targetType reflect.Type) reflect.Value {
	if !value.IsValid() {
		return reflect.Zero(targetType)
	}
	if value.Type() != targetType && value.Type().ConvertibleTo(targetType) {
		return value.Convert(targetType)
	}
	return value
}
