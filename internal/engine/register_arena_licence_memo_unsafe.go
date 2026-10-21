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
	"math/bits"
	"reflect"
	"sync"
	"unsafe"
)

const (
	// splitMixMultiplier is SplitMix64's second multiplier, which finishes mixing the memo's
	// key into the slot index.
	splitMixMultiplier = 0xbf58476d1ce4e5b9

	// licenceMemoShrinkRatio is how many slots per survivor a large memo table may have
	// before it is given back and a smaller one taken.
	licenceMemoShrinkRatio = 8
)

const (
	// licenceMemoInitialEntries is the table size made on first use.
	licenceMemoInitialEntries = 64

	// licenceMemoMaxEntries bounds the table. A walk visits at least one value per entry it
	// makes, so the epoch's leaf budget bounds the entries an epoch needs; at half load this
	// covers it.
	licenceMemoMaxEntries = 2 * licenceWalkLeafBudget

	// licenceMemoKeepEntries is the largest table an epoch boundary keeps when few of its
	// entries survive, so one huge walk does not pin its table to the arena for good.
	licenceMemoKeepEntries = 1 << 12
)

// licenceMemoTables holds emptied tables by size class (the log2 of their length), for
// grow and the epoch boundary to take before they allocate.
var licenceMemoTables [bits.UintSize]sync.Pool

// licenceMemoEntry is one memoised container.
type licenceMemoEntry struct {
	// pointer identifies a container by its pointee address, first slice element or map
	// address.
	pointer unsafe.Pointer

	// typ is the *abi.Type of the pointee, the slice's element or the map.
	typ unsafe.Pointer

	// extent is the slice capacity the walk covered, or zero.
	extent int

	// epoch is the memo epoch in which a walk last made or met the entry.
	epoch uint32
}

// licenceWalkMemo is an arena's memo table.
type licenceWalkMemo struct {
	// entries is the open-addressed table; its length is a power of two.
	entries []licenceMemoEntry

	// spare is the table the epoch boundary rehashes survivors into.
	spare []licenceMemoEntry

	// count is the number of occupied entries.
	count int

	// epoch counts the epoch boundaries the memo has seen in which a walk used it.
	epoch uint32

	// used is true once a walk made or met an entry since the last boundary.
	used bool

	// hint is the size to make the table at its first growth, when the arena's previous run
	// of the same program dropped a table that large at its release; zero otherwise. grow
	// consumes the hint.
	hint int
}

// takeMemoTable returns an empty table of size entries, a pooled one when there is one.
//
// Takes size (int) which is a power of two.
//
// Returns []licenceMemoEntry which is empty.
func takeMemoTable(size int) []licenceMemoEntry {
	if size >= licenceMemoInitialEntries {
		if table, ok := licenceMemoTables[bits.Len(uint(size))-1].Get().(*[]licenceMemoEntry); ok && len(*table) == size {
			return *table
		}
	}
	return make([]licenceMemoEntry, size)
}

// donateMemoTable empties table and pools it.
//
// Takes table ([]licenceMemoEntry) which the caller no longer uses.
func donateMemoTable(table []licenceMemoEntry) {
	if len(table) < licenceMemoInitialEntries {
		return
	}
	clear(table)
	licenceMemoTables[bits.Len(uint(len(table)))-1].Put(&table)
}

// release is the arena's return to the pool.
//
// A table larger than licenceMemoKeepEntries is emptied and pooled, with its spare, so
// the arenas the pool holds do not each keep one.
//
// Returns int which is the size of the table dropped, or zero.
func (m *licenceWalkMemo) release() int {
	if len(m.entries) <= licenceMemoKeepEntries {
		return 0
	}
	dropped := len(m.entries)
	donateMemoTable(m.entries)
	donateMemoTable(m.spare)
	m.entries, m.spare, m.count, m.used, m.hint = nil, nil, 0, false, 0
	m.epoch++
	return dropped
}

// licenceMemoHash hashes a container's address and type into a table of size mask+1.
//
// Takes pointer (unsafe.Pointer) which is the container's address.
// Takes typ (unsafe.Pointer) which is its type.
// Takes mask (int) which is the table size minus one.
//
// Returns int which is the first slot to probe.
func licenceMemoHash(pointer, typ unsafe.Pointer, mask int) int {
	hash := (uint64(uintptr(pointer)) ^ uint64(uintptr(typ))*fibonacciHashMultiplier) * splitMixMultiplier
	return int(hash>>32) & mask
}

// covers reports whether the memo holds the container with at least extent, marking the
// entry met in this epoch.
//
// Takes pointer (unsafe.Pointer) which is the container's address.
// Takes typ (unsafe.Pointer) which is its type.
// Takes extent (int) which is the capacity the caller would walk, or zero.
//
// Returns bool which is true when an earlier walk covered it.
func (m *licenceWalkMemo) covers(pointer, typ unsafe.Pointer, extent int) bool {
	if m.count == 0 {
		return false
	}
	mask := len(m.entries) - 1
	for index := licenceMemoHash(pointer, typ, mask); ; index = (index + 1) & mask {
		entry := &m.entries[index]
		if entry.pointer == nil {
			return false
		}
		if entry.pointer == pointer && entry.typ == typ {
			if entry.extent < extent {
				return false
			}
			entry.epoch = m.epoch
			m.used = true
			return true
		}
	}
}

