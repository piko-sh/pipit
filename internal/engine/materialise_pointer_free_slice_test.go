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
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

type pointerFreePair struct {
	A int32
	B float64
}

func TestMaterialisePointerFreeSliceCopiesInOneAllocation(t *testing.T) {
	tests := []struct {
		name   string
		source any
	}{
		{name: "int64 elements", source: []int64{1, 2, 3, 4, 5}},
		{name: "struct elements", source: []pointerFreePair{{A: 1, B: 1.5}, {A: 2, B: 2.5}, {A: 3, B: 3.5}}},
		{name: "complex128 elements", source: []complex128{complex(1, 2), complex(3, 4)}},
		{name: "empty slice with capacity", source: make([]int64, 0, 4)},
		{name: "empty slice without capacity", source: []int64{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := reflect.ValueOf(tt.source)
			n := source.Len()
			capacity := min(source.Cap(), 2*n)
			var copied reflect.Value
			allocations := testing.AllocsPerRun(20, func() {
				copied = materialisePointerFreeSlice(source, source.Type(), n, capacity)
			})
			require.Equal(t, 1.0, allocations, "one block must hold the header and the elements")
			require.Equal(t, source.Type(), copied.Type())
			require.Equal(t, n, copied.Len())
			require.Equal(t, capacity, copied.Cap())
			require.Equal(t, tt.source, copied.Interface())
			if n > 0 {
				require.NotEqual(t, source.UnsafePointer(), copied.UnsafePointer(), "the copy must not alias the source")
			}
		})
	}
}

func TestMaterialisePointerFreeSliceSurvivesCollection(t *testing.T) {
	source := reflect.ValueOf([]pointerFreePair{{A: 7, B: 7.5}, {A: 8, B: 8.5}, {A: 9, B: 9.5}})
	kept := keepOnlyTheHeaderCopy(source)
	for range 4 {
		runtime.GC()
	}
	require.Equal(t, []pointerFreePair{{A: 7, B: 7.5}, {A: 8, B: 8.5}, {A: 9, B: 9.5}}, kept)
}

func keepOnlyTheHeaderCopy(source reflect.Value) []pointerFreePair {
	copied := materialisePointerFreeSlice(source, source.Type(), source.Len(), source.Cap())
	kept, _ := reflect.TypeAssert[[]pointerFreePair](copied)
	return kept
}
