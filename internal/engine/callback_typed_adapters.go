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
	"go/ast"
	"go/types"
	"reflect"
)

// The typed callback route serves go/ast.Inspect without the argument and result slices
// reflect.MakeFunc requires.

// makeFuncImplName is the name reflect's panic texts give a MakeFunc implementation.
const makeFuncImplName = "closure"

var (
	// astInspectCallbackType is go/ast.Inspect's callback type, which has a typed adapter.
	astInspectCallbackType = reflect.TypeFor[func(ast.Node) bool]()

	// intCallbackType is the type of a callback that takes one int and returns nothing.
	intCallbackType = reflect.TypeFor[func(int)]()

	// typesVarPredicateType is the type of a predicate over a go/types variable.
	typesVarPredicateType = reflect.TypeFor[func(*types.Var) bool]()

	// astNodeNilArgument is the argument reflect.MakeFunc hands a func(ast.Node) wrapper for
	// a nil node, which is a non-addressable nil Value of the interface type.
	astNodeNilArgument = reflect.Zero(reflect.TypeFor[ast.Node]())
)

// astNodeBox holds an ast.Node so that the field of a boxed copy is the non-addressable
// interface Value reflect's argument copy is.
type astNodeBox struct {
	// Node is the boxed node.
	Node ast.Node
}

// closureWrapper is what a closure wrapper made by makeClosureWrapperFunc captures.
type closureWrapper struct {
	// spec builds or acquires the VM a call runs on.
	spec *callbackVMSpec

	// closure is the interpreted closure the wrapper runs.
	closure *RuntimeClosure

	// functionType is the wrapper's signature.
	functionType reflect.Type

	// handoff is the GIL hand-off pending when the wrapper was made, or nil.
	handoff *gilHandoff
}

// callbackResultSink receives a wrapper call's results while the callback VM is still
// held. A typed bool adapter takes a plain bool result as it is.
type callbackResultSink struct {
	// functionType is the wrapper's signature.
	functionType reflect.Type

	// values holds the results packaged for reflect, unless boolReady.
	values []reflect.Value

	// wantBool asks for a plain bool first result as it is, not as a reflect Value.
	wantBool bool

	// boolReady is true when boolResult holds the result.
	boolReady bool

	// boolResult is the result when boolReady.
	boolResult bool
}

// fromRaw records a completed run's results.
//
// Takes raw (closureResults) which are the run's results.
func (s *callbackResultSink) fromRaw(raw closureResults) {
	if s.wantBool && raw.single {
		if result, ok := raw.one.(bool); ok {
			s.boolResult, s.boolReady = result, true
			return
		}
	}
	var buffer [1]any
	s.boolReady = false
	s.values = buildReflectResults(raw.slice(&buffer), s.functionType)
}

// fromValues records results already packaged for reflect.
//
// Takes values ([]reflect.Value) which are the results.
func (s *callbackResultSink) fromValues(values []reflect.Value) {
	s.boolReady = false
	s.values = values
}

// reflectCall is the reflect.MakeFunc implementation of a closure wrapper.
//
// Takes arguments ([]reflect.Value) which are the call's arguments.
//
// Returns []reflect.Value which are the results packaged for reflect.
func (w *closureWrapper) reflectCall(arguments []reflect.Value) []reflect.Value {
	if opcodeCensusBuilt {
		slowCallCensusRecord(slowCallClosure, w.functionType)
	}
	sink := callbackResultSink{functionType: w.functionType, values: nil, wantBool: false, boolReady: false, boolResult: false}
	w.call(arguments, &sink)
	return sink.values
}

// typedInspect returns the typed adapter of a func(ast.Node) bool closure wrapper.
//
// Returns reflect.Value which holds a func(ast.Node) bool.
func (w *closureWrapper) typedInspect() reflect.Value {
	return reflect.ValueOf(func(node ast.Node) bool {
		var arguments [1]reflect.Value
		arguments[0] = astNodeClosureArgument(node)
		sink := callbackResultSink{functionType: w.functionType, values: nil, wantBool: true, boolReady: false, boolResult: false}
		w.call(arguments[:], &sink)
		if sink.boolReady {
			return sink.boolResult
		}
		return makeFuncBoolResult(sink.values)
	})
}

