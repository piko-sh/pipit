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

package typemap

import (
	"go/types"
	"reflect"
)

// TypeCache is one compile's memory of converted types (TypeToReflectCached).
//
// It keeps each named type's runtime type, so mutually recursive named types share one
// identity across calls, and the index of self-referential groups, so each type is walked
// once per compile however many named types reach it. After a conversion fails it keeps
// the failure, and every later conversion through it returns that failure at once rather
// than paying for work whose compile will fail. A TypeCache is not safe for concurrent
// use.
type TypeCache struct {
	// types maps each converted named type to its runtime type.
	types map[types.Type]reflect.Type

	// cycles finds the groups of self-referential named composite types.
	cycles *cycleIndex

	// failure is the first conversion failure, after which the cache converts nothing.
	failure error
}

// NewTypeCache returns an empty cache for one compile.
//
// Returns *TypeCache which holds nothing yet.
func NewTypeCache() *TypeCache {
	return &TypeCache{types: make(map[types.Type]reflect.Type), cycles: newCycleIndex(), failure: nil}
}

// Lookup returns the runtime type the cache holds for t.
//
// Takes t (types.Type) which is a converted type.
//
// Returns reflect.Type which is nil when the cache holds none.
func (cache *TypeCache) Lookup(t types.Type) reflect.Type {
	return cache.types[t]
}
