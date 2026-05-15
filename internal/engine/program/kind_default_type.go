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

package program

import (
	"reflect"

	"pipit.sh/pipit/internal/isa"
)

// KindDefaultReflectType returns the default reflect.Type for a given register kind, used
// to construct zero values when no result is available.
//
// Names the banks with a distinct default reflect type. Every other bank shares the `any`
// type the default returns.
//
// Takes k (isa.RegisterKind) which is the register kind to map.
//
// Returns reflect.Type which is the default Go type for that kind.
func KindDefaultReflectType(k isa.RegisterKind) reflect.Type {
	switch k {
	case isa.RegisterInt:
		return reflect.TypeFor[int]()
	case isa.RegisterFloat:
		return reflect.TypeFor[float64]()
	case isa.RegisterString:
		return reflect.TypeFor[string]()
	case isa.RegisterBool:
		return reflect.TypeFor[bool]()
	case isa.RegisterUint:
		return reflect.TypeFor[uint64]()
	case isa.RegisterComplex:
		return reflect.TypeFor[complex128]()
	default:
		return reflect.TypeFor[any]()
	}
}
