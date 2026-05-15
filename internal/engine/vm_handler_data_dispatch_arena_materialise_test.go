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

type hostContainer struct {
	Label string
	Items []string
}

func TestMaterialiseHostArgumentsWalksTheHostContainers(t *testing.T) {
	t.Parallel()

	t.Run("no arena leaves every argument alone", func(t *testing.T) {
		t.Parallel()

		values := map[string]string{"k": "v"}

		require.NotPanics(t, func() { MaterialiseHostArguments(nil, []reflect.Value{reflect.ValueOf(values)}) })
		require.Equal(t, "v", values["k"])
	})

	t.Run("heap-backed containers survive the walk unchanged", func(t *testing.T) {
		t.Parallel()

		arena := newTestArena(t)
		mapped := map[string]string{"k": "v"}
		slice := []string{"a"}
		holder := &hostContainer{Label: "label", Items: []string{"b"}}
		array := [2]string{"c", "d"}

		MaterialiseHostArguments(arena, []reflect.Value{
			reflect.ValueOf(mapped),
			reflect.ValueOf(slice),
			reflect.ValueOf(holder),
			reflect.ValueOf(&array),
			reflect.Value{},
		})

		require.Equal(t, "v", mapped["k"])
		require.Equal(t, []string{"a"}, slice)
		require.Equal(t, "label", holder.Label)
		require.Equal(t, [2]string{"c", "d"}, array)
	})

	t.Run("a nil map and a nil pointer are left alone", func(t *testing.T) {
		t.Parallel()

		arena := newTestArena(t)

		require.NotPanics(t, func() {
			MaterialiseHostArguments(arena, []reflect.Value{
				reflect.ValueOf(map[string]string(nil)),
				reflect.ValueOf((*hostContainer)(nil)),
			})
		})
	})

	t.Run("an arena-backed string in a host map is detached", func(t *testing.T) {
		t.Parallel()

		arena := newTestArena(t)

		mapped := map[string]string{"k": arenaBytesToString(arena, []byte("borrowed"))}
		MaterialiseHostArguments(arena, []reflect.Value{reflect.ValueOf(mapped)})

		require.Equal(t, "borrowed", mapped["k"])
	})

	t.Run("an arena-backed string in a host slice is detached", func(t *testing.T) {
		t.Parallel()

		arena := newTestArena(t)
		borrowed := arenaBytesToString(arena, []byte("borrowed"))
		slice := []string{borrowed}

		MaterialiseHostArguments(arena, []reflect.Value{reflect.ValueOf(slice)})

		require.Equal(t, "borrowed", slice[0])
	})

	t.Run("an arena-backed string in a host struct field is detached", func(t *testing.T) {
		t.Parallel()

		arena := newTestArena(t)
		borrowed := arenaBytesToString(arena, []byte("borrowed"))
		holder := &hostContainer{Label: borrowed}

		MaterialiseHostArguments(arena, []reflect.Value{reflect.ValueOf(holder)})

		require.Equal(t, "borrowed", holder.Label)
	})
}

func TestArenaResidentCompositeKindNamesTheTwoThatStay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind reflect.Kind
		want bool
	}{
		{name: "a struct keeps the escape copy", kind: reflect.Struct, want: true},
		{name: "an array keeps the escape copy", kind: reflect.Array, want: true},
		{name: "a string does not", kind: reflect.String, want: false},
		{name: "a slice does not", kind: reflect.Slice, want: false},
		{name: "an int does not", kind: reflect.Int, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, arenaResidentCompositeKind(tt.kind))
		})
	}
}

