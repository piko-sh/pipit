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

	"pipit.sh/pipit/internal/link"
)

var (
	// LinkedResultReflectValueType caches the reflect.Type descriptor for reflect.Value
	// itself. Used to identify parametric return slots in //piko:link sibling results.
	LinkedResultReflectValueType = reflect.TypeFor[reflect.Value]()

	// LinkedFunctionReflectType is cached once so the linked-call detection path does not
	// rebuild the type descriptor on every selector lookup.
	LinkedFunctionReflectType = reflect.TypeFor[link.LinkedFunction]()

	// LinkedMethodReflectType is cached for the same reason as LinkedFunctionReflectType,
	// for the Go 1.27 generic-method form.
	LinkedMethodReflectType = reflect.TypeFor[link.LinkedMethod]()
)
