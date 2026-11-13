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
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func pointerFreeFrontTypes(count int) []reflect.Type {
	elements := []reflect.Type{
		reflect.TypeFor[int](), reflect.TypeFor[uint8](), reflect.TypeFor[float64](),
		reflect.TypeFor[string](), reflect.TypeFor[*int](), reflect.TypeFor[[]int](),
		reflect.TypeFor[any](), reflect.TypeFor[map[int]int](), reflect.TypeFor[unsafe.Pointer](),
	}
	types := make([]reflect.Type, 0, 6+2*count*len(elements))
	types = append(types,
		reflect.TypeFor[struct{}](), reflect.TypeFor[[0]*int](), reflect.TypeFor[[0]string](),
		reflect.TypeFor[struct {
			A int
			B [0]*int
		}](), reflect.TypeFor[struct{ A, B int64 }](), reflect.TypeFor[complex128](),
	)
	for _, element := range elements {
		for length := range count {
			array := reflect.ArrayOf(length, element)
			types = append(types, array, reflect.StructOf([]reflect.StructField{
				{Name: "X", Type: reflect.TypeFor[int32]()},
				{Name: fmt.Sprintf("A%d", length), Type: array},
			}))
		}
	}
	return types
}

func TestTypeIsPointerFreeFrontAgreesWithTheTypeWalk(t *testing.T) {
	ResetTypeIsPointerFreeCacheForTest()
	t.Cleanup(ResetTypeIsPointerFreeCacheForTest)
	types := pointerFreeFrontTypes(200)
	require.Greater(t, len(types), 2*typeIsPointerFreeFrontSlots, "enough types to fill every slot twice")
	for round := range 3 {
		for _, typ := range types {
			require.Equal(t, computeTypeIsPointerFree(typ), typeIsPointerFree(typ), "round %d, %v", round, typ)
		}
		for _, t0 := range slices.Backward(types) {
			require.Equal(t, computeTypeIsPointerFree(t0), typeIsPointerFree(t0), "round %d reversed, %v", round, t0)
		}
	}
	require.False(t, typeIsPointerFree(nil))
	require.True(t, typeIsPointerFree(reflect.TypeFor[struct{}]()))
	require.False(t, typeIsPointerFree(reflect.TypeFor[[0]*int]()), "a zero-length array of pointers stays not pointer-free, as the type walk says")
}

func TestTypeIsPointerFreeFrontIsSafeForConcurrentUse(t *testing.T) {
	ResetTypeIsPointerFreeCacheForTest()
	t.Cleanup(ResetTypeIsPointerFreeCacheForTest)
	types := pointerFreeFrontTypes(120)
	want := make([]bool, len(types))
	for index, typ := range types {
		want[index] = computeTypeIsPointerFree(typ)
	}
	var group sync.WaitGroup
	failures := make(chan string, 8)
	for worker := range 8 {
		group.Go(func() {
			for step := range 4 * len(types) {
				index := (step*(2*worker+1) + worker) % len(types)
				if got := typeIsPointerFree(types[index]); got != want[index] {
					select {
					case failures <- fmt.Sprintf("worker %d: %v answered %v", worker, types[index], got):
					default:
					}
					return
				}
			}
		})
	}
	group.Wait()
	close(failures)
	var observed []string
	for failure := range failures {
		observed = append(observed, failure)
	}
	require.Empty(t, observed, "every worker sees the same pointer-free answers")
}
