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

	"pipit.sh/pipit/internal/link"
	"pipit.sh/pipit/internal/symtab"
)

func TestAnErasedPointerIsRetypedAtItsAddress(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)
	vm.nativeBackedErasurePointees = map[reflect.Type]struct{}{reflect.TypeFor[struct{}](): {}}
	text := "kept"
	noteErasedPointer(reflect.ValueOf(&text))
	erased := reflect.NewAt(reflect.TypeFor[struct{}](), reflect.ValueOf(&text).UnsafePointer())
	retyped, ok := retypeErasedPointer(vm, erased, reflect.TypeFor[*string]())
	require.True(t, ok)
	require.Same(t, &text, retyped.Interface().(*string), "the pointer keeps the address the program stored")

	nilRetyped, ok := retypeErasedPointer(vm, reflect.Zero(reflect.TypeFor[*struct{}]()), reflect.TypeFor[*string]())
	require.True(t, ok)
	require.True(t, nilRetyped.IsNil())

	_, ok = retypeErasedPointer(vm, reflect.ValueOf(new(int)), reflect.TypeFor[*string]())
	require.False(t, ok, "a pointer outside the erasure boundary is left to the ordinary conversion")
}

func TestAnErasedPointerIsNotRetypedAtAnotherLayout(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)
	vm.nativeBackedErasurePointees = map[reflect.Type]struct{}{reflect.TypeFor[struct{}](): {}}
	small := int64(7)
	noteErasedPointer(reflect.ValueOf(&small))
	erased := reflect.NewAt(reflect.TypeFor[struct{}](), reflect.ValueOf(&small).UnsafePointer())

	_, ok := retypeErasedPointer(vm, erased, reflect.TypeFor[*[64]int64]())
	require.False(t, ok, "a type table naming a larger type cannot read past the stored object")
	_, ok = retypeErasedPointer(vm, erased, reflect.TypeFor[**int64]())
	require.False(t, ok, "nor read its words as pointers")

	unrecorded := int64(8)
	_, ok = retypeErasedPointer(vm, reflect.NewAt(reflect.TypeFor[struct{}](), reflect.ValueOf(&unrecorded).UnsafePointer()), reflect.TypeFor[*int64]())
	require.False(t, ok, "a pointer the program never stored is not retyped")

	retyped, ok := retypeErasedPointer(vm, erased, reflect.TypeFor[*int64]())
	require.True(t, ok)
	require.Equal(t, int64(7), *retyped.Interface().(*int64))
}

func TestTheErasurePointeeSetIsSharedPerRegistry(t *testing.T) {
	t.Parallel()

	registry := symtab.NewSymbolRegistry(nil)
	registry.RegisterPackage("example.com/erased", map[string]reflect.Value{
		"Box": reflect.ValueOf(link.NativeBackedGenericType{Name: "Box", ErasedType: reflect.TypeFor[struct{ value int }](), ErasureArgs: []reflect.Type{reflect.TypeFor[struct{}]()}, TypeArgCount: 1}),
	})
	first := NewVM(t.Context(), NewGlobalStore(), registry)
	second := NewVM(t.Context(), NewGlobalStore(), registry)

	pointees := first.erasurePointees()
	require.Contains(t, pointees, reflect.TypeFor[struct{}]())
	require.Equal(t, reflect.ValueOf(pointees).Pointer(), reflect.ValueOf(second.erasurePointees()).Pointer(),
		"a second VM on the registry reuses the set rather than scanning every symbol again")

	registry.RegisterPackage("example.com/more", map[string]reflect.Value{
		"Other": reflect.ValueOf(link.NativeBackedGenericType{Name: "Other", ErasedType: reflect.TypeFor[struct{ other int }](), ErasureArgs: nil, TypeArgCount: 1}),
	})
	third := NewVM(t.Context(), NewGlobalStore(), registry)
	require.Contains(t, third.erasurePointees(), reflect.TypeFor[struct{ other int }](), "a registry that gained packages is scanned again")
}

func TestAPointerPassedAcrossTheErasureIsRetypedOnTheWayBack(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)
	vm.nativeBackedErasurePointees = map[reflect.Type]struct{}{reflect.TypeFor[struct{}](): {}}
	value := [3]int64{1, 2, 3}
	erased, ok := reinterpretPointerArgument(vm, reflect.ValueOf(&value), reflect.TypeFor[*struct{}]())
	require.True(t, ok)

	retyped, ok := retypeErasedPointer(vm, erased, reflect.TypeFor[*[3]int64]())
	require.True(t, ok, "the pointer the program passed in was recorded at its own type")
	require.Same(t, &value, retyped.Interface().(*[3]int64))
}
