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

//go:build go1.27 && !safe && !(js && wasm)

package engine

import (
	"fmt"
	"reflect"
	"unsafe"
)

const (
	// layoutProbeReplacement is the value written back through unsafeNewAt to prove the
	// synthesised Value addresses the same storage.
	layoutProbeReplacement = int64(0x5555_5555_5555_5555)

	// mapLayoutProbeTableEntries makes probes exercise a map backed by a table.
	mapLayoutProbeTableEntries = 64

	// mapLayoutProbeAssignKey is absent from the probe maps and exceeds 32 bits to detect
	// key truncation.
	mapLayoutProbeAssignKey = 1 << 40
)

// verifyUnsafeRuntimeLayout probes the reflect and runtime internals that the unsafe
// build depends on.
//
// Returns nil when every assumption holds, or an error wrapping errUnsafeRuntimeLayout
// naming the first violated one.
func verifyUnsafeRuntimeLayout() error {
	if err := verifyReflectValueLayout(); err != nil {
		return err
	}
	if err := verifyUnsafeNewAtRoundTrip(); err != nil {
		return err
	}
	if err := verifyMapAccessFastStr(); err != nil {
		return err
	}
	if err := verifyMapAssignFastStr(); err != nil {
		return err
	}
	if err := verifyMapInlineElemBoundary(); err != nil {
		return err
	}
	return verifyMapFast64()
}

// verifyUnsafeNewAtRoundTrip checks that a Value synthesised by unsafeNewAt reads and
// writes the storage it was pointed at.
//
// Returns an error when the read or the write misses the sentinel storage.
func verifyUnsafeNewAtRoundTrip() error {
	storage := layoutProbeSentinel
	view := unsafeNewAt(reflectValueABIType(reflect.TypeFor[int64]()), unsafe.Pointer(&storage), reflect.Int64)
	if !view.IsValid() || view.Kind() != reflect.Int64 || view.Int() != layoutProbeSentinel {
		return fmt.Errorf("%w: unsafeNewAt view did not read the sentinel back", errUnsafeRuntimeLayout)
	}
	view.SetInt(layoutProbeReplacement)
	if storage != layoutProbeReplacement {
		return fmt.Errorf("%w: unsafeNewAt view did not write through to its storage", errUnsafeRuntimeLayout)
	}
	return nil
}

// verifyMapAccessFastStr checks the linknamed runtime.mapaccess2_faststr hits and misses
// correctly on a two-entry map and on a table-sized map, which pins the symbol and both
// map shapes.
//
// Returns an error when a present key misses, a missing key hits, or the hit reads the
// wrong slot.
func verifyMapAccessFastStr() error {
	for _, entries := range []int{2, mapLayoutProbeTableEntries} {
		probe := make(map[string]int64, entries)
		for i := range entries {
			probe[fmt.Sprintf("key-%d", i)] = int64(i)
		}
		probe["beta"] = layoutProbeSentinel
		mapValue := reflect.ValueOf(probe)
		slot, found := mapAccessFastStrToGeneral(mapValue, "beta")
		if !found || slot == nil {
			return fmt.Errorf("%w: mapaccess2_faststr missed a present key in a %d-entry map", errUnsafeRuntimeLayout, len(probe))
		}
		if got := *(*int64)(slot); got != layoutProbeSentinel {
			return fmt.Errorf("%w: mapaccess2_faststr returned slot value %d, want %d", errUnsafeRuntimeLayout, got, layoutProbeSentinel)
		}
		if _, found := mapAccessFastStrToGeneral(mapValue, "gamma"); found {
			return fmt.Errorf("%w: mapaccess2_faststr reported a missing key as present", errUnsafeRuntimeLayout)
		}
	}
	return nil
}