// peek is covers for a shared walk.
//
// It reads the table only, leaving the entry's age and the use mark as they are, so a
// shared walk writes nothing into the memo.
//
// Takes pointer (unsafe.Pointer) which is the container's address.
// Takes typ (unsafe.Pointer) which is its type.
// Takes extent (int) which is the capacity the caller would walk, or zero.
//
// Returns bool which is true when an earlier private walk covered it.
func (m *licenceWalkMemo) peek(pointer, typ unsafe.Pointer, extent int) bool {
	if m.count == 0 {
		return false
	}
	mask := len(m.entries) - 1
	for index := licenceMemoHash(pointer, typ, mask); ; index = (index + 1) & mask {
		entry := &m.entries[index]
		if entry.pointer == nil {
			return false
		}
		if entry.pointer == pointer && entry.typ == typ {
			return entry.extent >= extent
		}
	}
}

// note records the container as walked to extent.
//
// Takes pointer (unsafe.Pointer) which is the container's address.
// Takes typ (unsafe.Pointer) which is its type.
// Takes extent (int) which is the capacity walked, or zero.
func (m *licenceWalkMemo) note(pointer, typ unsafe.Pointer, extent int) {
	if 2*(m.count+1) > len(m.entries) {
		if len(m.entries) >= licenceMemoMaxEntries {
			return
		}
		m.grow()
	}
	m.insert(licenceMemoEntry{pointer: pointer, typ: typ, extent: extent, epoch: m.epoch})
	m.used = true
}

// insert places entry in the table, merging it with an equal container's entry.
//
// Takes entry (licenceMemoEntry) which is the entry; the table has room for it.
func (m *licenceWalkMemo) insert(entry licenceMemoEntry) {
	mask := len(m.entries) - 1
	for index := licenceMemoHash(entry.pointer, entry.typ, mask); ; index = (index + 1) & mask {
		slot := &m.entries[index]
		if slot.pointer == nil {
			*slot = entry
			m.count++
			return
		}
		if slot.pointer == entry.pointer && slot.typ == entry.typ {
			slot.extent = max(slot.extent, entry.extent)
			slot.epoch = max(slot.epoch, entry.epoch)
			return
		}
	}
}

// grow doubles the table, or makes it on first use, rehashing what it holds.
func (m *licenceWalkMemo) grow() {
	previous := m.entries
	size := max(2*len(previous), licenceMemoInitialEntries)
	if previous == nil && m.hint > size {
		size = m.hint
	}
	m.hint = 0
	m.entries = takeMemoTable(size)
	donateMemoTable(m.spare)
	m.spare = nil
	m.count = 0
	for _, entry := range previous {
		if entry.pointer != nil {
			m.insert(entry)
		}
	}
	donateMemoTable(previous)
}

// reset is the epoch boundary.
//
// Entries a walk made or met in the epoch that ends survive, rehashed into the spare
// table, and the rest are dropped. A table larger than licenceMemoKeepEntries that keeps
// less than an eighth of its capacity is dropped whole.
func (m *licenceWalkMemo) reset() {
	if !m.used {
		return
	}
	m.used = false
	if m.count == 0 {
		m.epoch++
		return
	}
	survivors := 0
	for index := range m.entries {
		if entry := &m.entries[index]; entry.pointer != nil && entry.epoch == m.epoch {
			survivors++
		}
	}
	if len(m.entries) > licenceMemoKeepEntries && licenceMemoShrinkRatio*survivors < len(m.entries) {
		donateMemoTable(m.entries)
		donateMemoTable(m.spare)
		m.entries, m.spare, m.count = nil, nil, 0
		m.epoch++
		return
	}
	if len(m.spare) != len(m.entries) {
		donateMemoTable(m.spare)
		m.spare = takeMemoTable(len(m.entries))
	}
	previous := m.entries
	m.entries, m.spare = m.spare, previous
	m.count = 0
	for index := range previous {
		if entry := previous[index]; entry.pointer != nil && entry.epoch == m.epoch {
			m.insert(entry)
		}
	}
	clear(previous)
	m.epoch++
}

// seedFreshObject records a fresh, zeroed boundary slot in the arena's walk memo. Its one
// caller, acquireStructLiteralSlot, passes only boundary chunk slots, never a KRef slot;
// the KRef test stays as a guard.
//
// Takes slot (reflect.Value) which is the struct a struct literal just took.
func (vm *VM) seedFreshObject(slot reflect.Value) {
	arena := vm.Arena
	if !licenceExactProbes || arena == nil || !arena.licensed {
		return
	}
	if arena.licenceState.epochRevoked || arena.licenceShared(0) {
		return
	}
	pointer := ReflectValuePtr(slot)
	if pointer == nil || !licenceOutsideEveryArena(arena, uintptr(pointer)) || arena.krefContains(pointer) {
		return
	}
	arena.licenceState.memo.note(pointer, reflectValueABIType(slot.Type()), 0)
}
