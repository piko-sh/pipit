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

//go:build safe || (js && wasm)

package engine

import (
	"reflect"
	"sync"
	"unsafe"
)

const (
	// flagKindMask masks the low 5 bits where reflect.Kind is stored in reflect.Value.flag.
	flagKindMask uintptr = 1<<5 - 1

	// flagIndir indicates that reflect.Value.ptr is a *T rather than the value itself.
	flagIndir uintptr = 1 << 7

	// flagAddr indicates that the reflect.Value is addressable.
	flagAddr uintptr = 1 << 8
)

var (
	// safeTypeRegistryMu guards the safe-build type registry maps.
	safeTypeRegistryMu sync.RWMutex

	// safeTypeRegistryByPtr recovers the reflect.Type from a token.
	safeTypeRegistryByPtr = make(map[unsafe.Pointer]reflect.Type)
)

// unsafeReflectValue mirrors the runtime layout of reflect.Value. On the safe build
// callers must NOT cast a *reflect.Value to a *unsafeReflectValue; the type exists only
// so reference sites compile under -tags safe.
type unsafeReflectValue struct {
	// typ would point at the *abi.Type on the unsafe build.
	typ unsafe.Pointer

	// ptr would carry the value's storage word on the unsafe build.
	ptr unsafe.Pointer

	// flag would carry flagAddr | flagIndir | Kind on the unsafe build.
	flag uintptr
}

// emptyInterface mirrors the runtime layout of an interface header.
type emptyInterface struct {
	// typ is the dynamic type pointer for the interface header.
	typ unsafe.Pointer

	// data is the dynamic value word; for reflect.Type it IS the *abi.Type pointer.
	data unsafe.Pointer
}

// snapshotSliceHeader mirrors the runtime layout of a Go slice header. The struct is
// exposed so callers that bump-allocate snapshot slots in vm.go compile under -tags safe;
// the GC tracks Data as a pointer.
type snapshotSliceHeader struct {
	// Data points at the slice's element backing array.
	Data unsafe.Pointer

	// Len is the slice's logical length.
	Len int

	// Cap is the slice's underlying capacity.
	Cap int
}

// safeTypeAnchor is the heap-resident anchor that backs each safe-build type token.
// Storing the reflect.Type inside the anchor keeps both the anchor and the type reachable
// for the GC, so passing the anchor's address through unsafe.Pointer fields is GC-safe.
type safeTypeAnchor struct {
	// t is the reflect.Type the anchor represents.
	t reflect.Type
}

// safeTypeRegistryByType deduplicates anchors so that calling reflectValueABIType twice
// for the same reflect.Type yields the same token.
var safeTypeRegistryByType = make(map[reflect.Type]*safeTypeAnchor)

// ResetSafeTypeRegistryForTest is a no-op for the safe build.
func ResetSafeTypeRegistryForTest() {}

// ReflectValuePtr extracts the storage address of a reflect.Value via the public reflect
// API. For addressable Values the returned pointer matches the unsafe path's behaviour;
// for non-addressable direct-iface kinds the fallback returns the held value via
// UnsafePointer().
//
// Takes v (reflect.Value) which the caller asserts is addressable or of a direct-iface
// kind.
//
// Returns the storage / value pointer, or nil for invalid Values.
func ReflectValuePtr(v reflect.Value) unsafe.Pointer {
	if !v.IsValid() {
		return nil
	}
	if v.CanAddr() {
		return v.Addr().UnsafePointer()
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return v.UnsafePointer()
	default:
		return nil
	}
}

// reflectValueABIType extracts the *abi.Type pointer from a reflect.Type by reading the
// interface header.
//
// The returned token also indexes a side registry so safeLookupType can recover the
// reflect.Type for helpers that need it. The implementation relies on reflect.Type's
// interface representation being stable while avoiding the go:linkname trampolines that
// wasm and -tags safe forbid.
//
// Takes t (reflect.Type) which the caller pre-derives.
//
// Returns nil for a nil t; otherwise the underlying *abi.Type pointer.
//
// Concurrency: Safe for concurrent use; guarded by safeTypeRegistryMu.
func reflectValueABIType(t reflect.Type) unsafe.Pointer {
	if t == nil {
		return nil
	}
	abiPtr := (*emptyInterface)(unsafe.Pointer(&t)).data
	safeTypeRegistryMu.RLock()
	if _, ok := safeTypeRegistryByPtr[abiPtr]; ok {
		safeTypeRegistryMu.RUnlock()
		return abiPtr
	}
	safeTypeRegistryMu.RUnlock()
	safeTypeRegistryMu.Lock()
	if _, ok := safeTypeRegistryByPtr[abiPtr]; !ok {
		anchor := &safeTypeAnchor{t: t}
		safeTypeRegistryByType[t] = anchor
		safeTypeRegistryByPtr[abiPtr] = t
	}
	safeTypeRegistryMu.Unlock()
	return abiPtr
}

