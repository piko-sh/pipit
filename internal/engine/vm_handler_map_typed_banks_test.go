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

type namedStringMapKey string

func TestTypedMapReadsHitTheConcreteFastPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler opcodeHandler
		source  any
		load    func(*Registers)
		inspect func(*testing.T, *Registers)
	}{
		{
			name: "an int-keyed int map", handler: handleMapGetIntInt, source: map[int]int{1: 10},
			load:    func(r *Registers) { r.Ints[2] = 1 },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(10), r.Ints[0]) },
		},
		{
			name: "an int64-keyed int64 map", handler: handleMapGetIntInt, source: map[int64]int64{1: 10},
			load:    func(r *Registers) { r.Ints[2] = 1 },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(10), r.Ints[0]) },
		},
		{
			name: "a missing int key yields the zero value", handler: handleMapGetIntInt, source: map[int]int{},
			load:    func(r *Registers) { r.Ints[2] = 9 },
			inspect: func(t *testing.T, r *Registers) { require.Zero(t, r.Ints[0]) },
		},
		{
			name: "a string-keyed int map", handler: handleMapGetStringInt, source: map[string]int{"a": 3},
			load:    func(r *Registers) { r.Strings[2] = "a" },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(3), r.Ints[0]) },
		},
		{
			name: "a named string key falls to the reflect path", handler: handleMapGetStringInt,
			source:  map[namedStringMapKey]int{"a": 4},
			load:    func(r *Registers) { r.Strings[2] = "a" },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(4), r.Ints[0]) },
		},
		{
			name: "a string-keyed string map", handler: handleMapGetStringString, source: map[string]string{"a": "v"},
			load:    func(r *Registers) { r.Strings[2] = "a" },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, "v", r.Strings[0]) },
		},
		{
			name: "an int-keyed string map", handler: handleMapGetIntString, source: map[int]string{2: "v"},
			load:    func(r *Registers) { r.Ints[2] = 2 },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, "v", r.Strings[0]) },
		},
		{
			name: "an int-keyed interface map", handler: handleMapGetIntGeneral, source: map[int]any{2: "boxed"},
			load:    func(r *Registers) { r.Ints[2] = 2 },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, "boxed", r.General[0].Interface()) },
		},
		{
			name: "a string-keyed interface map", handler: handleMapGetStringGeneral, source: map[string]any{"a": 5},
			load:    func(r *Registers) { r.Strings[2] = "a" },
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, 5, r.General[0].Interface()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newStandardVM(t)
			registers.General[1] = reflect.ValueOf(tt.source)
			tt.load(registers)

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)))
			tt.inspect(t, registers)
		})
	}
}

func TestTypedMapWritesLandInTheSourceMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler opcodeHandler
		source  any
		load    func(*Registers)
		inspect func(*testing.T, any)
	}{
		{
			name: "an int-keyed int map", handler: handleMapSetIntInt, source: map[int]int{},
			load:    func(r *Registers) { r.Ints[1] = 1; r.Ints[2] = 10 },
			inspect: func(t *testing.T, m any) { require.Equal(t, 10, m.(map[int]int)[1]) },
		},
		{
			name: "an int64-keyed int64 map", handler: handleMapSetIntInt, source: map[int64]int64{},
			load:    func(r *Registers) { r.Ints[1] = 1; r.Ints[2] = 10 },
			inspect: func(t *testing.T, m any) { require.Equal(t, int64(10), m.(map[int64]int64)[1]) },
		},
		{
			name: "a string-keyed int map", handler: handleMapSetStringInt, source: map[string]int{},
			load:    func(r *Registers) { r.Strings[1] = "a"; r.Ints[2] = 3 },
			inspect: func(t *testing.T, m any) { require.Equal(t, 3, m.(map[string]int)["a"]) },
		},
		{
			name: "a string-keyed string map", handler: handleMapSetStringString, source: map[string]string{},
			load:    func(r *Registers) { r.Strings[1] = "a"; r.Strings[2] = "v" },
			inspect: func(t *testing.T, m any) { require.Equal(t, "v", m.(map[string]string)["a"]) },
		},
		{
			name: "an int-keyed string map", handler: handleMapSetIntString, source: map[int]string{},
			load:    func(r *Registers) { r.Ints[1] = 2; r.Strings[2] = "v" },
			inspect: func(t *testing.T, m any) { require.Equal(t, "v", m.(map[int]string)[2]) },
		},
		{
			name: "an int-keyed interface map", handler: handleMapSetIntGeneral, source: map[int]any{},
			load:    func(r *Registers) { r.Ints[1] = 2; r.General[2] = reflect.ValueOf("boxed") },
			inspect: func(t *testing.T, m any) { require.Equal(t, "boxed", m.(map[int]any)[2]) },
		},
		{
			name: "a string-keyed interface map", handler: handleMapSetStringGeneral, source: map[string]any{},
			load:    func(r *Registers) { r.Strings[1] = "a"; r.General[2] = reflect.ValueOf(6) },
			inspect: func(t *testing.T, m any) { require.Equal(t, 6, m.(map[string]any)["a"]) },
		},
		{
			name: "an int-keyed accumulate", handler: handleMapAddIntInt, source: map[int]int{1: 5},
			load:    func(r *Registers) { r.Ints[1] = 1; r.Ints[2] = 4 },
			inspect: func(t *testing.T, m any) { require.Equal(t, 9, m.(map[int]int)[1]) },
		},
		{
			name: "a string-keyed accumulate", handler: handleMapAddStringInt, source: map[string]int{"a": 5},
			load:    func(r *Registers) { r.Strings[1] = "a"; r.Ints[2] = 4 },
			inspect: func(t *testing.T, m any) { require.Equal(t, 9, m.(map[string]int)["a"]) },
		},
		{
			name: "an accumulate onto a missing key starts from zero", handler: handleMapAddStringInt,
			source:  map[string]int{},
			load:    func(r *Registers) { r.Strings[1] = "new"; r.Ints[2] = 4 },
			inspect: func(t *testing.T, m any) { require.Equal(t, 4, m.(map[string]int)["new"]) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newStandardVM(t)
			registers.General[0] = reflect.ValueOf(tt.source)
			tt.load(registers)

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)))
			tt.inspect(t, tt.source)
		})
	}
}

