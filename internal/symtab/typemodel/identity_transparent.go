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

package typemodel

import (
	"reflect"
)

var (
	// identityTransparentFunctionPointers holds host functions that compare or combine
	// values by identity. These must observe the source-level value rather than a cosmetic
	// interface adapter the interpreter built; otherwise the adapter pointer is compared
	// instead of the value it wraps.
	identityTransparentFunctionPointers = map[uintptr]struct{}{}
)

// RegisterIdentityTransparentFunction marks fn as a host function that observes a value's
// identity rather than consuming it behaviourally. The interpreter strips interface
// adapters from arguments before calling registered functions.
//
// Call before the first evaluation; the registry is read without a lock on the
// native-call path. Passing a nil or non-func value is ignored.
//
// Takes fn (any) which must be a func value.
func RegisterIdentityTransparentFunction(fn any) {
	if fn == nil {
		return
	}

	reflected := reflect.ValueOf(fn)
	if reflected.Kind() != reflect.Func {
		return
	}

	identityTransparentFunctionPointers[reflected.Pointer()] = struct{}{}
}

// IsIdentityTransparentFunction reports whether the function at ptr is registered as
// identity-transparent.
//
// Takes ptr (uintptr) which is the function pointer to check.
//
// Returns true when the function was registered via
// RegisterIdentityTransparentFunction().
func IsIdentityTransparentFunction(ptr uintptr) bool {
	_, ok := identityTransparentFunctionPointers[ptr]
	return ok
}

// IdentityTransparentFunctionCount reports how many functions are registered as
// identity-transparent.
//
// Returns int which is the number of registered functions.
func IdentityTransparentFunctionCount() int {
	return len(identityTransparentFunctionPointers)
}