// typedInt returns the typed adapter of a func(int) closure wrapper.
//
// The argument is the Value reflect.MakeFunc would hand its implementation, a
// non-addressable copy of the int. A run that hands back results is refused as reflect
// refuses it.
//
// Returns reflect.Value which holds a func(int).
//
// Panics with reflect's text, from the function it returns, when a run hands back a
// result.
func (w *closureWrapper) typedInt() reflect.Value {
	return reflect.ValueOf(func(value int) {
		var arguments [1]reflect.Value
		arguments[0] = intClosureArgument(value)
		sink := callbackResultSink{functionType: w.functionType, values: nil, wantBool: false, boolReady: false, boolResult: false}
		w.call(arguments[:], &sink)
		if len(sink.values) != 0 {
			panic("reflect: wrong return count from function created by MakeFunc")
		}
	})
}

// typedTypesVarPredicate returns the typed adapter of a func(*types.Var) bool closure
// wrapper.
//
// A pointer argument is its own reflect Value, nil included, so the argument is the one
// reflect.MakeFunc would hand its implementation, and a plain bool result is taken as it
// is.
//
// Returns reflect.Value which holds a func(*types.Var) bool. That function raises
// reflect's panic for a result reflect would refuse (makeFuncBoolResult()).
func (w *closureWrapper) typedTypesVarPredicate() reflect.Value {
	return reflect.ValueOf(func(variable *types.Var) bool {
		var arguments [1]reflect.Value
		arguments[0] = typesVarClosureArgument(variable)
		sink := callbackResultSink{functionType: w.functionType, values: nil, wantBool: true, boolReady: false, boolResult: false}
		w.call(arguments[:], &sink)
		if sink.boolReady {
			return sink.boolResult
		}
		return makeFuncBoolResult(sink.values)
	})
}

// call runs one call of the wrapper as a declared top-level run, or on the parent's
// cached callback VM or a fresh one, handing the results to sink before the VM is
// released.
//
// Takes arguments ([]reflect.Value) which are the call's arguments.
// Takes sink (*callbackResultSink) which receives the results.
//
// A guest panic crosses an active native frame, and a stop propagates through its native
// caller.
func (w *closureWrapper) call(arguments []reflect.Value, sink *callbackResultSink) {
	spec, closure, functionType, handoff := w.spec, w.closure, w.functionType, w.handoff
	if handoff == nil && spec.runTopLevel(closure, arguments, functionType, sink) {
		return
	}
	callbackVM := spec.acquire(closure)
	callbackVM.parkedMaker = spec.makerArena
	defer spec.finish(callbackVM, callbackVM.enterServedChain())
	restoreUnwindSlice := callbackVM.adoptAbortUnwindSliceIfNested()
	defer restoreUnwindSlice()

	acquiredGil := handoff.acquireForCallback()
	if acquiredGil {
		callbackVM.holdsInterpreterLock = true
	}
	defer func() {
		if acquiredGil {
			callbackVM.holdsInterpreterLock = false
			handoff.releaseAfterCallback(true)
		}
	}()
	raw := callbackVM.callClosureRaw(closure, arguments)
	if callbackVM.evalError != nil {
		sink.fromValues(spec.stoppedCallbackResults(callbackVM, functionType))
		return
	}
	sink.fromRaw(raw)
}

// boundMethodWrapper is what the function value of a method value made by
// makeBoundMethodWrapper captures.
type boundMethodWrapper struct {
	// bound runs the method.
	bound *boundMethodVM

	// held holds the receiver, which licence walks find through the wrapper registry.
	held *wrapperBoundReceiver

	// signature is the method value's function type.
	signature reflect.Type
}

