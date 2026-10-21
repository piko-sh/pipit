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
	"sync/atomic"
)

var (
	// krefPlacementCounters counts KRef placements and vetoes, for tests and diagnostics.
	krefPlacementCounters struct {
		// snapshotSlots counts boundary snapshots placed in KRef.
		snapshotSlots atomic.Int64

		// pointeePlaced counts reference-holding pointees placed in KRef slots.
		pointeePlaced atomic.Int64

		// pointeeClean counts pointee placements whose chunk no hand-off revoked by the
		// release.
		pointeeClean atomic.Int64

		// pointeeRevoked counts pointee placements whose chunk a hand-off revoked by the
		// release.
		pointeeRevoked atomic.Int64

		// pointeeVetoes counts pointee sites vetoed.
		pointeeVetoes atomic.Int64

		// pointeeEpochVetoes counts the sites among pointeeVetoes vetoed because their
		// placement's epoch was revoked whole.
		pointeeEpochVetoes atomic.Int64

		// snapshotVetoes counts element types vetoed for snapshot placement
		// (settleSnapshotPlacements).
		snapshotVetoes atomic.Int64
	}
)

// KRefPlacementCounts is a snapshot of the KRef placement counters.
type KRefPlacementCounts struct {
	// SnapshotSlots is the number of boundary snapshots placed in KRef chunks.
	SnapshotSlots int64

	// PointeePlaced is the number of reference-holding pointees placed in KRef slots.
	PointeePlaced int64

	// PointeeClean is the number of pointee placements whose chunk stayed clean to the
	// release.
	PointeeClean int64

	// PointeeRevoked is the number of pointee placements whose chunk was revoked by the
	// release.
	PointeeRevoked int64

	// PointeeVetoes is the number of pointee sites vetoed.
	PointeeVetoes int64

	// PointeeEpochVetoes is the number of those sites vetoed because their placement's epoch
	// was revoked whole.
	PointeeEpochVetoes int64

	// SnapshotVetoes is the number of element types vetoed for snapshot placement after a
	// walk-budget epoch.
	SnapshotVetoes int64
}

// krefPlacementStatistics returns the KRef placement counters since the process started.
//
// Returns KRefPlacementCounts which is the snapshot.
func krefPlacementStatistics() KRefPlacementCounts {
	counters := &krefPlacementCounters
	return KRefPlacementCounts{
		SnapshotSlots: counters.snapshotSlots.Load(),
		PointeePlaced: counters.pointeePlaced.Load(), PointeeClean: counters.pointeeClean.Load(), PointeeRevoked: counters.pointeeRevoked.Load(), PointeeVetoes: counters.pointeeVetoes.Load(),
		PointeeEpochVetoes: counters.pointeeEpochVetoes.Load(), SnapshotVetoes: counters.snapshotVetoes.Load(),
	}
}

// krefCompositeOwned reports whether a composite's storage is a KRef slot, which a holder
// that keeps the reflect.Value itself beyond the epoch must copy.
//
// Takes arena (*RegisterArena) which owns the candidate chunks.
// Takes v (reflect.Value) which is a struct or array.
//
// Returns bool which is true when v's storage is a KRef slot.
func krefCompositeOwned(arena *RegisterArena, v reflect.Value) bool {
	return len(arena.kref.sorted) != 0 && arena.krefContains(ReflectValuePtr(v))
}

// krefHolderMustCopy reports whether a holder that keeps a composite in a KRef slot
// beyond the epoch, such as a package variable, must copy it onto the Go heap first.
//
// Takes arena (*RegisterArena) which owns the slot.
//
// Returns bool which is true when the holder must copy the composite.
func krefHolderMustCopy(arena *RegisterArena) bool {
	return !arena.licenceState.epochRevoked
}

// adapterHeldValue returns the value kept by an interface adapter or fmt renderer.
//
// Takes value (reflect.Value) which is the wrapped value.
//
// Returns reflect.Value which is value, or its heap copy.
func adapterHeldValue(_ *VM, value reflect.Value) reflect.Value {
	if !value.IsValid() || (value.Kind() != reflect.Struct && value.Kind() != reflect.Array) {
		return value
	}
	readable := licenceReadable(value)
	if !readable.CanInterface() {
		return value
	}
	return licenceKeepReadOnly(copyReflectValue(readable), value)
}

// krefCopy copies a struct or array out of a KRef slot onto the Go heap. A value read
// through an unexported field is read-only, which reflect.Value.Set refuses as a source,
// so the copy reads it without the flag and gives the copy the flag back.
//
// Takes v (reflect.Value) which is the value.
//
// Returns reflect.Value which is the heap copy, as read-only as v.
func krefCopy(v reflect.Value) reflect.Value {
	return licenceKeepReadOnly(copyReflectValue(licenceReadable(v)), v)
}