func TestTypedMapCommaOkReportsPresence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler opcodeHandler
		source  any
		load    func(*Registers)
		wantOk  int64
		inspect func(*testing.T, *Registers)
	}{
		{
			name: "an int-keyed int hit", handler: handleMapIndexOkIntInt, source: map[int]int{1: 10},
			load: func(r *Registers) { r.Ints[2] = 1 }, wantOk: 1,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(10), r.Ints[0]) },
		},
		{
			name: "an int-keyed int miss", handler: handleMapIndexOkIntInt, source: map[int]int{},
			load: func(r *Registers) { r.Ints[2] = 9 }, wantOk: 0,
			inspect: func(t *testing.T, r *Registers) { require.Zero(t, r.Ints[0]) },
		},
		{
			name: "an int64-keyed int64 hit", handler: handleMapIndexOkIntInt, source: map[int64]int64{1: 10},
			load: func(r *Registers) { r.Ints[2] = 1 }, wantOk: 1,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(10), r.Ints[0]) },
		},
		{
			name: "a named-key map falls to the reflect path", handler: handleMapIndexOkStringInt,
			source: map[namedStringMapKey]int{"a": 4},
			load:   func(r *Registers) { r.Strings[2] = "a" }, wantOk: 1,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(4), r.Ints[0]) },
		},
		{
			name: "a string-keyed int hit", handler: handleMapIndexOkStringInt, source: map[string]int{"a": 3},
			load: func(r *Registers) { r.Strings[2] = "a" }, wantOk: 1,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(3), r.Ints[0]) },
		},
		{
			name: "a string-keyed string hit", handler: handleMapIndexOkStringString,
			source: map[string]string{"a": "v"},
			load:   func(r *Registers) { r.Strings[2] = "a" }, wantOk: 1,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, "v", r.Strings[0]) },
		},
		{
			name: "a string-keyed string miss", handler: handleMapIndexOkStringString, source: map[string]string{},
			load: func(r *Registers) { r.Strings[2] = "a" }, wantOk: 0,
			inspect: func(t *testing.T, r *Registers) { require.Empty(t, r.Strings[0]) },
		},
		{
			name: "an int-keyed string hit", handler: handleMapIndexOkIntString, source: map[int]string{2: "v"},
			load: func(r *Registers) { r.Ints[2] = 2 }, wantOk: 1,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, "v", r.Strings[0]) },
		},
		{
			name: "an int-keyed interface hit", handler: handleMapIndexOkIntGeneral, source: map[int]any{2: "boxed"},
			load: func(r *Registers) { r.Ints[2] = 2 }, wantOk: 1,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, "boxed", r.General[0].Interface()) },
		},
		{
			name: "a string-keyed interface hit", handler: handleMapIndexOkStringGeneral,
			source: map[string]any{"a": 5},
			load:   func(r *Registers) { r.Strings[2] = "a" }, wantOk: 1,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, 5, r.General[0].Interface()) },
		},
		{
			name: "a string-keyed interface miss", handler: handleMapIndexOkStringGeneral, source: map[string]any{},
			load: func(r *Registers) { r.Strings[2] = "a" }, wantOk: 0,
			inspect: func(t *testing.T, r *Registers) {
				require.True(t, r.General[0].IsValid(), "a miss writes the element type's zero value, not nothing")
				require.Nil(t, r.General[0].Interface())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newExtWordFrame(t, op(4, 0, 0))
			registers.General[1] = reflect.ValueOf(tt.source)
			tt.load(registers)

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)))
			require.Equal(t, 2, frame.ProgramCounter, "the ok-register word must be stepped over")
			require.Equal(t, tt.wantOk, registers.Ints[4])
			tt.inspect(t, registers)
		})
	}
}

