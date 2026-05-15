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

package core

import (
	"fmt"
	"math/rand/v2"
	"reflect"

	"pipit.sh/pipit/internal/link"
)

// wrappedRandV2RandN is the non-generic sibling for (*math/rand/v2.Rand).N, the only
// generic method in the Go 1.27 standard library.
//
// reflect cannot represent an uninstantiated generic method, so MethodByName("N") yields
// an invalid Value and there is no channel through which to hand reflect the
// instantiation. The interpreter resolves the type argument at compile time and passes it
// here instead, and this switch selects the matching instantiation of the real method.
//
// The result is converted back to typeArg rather than returned as the basic type it was
// computed at, because N's constraint is written with ~ and so admits named types: an
// interpreted "type Tag int32" must see a Tag come back, not an int32.
//
// Takes typeArg (reflect.Type) which is the type argument N was instantiated at.
// Takes receiver (*rand.Rand) which is the source the number is drawn from.
// Takes bound (any) which is N's argument, the exclusive upper bound.
//
// Returns the pseudo-random value, typed as typeArg.
//
// Panics when typeArg is not an integer type, mirroring the constraint the type checker
// has already enforced, and when bound is not positive, mirroring N itself.
func wrappedRandV2RandN(typeArg reflect.Type, receiver *rand.Rand, bound any) any {
	value := reflect.ValueOf(bound)
	if !value.IsValid() {
		panic("math/rand/v2: N called with an invalid bound")
	}

	var drawn reflect.Value
	//nolint:gosec // bound has type typeArg
	switch typeArg.Kind() {
	case reflect.Int:
		drawn = reflect.ValueOf(receiver.N(int(value.Int())))
	case reflect.Int8:
		drawn = reflect.ValueOf(receiver.N(int8(value.Int())))
	case reflect.Int16:
		drawn = reflect.ValueOf(receiver.N(int16(value.Int())))
	case reflect.Int32:
		drawn = reflect.ValueOf(receiver.N(int32(value.Int())))
	case reflect.Int64:
		drawn = reflect.ValueOf(receiver.N(value.Int()))
	case reflect.Uint:
		drawn = reflect.ValueOf(receiver.N(uint(value.Uint())))
	case reflect.Uint8:
		drawn = reflect.ValueOf(receiver.N(uint8(value.Uint())))
	case reflect.Uint16:
		drawn = reflect.ValueOf(receiver.N(uint16(value.Uint())))
	case reflect.Uint32:
		drawn = reflect.ValueOf(receiver.N(uint32(value.Uint())))
	case reflect.Uint64:
		drawn = reflect.ValueOf(receiver.N(value.Uint()))
	case reflect.Uintptr:
		drawn = reflect.ValueOf(receiver.N(uintptr(value.Uint())))
	default:
		panic(fmt.Sprintf("math/rand/v2: N instantiated at non-integer type %s", typeArg))
	}

	return drawn.Convert(typeArg).Interface()
}

func init() {
	const packagePath = "math/rand/v2"
	if _, ok := Symbols[packagePath]; ok {
		Symbols[packagePath][link.MethodSymbolKey("Rand", "N")] =
			reflect.ValueOf(link.WrapMethod(1, wrappedRandV2RandN))
	}
}