func TestMaterialiseArenaValueDetachesArenaStrings(t *testing.T) {
	t.Parallel()

	t.Run("an arena string is copied out", func(t *testing.T) {
		t.Parallel()

		arena := newTestArena(t)
		borrowed := arenaBytesToString(arena, []byte("borrowed"))

		detached := materialiseArenaValueUnconditional(arena, reflect.ValueOf(borrowed))
		require.Equal(t, "borrowed", detached.Interface())
	})

	t.Run("a heap string is handed straight back", func(t *testing.T) {
		t.Parallel()

		arena := newTestArena(t)
		value := reflect.ValueOf("heap")

		require.Equal(t, "heap", materialiseArenaValueUnconditional(arena, value).Interface())
	})

	t.Run("the aliasing form keeps a slice's backing array", func(t *testing.T) {
		t.Parallel()

		arena := newTestArena(t)
		backing := []int{1, 2, 3}

		aliased := materialiseArenaValueAliasing(arena, reflect.ValueOf(backing))
		require.Equal(t, backing, aliased.Interface())
		require.Equal(t, reflect.ValueOf(backing).Pointer(), aliased.Pointer(),
			"a deferred body that writes through the captured slice must reach the caller's array")
	})

	t.Run("an interface value re-wraps its concrete value", func(t *testing.T) {
		t.Parallel()

		arena := newTestArena(t)
		borrowed := arenaBytesToString(arena, []byte("borrowed"))
		boxed := reflect.ValueOf(map[string]any{"k": borrowed}).MapIndex(reflect.ValueOf("k"))

		detached := materialiseArenaInterfaceValue(arena, boxed, false)
		require.Equal(t, "borrowed", detached.Interface())
	})

	t.Run("a nil interface is handed straight back", func(t *testing.T) {
		t.Parallel()

		arena := newTestArena(t)
		nilBoxed := reflect.ValueOf(map[string]any{"k": nil}).MapIndex(reflect.ValueOf("k"))

		require.Equal(t, nilBoxed, materialiseArenaInterfaceValue(arena, nilBoxed, false))
	})
}

func TestRewrapSliceHeaderOnHeapKeepsTheBackingArray(t *testing.T) {
	t.Parallel()

	backing := []int{1, 2, 3}

	rewrapped := rewrapSliceHeaderOnHeap(reflect.ValueOf(backing))
	require.Equal(t, backing, rewrapped.Interface())
	require.Equal(t, reflect.ValueOf(backing).Pointer(), rewrapped.Pointer())
	require.Equal(t, 3, rewrapped.Len())
	require.Equal(t, 3, rewrapped.Cap())
}

func TestMaterialiseArenaSliceAliasingWalksHeapHeaders(t *testing.T) {
	t.Parallel()

	arena := newTestArena(t)
	borrowed := arenaBytesToString(arena, []byte("borrowed"))
	slice := []string{borrowed}

	result := materialiseArenaSliceAliasing(arena, reflect.ValueOf(slice))
	require.Equal(t, []string{"borrowed"}, result.Interface(),
		"a heap-resident header still has to have its arena-backed elements walked")
}

func TestCopyReflectValueArenaProducesAnIndependentValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source any
	}{
		{name: "a pointer-free struct is copied into the arena", source: struct{ X, Y int }{1, 2}},
		{name: "a pointer-bearing struct still copies", source: hostContainer{Label: "label", Items: []string{"a"}}},
		{name: "a slice keeps its contents", source: []int{1, 2, 3}},
		{name: "a string keeps its bytes", source: "borrowed"},
		{name: "a map keeps its entries", source: map[string]int{"k": 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			arena := newTestArena(t)

			copied := copyReflectValueArena(arena, reflect.ValueOf(tt.source))

			require.Equal(t, tt.source, copied.Interface(),
				"a copy into the arena has to carry the same value, whatever the arena does with its bytes")
		})
	}
}

func TestConvertUnsafePointerBridgesBothDirections(t *testing.T) {
	t.Parallel()

	value := 7
	pointer := reflect.ValueOf(&value)

	t.Run("a typed pointer becomes an unsafe pointer", func(t *testing.T) {
		t.Parallel()

		converted := convertUnsafePointer(pointer, unsafePointerType)
		require.Equal(t, reflect.UnsafePointer, converted.Kind())
		require.Equal(t, pointer.Pointer(), converted.Pointer())
	})

	t.Run("an unsafe pointer becomes a typed pointer", func(t *testing.T) {
		t.Parallel()

		asUnsafe := convertUnsafePointer(pointer, unsafePointerType)

		converted := convertUnsafePointer(asUnsafe, reflect.TypeFor[*int]())
		require.Equal(t, reflect.Pointer, converted.Kind())
		require.Equal(t, 7, converted.Elem().Interface())
	})
}
