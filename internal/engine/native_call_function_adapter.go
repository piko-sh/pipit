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

	"pipit.sh/pipit/internal/fault"
)

// maxFunctionBridgeDepth bounds how deeply bridgeableFunctionTypes follows function types
// nested in parameters and results, such as an iterator's yield function.
const maxFunctionBridgeDepth = 8

// functionBridge carries what a function adapter reports to.
//
// The copies it makes of slices are charged to the execution's cost budget.
type functionBridge struct {
	// globals receives a failure as a goroutine panic, or is nil to panic with it instead,
	// as an adapter built with no VM does.
	globals *GlobalStore

	// tracker holds the execution's shared cost budget, or is nil.
	tracker *ResourceTracker

	// metered is true when the execution has a cost budget.
	metered bool
}

// newFunctionBridge returns the bridge for adapters built on vm's behalf.
//
// Takes vm (*VM) which owns the globals and budget, or nil.
//
// Returns functionBridge which reports to vm, or panics when vm is nil.
func newFunctionBridge(vm *VM) functionBridge {
	if vm == nil {
		return functionBridge{globals: nil, tracker: nil, metered: false}
	}
	return functionBridge{globals: vm.Globals, tracker: vm.Limits.Tracker, metered: vm.Limits.CostBudget != 0}
}

// adapt gives a native function value the function type a native parameter declares when
// Go would accept the call but the two types differ.
//
// Takes function (reflect.Value) which is the native function value.
// Takes expectedType (reflect.Type) which is the parameter's function type.
//
// Returns reflect.Value which is the adapter, or the zero value of expectedType for a nil
// function.
// Returns bool which is false when the two signatures cannot be bridged.
func (b functionBridge) adapt(function reflect.Value, expectedType reflect.Type) (reflect.Value, bool) {
	if function.Kind() != reflect.Func || expectedType.Kind() != reflect.Func {
		return reflect.Value{}, false
	}
	functionType := function.Type()
	if !bridgeableFunctionTypes(expectedType, functionType, 0) {
		return reflect.Value{}, false
	}
	if function.IsNil() {
		return reflect.Zero(expectedType), true
	}
	return reflect.MakeFunc(expectedType, func(incoming []reflect.Value) []reflect.Value {
		results, err := b.call(function, expectedType, incoming)
		if err != nil {
			return b.fail(err, expectedType)
		}
		return results
	}), true
}

// call runs function for an adapter of type expectedType, bridging its arguments and
// results.
//
// Takes function (reflect.Value) which is the adapted native function.
// Takes expectedType (reflect.Type) which is the adapter's type.
// Takes incoming ([]reflect.Value) which are the adapter's arguments.
//
// Returns []reflect.Value which are the results, with expectedType's result types.
// Returns error which wraps fault.ErrUnsupportedFunctionArgument for a value that cannot
// cross, or fault.ErrCostBudgetExceeded when a copy exhausts the cost budget.
func (b functionBridge) call(function reflect.Value, expectedType reflect.Type, incoming []reflect.Value) ([]reflect.Value, error) {
	functionType := function.Type()
	arguments := make([]reflect.Value, len(incoming))
	for i, argument := range incoming {
		bridged, err := b.value(argument, functionType.In(i))
		if err != nil {
			return nil, err
		}
		arguments[i] = bridged
	}
	var results []reflect.Value
	if functionType.IsVariadic() {
		results = function.CallSlice(arguments)
	} else {
		results = function.Call(arguments)
	}
	for i, result := range results {
		bridged, err := b.value(result, expectedType.Out(i))
		if err != nil {
			return nil, err
		}
		results[i] = bridged
	}
	return results, nil
}

// fail reports an adapter's failure and returns the zero results it then gives native
// code. With no globals to record the failure on, it panics with err.
//
// Takes err (error) which is the failure.
// Takes expectedType (reflect.Type) which is the adapter's type.
//
// Returns []reflect.Value which are expectedType's zero results.
//
// Panics with err when the bridge has no globals.
func (b functionBridge) fail(err error, expectedType reflect.Type) []reflect.Value {
	if b.globals == nil {
		panic(err)
	}
	b.globals.recordGoroutinePanic(err, "")
	results := make([]reflect.Value, expectedType.NumOut())
	for i := range results {
		results[i] = reflect.Zero(expectedType.Out(i))
	}
	return results
}

// charge takes units from the execution's shared cost budget.
//
// Takes units (int) which is the cost of the work.
//
// Returns error which is fault.ErrCostBudgetExceeded once the budget is spent, or nil.
func (b functionBridge) charge(units int) error {
	if !b.metered || b.tracker == nil {
		return nil
	}
	if b.tracker.CostRemaining.Add(-int64(units)) < 0 {
		return fault.ErrCostBudgetExceeded
	}
	return nil
}

// bridgeableFunctionTypes reports whether an adapter of type outer can call a function of
// type inner. The two need the same number of parameters and results and the same
// variadic shape, with each outer parameter bridgeable to the inner one and each inner
// result to the outer one.
//
// Takes outer (reflect.Type) which is the type the adapter has.
// Takes inner (reflect.Type) which is the type of the function it calls.
// Takes depth (int) which counts the function types already followed.
//
// Returns bool which is true when every value can cross.
func bridgeableFunctionTypes(outer, inner reflect.Type, depth int) bool {
	if depth > maxFunctionBridgeDepth {
		return false
	}
	if outer.NumIn() != inner.NumIn() || outer.NumOut() != inner.NumOut() || outer.IsVariadic() != inner.IsVariadic() {
		return false
	}
	for i := range outer.NumIn() {
		if !bridgeableTypes(outer.In(i), inner.In(i), depth) {
			return false
		}
	}
	for i := range outer.NumOut() {
		if !bridgeableTypes(inner.Out(i), outer.Out(i), depth) {
			return false
		}
	}
	return true
}

