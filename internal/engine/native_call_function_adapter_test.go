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
	"iter"
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
)

type adapterShape interface{ Area() int }

type adapterSquare int

func (s adapterSquare) Area() int { return int(s * s) }

type adapterAnyPredicate func(any) bool

func requireFunctionArgumentPanic(t *testing.T, call func()) {
	t.Helper()
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		call()
	}()
	err, ok := recovered.(error)
	require.True(t, ok, "panic value %v is not an error", recovered)
	require.ErrorIs(t, err, fault.ErrUnsupportedFunctionArgument)
}

func TestANativeFunctionValueIsAdaptedToAnErasedParameterType(t *testing.T) {
	t.Parallel()
	isBig := func(shape adapterShape) bool { return shape.Area() > 10 }
	adapted := coerceReflectArgument(nil, reflect.ValueOf(isBig), reflect.TypeFor[func(any) bool](), argumentTypeContext{})
	predicate, ok := reflect.TypeAssert[func(any) bool](adapted)
	require.True(t, ok)
	require.True(t, slices.ContainsFunc([]any{adapterSquare(2), adapterSquare(5)}, predicate))
	require.False(t, predicate(adapterSquare(3)))
}

func TestAFunctionValueAdaptsToANamedFunctionType(t *testing.T) {
	t.Parallel()
	isEven := func(n int) bool { return n%2 == 0 }
	adapted := coerceReflectArgument(nil, reflect.ValueOf(isEven), reflect.TypeFor[adapterAnyPredicate](), argumentTypeContext{})
	require.Equal(t, reflect.TypeFor[adapterAnyPredicate](), adapted.Type())
	predicate, ok := reflect.TypeAssert[adapterAnyPredicate](adapted)
	require.True(t, ok)
	require.True(t, predicate(4))
	require.False(t, predicate(3))
}

func TestAnAdaptedFunctionReturnsItsResultsAsTheParameterTypeDeclares(t *testing.T) {
	t.Parallel()
	produce := func() adapterShape { return adapterSquare(9) }
	adapted, ok := newFunctionBridge(nil).adapt(reflect.ValueOf(produce), reflect.TypeFor[func() any]())
	require.True(t, ok)
	producer, ok := reflect.TypeAssert[func() any](adapted)
	require.True(t, ok)
	require.Equal(t, adapterSquare(9), producer())
}

func TestAnAdaptedIteratorAdaptsTheYieldFunctionItReceives(t *testing.T) {
	t.Parallel()
	var sequence iter.Seq2[string, int] = func(yield func(string, int) bool) {
		_ = yield("a", 1) && yield("bb", 2)
	}
	adapted, ok := newFunctionBridge(nil).adapt(reflect.ValueOf(sequence), reflect.TypeFor[func(func(any, any) bool)]())
	require.True(t, ok)
	erased, ok := reflect.TypeAssert[func(func(any, any) bool)](adapted)
	require.True(t, ok)
	var seen []string
	erased(func(key, value any) bool {
		seen = append(seen, fmt.Sprint(key, value))
		return true
	})
	require.Equal(t, []string{"a1", "bb2"}, seen)
}

func TestAnAdaptedVariadicFunctionReceivesItsTailAsItsOwnSliceType(t *testing.T) {
	t.Parallel()
	join := func(shapes ...adapterShape) int {
		total := 0
		for _, shape := range shapes {
			total += shape.Area()
		}
		return total
	}
	adapted, ok := newFunctionBridge(nil).adapt(reflect.ValueOf(join), reflect.TypeFor[func(...any) int]())
	require.True(t, ok)
	erased, ok := reflect.TypeAssert[func(...any) int](adapted)
	require.True(t, ok)
	require.Equal(t, 13, erased(adapterSquare(2), adapterSquare(3)))
}

func TestANilFunctionValueBecomesTheParametersNilFunction(t *testing.T) {
	t.Parallel()
	var missing func(adapterShape) bool
	adapted, ok := newFunctionBridge(nil).adapt(reflect.ValueOf(missing), reflect.TypeFor[func(any) bool]())
	require.True(t, ok)
	require.True(t, adapted.IsNil())
	require.Equal(t, reflect.TypeFor[func(any) bool](), adapted.Type())
}

func TestAFunctionWhoseSignatureCannotBridgeIsNotAdapted(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name     string
		function any
		expected reflect.Type
	}{
		{"another parameter count", func(int, int) bool { return true }, reflect.TypeFor[func(any) bool]()},
		{"another result count", func(any) {}, reflect.TypeFor[func(any) bool]()},
		{"variadic against fixed", func(...int) bool { return true }, reflect.TypeFor[func([]int) bool]()},
		{"an integer bound for a string", func(string) bool { return true }, reflect.TypeFor[func(int) bool]()},
		{"a result that cannot convert", func(any) string { return "" }, reflect.TypeFor[func(any) bool]()},
	} {
		_, ok := newFunctionBridge(nil).adapt(reflect.ValueOf(testCase.function), testCase.expected)
		require.False(t, ok, testCase.name)
	}
}