// safeLookupType recovers the reflect.Type a token was minted from.
//
// Takes token (unsafe.Pointer) which is the *abi.Type pointer previously returned by
// reflectValueABIType.
//
// Returns the reflect.Type for the token; nil when the token is not in the registry.
//
// Concurrency: Safe for concurrent use; guarded by safeTypeRegistryMu.
func safeLookupType(token unsafe.Pointer) reflect.Type {
	if token == nil {
		return nil
	}
	safeTypeRegistryMu.RLock()
	t := safeTypeRegistryByPtr[token]
	safeTypeRegistryMu.RUnlock()
	return t
}

// wrapTypedSliceSrcType extracts the abi-type token from a slice-typed reflect.Value.
// Safe build routes through reflectValueABIType (which consults the type registry); the
// unsafe build sibling uses a direct layout-cast field load to keep the typed-append hot
// path one-cycle thin.
//
// Takes v (reflect.Value) which is the source slice value.
//
// Returns the abi-type token suitable for unsafeNewAt.
func wrapTypedSliceSrcType(v reflect.Value) unsafe.Pointer {
	return reflectValueABIType(v.Type())
}

// unsafeNewAt is the safe-build equivalent of reflect.NewAt(elem, p).Elem().
//
// Takes cachedABIType (unsafe.Pointer) which is the token for the element type T.
// Takes p (unsafe.Pointer) which is the address of the element storage.
//
// Returns an addressable reflect.Value of type T pointing at p.
func unsafeNewAt(cachedABIType unsafe.Pointer, p unsafe.Pointer, _ reflect.Kind) reflect.Value {
	t := safeLookupType(cachedABIType)
	if t == nil || p == nil {
		return reflect.Value{}
	}
	return reflect.NewAt(t, p).Elem()
}

// unsafeReadOnlyValue is the safe-build equivalent of the non-addressable variant.
//
// A fresh heap-allocated copy of the value at p is produced so the returned Value cannot
// mutate the source storage. The reflect.Kind argument is accepted to share a signature
// with the unsafe build and is not read here.
//
// Takes cachedABIType (unsafe.Pointer) which is the token for T.
// Takes p (unsafe.Pointer) which is the source storage.
//
// Returns a non-addressable copy.
func unsafeReadOnlyValue(cachedABIType unsafe.Pointer, p unsafe.Pointer, _ reflect.Kind) reflect.Value {
	t := safeLookupType(cachedABIType)
	if t == nil || p == nil {
		return reflect.Value{}
	}
	source := reflect.NewAt(t, p).Elem()
	detached := reflect.New(t).Elem()
	detached.Set(source)
	return detached
}

// unsafePointerKindValue constructs a Pointer-kind reflect.Value holding ptrValue. The
// token must be the *T pointer type, not T.
//
// Takes cachedABIType (unsafe.Pointer) which is the token for *T.
// Takes ptrValue (unsafe.Pointer) which is the pointer value itself.
//
// Returns a non-addressable Pointer-kind Value carrying ptrValue, or an invalid
// reflect.Value when the token is nil.
func unsafePointerKindValue(cachedABIType unsafe.Pointer, ptrValue unsafe.Pointer) reflect.Value {
	t := safeLookupType(cachedABIType)
	if t == nil {
		return reflect.Value{}
	}
	if t.Kind() != reflect.Pointer {
		return reflect.Value{}
	}
	return reflect.NewAt(t.Elem(), ptrValue)
}

// unsafeDirectIfaceKindValue constructs a non-addressable direct-iface-kind Value
// carrying a 1-word value.
//
// Takes cachedABIType (unsafe.Pointer) which is the *abi.Type for T.
// Takes value (unsafe.Pointer) which is the held 1-word value.
// Takes kind (reflect.Kind) which discriminates the direct-iface shape.
//
// Returns a non-addressable Value of type T carrying value.
func unsafeDirectIfaceKindValue(cachedABIType unsafe.Pointer, value unsafe.Pointer, kind reflect.Kind) reflect.Value {
	v := unsafeReflectValue{
		typ:  cachedABIType,
		ptr:  value,
		flag: uintptr(kind),
	}
	return *(*reflect.Value)(unsafe.Pointer(&v))
}

// structFieldUnsafeBase is the safe-lane stub that always declines.
//
// The unsafe lane provides the real base-pointer resolver for struct-field handlers. Both
// arguments share the unsafe signature and are not read here.
//
// Takes registers (*Registers) which would provide the general bank.
// Takes generalRegister (uint8) which would index the receiver.
//
// Returns (nil, false) unconditionally.
func structFieldUnsafeBase(_ *Registers, _ uint8) (unsafe.Pointer, bool) {
	return nil, false
}

// stampEmbeddedReadOnly returns v unchanged in the safe build.
//
// Takes v (reflect.Value) which is the field value.
//
// Returns reflect.Value which is v.
func stampEmbeddedReadOnly(v reflect.Value) reflect.Value { return v }
