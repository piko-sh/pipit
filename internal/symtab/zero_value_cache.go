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

package symtab

import (
	"reflect"
	"sync"

	"pipit.sh/pipit/internal/isa"
)

const (
	// numRegisterKindEntries sizes kindZeroValueCache to cover the isa.RegisterKind enum
	// plus a fallback "any" slot at the end.
	numRegisterKindEntries = isa.NumRegisterKinds + 1
)

var (
	// kindZeroValueCache holds pre-computed reflect.Zero values per register kind, populated
	// at init. Each zero value is immutable, so sharing across callers is safe.
	kindZeroValueCache [numRegisterKindEntries]reflect.Value

	// typeZeroValueCache deduplicates reflect.Zero(t) calls per reflect.Type. Zero values
	// are immutable, so sharing one instance across callers is correct.
	typeZeroValueCache sync.Map
)

// ZeroValueForKind returns the cached zero value for a register kind.
//
// Replaces the reflect.Zero(KindDefaultReflectType(k)) pattern at every call site, which
// allocates a fresh 24-byte reflect.Value per call, with one shared allocation-free
// value.
//
// Takes k (isa.RegisterKind) which selects the kind.
//
// Returns the immutable cached zero reflect.Value for that kind. For kinds outside the
// scalar set (typed-slice banks, etc.) returns the any zero, matching
// KindDefaultReflectType's fallback.
func ZeroValueForKind(k isa.RegisterKind) reflect.Value {
	switch k {
	case isa.RegisterInt, isa.RegisterFloat, isa.RegisterString, isa.RegisterBool, isa.RegisterUint, isa.RegisterComplex:
		return kindZeroValueCache[k]
	default:
	}
	return kindZeroValueCache[isa.NumRegisterKinds]
}

// ZeroValueForType returns the cached reflect.Zero for type t.
//
// On cache miss, allocates via reflect.Zero and publishes. Used by hot-path handlers
// (HandleTypeAssert no-match, map index miss, channel recv default) where reflect.Zero
// would otherwise allocate per call.
//
// Takes t (reflect.Type) which is the type whose zero value is requested. May be nil; nil
// returns the invalid reflect.Value.
//
// Returns the cached immutable zero value for t.
func ZeroValueForType(t reflect.Type) reflect.Value {
	if t == nil {
		return reflect.Value{}
	}
	if cached, ok := typeZeroValueCache.Load(t); ok {
		if cachedValue, isValue := cached.(reflect.Value); isValue {
			return cachedValue
		}
	}
	zero := reflect.Zero(t)
	typeZeroValueCache.Store(t, zero)
	return zero
}

func init() {
	kindZeroValueCache[isa.RegisterInt] = reflect.Zero(reflect.TypeFor[int64]())
	kindZeroValueCache[isa.RegisterFloat] = reflect.Zero(reflect.TypeFor[float64]())
	kindZeroValueCache[isa.RegisterString] = reflect.Zero(reflect.TypeFor[string]())
	kindZeroValueCache[isa.RegisterBool] = reflect.Zero(reflect.TypeFor[bool]())
	kindZeroValueCache[isa.RegisterUint] = reflect.Zero(reflect.TypeFor[uint64]())
	kindZeroValueCache[isa.RegisterComplex] = reflect.Zero(reflect.TypeFor[complex128]())
	kindZeroValueCache[isa.NumRegisterKinds] = reflect.Zero(reflect.TypeFor[any]())
}
