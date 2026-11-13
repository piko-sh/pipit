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
	"errors"
	"fmt"
	"reflect"
	"unsafe"
)

const (
	// layoutProbeSentinel is the int64 written through each layout-probe view.
	layoutProbeSentinel = int64(0x0123_4567_89AB_CDEF)

	// wantReflectValueSize is the required three-word size of reflect.Value.
	wantReflectValueSize = 24
)

// errUnsafeRuntimeLayout is the sentinel every layout probe failure wraps.
var errUnsafeRuntimeLayout = errors.New("unsafe runtime layout assumption violated")

// layoutProbeFields is the struct the read-only probe reads a field of each kind from.
type layoutProbeFields struct {
	// Exported is reached without the read-only flags.
	Exported int64

	// hidden is reached with them.
	hidden int64
}

// verifyReflectValueLayout checks reflect.Value against the mirrored runtime layout.
//
// Returns nil when every assumption holds, or an error wrapping errUnsafeRuntimeLayout
// naming the first violated one.
func verifyReflectValueLayout() error {
	if err := verifyReflectValueSize(); err != nil {
		return err
	}
	if err := verifyAddressableValueLayout(); err != nil {
		return err
	}
	if err := verifyNonAddressableValueLayout(); err != nil {
		return err
	}
	if err := verifyReadOnlyFlagLayout(); err != nil {
		return err
	}
	return verifySliceValueLayout()
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

// verifyReadOnlyFlagLayout checks that a Value reached through an unexported field
// carries reflect's read-only flags and one reached through an exported field does not.
//
// Returns an error when either field's flags are not as expected.
func verifyReadOnlyFlagLayout() error {
	fields := reflect.ValueOf(&layoutProbeFields{Exported: layoutProbeSentinel, hidden: layoutProbeSentinel}).Elem()
	exported, hidden := fields.Field(0), fields.Field(1)
	if raw := (*unsafeReflectValue)(unsafe.Pointer(&exported)); raw.flag&licenceFlagRO != 0 {
		return fmt.Errorf("%w: exported field Value flag is %#x, want the read-only bits clear", errUnsafeRuntimeLayout, raw.flag)
	}
	if raw := (*unsafeReflectValue)(unsafe.Pointer(&hidden)); raw.flag&licenceFlagRO == 0 {
		return fmt.Errorf("%w: unexported field Value flag is %#x, want a read-only bit set", errUnsafeRuntimeLayout, raw.flag)
	}
	return nil
}

// verifySliceValueLayout checks that a slice Value holds a pointer to its header, with
// flagIndir set, and that the header is laid out as snapshotSliceHeader mirrors it.
//
// Returns an error naming the first mismatch.
func verifySliceValueLayout() error {
	storage := []int64{layoutProbeSentinel, 0, 0}[:2]
	view := reflect.ValueOf(storage)
	raw := (*unsafeReflectValue)(unsafe.Pointer(&view))
	if reflect.Kind(raw.flag&flagKindMask) != reflect.Slice || raw.flag&flagIndir == 0 || raw.ptr == nil {
		return fmt.Errorf("%w: slice Value flag is %#x, want kind Slice with flagIndir set", errUnsafeRuntimeLayout, raw.flag)
	}
	header := (*snapshotSliceHeader)(raw.ptr)
	if header.Data != unsafe.Pointer(unsafe.SliceData(storage)) || header.Len != len(storage) || header.Cap != cap(storage) {
		return fmt.Errorf("%w: slice header is not data, length, capacity", errUnsafeRuntimeLayout)
	}
	return nil
}
