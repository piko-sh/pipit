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
	"reflect"
	"runtime"
	"sync/atomic"
	"unsafe"
)

// methodValueClassesMax bounds methodValueClasses.
const methodValueClassesMax = 4096

// methodValueFastSlots is the size of methodValueFast, a power of two.
const methodValueFastSlots = 256

// methodValueKey names a method by its receiver type and index.
type methodValueKey struct {
	// receiver is the reflected type that declares the method.
	receiver reflect.Type

	// index identifies the method within the receiver type's method set.
	index int
}

// methodValueClasses caches classifyMethodValue's answers, per registry, at most
// methodValueClassesMax of them, so the receiver types it holds are bounded; past the
// bound a method value is resolved without caching.
var methodValueClasses typedCache[methodValueClassKey, BorrowClass]

// methodValueClassCount counts methodValueClasses' entries.
var methodValueClassCount atomic.Int64

// methodValueClassKey is methodValueClasses' key.
type methodValueClassKey struct {
	// registry supplies the borrow classes used by this probe.
	registry *nativeBorrowRegistry

	// method identifies the receiver type and method being probed.
	method methodValueKey
}

// methodValueFastEntry holds one methodValueFast cache entry.
type methodValueFastEntry struct {
	// registry supplies the borrow classes used by this probe.
	registry *nativeBorrowRegistry

	// typ is the runtime type pointer of the reflected method value.
	typ unsafe.Pointer

	// flag holds the reflected method value's runtime flags.
	flag uintptr

	// class is the borrow class returned by the probe.
	class BorrowClass
}

// methodValueFast is a direct-mapped, lock-free cache in front of methodValueClasses.
var methodValueFast [methodValueFastSlots]atomic.Pointer[methodValueFastEntry]

// classifyMethodValue classes a method value reflect made by its method.
//
// Takes function (reflect.Value) which is a method value (its pointer is
// methodValueCallPC).
//
// Returns BorrowClass which is the method's class, or BorrowUnknown.
func (r *nativeBorrowRegistry) classifyMethodValue(function reflect.Value) BorrowClass {
	raw := (*unsafeReflectValue)(unsafe.Pointer(&function))
	if raw.flag&flagWrapperMethod == 0 || raw.typ == nil {
		return BorrowUnknown
	}
	slot := &methodValueFast[(uintptr(raw.typ)>>pointerAlignShift^raw.flag>>flagMethodShift)&(methodValueFastSlots-1)]
	if entry := slot.Load(); entry != nil && entry.registry == r && entry.typ == raw.typ && entry.flag == raw.flag>>flagMethodShift {
		return entry.class
	}
	class := r.classifyMethodValueSlow(raw)
	slot.Store(&methodValueFastEntry{registry: r, typ: raw.typ, flag: raw.flag >> 10, class: class})
	return class
}

// classifyMethodValueSlow is classifyMethodValue past its fast cache.
//
// Takes raw (*unsafeReflectValue) which is the method value's words.
//
// Returns BorrowClass which is the method's class, or BorrowUnknown.
func (r *nativeBorrowRegistry) classifyMethodValueSlow(raw *unsafeReflectValue) BorrowClass {
	var iface struct{ typ, data unsafe.Pointer }
	iface.typ = raw.typ
	receiver := reflect.TypeOf(*(*any)(unsafe.Pointer(&iface)))
	index := int(raw.flag >> 10)
	if receiver == nil || receiver.Kind() == reflect.Interface || index < 0 || index >= receiver.NumMethod() {
		return BorrowUnknown
	}
	key := methodValueClassKey{registry: r, method: methodValueKey{receiver: receiver, index: index}}
	if cached, ok := methodValueClasses.load(key); ok {
		return cached
	}
	class := BorrowUnknown
	method := receiver.Method(index)
	if info := runtime.FuncForPC(method.Func.Pointer()); info != nil {
		class = r.classes[info.Name()]
	}
	if methodValueClassCount.Load() < methodValueClassesMax {
		if _, loaded := methodValueClasses.loadOrStore(key, class); !loaded {
			methodValueClassCount.Add(1)
		}
	}
	return class
}
