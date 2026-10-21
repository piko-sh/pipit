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
	"reflect"
	"unsafe"
)

// concreteMethodValue splits a method value reflect made with Value.Method into its
// receiver and its type's method function, when the receiver has a concrete type.
//
// Takes function (reflect.Value) which is the callee.
//
// Returns receiver (reflect.Value) which is the bound receiver.
// Returns method (reflect.Value) which is the receiver type's method function.
// Returns ok (bool) which is false when the callee is not such a method value.
func concreteMethodValue(function reflect.Value) (receiver, method reflect.Value, ok bool) {
	header := (*unsafeReflectValue)(unsafe.Pointer(&function))
	if header.flag&flagMethodBit == 0 || header.flag&licenceFlagRO != 0 || header.typ == nil {
		return reflect.Value{}, reflect.Value{}, false
	}
	kind := uintptr(*(*uint8)(unsafe.Add(header.typ, abiTypeKindOffset))) & flagKindMask
	if reflect.Kind(kind) == reflect.Interface {
		return reflect.Value{}, reflect.Value{}, false
	}
	receiverHeader := unsafeReflectValue{typ: header.typ, ptr: header.ptr, flag: header.flag&flagIndir | kind}
	receiver = *(*reflect.Value)(unsafe.Pointer(&receiverHeader))
	method, ok = cachedMethodFunction(receiver.Type(), int(header.flag>>flagMethodShift))
	if !ok {
		return reflect.Value{}, reflect.Value{}, false
	}
	return receiver, method, true
}