func TestAnAdaptedFunctionRefusesAValueItsOwnSignatureCannotTake(t *testing.T) {
	t.Parallel()
	isEven := func(n int) bool { return n%2 == 0 }
	adapted, ok := newFunctionBridge(nil).adapt(reflect.ValueOf(isEven), reflect.TypeFor[func(any) bool]())
	require.True(t, ok)
	predicate, ok := reflect.TypeAssert[func(any) bool](adapted)
	require.True(t, ok)
	requireFunctionArgumentPanic(t, func() { predicate("four") })
	requireFunctionArgumentPanic(t, func() { predicate(nil) })
}

func TestANativeCallWithAnUnbridgeableFunctionFailsWithANamedError(t *testing.T) {
	t.Parallel()
	callee := reflect.ValueOf(func(f func(any) bool) bool { return f(1) })
	pair := reflect.ValueOf(func(int, int) bool { return true })
	coerced := coerceReflectArgument(nil, pair, callee.Type().In(0), argumentTypeContext{})
	err := checkNativeFunctionArguments(callee, []reflect.Value{coerced}, false)
	require.ErrorIs(t, err, fault.ErrUnsupportedFunctionArgument)
	require.Contains(t, err.Error(), "func(int, int) bool")

	adapted := coerceReflectArgument(nil, reflect.ValueOf(func(n int) bool { return n == 1 }), callee.Type().In(0), argumentTypeContext{})
	require.NoError(t, checkNativeFunctionArguments(callee, []reflect.Value{adapted}, false))
	require.True(t, callee.Call([]reflect.Value{adapted})[0].Bool())
}

func TestTheFunctionBridgeDepthIsBounded(t *testing.T) {
	t.Parallel()
	outer := reflect.TypeFor[func(any) bool]()
	inner := reflect.TypeFor[func(int) bool]()
	for range maxFunctionBridgeDepth + 1 {
		outer = reflect.FuncOf([]reflect.Type{outer}, nil, false)
		inner = reflect.FuncOf([]reflect.Type{inner}, nil, false)
	}
	require.False(t, bridgeableFunctionTypes(outer, inner, 0))
}

func TestAnAdapterCalledOnAnotherGoroutineReportsInsteadOfPanicking(t *testing.T) {
	t.Parallel()
	vm := newTestVM(t)
	isEven := func(n int) bool { return n%2 == 0 }
	adapted, ok := newFunctionBridge(vm).adapt(reflect.ValueOf(isEven), reflect.TypeFor[func(any) bool]())
	require.True(t, ok)
	predicate, ok := reflect.TypeAssert[func(any) bool](adapted)
	require.True(t, ok)

	done := make(chan bool)
	go func() { done <- predicate("four") }()
	require.False(t, <-done, "the adapter returns zero results rather than panicking where nothing recovers")
	info := vm.TakeGoroutinePanic()
	require.NotNil(t, info, "the failure is recorded so the execution fails")
	err, isError := info.Value().(error)
	require.True(t, isError)
	require.ErrorIs(t, err, fault.ErrUnsupportedFunctionArgument)
}

func TestAnAdaptedVariadicTailIsChargedToTheCostBudget(t *testing.T) {
	t.Parallel()
	vm := newTestVM(t)
	vm.Limits.Tracker = &ResourceTracker{}
	vm.Limits.CostBudget = 2
	vm.Limits.Tracker.CostRemaining.Store(2)
	count := func(shapes ...adapterShape) int { return len(shapes) }
	adapted, ok := newFunctionBridge(vm).adapt(reflect.ValueOf(count), reflect.TypeFor[func(...any) int]())
	require.True(t, ok)
	erased, ok := reflect.TypeAssert[func(...any) int](adapted)
	require.True(t, ok)

	require.Equal(t, 2, erased(adapterSquare(1), adapterSquare(2)))
	require.Nil(t, vm.GoroutinePanic())
	require.Zero(t, erased(adapterSquare(1)), "a copy past the budget is refused")
	info := vm.TakeGoroutinePanic()
	require.NotNil(t, info)
	err, isError := info.Value().(error)
	require.True(t, isError)
	require.ErrorIs(t, err, fault.ErrCostBudgetExceeded)
}

func TestAConvertibleSliceCrossesWithoutACopy(t *testing.T) {
	t.Parallel()
	type numbers []int
	source := []int{1, 2, 3}
	bridged, err := newFunctionBridge(nil).slice(reflect.ValueOf(source), reflect.TypeFor[numbers]())
	require.NoError(t, err)
	converted, ok := reflect.TypeAssert[numbers](bridged)
	require.True(t, ok)
	converted[0] = 9
	require.Equal(t, 9, source[0], "a slice whose type converts shares its backing array")
}
