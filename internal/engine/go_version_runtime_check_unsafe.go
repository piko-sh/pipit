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
	"errors"
	"fmt"
	"reflect"
	"unsafe"
)

const (
	// layoutProbeSentinel is the int64 written through each probe view so a wrong pointer or
	// flag word is detected by value rather than by crashing.
	layoutProbeSentinel = int64(0x0123_4567_89AB_CDEF)

	// layoutProbeReplacement is the value written back through unsafeNewAt to prove the
	// synthesised Value addresses the same storage.
	layoutProbeReplacement = int64(0x5555_5555_5555_5555)

	// wantReflectValueSize is the size of reflect.Value the punning code relies on: three
	// machine words on a 64-bit target.
	wantReflectValueSize = 24
)

var (
	// errUnsafeRuntimeLayout is the sentinel every layout probe failure wraps.
	errUnsafeRuntimeLayout = errors.New("unsafe runtime layout assumption violated")
)

// verifyUnsafeRuntimeLayout probes the reflect and runtime internals that the unsafe
// build depends on.
//
// Each probe mirrors one assumption in reflect_value_unsafe.go or
// vm_handler_map_linkname.go. A toolchain whose internals have moved refuses to start
// instead of corrupting memory later.
//
// Returns nil when every assumption holds, or an error wrapping errUnsafeRuntimeLayout
// naming the first violated one.
func verifyUnsafeRuntimeLayout() error {
	if err := verifyReflectValueSize(); err != nil {
		return err
	}
	if err := verifyAddressableValueLayout(); err != nil {
		return err
	}
	if err := verifyNonAddressableValueLayout(); err != nil {
		return err
	}
	if err := verifyUnsafeNewAtRoundTrip(); err != nil {
		return err
	}
	return verifyMapAccessFastStr()
}

// verifyReflectValueSize checks reflect.Value is still three machine words.
//
// Returns an error when the size differs from wantReflectValueSize.
func verifyReflectValueSize() error {
	if got := unsafe.Sizeof(reflect.Value{}); got != wantReflectValueSize {
		return fmt.Errorf("%w: reflect.Value size is %d, want %d", errUnsafeRuntimeLayout, got, wantReflectValueSize)
	}
	return nil
}

// verifyAddressableValueLayout checks that an addressable Elem() view exposes the
// expected type word, storage pointer, kind bits and flagAddr|flagIndir flags.
//
// Returns an error naming the first mismatching word.
func verifyAddressableValueLayout() error {
	storage := layoutProbeSentinel
	view := reflect.ValueOf(&storage).Elem()
	raw := (*unsafeReflectValue)(unsafe.Pointer(&view))
	if raw.typ != reflectValueABIType(reflect.TypeFor[int64]()) {
		return fmt.Errorf("%w: addressable Value word 0 is not the *abi.Type", errUnsafeRuntimeLayout)
	}
	if raw.ptr != unsafe.Pointer(&storage) {
		return fmt.Errorf("%w: addressable Value word 1 is not the storage pointer", errUnsafeRuntimeLayout)
	}
	if reflect.Kind(raw.flag&flagKindMask) != reflect.Int64 {
		return fmt.Errorf("%w: addressable Value flag kind bits are %#x, want Int64", errUnsafeRuntimeLayout, raw.flag&flagKindMask)
	}
	if raw.flag&(flagAddr|flagIndir) != flagAddr|flagIndir {
		return fmt.Errorf("%w: addressable Value flag is %#x, want flagAddr|flagIndir set", errUnsafeRuntimeLayout, raw.flag)
	}
	return nil
}

// verifyNonAddressableValueLayout checks that a by-value reflect.Value does not carry
// flagAddr, which the read-only punning helpers rely on.
//
// Returns an error when flagAddr is set.
func verifyNonAddressableValueLayout() error {
	view := reflect.ValueOf(layoutProbeSentinel)
	raw := (*unsafeReflectValue)(unsafe.Pointer(&view))
	if raw.flag&flagAddr != 0 {
		return fmt.Errorf("%w: non-addressable Value flag is %#x, want flagAddr clear", errUnsafeRuntimeLayout, raw.flag)
	}
	return nil
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
// correctly on a two-entry map, which pins both the symbol and the map header shape.
//
// Returns an error when a present key misses, a missing key hits, or the hit reads the
// wrong slot.
func verifyMapAccessFastStr() error {
	probe := map[string]int64{"alpha": 1, "beta": layoutProbeSentinel}
	mapValue := reflect.ValueOf(probe)
	slot, found := mapAccessFastStrToGeneral(mapValue, "beta")
	if !found || slot == nil {
		return fmt.Errorf("%w: mapaccess2_faststr missed a present key", errUnsafeRuntimeLayout)
	}
	if got := *(*int64)(slot); got != layoutProbeSentinel {
		return fmt.Errorf("%w: mapaccess2_faststr returned slot value %d, want %d", errUnsafeRuntimeLayout, got, layoutProbeSentinel)
	}
	if _, found := mapAccessFastStrToGeneral(mapValue, "gamma"); found {
		return fmt.Errorf("%w: mapaccess2_faststr reported a missing key as present", errUnsafeRuntimeLayout)
	}
	return nil
}
