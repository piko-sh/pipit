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

// safeTypeFrontSlots is the number of slots in the registry's lock-free front.
const safeTypeFrontSlots = 1 << 10

var (
	// safeTypeRegistryMu guards safeTypeRegistryByPtr.
	safeTypeRegistryMu sync.RWMutex

	// safeTypeRegistryByPtr recovers the registered entry from a token.
	safeTypeRegistryByPtr = make(map[unsafe.Pointer]*safeTypeEntry)

	// safeTypeFront holds, per slot, the entry of the token last looked up there.
	safeTypeFront [safeTypeFrontSlots]frontPointer[safeTypeEntry]
)

// safeTypeEntry pairs a token with the reflect.Type it was minted from. An entry is never
// changed once it is registered, so the front can hand it out without a lock.
type safeTypeEntry struct {
	// t is the reflect.Type the token stands for. Holding it keeps the type reachable.
	t reflect.Type

	// token is the *abi.Type pointer read from t's interface header.
	token unsafe.Pointer
}

// ResetSafeTypeRegistryForTest is a no-op for the safe build.
func ResetSafeTypeRegistryForTest() {}

// ReflectValuePtr extracts the storage address of a reflect.Value via the public API.
//
// For non-addressable direct-iface kinds it returns the held value via UnsafePointer().
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
		return unsafe.Pointer(v.UnsafeAddr())
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
// interface representation being stable.
//
// Takes t (reflect.Type) which the caller pre-derives.
//
// Returns nil for a nil t. Otherwise it returns the underlying *abi.Type pointer.
//
// Safe for concurrent use. A registered token is found without a lock, and registration
// is guarded by safeTypeRegistryMu.
func reflectValueABIType(t reflect.Type) unsafe.Pointer {
	if t == nil {
		return nil
	}
	abiPtr := (*emptyInterface)(unsafe.Pointer(&t)).data
	if safeTypeEntryFor(abiPtr) != nil {
		return abiPtr
	}
	safeTypeRegistryMu.Lock()
	entry := safeTypeRegistryByPtr[abiPtr]
	if entry == nil {
		entry = &safeTypeEntry{t: t, token: abiPtr}
		safeTypeRegistryByPtr[abiPtr] = entry
	}
	safeTypeRegistryMu.Unlock()
	safeTypeFrontSlot(abiPtr).Store(entry)
	return abiPtr
}

// safeLookupType recovers the reflect.Type a token was minted from.
//
// Takes token (unsafe.Pointer) which is the *abi.Type pointer previously returned by
// reflectValueABIType.
//
// Returns reflect.Type which is the token's type, or nil when it is not registered.
func safeLookupType(token unsafe.Pointer) reflect.Type {
	if token == nil {
		return nil
	}
	if entry := safeTypeEntryFor(token); entry != nil {
		return entry.t
	}
	return nil
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

// stampEmbeddedReadOnly returns v unchanged in the safe build.
//
// Takes v (reflect.Value) which is the field value.
//
// Returns reflect.Value which is v.
func stampEmbeddedReadOnly(v reflect.Value) reflect.Value { return v }

// safeTypeFrontSlot returns the front slot a token maps to.
//
// Takes token (unsafe.Pointer) which is a type token.
//
// Returns *frontPointer[safeTypeEntry] which is its slot.
func safeTypeFrontSlot(token unsafe.Pointer) *frontPointer[safeTypeEntry] {
	address := uintptr(token)
	return &safeTypeFront[(address>>4^address>>14)&(safeTypeFrontSlots-1)]
}

// safeTypeEntryFor returns the registered entry for a token.
//
// Takes token (unsafe.Pointer) which is a non-nil type token.
//
// Returns *safeTypeEntry which is nil when the token was never registered.
//
// Safe for concurrent use. The front is read and written atomically, and the map is read
// under safeTypeRegistryMu.
func safeTypeEntryFor(token unsafe.Pointer) *safeTypeEntry {
	slot := safeTypeFrontSlot(token)
	if entry := slot.Load(); entry != nil && entry.token == token {
		return entry
	}
	safeTypeRegistryMu.RLock()
	entry := safeTypeRegistryByPtr[token]
	safeTypeRegistryMu.RUnlock()
	if entry != nil {
		slot.Store(entry)
	}
	return entry
}
