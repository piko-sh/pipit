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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTheAddressOfAValueHasItsOwnPointerTypeAcrossManyTypes(t *testing.T) {
	t.Parallel()

	type first struct{ value int }
	type second struct{ value string }
	values := []reflect.Value{
		reflect.ValueOf(new(first)).Elem(),
		reflect.ValueOf(new(second)).Elem(),
		reflect.ValueOf(new(int)).Elem(),
		reflect.ValueOf(new([3]byte)).Elem(),
		reflect.ValueOf(new(float64)).Elem(),
		reflect.ValueOf(new(string)).Elem(),
	}
	vm, _, _ := newExtWordFrame(t, op(0, 0, 0))
	for range 3 {
		for _, value := range values {
			address := vm.cachedAddr(value)
			require.Equal(t, reflect.PointerTo(value.Type()), address.Type(),
				"more types are in play than the cache holds, so entries are replaced and found again")
			require.Equal(t, value.Addr().Pointer(), address.Pointer())
		}
	}
	held := map[reflect.Type]bool{}
	for _, pair := range vm.pointerTypeCache {
		held[pair.element] = true
	}
	require.Len(t, held, pointerTypeCacheSlots, "each miss replaces the oldest entry, so every slot holds a different type")
}