// bridgeableTypes reports whether a value of type source can be given type target by
// functionBridge.value.
//
// Takes source (reflect.Type) which is the type of the values that arrive.
// Takes target (reflect.Type) which is the type they must have.
// Takes depth (int) which counts the function types already followed.
//
// Returns bool which is true when values may cross.
func bridgeableTypes(source, target reflect.Type, depth int) bool {
	switch {
	case source.AssignableTo(target), source.Kind() == reflect.Interface:
		return true
	case source.Kind() == reflect.Func && target.Kind() == reflect.Func:
		return bridgeableFunctionTypes(target, source, depth+1)
	case source.Kind() == reflect.Slice && target.Kind() == reflect.Slice:
		return bridgeableTypes(source.Elem(), target.Elem(), depth)
	default:
		return source.Kind() == target.Kind() && source.ConvertibleTo(target)
	}
}

// value gives value, which crosses a function adapter, the type target.
//
// An interface value is unwrapped first, so an `any` holding a types.Type reaches a
// func(types.Type) bool as the types.Type it holds. A nil interface becomes target's zero
// value when target can be nil. A function value is adapted in turn, and a slice (a
// variadic tail) is rebuilt element by element.
//
// Takes value (reflect.Value) which is the crossing value.
// Takes target (reflect.Type) which is the type it must have.
//
// Returns reflect.Value which has a type assignable to target.
// Returns error which wraps fault.ErrUnsupportedFunctionArgument when value cannot be
// given target, or is the cost error of a slice copy.
func (b functionBridge) value(value reflect.Value, target reflect.Type) (reflect.Value, error) {
	if !value.IsValid() {
		return zeroForBridge(value, target)
	}
	if value.Type().AssignableTo(target) {
		return value, nil
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return zeroForBridge(value, target)
		}
		return b.value(value.Elem(), target)
	}
	if adapted, ok := b.adapt(value, target); ok {
		return adapted, nil
	}
	if value.Kind() == reflect.Slice && target.Kind() == reflect.Slice {
		return b.slice(value, target)
	}
	if value.Kind() == target.Kind() && value.Type().ConvertibleTo(target) {
		return value.Convert(target), nil
	}
	return reflect.Value{}, fmt.Errorf("%w: a function adapted to another function type received %s where its own signature needs %s",
		fault.ErrUnsupportedFunctionArgument, value.Type(), target)
}

// slice gives a slice crossing a function adapter, such as a variadic tail, target's
// type. A slice whose type converts shares its backing array.
//
// Takes value (reflect.Value) which is the crossing slice.
// Takes target (reflect.Type) which is the slice type it must have.
//
// Returns reflect.Value which is a slice of type target, or its nil value.
// Returns error which is an element's bridging error or the copy's cost error.
func (b functionBridge) slice(value reflect.Value, target reflect.Type) (reflect.Value, error) {
	if value.IsNil() {
		return reflect.Zero(target), nil
	}
	if value.Type().ConvertibleTo(target) {
		return value.Convert(target), nil
	}
	if err := b.charge(value.Len()); err != nil {
		return reflect.Value{}, err
	}
	out := reflect.MakeSlice(target, value.Len(), value.Len())
	for i := range value.Len() {
		element, err := b.value(value.Index(i), target.Elem())
		if err != nil {
			return reflect.Value{}, err
		}
		out.Index(i).Set(element)
	}
	return out, nil
}

// zeroForBridge returns target's zero value for a nil crossing value, which Go permits
// only when target can be nil.
//
// Takes value (reflect.Value) which is the nil value, for the message.
// Takes target (reflect.Type) which is the type it must have.
//
// Returns reflect.Value which is target's zero value.
// Returns error which wraps fault.ErrUnsupportedFunctionArgument when target cannot be
// nil.
func zeroForBridge(value reflect.Value, target reflect.Type) (reflect.Value, error) {
	switch target.Kind() {
	case reflect.Interface, reflect.Pointer, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan, reflect.UnsafePointer:
		return reflect.Zero(target), nil
	default:
		described := "an invalid value"
		if value.IsValid() {
			described = "a nil " + value.Type().String()
		}
		return reflect.Value{}, fmt.Errorf("%w: a function adapted to another function type received %s where its own signature needs %s",
			fault.ErrUnsupportedFunctionArgument, described, target)
	}
}

// checkNativeFunctionArguments verifies that every function value bound for a native
// function-typed parameter has a type the parameter accepts, so a function whose
// signature no adapter can bridge fails with a named error rather than a reflect.Call
// panic.
//
// Takes reflectedFunction (reflect.Value) which is the native callee.
// Takes arguments ([]reflect.Value) which are the coerced arguments about to be passed.
// Takes ellipsisSpread (bool) which reports whether the final argument is the variadic
// slice itself rather than an element of it.
//
// Returns error which wraps fault.ErrUnsupportedFunctionArgument naming the argument, or
// nil.
func checkNativeFunctionArguments(reflectedFunction reflect.Value, arguments []reflect.Value, ellipsisSpread bool) error {
	functionType := reflectedFunction.Type()
	for index, argument := range arguments {
		if !argument.IsValid() || argument.Kind() != reflect.Func {
			continue
		}
		expected, ok := NativeParameterType(functionType, index, ellipsisSpread)
		if !ok || expected.Kind() != reflect.Func || argument.Type().AssignableTo(expected) {
			continue
		}
		return fmt.Errorf("%w: argument %d of %s is a %s, which cannot be called as %s",
			fault.ErrUnsupportedFunctionArgument, index, resolveNativeFunctionPath(reflectedFunction),
			argument.Type(), expected)
	}
	return nil
}