func TestTypedMapWriteToANilMapSurfacesAsAnInterpretedPanic(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newStandardVM(t)
	registers.General[0] = reflect.ValueOf(map[int]int(nil))
	registers.Ints[1] = 1
	registers.Ints[2] = 2

	got := handleMapSetIntInt(vm, frame, registers, op(0, 1, 2))
	requireRuntimePanic(t, vm, got, "nil map")
}

func TestTypedMapReadFromAnUnsetRegisterIsAnInvariantBreak(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newStandardVM(t)

	require.Panics(t, func() { handleMapGetIntInt(vm, frame, registers, op(0, 1, 2)) })
}

type namedIntMapKey int

type namedIntMapValue int

func TestTypedMapAccumulateFallsBackForNamedTypes(t *testing.T) {
	t.Parallel()

	t.Run("an int-keyed map of named types accumulates through reflect", func(t *testing.T) {
		t.Parallel()

		source := map[namedIntMapKey]namedIntMapValue{1: 5}
		vm, frame, registers := newStandardVM(t)
		registers.General[0] = reflect.ValueOf(source)
		registers.Ints[1] = 1
		registers.Ints[2] = 4

		require.Equal(t, opContinue, handleMapAddIntInt(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, namedIntMapValue(9), source[1])
	})

	t.Run("an absent int key starts from zero", func(t *testing.T) {
		t.Parallel()

		source := map[namedIntMapKey]namedIntMapValue{}
		vm, frame, registers := newStandardVM(t)
		registers.General[0] = reflect.ValueOf(source)
		registers.Ints[1] = 2
		registers.Ints[2] = 7

		require.Equal(t, opContinue, handleMapAddIntInt(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, namedIntMapValue(7), source[2])
	})

	t.Run("an int64-keyed map takes the widened fast path", func(t *testing.T) {
		t.Parallel()

		source := map[int64]int64{1: 5}
		vm, frame, registers := newStandardVM(t)
		registers.General[0] = reflect.ValueOf(source)
		registers.Ints[1] = 1
		registers.Ints[2] = 4

		require.Equal(t, opContinue, handleMapAddIntInt(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, int64(9), source[1])
	})

	t.Run("a string-keyed map of named types accumulates through reflect", func(t *testing.T) {
		t.Parallel()

		source := map[namedStringMapKey]namedIntMapValue{"a": 5}
		vm, frame, registers := newStandardVM(t)
		registers.General[0] = reflect.ValueOf(source)
		registers.Strings[1] = "a"
		registers.Ints[2] = 4

		require.Equal(t, opContinue, handleMapAddStringInt(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, namedIntMapValue(9), source["a"])
	})

	t.Run("a string-keyed int64 map takes the widened fast path", func(t *testing.T) {
		t.Parallel()

		source := map[string]int64{"a": 5}
		vm, frame, registers := newStandardVM(t)
		registers.General[0] = reflect.ValueOf(source)
		registers.Strings[1] = "a"
		registers.Ints[2] = 4

		require.Equal(t, opContinue, handleMapAddStringInt(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, int64(9), source["a"])
	})

	t.Run("an unset map register is an invariant break", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)

		require.Panics(t, func() { handleMapAddIntInt(vm, frame, registers, op(0, 1, 2)) })
		require.Panics(t, func() { handleMapAddStringInt(vm, frame, registers, op(0, 1, 2)) })
	})

	t.Run("a register holding something other than a map is refused", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[0] = reflect.ValueOf(4)

		require.Equal(t, opPanicError, handleMapAddIntInt(vm, frame, registers, op(0, 1, 2)))
		require.Error(t, vm.evalError)
	})
}
