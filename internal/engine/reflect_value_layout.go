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

import "unsafe"

const (
	// flagKindMask masks the low 5 bits where reflect.Kind is stored in reflect.Value.flag.
	flagKindMask uintptr = 1<<5 - 1

	// licenceFlagRO is reflect's read-only flag pair (flagStickyRO | flagEmbedRO), set on a
	// Value reached through an unexported field.
	licenceFlagRO uintptr = 1<<5 | 1<<6

	// flagIndir indicates that reflect.Value.ptr is a *T rather than the value itself.
	// reflect.NewAt(elem, p).Elem() sets this because p points at the element's storage.
	flagIndir uintptr = 1 << 7

	// flagAddr indicates that the reflect.Value is addressable (CanAddr() returns true).
	// reflect.NewAt(elem, p).Elem() sets this because the storage at p is settable via Elem.
	flagAddr uintptr = 1 << 8
)

// unsafeReflectValue mirrors the runtime layout of reflect.Value on Go 1.27.
//
// Every build casts a *reflect.Value to it, which is why verifyReflectValueLayout() runs
// at start-up in every build.
type unsafeReflectValue struct {
	// typ points at the *abi.Type of the value's type.
	typ unsafe.Pointer

	// ptr holds the value, or a pointer to it when flagIndir is set.
	ptr unsafe.Pointer

	// flag carries flagAddr | flagIndir | the read-only bits | Kind.
	flag uintptr
}

// emptyInterface mirrors the runtime layout of an interface header.
type emptyInterface struct {
	// typ is the dynamic type pointer for the interface header.
	typ unsafe.Pointer

	// data holds the value word, which is the *abi.Type pointer for reflect.Type.
	data unsafe.Pointer
}

// snapshotSliceHeader mirrors the runtime layout of a Go slice header. The Data field is
// unsafe.Pointer so the garbage collector tracks the backing array as reachable.
type snapshotSliceHeader struct {
	// Data points at the slice's element backing array.
	Data unsafe.Pointer

	// Len is the slice's logical length.
	Len int

	// Cap is the slice's underlying capacity.
	Cap int
}