// reflectCall is the reflect.MakeFunc implementation of a method value.
//
// Takes arguments ([]reflect.Value) which are the call's arguments.
//
// Returns []reflect.Value which are the results conformed to the signature.
func (w *boundMethodWrapper) reflectCall(arguments []reflect.Value) []reflect.Value {
	if opcodeCensusBuilt {
		slowCallCensusRecord(slowCallMethod, w.signature)
	}
	return conformReflectResults(w.bound.invoke(w.held.receiver, arguments, identityArg), w.signature)
}

// typedInspect returns the typed adapter of a func(ast.Node) bool method value.
//
// Returns reflect.Value which holds a func(ast.Node) bool.
func (w *boundMethodWrapper) typedInspect() reflect.Value {
	return reflect.ValueOf(func(node ast.Node) bool {
		var arguments [1]reflect.Value
		arguments[0] = astNodeBoundArgument(node)
		return makeFuncBoolResult(w.reflectCall(arguments[:]))
	})
}

// astNodeBoundArgument returns the argument a method value's run receives for node.
//
// A bound method's arguments reach its registers without unwrapping (identityArg), so the
// argument is exactly the Value reflect.MakeFunc hands its implementation. That is a
// non-addressable Value of the interface type holding its own copy of the node, built
// here through the public API with the one allocation reflect's copy costs. A nil node is
// reflect's nil interface Value, which needs none.
//
// Takes node (ast.Node) which is the argument.
//
// Returns reflect.Value which is the argument.
func astNodeBoundArgument(node ast.Node) reflect.Value {
	if node == nil {
		return astNodeNilArgument
	}
	return reflect.ValueOf(astNodeBox{Node: node}).Field(0)
}

// astNodeClosureArgument returns the argument a closure wrapper's run receives for node.
//
// assignReflectParams unwraps a non-nil interface argument before it reaches a register,
// so the node's own dynamic value, which reflect.ValueOf gives without allocating, places
// exactly what the Elem of reflect's interface argument places. A nil node is reflect's
// nil interface Value.
//
// Takes node (ast.Node) which is the argument.
//
// Returns reflect.Value which is the argument.
func astNodeClosureArgument(node ast.Node) reflect.Value {
	if node == nil {
		return astNodeNilArgument
	}
	return reflect.ValueOf(node)
}

// makeFuncBoolResult returns the bool a reflect.MakeFunc wrapper of a single-bool-result
// function type hands its caller for values, raising reflect's own panic for results it
// would refuse (reflect.callReflect and Value.assignTo).
//
// Takes values ([]reflect.Value) which are the results as reflect Values.
//
// Returns bool which is the result.
//
// Panics with reflect's text for a wrong result count, a zero Value, a value obtained
// from an unexported field or a value not assignable to bool.
func makeFuncBoolResult(values []reflect.Value) bool {
	if len(values) != 1 {
		panic("reflect: wrong return count from function created by MakeFunc")
	}
	value := values[0]
	if !value.IsValid() {
		panic("reflect: function created by MakeFunc using " + makeFuncImplName + " returned zero Value")
	}
	if !value.CanInterface() {
		panic("reflect: function created by MakeFunc using " + makeFuncImplName + " returned value obtained from unexported field")
	}
	if value.Type() != plainBoolReflectType {
		panic("reflect.MakeFunc: value of type " + value.Type().String() + " is not assignable to type bool")
	}
	return value.Bool()
}

// typesVarClosureArgument returns the argument a closure wrapper's run receives for a
// go/types variable.
//
// Takes variable (*types.Var) which is the argument.
//
// Returns reflect.Value which is the argument.
func typesVarClosureArgument(variable *types.Var) reflect.Value {
	return reflect.ValueOf(variable)
}

// intClosureArgument returns the argument a closure wrapper's run receives for an int,
// the non-addressable copy reflect.MakeFunc hands its implementation.
//
// Takes value (int) which is the argument.
//
// Returns reflect.Value which is the argument.
func intClosureArgument(value int) reflect.Value {
	return reflect.ValueOf(value)
}