// verifyMapAssignFastStr checks that slots returned by the linknamed
// runtime.mapassign_faststr hold the values written through them, as seen by native map
// reads, while the map grows from one group into a table.
//
// Returns an error when a native read misses a written value or the length is wrong.
func verifyMapAssignFastStr() error {
	probe := map[string]int64{}
	mapValue := reflect.ValueOf(probe)
	for i := range mapLayoutProbeTableEntries {
		slot := mapAssignFastStr(mapValue, fmt.Sprintf("key-%d", i))
		if slot == nil {
			return fmt.Errorf("%w: mapassign_faststr returned no slot", errUnsafeRuntimeLayout)
		}
		*(*int64)(slot) = layoutProbeSentinel + int64(i)
	}
	if len(probe) != mapLayoutProbeTableEntries {
		return fmt.Errorf("%w: mapassign_faststr left %d entries, want %d", errUnsafeRuntimeLayout, len(probe), mapLayoutProbeTableEntries)
	}
	for i := range mapLayoutProbeTableEntries {
		if got := probe[fmt.Sprintf("key-%d", i)]; got != layoutProbeSentinel+int64(i) {
			return fmt.Errorf("%w: a value written through a mapassign_faststr slot read back natively as %d", errUnsafeRuntimeLayout, got)
		}
	}
	return nil
}

// verifyMapInlineElemBoundary checks that elements of mapFastPathMaxElemBytes remain
// inline, as the map fast paths require.
//
// Returns an error when the slot does not hold the element's first and last words.
func verifyMapInlineElemBoundary() error {
	const words = mapFastPathMaxElemBytes / 8
	var element [words]int64
	element[0], element[words-1] = layoutProbeSentinel, layoutProbeReplacement
	probe := map[string][words]int64{"edge": element}
	slot, found := mapAccessFastStrToGeneral(reflect.ValueOf(probe), "edge")
	if !found || slot == nil {
		return fmt.Errorf("%w: mapaccess2_faststr missed the %d-byte element", errUnsafeRuntimeLayout, mapFastPathMaxElemBytes)
	}

	if *(*int64)(slot) != layoutProbeSentinel || *(*int64)(unsafe.Add(slot, (words-1)*8)) != layoutProbeReplacement {
		return fmt.Errorf("%w: a %d-byte map element is not stored inline", errUnsafeRuntimeLayout, mapFastPathMaxElemBytes)
	}
	return nil
}

// verifyMapFast64 checks the linknamed runtime.mapaccess2_fast64 and mapassign_fast64 on
// a two-entry map and a table-sized map.
//
// Returns an error when a read or a write through a slot disagrees with native access.
func verifyMapFast64() error {
	for _, entries := range []int{2, mapLayoutProbeTableEntries} {
		probe := make(map[int64]int64, entries)
		for i := range entries {
			probe[int64(i)] = int64(i)
		}
		probe[-1] = layoutProbeSentinel
		mapValue := reflect.ValueOf(probe)
		slot, found := mapAccessFast64ToGeneral(mapValue, -1)
		if !found || slot == nil || *(*int64)(slot) != layoutProbeSentinel {
			return fmt.Errorf("%w: mapaccess2_fast64 did not read a present key in a %d-entry map", errUnsafeRuntimeLayout, len(probe))
		}
		if _, found := mapAccessFast64ToGeneral(mapValue, -2); found {
			return fmt.Errorf("%w: mapaccess2_fast64 reported a missing key as present", errUnsafeRuntimeLayout)
		}
		mapPtr := dereferenceIndirectPointer((*unsafeReflectValue)(unsafe.Pointer(&mapValue)))
		assigned := runtimeMapassignFast64(reflectValueABIType(mapValue.Type()), mapPtr, mapLayoutProbeAssignKey)
		if assigned == nil {
			return fmt.Errorf("%w: mapassign_fast64 returned no slot", errUnsafeRuntimeLayout)
		}
		*(*int64)(assigned) = layoutProbeReplacement
		if probe[mapLayoutProbeAssignKey] != layoutProbeReplacement {
			return fmt.Errorf("%w: a value written through a mapassign_fast64 slot did not read back natively", errUnsafeRuntimeLayout)
		}
	}
	return nil
}
