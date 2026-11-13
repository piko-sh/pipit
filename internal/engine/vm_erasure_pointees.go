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
	"weak"

	"pipit.sh/pipit/internal/symtab"
)

const (
	// erasedPointerRecordLimit bounds the erased pointer records kept. Reaching it drops
	// them all, after which a pointer stored before is no longer retyped (fail closed) until
	// the program stores it again.
	erasedPointerRecordLimit = 1 << 20

	// erasurePointeeCachePruneMinimum is the fewest cached registries the cache holds before
	// it drops those of collected registries.
	erasurePointeeCachePruneMinimum = 64
)

var (
	// erasurePointeeSets shares each symbol registry's erasure-boundary pointee set among
	// the VMs that use the registry.
	erasurePointeeSets = &erasurePointeeCache{
		mu:      sync.Mutex{},
		entries: nil,
		pruneAt: erasurePointeeCachePruneMinimum,
	}

	// erasedPointers records the type each pointer a program passed across an erasure
	// boundary had.
	erasedPointers = &erasedPointerRegistry{
		mu:      sync.Mutex{},
		records: nil,
	}
)

// erasurePointeeEntry is one registry's cached pointee set.
type erasurePointeeEntry struct {
	// pointees is the set, which is never changed once built.
	pointees map[reflect.Type]struct{}

	// packages is the number of packages the registry held when the set was built. A
	// registry that gained packages since is scanned again.
	packages int
}

// erasurePointeeCache maps a symbol registry to its erasure-boundary pointee set.
//
// Scanning every symbol of every package is the cost of building a set, which each VM
// used to pay on its first pointer conversion between different types, including every
// guest goroutine's VM and every callback VM. The set depends only on the registry, so it
// is built once per registry, and again only when packages were registered since.
type erasurePointeeCache struct {
	// entries maps a registry, held weakly, to its set. The mu guards it.
	entries map[weak.Pointer[symtab.SymbolRegistry]]erasurePointeeEntry

	// mu guards entries and pruneAt.
	mu sync.Mutex

	// pruneAt is the entry count at which entries of collected registries are dropped.
	pruneAt int
}

// erasedPointerKey identifies one pointer a program passed across an erasure boundary.
type erasedPointerKey struct {
	// element is the pointer's element type as the program held it.
	element reflect.Type

	// address is the pointer's address.
	address uintptr
}

// erasedPointerRegistry records the element type of each pointer a program passed to a
// native-backed generic type, which reinterpretPointerArgument retypes at the erased
// type.
type erasedPointerRegistry struct {
	// records holds the stored pointers. The mu guards it.
	records map[erasedPointerKey]struct{}

	// mu guards records.
	mu sync.Mutex
}

// erasurePointees returns the erasure-boundary pointee set of vm's symbol registry,
// building it on first use and sharing it with every VM on the registry.
//
// Returns map[reflect.Type]struct{} which must not be changed.
func (vm *VM) erasurePointees() map[reflect.Type]struct{} {
	if vm.nativeBackedErasurePointees == nil {
		vm.nativeBackedErasurePointees = erasurePointeeSets.lookup(vm)
	}
	return vm.nativeBackedErasurePointees
}

// lookup returns the cached set of vm's registry, building it when absent or stale.
//
// Takes vm (*VM) which names the registry and builds the set.
//
// Returns map[reflect.Type]struct{} which is the set.
//
// Safe for concurrent use. The set is built outside the lock.
func (c *erasurePointeeCache) lookup(vm *VM) map[reflect.Type]struct{} {
	if vm.symbols == nil {
		return vm.buildNativeBackedErasurePointees()
	}
	key := weak.Make(vm.symbols)
	packages := len(vm.symbols.AllPackages())
	c.mu.Lock()
	entry, ok := c.entries[key]
	c.mu.Unlock()
	if ok && entry.packages == packages {
		return entry.pointees
	}
	pointees := vm.buildNativeBackedErasurePointees()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[weak.Pointer[symtab.SymbolRegistry]]erasurePointeeEntry)
	}
	c.entries[key] = erasurePointeeEntry{pointees: pointees, packages: packages}
	if len(c.entries) >= c.pruneAt {
		for registry := range c.entries {
			if registry.Value() == nil {
				delete(c.entries, registry)
			}
		}
		c.pruneAt = max(erasurePointeeCachePruneMinimum, 2*len(c.entries))
	}
	return pointees
}

// noteErasedPointer records a pointer a program passes to a native-backed generic type
// before it is reinterpreted at the erased type.
//
// Takes pointer (reflect.Value) which is the program's pointer at its own type.
//
// Safe for concurrent use.
func noteErasedPointer(pointer reflect.Value) {
	if !pointer.IsValid() || pointer.Kind() != reflect.Pointer || pointer.IsNil() {
		return
	}
	key := erasedPointerKey{element: pointer.Type().Elem(), address: pointer.Pointer()}
	erasedPointers.mu.Lock()
	defer erasedPointers.mu.Unlock()
	if erasedPointers.records == nil || len(erasedPointers.records) >= erasedPointerRecordLimit {
		erasedPointers.records = make(map[erasedPointerKey]struct{})
	}
	erasedPointers.records[key] = struct{}{}
}

// erasedPointerHeld reports whether a pointer of element type element was stored at
// address.
//
// Takes address (uintptr) which is the pointer's address.
// Takes element (reflect.Type) which is the element type asked for.
//
// Returns bool which is true when such a pointer was recorded.
//
// Safe for concurrent use.
func erasedPointerHeld(address uintptr, element reflect.Type) bool {
	erasedPointers.mu.Lock()
	defer erasedPointers.mu.Unlock()
	_, held := erasedPointers.records[erasedPointerKey{element: element, address: address}]
	return held
}

// retypeErasedPointer gives a pointer a native-backed generic type returned, whose
// element is the erased type argument (struct{} for sync/atomic.Pointer), the pointer
// type the program instantiated it at, keeping its address. The native stored the pointer
// the program gave it, reinterpreted at the erased type by reinterpretPointerArgument.
//
// The pointer is retyped only when the program stored a pointer of the instantiated type
// at that address (noteErasedPointer), or when the instantiated element has no size, so
// that a converted type taken from the type table cannot reinterpret the memory at
// another layout. A zero-size element reads nothing.
//
// Takes vm (*VM) which provides the erasure boundary.
// Takes source (reflect.Value) which is the returned pointer.
// Takes reflectType (reflect.Type) which is the instantiated pointer type.
//
// Returns the retyped pointer and true, or false when source is no erased pointer or no
// pointer of reflectType was stored at its address.
func retypeErasedPointer(vm *VM, source reflect.Value, reflectType reflect.Type) (reflect.Value, bool) {
	if reflectType.Kind() != reflect.Pointer || source.Kind() != reflect.Pointer || source.Type() == reflectType {
		return reflect.Value{}, false
	}
	if _, erased := vm.erasurePointees()[source.Type().Elem()]; !erased {
		return reflect.Value{}, false
	}
	if source.IsNil() {
		return reflect.Zero(reflectType), true
	}
	element := reflectType.Elem()
	if element.Size() != 0 && !erasedPointerHeld(source.Pointer(), element) {
		return reflect.Value{}, false
	}
	return reflect.NewAt(element, source.UnsafePointer()), true
}
