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
	"unsafe"
)

// launderReadOnlyFieldValue strips reflect's read-only taint from a value read out of a
// synthesised struct field before it escapes into the general register bank.
//
// Takes value (reflect.Value) which is the freshly read field (or field-derived) value.
//
// Returns an equivalent reflect.Value that Interface and Set accept.
// Returns value unchanged when it is invalid or already untainted.
func launderReadOnlyFieldValue(value reflect.Value) reflect.Value {
	if !value.IsValid() || value.CanInterface() {
		return value
	}
	if value.CanAddr() {
		return reflect.NewAt(value.Type(), unsafe.Pointer(value.UnsafeAddr())).Elem()
	}
	return detachReadOnlyValue(value)
}

// detachReadOnlyScalar copies a read-only value whose whole content is reachable through
// a single accessor, so detachReadOnlyValue is left with the composite kinds.
//
// Takes value (reflect.Value) which is the read-only value to copy.
//
// Returns the detached copy and true, or the zero reflect.Value and false when the kind
// is not a scalar one.
func detachReadOnlyScalar(value reflect.Value) (reflect.Value, bool) {
	detached := reflect.New(value.Type()).Elem()
	switch value.Kind() {
	case reflect.Bool:
		detached.SetBool(value.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		detached.SetInt(value.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		detached.SetUint(value.Uint())
	case reflect.Float32, reflect.Float64:
		detached.SetFloat(value.Float())
	case reflect.Complex64, reflect.Complex128:
		detached.SetComplex(value.Complex())
	case reflect.String:
		detached.SetString(value.String())
	default:
		return reflect.Value{}, false
	}
	return detached, true
}

// detachReadOnlyValue copies a non-addressable read-only value into a fresh reflect.Value
// with clean flags.
//
// reflect.Value.Set refuses read-only sources, so the copy is assembled through the
// per-kind accessors (Int, String, UnsafePointer, ...) which tolerate the read-only flag.
// Struct and array contents are copied element-wise with the same treatment; pointers are
// rebuilt with reflect.NewAt on the pointee (matching snapshotPointerLeaf); slices are
// rebuilt from a detached header aliasing the same backing array; maps and channels are
// rebuilt from their reference word so they still alias the same container.
//
// Takes value (reflect.Value) which the caller guarantees is valid and non-addressable.
//
// Returns the detached copy.
// Returns value unchanged for kinds that cannot be rebuilt through the public reflect API
// (funcs expose only their code pointer), preserving the pre-existing behaviour for
// those.
func detachReadOnlyValue(value reflect.Value) reflect.Value {
	if detached, ok := detachReadOnlyScalar(value); ok {
		return detached
	}
	valueType := value.Type()
	switch value.Kind() {
	case reflect.Pointer:
		pointer := value.UnsafePointer()
		if pointer == nil {
			return reflect.New(valueType).Elem()
		}
		return reflect.NewAt(valueType.Elem(), pointer)
	case reflect.Map, reflect.Chan, reflect.UnsafePointer:
		return unsafeDirectIfaceKindValue(reflectValueABIType(valueType), value.UnsafePointer(), value.Kind())
	case reflect.Slice:
		header := &snapshotSliceHeader{
			Data: value.UnsafePointer(),
			Len:  value.Len(),
			Cap:  value.Cap(),
		}
		return unsafeNewAt(reflectValueABIType(valueType), unsafe.Pointer(header), reflect.Slice)
	case reflect.Interface:
		detached := reflect.New(valueType).Elem()
		if !value.IsNil() {
			detached.Set(launderReadOnlyFieldValue(value.Elem()))
		}
		return detached
	case reflect.Struct:
		detached := reflect.New(valueType).Elem()
		for i := range valueType.NumField() {
			destination := detached.Field(i)
			if !destination.CanSet() {
				destination = reflect.NewAt(destination.Type(), unsafe.Pointer(destination.UnsafeAddr())).Elem()
			}
			destination.Set(launderReadOnlyFieldValue(value.Field(i)))
		}
		return detached
	case reflect.Array:
		detached := reflect.New(valueType).Elem()
		for i := range value.Len() {
			detached.Index(i).Set(launderReadOnlyFieldValue(value.Index(i)))
		}
		return detached
	default:
		return value
	}
}
