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
)

const (
	// finaliserAllocationsPruneMinimum is the fewest records the registry holds before it
	// drops those of collected objects.
	finaliserAllocationsPruneMinimum = 1024
)

const (
	// finaliserAllocationUnknown is a pointer the interpreter did not record as the start of
	// an allocation.
	finaliserAllocationUnknown finaliserAllocationState = iota

	// finaliserAllocationFree is a recorded allocation with no finaliser set.
	finaliserAllocationFree

	// finaliserAllocationSet is a recorded allocation whose finaliser is set.
	finaliserAllocationSet
)

var (
	// finaliserAllocations records the Go allocations the interpreter made whole for
	// programs that set finalisers.
	finaliserAllocations = &finaliserAllocationRegistry{
		mu:      sync.Mutex{},
		entries: nil,
		pruneAt: finaliserAllocationsPruneMinimum,
	}
)

// finaliserAllocationState says what the registry knows of a pointer.
type finaliserAllocationState uint8

// finaliserAllocation is the record of one whole allocation.
type finaliserAllocation struct {
	// object is the allocation, which the record does not keep reachable.
	object weak.Pointer[byte]

	// finaliserSet is true while a finaliser registered through the interpreter is set on
	// the allocation.
	finaliserSet bool
}

// finaliserAllocationRegistry records, by address, the Go allocations the interpreter
// made whole for a program that sets finalisers, and which of them have a finaliser set.
//
// runtime.SetFinalizer ends the process, rather than panicking, for a pointer that is not
// the start of an allocation or for an object whose finaliser is already set. The Go
// runtime's allocation boundaries are not visible to the interpreter, so it forwards a
// registration only for a pointer it recorded here when it allocated the object
// (recordFinaliserPointer), and only when no finaliser is set. A record holds its object
// through a weak pointer, so it keeps nothing reachable, and a record whose object was
// collected never matches a later allocation at the same address. An object whose
// finaliser is set cannot be collected before the finaliser runs, so its record stays
// valid until finaliserRan.
type finaliserAllocationRegistry struct {
	// entries maps an allocation's address to its record. The mu guards it.
	entries map[uintptr]finaliserAllocation

	// mu guards entries and pruneAt, and serialises registrations with the runtime.
	mu sync.Mutex

	// pruneAt is the record count at which records of collected objects are dropped.
	pruneAt int
}

// record notes pointer as the start of a Go allocation the interpreter made.
//
// Takes pointer (reflect.Value) which is a pointer returned by reflect.New. A nil
// pointer, or one to a zero-size type, which points at no allocation of its own, is
// ignored.
//
// Safe for concurrent use.
func (r *finaliserAllocationRegistry) record(pointer reflect.Value) {
	if !pointer.IsValid() || pointer.Kind() != reflect.Pointer || pointer.IsNil() || pointer.Type().Elem().Size() == 0 {
		return
	}
	handle := weak.Make((*byte)(pointer.UnsafePointer()))
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries == nil {
		r.entries = make(map[uintptr]finaliserAllocation)
	}
	r.entries[pointer.Pointer()] = finaliserAllocation{object: handle, finaliserSet: false}
	if len(r.entries) >= r.pruneAt {
		r.prune()
	}
}

// prune drops the records of collected objects whose finaliser is not set.
//
// Called with mu held.
func (r *finaliserAllocationRegistry) prune() {
	for address, entry := range r.entries {
		if !entry.finaliserSet && entry.object.Value() == nil {
			delete(r.entries, address)
		}
	}
	r.pruneAt = max(finaliserAllocationsPruneMinimum, 2*len(r.entries))
}

// stateLocked reports what the registry knows of pointer.
//
// Takes pointer (reflect.Value) which is a non-nil pointer.
//
// Returns finaliserAllocationState which is the pointer's state.
//
// Called with mu held.
func (r *finaliserAllocationRegistry) stateLocked(pointer reflect.Value) finaliserAllocationState {
	entry, ok := r.entries[pointer.Pointer()]
	if !ok {
		return finaliserAllocationUnknown
	}
	if entry.finaliserSet {
		return finaliserAllocationSet
	}
	if entry.object.Value() != (*byte)(pointer.UnsafePointer()) {
		return finaliserAllocationUnknown
	}
	return finaliserAllocationFree
}

// update applies a registration to pointer's record under the registry's lock.
//
// Takes pointer (reflect.Value) which is the object.
// Takes apply (func(finaliserAllocationState) (bool, error)) which receives the record's
// state, calls the runtime, and returns whether a finaliser is now set. A pointer with no
// record that takes a finaliser gets a record with no allocation, which the finaliser
// keeps valid, as it keeps the object reachable, until the finaliser runs or is cleared.
//
// Returns error which is apply's.
//
// Safe for concurrent use. apply runs with the lock held, so two registrations on one
// object cannot both see it free.
func (r *finaliserAllocationRegistry) update(pointer reflect.Value, apply func(finaliserAllocationState) (bool, error)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.stateLocked(pointer)
	set, err := apply(state)
	if err != nil {
		return err
	}
	address := pointer.Pointer()
	entry, ok := r.entries[address]
	if !ok && !set {
		return nil
	}
	if r.entries == nil {
		r.entries = make(map[uintptr]finaliserAllocation)
	}
	entry.finaliserSet = set
	r.entries[address] = entry
	return nil
}

// finaliserRan re-records an object whose finaliser the runtime is about to run. The
// runtime cleared the finaliser before calling it and the object is reachable again, so
// it may take a new one, as in Go.
//
// Takes object (reflect.Value) which is the pointer the runtime passed.
//
// The allocation registry serialises updates.
func (r *finaliserAllocationRegistry) finaliserRan(object reflect.Value) {
	r.record(object)
}

// recordFinaliserPointer records pointer, which reflect.New returned, as a whole
// allocation when vm's program sets finalisers.
//
// Takes vm (*VM) which allocated the object.
// Takes pointer (reflect.Value) which points at the start of the allocation.
//
// Returns reflect.Value which is pointer, for chaining.
func recordFinaliserPointer(vm *VM, pointer reflect.Value) reflect.Value {
	if vm.allocatesForFinalisers() {
		finaliserAllocations.record(pointer)
	}
	return pointer
}

// recordFinaliserSlot records the allocation slot is, the Elem of a pointer reflect.New
// returned, as a whole allocation when vm's program sets finalisers.
//
// Takes vm (*VM) which allocated the slot.
// Takes slot (reflect.Value) which is the addressable allocated value.
//
// Returns reflect.Value which is slot, for chaining.
func recordFinaliserSlot(vm *VM, slot reflect.Value) reflect.Value {
	if slot.CanAddr() && vm.allocatesForFinalisers() {
		finaliserAllocations.record(slot.Addr())
	}
	return slot
}

// recordFinaliserBacking records the backing array of slice, which reflect.MakeSlice
// returned, as a whole allocation when vm's program sets finalisers, so that a pointer to
// its first element may take a finaliser, as in Go.
//
// Takes vm (*VM) which made the slice.
// Takes slice (reflect.Value) which is the new slice.
//
// Returns reflect.Value which is slice, for chaining.
func recordFinaliserBacking(vm *VM, slice reflect.Value) reflect.Value {
	if slice.Cap() > 0 && vm.allocatesForFinalisers() {
		finaliserAllocations.record(slice.Slice(0, 1).Index(0).Addr())
	}
	return slice
}
