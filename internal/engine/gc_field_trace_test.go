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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/mem"
)

type fieldHolder struct {
	Numbers []int64
	Label   string
}

type nestedHolder struct {
	Inner fieldHolder
	Any   any
}

func holdInFrame(vm *VM, value reflect.Value) {
	vm.CallStack = []CallFrame{{Registers: Registers{General: []reflect.Value{value}}}}
	vm.FramePointer = 0
}

func TestMinorGCTracesArenaBackingsThroughStructFields(t *testing.T) {
	if !arenaUsesUnsafeSlabs {
		t.Skip("safe build: slab retention semantics require the unsafe arena")
	}
	vm := newGCTestVM()
	arena := vm.Arena

	numbers := arena.AllocIntBacking(16)
	for i := range numbers {
		numbers[i] = int64(i)
	}
	buffer := arena.AllocStringBytes(64)
	copy(buffer, strings.Repeat("b", 64))
	label := mem.String(buffer)
	holdInFrame(vm, reflect.ValueOf(fieldHolder{Numbers: numbers, Label: label}))

	arena.GrowIntBackingSlab(InitialIntBackingSize)
	arena.GrowByteSlab(InitialByteSlabSize)
	require.Len(t, arena.oldIntBackings, 1)
	require.Len(t, arena.oldByteSlabs, 1)

	arena.MinorGC(vm)

	require.Len(t, arena.oldIntBackings, 1, "the int backing referenced from a struct field must survive")
	require.Len(t, arena.oldByteSlabs, 1, "the string referenced from a struct field must survive")
}

func TestMinorGCTracesNestedAndInterfaceFields(t *testing.T) {
	if !arenaUsesUnsafeSlabs {
		t.Skip("safe build: slab retention semantics require the unsafe arena")
	}
	vm := newGCTestVM()
	arena := vm.Arena

	numbers := arena.AllocIntBacking(16)
	buffer := arena.AllocStringBytes(64)
	copy(buffer, strings.Repeat("c", 64))
	label := mem.String(buffer)
	holder := nestedHolder{Inner: fieldHolder{Numbers: numbers}, Any: fieldHolder{Label: label}}
	holdInFrame(vm, reflect.ValueOf(holder))

	arena.GrowIntBackingSlab(InitialIntBackingSize)
	arena.GrowByteSlab(InitialByteSlabSize)

	arena.MinorGC(vm)

	require.Len(t, arena.oldIntBackings, 1, "a backing behind a nested field must survive")
	require.Len(t, arena.oldByteSlabs, 1, "a string behind an interface field must survive")
}

func TestMinorGCSkipsPointerFreeStructFields(t *testing.T) {
	vm := newGCTestVM()
	arena := vm.Arena
	var scalars [4096]int64
	holdInFrame(vm, reflect.ValueOf(scalars))
	arena.GrowIntBackingSlab(InitialIntBackingSize)

	arena.MinorGC(vm)

	require.Empty(t, arena.oldIntBackings, "nothing references the old backing")
}

type hiddenHolder struct {
	numbers []int64
	label   string
}

func TestMinorGCTracesUnexportedStructFields(t *testing.T) {
	if !arenaUsesUnsafeSlabs {
		t.Skip("safe build: slab retention semantics require the unsafe arena")
	}
	vm := newGCTestVM()
	arena := vm.Arena

	numbers := arena.AllocIntBacking(16)
	buffer := arena.AllocStringBytes(64)
	copy(buffer, strings.Repeat("d", 64))
	holdInFrame(vm, reflect.ValueOf(hiddenHolder{numbers: numbers, label: mem.String(buffer)}))

	arena.GrowIntBackingSlab(InitialIntBackingSize)
	arena.GrowByteSlab(InitialByteSlabSize)

	require.NotPanics(t, func() { arena.MinorGC(vm) })
	require.Len(t, arena.oldIntBackings, 1, "an int backing behind an unexported field must survive")
	require.Len(t, arena.oldByteSlabs, 1, "a string behind an unexported field must survive")
}
