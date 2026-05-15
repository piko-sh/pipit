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
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

type backingPair struct{ X, Y int32 }

type pointerBearingPair struct {
	X     int32
	Label string
}

func TestArenaMakeSliceBackingCoversTheTypedSlabs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		reflectType reflect.Type
		wantArena   bool
	}{
		{name: "a byte slice", reflectType: reflect.TypeFor[[]byte](), wantArena: true},
		{name: "an int64 slice", reflectType: reflect.TypeFor[[]int64](), wantArena: true},
		{name: "a float64 slice", reflectType: reflect.TypeFor[[]float64](), wantArena: true},
		{name: "a bool slice", reflectType: reflect.TypeFor[[]bool](), wantArena: true},
		{name: "a uint64 slice", reflectType: reflect.TypeFor[[]uint64](), wantArena: true},
		{name: "a string slice", reflectType: reflect.TypeFor[[]string](), wantArena: true},
		{name: "an int32 slice", reflectType: reflect.TypeFor[[]int32](), wantArena: true},
		{name: "a uint32 slice", reflectType: reflect.TypeFor[[]uint32](), wantArena: true},
		{name: "a float32 slice", reflectType: reflect.TypeFor[[]float32](), wantArena: true},
		{name: "an int16 slice", reflectType: reflect.TypeFor[[]int16](), wantArena: true},
		{name: "a uint16 slice", reflectType: reflect.TypeFor[[]uint16](), wantArena: true},
		{name: "a pointer-free struct slice", reflectType: reflect.TypeFor[[]backingPair](), wantArena: true},
		{name: "a pointer-free array slice", reflectType: reflect.TypeFor[[][2]int32](), wantArena: true},
		{name: "a pointer-bearing struct slice declines", reflectType: reflect.TypeFor[[]pointerBearingPair]()},
		{name: "an int slice declines", reflectType: reflect.TypeFor[[]int]()},
		{name: "a pointer slice declines", reflectType: reflect.TypeFor[[]*int]()},
		{name: "a type that is not a slice declines", reflectType: reflect.TypeFor[int]()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, _, _ := newStandardVM(t)

			value, ok := arenaMakeSliceBacking(vm, tt.reflectType, 2, 4)
			require.Equal(t, tt.wantArena, ok)
			if !tt.wantArena {
				return
			}
			require.Equal(t, tt.reflectType, value.Type())
			require.Equal(t, 2, value.Len())
			require.Equal(t, 4, value.Cap())
			require.Equal(t, reflect.Zero(tt.reflectType.Elem()).Interface(), value.Index(0).Interface(),
				"an arena-backed make must hand back zeroed elements")
		})
	}
}

func TestArenaMakeStructSliceBackingFallsBackForAwkwardShapes(t *testing.T) {
	t.Parallel()

	t.Run("a zero capacity uses a plain make", func(t *testing.T) {
		t.Parallel()

		vm, _, _ := newStandardVM(t)

		value := arenaMakeStructSliceBacking(vm, reflect.TypeFor[[]backingPair](), 0, 0)
		require.Zero(t, value.Len())
		require.Zero(t, value.Cap())
	})

	t.Run("a populated capacity is zeroed and sized", func(t *testing.T) {
		t.Parallel()

		vm, _, _ := newStandardVM(t)

		value := arenaMakeStructSliceBacking(vm, reflect.TypeFor[[]backingPair](), 1, 3)
		require.Equal(t, 1, value.Len())
		require.Equal(t, 3, value.Cap())
		require.Equal(t, backingPair{}, value.Index(0).Interface())
	})
}

func TestClosureCallableValueLeavesNonClosuresAlone(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)
	value := reflect.ValueOf(3)

	require.Equal(t, value, closureCallableValue(vm, value))
}

func TestClosureCallableValueBuildsTheErasedSignature(t *testing.T) {
	t.Parallel()

	t.Run("a recorded signature is used as given", func(t *testing.T) {
		t.Parallel()

		vm, _, _ := newStandardVM(t)
		callee := doublingCallee()
		callee.SignatureReflectType = reflect.TypeFor[func(int) int]()

		callable := closureCallableValue(vm, reflect.ValueOf(&RuntimeClosure{Function: callee}))
		require.Equal(t, reflect.TypeFor[func(int) int](), callable.Type())
	})

	t.Run("a callee with no signature gets the erased shape", func(t *testing.T) {
		t.Parallel()

		vm, _, _ := newStandardVM(t)
		callee := doublingCallee()

		callable := closureCallableValue(vm, reflect.ValueOf(&RuntimeClosure{Function: callee}))
		require.Equal(t, reflect.Func, callable.Kind())
		require.Equal(t, 1, callable.Type().NumIn())
		require.Equal(t, reflect.TypeFor[any](), callable.Type().In(0))
		require.Equal(t, 1, callable.Type().NumOut())
	})

	t.Run("a variadic callee ends in a slice parameter", func(t *testing.T) {
		t.Parallel()

		vm, _, _ := newStandardVM(t)
		callee := doublingCallee()
		callee.IsVariadic = true

		callable := closureCallableValue(vm, reflect.ValueOf(&RuntimeClosure{Function: callee}))
		require.True(t, callable.Type().IsVariadic())
		require.Equal(t, reflect.TypeFor[[]any](), callable.Type().In(0))
	})
}

func TestBuildClosureErrorReturnsFillsTheSignature(t *testing.T) {
	t.Parallel()

	failure := errors.New("inner failed")

	tests := []struct {
		name       string
		targetType reflect.Type
		inspect    func(*testing.T, []reflect.Value)
	}{
		{
			name: "a signature with no results yields nothing", targetType: reflect.TypeFor[func()](),
			inspect: func(t *testing.T, returns []reflect.Value) { require.Nil(t, returns) },
		},
		{
			name: "a trailing error slot carries the failure", targetType: reflect.TypeFor[func() (int, error)](),
			inspect: func(t *testing.T, returns []reflect.Value) {
				require.Len(t, returns, 2)
				require.Equal(t, 0, returns[0].Interface())
				require.ErrorIs(t, returns[1].Interface().(error), failure)
			},
		},
		{
			name: "a signature with no error slot yields zero values", targetType: reflect.TypeFor[func() (int, string)](),
			inspect: func(t *testing.T, returns []reflect.Value) {
				require.Len(t, returns, 2)
				require.Equal(t, 0, returns[0].Interface())
				require.Equal(t, "", returns[1].Interface())
			},
		},
		{
			name: "a lone error slot carries the failure", targetType: reflect.TypeFor[func() error](),
			inspect: func(t *testing.T, returns []reflect.Value) {
				require.Len(t, returns, 1)
				require.ErrorIs(t, returns[0].Interface().(error), failure)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tt.inspect(t, buildClosureErrorReturns(context.Background(), tt.targetType, failure))
		})
	}
}
