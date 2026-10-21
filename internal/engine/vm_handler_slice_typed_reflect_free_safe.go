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

//go:build safe

package engine

import (
	"reflect"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/isa"
)

// readStructElementUintFields declines direct field reads in safe builds.
//
// Returns bool which is always false.
func readStructElementUintFields(_ *CallFrame, _ *Registers, _ *reflect.Value, _ int64, _ []isa.Instruction, _ int) bool {
	return false
}

// byteSliceElements declines direct byte access in safe builds.
//
// Returns []byte which is always nil.
func byteSliceElements(_ *reflect.Value) []byte {
	return nil
}

// writableByteSliceElements declines every register, as byteSliceElements does.
//
// Returns []byte which is always nil.
func writableByteSliceElements(_ *reflect.Value) []byte {
	return nil
}

// directElement returns element index of a collection register that holds a slice, an
// array or a pointer to an array.
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
//
// Returns element (reflect.Value) which is the element.
// Returns ok (bool) which is false when the caller must take the general reflect route.
func directElement(collection reflect.Value, index int64) (element reflect.Value, ok bool) {
	kind := collection.Kind()
	if kind == reflect.Pointer {
		if collection.IsNil() {
			return reflect.Value{}, false
		}
		collection = collection.Elem()
		kind = collection.Kind()
	}
	if kind != reflect.Slice && kind != reflect.Array {
		return reflect.Value{}, false
	}
	if index < 0 || index >= int64(collection.Len()) {
		return reflect.Value{}, false
	}
	return collection.Index(int(index)), true
}

// isFloatKind reports whether kind is a floating-point kind.
//
// Takes kind (reflect.Kind) which is the kind to classify.
//
// Returns bool which is true for float32 and float64.
func isFloatKind(kind reflect.Kind) bool {
	return kind == reflect.Float32 || kind == reflect.Float64
}

// getElementInt64WithoutReflect reads a signed or unsigned integer element.
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
//
// Returns value (int64) which is the element.
// Returns ok (bool) which is false when the caller must take the general reflect route.
func getElementInt64WithoutReflect(collection reflect.Value, index int64) (value int64, ok bool) {
	element, ok := directElement(collection, index)
	switch {
	case !ok:
		return 0, false
	case isSignedIntKind(element.Kind()):
		return element.Int(), true
	case isUnsignedIntKind(element.Kind()):
		return safeconv.Uint64ToInt64Reinterpret(element.Uint()), true
	default:
		return 0, false
	}
}

// setElementInt64WithoutReflect stores an integer element.
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
// Takes value (int64) which is the value stored.
//
// Returns bool which is false when the caller must take the general reflect route.
func setElementInt64WithoutReflect(_ *RegisterArena, collection reflect.Value, index int64, value int64) bool {
	element, ok := directElement(collection, index)
	switch {
	case !ok || !element.CanSet():
		return false
	case isSignedIntKind(element.Kind()):
		element.SetInt(value)
		return true
	case isUnsignedIntKind(element.Kind()):
		element.SetUint(safeconv.Int64ToUint64Reinterpret(value))
		return true
	default:
		return false
	}
}

// getElementUint64WithoutReflect reads an unsigned integer element.
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
//
// Returns value (uint64) which is the element.
// Returns ok (bool) which is false when the caller must take the general reflect route.
func getElementUint64WithoutReflect(collection reflect.Value, index int64) (value uint64, ok bool) {
	element, ok := directElement(collection, index)
	if !ok || !isUnsignedIntKind(element.Kind()) {
		return 0, false
	}
	return element.Uint(), true
}

// setElementUint64WithoutReflect stores an unsigned integer element.
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
// Takes value (uint64) which is the value stored.
//
// Returns bool which is false when the caller must take the general reflect route.
func setElementUint64WithoutReflect(_ *RegisterArena, collection reflect.Value, index int64, value uint64) bool {
	element, ok := directElement(collection, index)
	if !ok || !isUnsignedIntKind(element.Kind()) || !element.CanSet() {
		return false
	}
	element.SetUint(value)
	return true
}

// getElementFloat64WithoutReflect reads a floating-point element.
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
//
// Returns value (float64) which is the element.
// Returns ok (bool) which is false when the caller must take the general reflect route.
func getElementFloat64WithoutReflect(collection reflect.Value, index int64) (value float64, ok bool) {
	element, ok := directElement(collection, index)
	if !ok || !isFloatKind(element.Kind()) {
		return 0, false
	}
	return element.Float(), true
}

// setElementFloat64WithoutReflect stores a floating-point element.
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
// Takes value (float64) which is the value stored.
//
// Returns bool which is false when the caller must take the general reflect route.
func setElementFloat64WithoutReflect(_ *RegisterArena, collection reflect.Value, index int64, value float64) bool {
	element, ok := directElement(collection, index)
	if !ok || !isFloatKind(element.Kind()) || !element.CanSet() {
		return false
	}
	element.SetFloat(value)
	return true
}

// getElementBoolWithoutReflect reads a boolean element.
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
//
// Returns value (bool) which is the element.
// Returns ok (bool) which is false when the caller must take the general reflect route.
func getElementBoolWithoutReflect(collection reflect.Value, index int64) (value bool, ok bool) {
	element, ok := directElement(collection, index)
	if !ok || element.Kind() != reflect.Bool {
		return false, false
	}
	return element.Bool(), true
}

// setElementBoolWithoutReflect stores a boolean element.
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
// Takes value (bool) which is the value stored.
//
// Returns bool which is false when the caller must take the general reflect route.
func setElementBoolWithoutReflect(_ *RegisterArena, collection reflect.Value, index int64, value bool) bool {
	element, ok := directElement(collection, index)
	if !ok || element.Kind() != reflect.Bool || !element.CanSet() {
		return false
	}
	element.SetBool(value)
	return true
}

// getElementStringWithoutReflect reads a string element.
//
// Takes collection (reflect.Value) which is the collection register.
// Takes index (int64) which is the element index.
//
// Returns value (string) which is the element.
// Returns ok (bool) which is false when the caller must take the general reflect route.
func getElementStringWithoutReflect(collection reflect.Value, index int64) (value string, ok bool) {
	element, ok := directElement(collection, index)
	if !ok || element.Kind() != reflect.String {
		return "", false
	}
	return element.String(), true
}
