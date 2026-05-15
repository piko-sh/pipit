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

//go:build safe

package engine

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

type safeEscapeProbe struct {
	N int
}

func TestSafeBuildStructLiteralEscapesGenericByteSlab(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()
	vm := &VM{Arena: arena}
	probeType := reflect.TypeFor[safeEscapeProbe]()

	resident := allocateStructLiteralValue(vm, probeType)
	require.True(t, arena.ownsBytePointer(ReflectValuePtr(resident)), "struct literal storage must be recognised as arena-owned")

	escaped := MaterialiseArenaValue(arena, resident)
	require.False(t, arena.ownsBytePointer(ReflectValuePtr(escaped)), "escaped struct must live on the heap")
	require.Equal(t, resident.Interface(), escaped.Interface())

	heapValue := reflect.New(probeType).Elem()
	require.False(t, arena.ownsBytePointer(ReflectValuePtr(heapValue)))
	require.Equal(t, heapValue, MaterialiseArenaValue(arena, heapValue), "heap structs pass through unchanged")
}
