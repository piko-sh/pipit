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
	"go/ast"
	"go/types"
	"reflect"

	"pipit.sh/pipit/internal/engine/program"
)

// fastPathArgumentGuards checks that interface arguments can pass directly to native
// code.
var fastPathArgumentGuards = [fastPathTagBytesArr64 + 1]func(site *program.CallSite, registers *Registers) bool{
	fastPathTagTypesTypeTypesType: interfaceArgumentFits[types.Type],
	fastPathTagTypesTypeBool:      interfaceArgumentFits[types.Type],
	fastPathTagAstExprAstExpr:     interfaceArgumentFits[ast.Expr],
}

// fastPathArgumentsFit reports whether a call's arguments suit the typed fast path of its
// tag.
//
// Takes tag (nativeFastPathTag) which is the native's classified tag.
// Takes site (*program.CallSite) which locates the arguments.
// Takes registers (*Registers) which hold them.
//
// Returns bool which is false when an argument must go through the reflective call.
func fastPathArgumentsFit(tag nativeFastPathTag, site *program.CallSite, registers *Registers) bool {
	if int(tag) >= len(fastPathArgumentGuards) || fastPathArgumentGuards[tag] == nil {
		return true
	}
	return fastPathArgumentGuards[tag](site, registers)
}

// interfaceArgumentFits reports whether the first argument of a call holds a value of the
// interface T, or none.
//
// Takes site (*program.CallSite) which locates the argument.
// Takes registers (*Registers) which hold it.
//
// Returns bool which is true when the argument can be passed as a T directly.
func interfaceArgumentFits[T any](site *program.CallSite, registers *Registers) bool {
	if len(site.Arguments) == 0 {
		return false
	}
	value := readAnyArg(registers, site.Arguments[0])
	if value == nil {
		return true
	}
	_, ok := value.(T)
	return ok
}

// nativeArgument converts a general-register argument to its native interface type.
//
// Takes value (any) which is the argument as read from its register.
//
// Returns T which is the argument, or T's zero value when value does not hold one.
func nativeArgument[T any](value any) T {
	converted, ok := value.(T)
	if !ok {
		var zero T
		return zero
	}
	return converted
}

// nativeInterfaceResult boxes an interface result as its dynamic value or a typed nil.
//
// Takes value (T) which is the result.
//
// Returns reflect.Value which is the value the register receives.
func nativeInterfaceResult[T any](value T) reflect.Value {
	if any(value) == nil {
		return reflect.Zero(reflect.TypeFor[T]())
	}
	return reflect.ValueOf(value)
}
