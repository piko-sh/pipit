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

//go:build !safe

package engine

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestMaterialiseArenaValueDetachesScalarBoxes(t *testing.T) {
	t.Parallel()
	arena := GetRegisterArena()
	defer PutRegisterArena(arena)
	intSlot := arena.allocIntBox(1234567)
	floatSlot := arena.allocFloatBox(2.5)
	uintSlot := arena.allocUintBox(7654321)
	complexSlot := arena.allocComplexBox(complex(1, 2))
	boxes := []reflect.Value{
		unsafeReadOnlyValue(reflectValueABIType(reflect.TypeFor[int64]()), unsafe.Pointer(intSlot), reflect.Int64),
		unsafeReadOnlyValue(reflectValueABIType(reflect.TypeFor[float64]()), unsafe.Pointer(floatSlot), reflect.Float64),
		unsafeReadOnlyValue(reflectValueABIType(reflect.TypeFor[uint64]()), unsafe.Pointer(uintSlot), reflect.Uint64),
		unsafeReadOnlyValue(reflectValueABIType(reflect.TypeFor[complex128]()), unsafe.Pointer(complexSlot), reflect.Complex128),
	}
	for _, boxed := range boxes {
		require.True(t, arena.ownsScalarBox(ReflectValuePtr(boxed)), boxed.Kind().String())
		out := materialiseArenaValueUnconditional(arena, boxed)
		require.False(t, arena.ownsScalarBox(ReflectValuePtr(out)), boxed.Kind().String())
		require.Equal(t, boxed.Interface(), out.Interface(), boxed.Kind().String())
		var holder any
		reflect.ValueOf(&holder).Elem().Set(out)
		data := (*[2]unsafe.Pointer)(unsafe.Pointer(&holder))[1]
		require.False(t, arena.ownsScalarBox(data), boxed.Kind().String())
	}
	heapBoxed := reflect.ValueOf(int64(1234567))
	require.Equal(t, heapBoxed, materialiseArenaValueUnconditional(arena, heapBoxed))
}
