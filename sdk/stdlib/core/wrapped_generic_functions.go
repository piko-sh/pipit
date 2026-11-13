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
	"reflect"

	"pipit.sh/pipit/internal/link"
	"pipit.sh/pipit/internal/stdlink"
)

// linkedGenericFunctions lists the standard library's generic functions the interpreter
// calls through their non-generic siblings in stdlink, by package path and name, with the
// number of type parameters each declares.
var linkedGenericFunctions = []struct {
	// sibling is the non-generic function the interpreter calls.
	sibling any

	// packagePath is the import path of the generic function's package.
	packagePath string

	// name is the generic function's name.
	name string

	// typeArgCount is the number of type parameters the generic function declares.
	typeArgCount int
}{
	{packagePath: "errors", name: "AsType", sibling: stdlink.ErrorsAsType, typeArgCount: 1},
	{packagePath: "math/rand/v2", name: "N", sibling: stdlink.RandN, typeArgCount: 1},
	{packagePath: "reflect", name: "TypeAssert", sibling: stdlink.ReflectTypeAssert, typeArgCount: 1},
	{packagePath: "reflect", name: "TypeFor", sibling: stdlink.ReflectTypeFor, typeArgCount: 1},
	{packagePath: "runtime", name: "AddCleanup", sibling: stdlink.RuntimeAddCleanup, typeArgCount: 2},
}

func init() {
	for _, function := range linkedGenericFunctions {
		symbols, ok := Symbols[function.packagePath]
		if !ok {
			continue
		}
		symbols[function.name] = reflect.ValueOf(link.Wrap(function.typeArgCount, function.sibling))
	}
}
