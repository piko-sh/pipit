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
	"go/ast"
	"go/types"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab"
)

type inspectFunc func(ast.Node) bool

type namedTruth bool

type intCallback func(int)

type varPredicate func(*types.Var) bool

type inspectOutcome struct {
	panicked any
	result   bool
}

func inspectClosureFixture(t *testing.T, body func(builder *bytecodeBuilder)) (*VM, *RuntimeClosure) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.generalRegisters(2).returnGeneral()
	builder.parameterKinds = []isa.RegisterKind{isa.RegisterGeneral}
	body(builder)
	function := builder.build()
	root := &program.CompiledFunction{}
	root.Functions = []*program.CompiledFunction{root, function}
	maker := NewVM(t.Context(), NewGlobalStore(), symtab.NewSymbolRegistry(nil))
	maker.functions = root.Functions
	maker.rootFunction = root
	maker.Limits.Tracker = &ResourceTracker{}
	return maker, &RuntimeClosure{Function: function, RootFunction: root}
}

func returnsConstant(value reflect.Value) func(builder *bytecodeBuilder) {
	return func(builder *bytecodeBuilder) {
		constant := builder.addGeneralConst(value)
		builder.Emit(isa.OpLoadGeneralConstant, 0, constant, 0)
		builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	}
}

func returnsItsArgument(builder *bytecodeBuilder) {
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
}

func panicsWithItsArgument(builder *bytecodeBuilder) {
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Panic), 0)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
}

func callInspect(f func(ast.Node) bool, node ast.Node) (outcome inspectOutcome) {
	defer func() {
		if recovered := recover(); recovered != nil {
			outcome.panicked = recovered
		}
	}()
	outcome.result = f(node)
	return outcome
}

func describePanic(recovered any) string {
	if recovered == nil {
		return ""
	}
	if err, ok := recovered.(error); ok {
		return fmt.Sprintf("%T: %v", recovered, err)
	}
	return fmt.Sprintf("%T: %v", recovered, recovered)
}

func inspectWrappers(t *testing.T, maker *VM, closure *RuntimeClosure) (typed, viaMakeFunc func(ast.Node) bool) {
	t.Helper()
	typedValue := makeClosureWrapperFunc(maker, closure, reflect.TypeFor[func(ast.Node) bool]())
	typed, ok := reflect.TypeAssert[func(ast.Node) bool](typedValue)
	require.True(t, ok)
	named, ok := reflect.TypeAssert[inspectFunc](makeClosureWrapperFunc(maker, closure, reflect.TypeFor[inspectFunc]()))
	require.True(t, ok)
	return typed, named
}

func TestTheTypedInspectAdapterBehavesAsItsMakeFuncWrapper(t *testing.T) {
	ident := &ast.Ident{Name: "x"}
	tests := []struct {
		name string

		body func(builder *bytecodeBuilder)

		node ast.Node
	}{
		{name: "true", body: returnsConstant(reflect.ValueOf(true)), node: ident},
		{name: "false", body: returnsConstant(reflect.ValueOf(false)), node: ident},
		{name: "true for a nil node", body: returnsConstant(reflect.ValueOf(true)), node: nil},
		{name: "a named bool", body: returnsConstant(reflect.ValueOf(namedTruth(true))), node: ident},
		{name: "no value", body: returnsConstant(reflect.Value{}), node: ident},
		{name: "a string, which reflect refuses", body: returnsConstant(reflect.ValueOf("yes")), node: ident},
		{name: "its argument, which reflect refuses", body: returnsItsArgument, node: ident},
		{name: "its nil argument", body: returnsItsArgument, node: nil},
		{name: "a guest panic", body: panicsWithItsArgument, node: ident},
		{name: "a guest panic with a nil value", body: panicsWithItsArgument, node: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maker, closure := inspectClosureFixture(t, tt.body)
			typed, viaMakeFunc := inspectWrappers(t, maker, closure)
			want := callInspect(viaMakeFunc, tt.node)
			got := callInspect(typed, tt.node)
			require.Equal(t, want.result, got.result)
			require.Equal(t, describePanic(want.panicked), describePanic(got.panicked))
		})
	}
}

func TestTheTypedInspectAdapterPlacesTheArgumentReflectWould(t *testing.T) {
	var captured reflect.Value
	capture, ok := reflect.TypeAssert[func(ast.Node) bool](reflect.MakeFunc(astInspectCallbackType, func(arguments []reflect.Value) []reflect.Value {
		captured = arguments[0]
		return []reflect.Value{reflect.ValueOf(true)}
	}))
	require.True(t, ok)
	for _, node := range []ast.Node{&ast.Ident{Name: "x"}, &ast.BasicLit{Value: "1"}, nil} {
		capture(node)
		place := func(argument reflect.Value) reflect.Value {
			registers := NewRegisters([isa.NumRegisterKinds]uint32{isa.RegisterGeneral: 1})
			assignReflectParams(&registers, []isa.RegisterKind{isa.RegisterGeneral}, nil, []reflect.Value{argument})
			return registers.General[0]
		}
		want, got := place(captured), place(astNodeClosureArgument(node))
		require.Equal(t, want.Type(), got.Type())
		require.Equal(t, want.Kind(), got.Kind())
		require.Equal(t, want.CanAddr(), got.CanAddr())
		require.Equal(t, want.CanSet(), got.CanSet())
		require.Equal(t, want.CanInterface(), got.CanInterface())
		require.Equal(t, want.Interface(), got.Interface())
	}
}

func TestMakeFuncBoolResultRaisesWhatReflectRaises(t *testing.T) {
	type holder struct {
		hidden bool
	}
	hidden := reflect.ValueOf(holder{hidden: true}).Field(0)
	tests := []struct {
		name string

		values []reflect.Value
	}{
		{name: "true", values: []reflect.Value{reflect.ValueOf(true)}},
		{name: "false", values: []reflect.Value{reflect.ValueOf(false)}},
		{name: "a named bool", values: []reflect.Value{reflect.ValueOf(namedTruth(true))}},
		{name: "a string", values: []reflect.Value{reflect.ValueOf("yes")}},
		{name: "a zero Value", values: []reflect.Value{{}}},
		{name: "a value from an unexported field", values: []reflect.Value{hidden}},
		{name: "no results", values: nil},
		{name: "two results", values: []reflect.Value{reflect.ValueOf(true), reflect.ValueOf(true)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			implementation := func([]reflect.Value) []reflect.Value { return tt.values }
			viaMakeFunc, ok := reflect.TypeAssert[func(ast.Node) bool](reflect.MakeFunc(astInspectCallbackType, implementation))
			require.True(t, ok)
			want := callInspect(viaMakeFunc, nil)
			got := callInspect(func(ast.Node) bool { return makeFuncBoolResult(tt.values) }, nil)
			require.Equal(t, want.result, got.result)
			require.Equal(t, describePanic(want.panicked), describePanic(got.panicked))
		})
	}
}

func TestTheTypedInspectAdapterAllocatesLessThanItsMakeFuncWrapper(t *testing.T) {
	if raceDetectorOn {
		t.Skip("the race detector changes allocation counts")
	}
	maker, closure := inspectClosureFixture(t, returnsConstant(reflect.ValueOf(true)))
	typed, viaMakeFunc := inspectWrappers(t, maker, closure)
	node := ast.Node(&ast.Ident{Name: "x"})
	typedAllocations := testing.AllocsPerRun(200, func() { typed(node) })
	makeFuncAllocations := testing.AllocsPerRun(200, func() { viaMakeFunc(node) })

	t.Logf("typed %v, MakeFunc %v objects per call", typedAllocations, makeFuncAllocations)
	require.LessOrEqual(t, typedAllocations, makeFuncAllocations-3,
		"the typed adapter allocates %v objects per call against %v for the MakeFunc wrapper", typedAllocations, makeFuncAllocations)
}

func boundInspectFixture(t *testing.T, body func(builder *bytecodeBuilder)) (*VM, *boundMethodVM) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.generalRegisters(2).returnGeneral()
	builder.parameterKinds = []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterGeneral}
	body(builder)
	method := builder.build()
	root := &program.CompiledFunction{}
	root.Functions = []*program.CompiledFunction{root, method}
	maker := NewVM(t.Context(), NewGlobalStore(), symtab.NewSymbolRegistry(nil))
	maker.functions = root.Functions
	maker.rootFunction = root
	maker.Limits.Tracker = &ResourceTracker{}
	maker.exportGuestCode()
	return maker, newBoundMethodVM(maker, method, nil)
}

func returnsItsNodeArgument(builder *bytecodeBuilder) {
	builder.Emit(isa.OpMoveGeneral, 0, 1, 0)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
}

func boundInspectWrappers(t *testing.T, maker *VM, bound *boundMethodVM) (typed, viaMakeFunc func(ast.Node) bool) {
	t.Helper()
	receiver := reflect.ValueOf(&ast.Ident{Name: "receiver"})
	typed, ok := reflect.TypeAssert[func(ast.Node) bool](makeBoundMethodWrapper(maker, bound, receiver, astInspectCallbackType))
	require.True(t, ok)
	named, ok := reflect.TypeAssert[inspectFunc](makeBoundMethodWrapper(maker, bound, receiver, reflect.TypeFor[inspectFunc]()))
	require.True(t, ok)
	return typed, named
}

func TestTheTypedBoundInspectAdapterBehavesAsItsMakeFuncWrapper(t *testing.T) {
	ident := &ast.Ident{Name: "x"}
	tests := []struct {
		name string

		body func(builder *bytecodeBuilder)

		node ast.Node
	}{
		{name: "true", body: returnsConstant(reflect.ValueOf(true)), node: ident},
		{name: "false for a nil node", body: returnsConstant(reflect.ValueOf(false)), node: nil},
		{name: "a named bool", body: returnsConstant(reflect.ValueOf(namedTruth(true))), node: ident},
		{name: "no value", body: returnsConstant(reflect.Value{}), node: ident},
		{name: "a string, which reflect refuses", body: returnsConstant(reflect.ValueOf("yes")), node: ident},
		{name: "its argument, which reflect refuses", body: returnsItsNodeArgument, node: ident},
		{name: "its nil argument", body: returnsItsNodeArgument, node: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maker, bound := boundInspectFixture(t, tt.body)
			typed, viaMakeFunc := boundInspectWrappers(t, maker, bound)
			want := callInspect(viaMakeFunc, tt.node)
			got := callInspect(typed, tt.node)
			require.Equal(t, want.result, got.result)
			require.Equal(t, describePanic(want.panicked), describePanic(got.panicked))
		})
	}
}

func TestTheTypedBoundInspectAdapterPassesTheArgumentReflectWould(t *testing.T) {
	var captured reflect.Value
	capture, ok := reflect.TypeAssert[func(ast.Node) bool](reflect.MakeFunc(astInspectCallbackType, func(arguments []reflect.Value) []reflect.Value {
		captured = arguments[0]
		return []reflect.Value{reflect.ValueOf(true)}
	}))
	require.True(t, ok)
	for _, node := range []ast.Node{&ast.Ident{Name: "x"}, &ast.BasicLit{Value: "1"}, nil} {
		capture(node)
		got := astNodeBoundArgument(node)
		require.Equal(t, captured.Type(), got.Type())
		require.Equal(t, captured.Kind(), got.Kind())
		require.Equal(t, captured.CanAddr(), got.CanAddr())
		require.Equal(t, captured.CanSet(), got.CanSet())
		require.Equal(t, captured.CanInterface(), got.CanInterface())
		require.Equal(t, captured.IsNil(), got.IsNil())
		require.Equal(t, captured.Interface(), got.Interface())
	}
	node := ast.Node(&ast.Ident{Name: "x"})
	if raceDetectorOn {
		return
	}
	require.Equal(t, 1.0, testing.AllocsPerRun(100, func() { astNodeBoundArgument(node) }),
		"the argument costs the one copy reflect makes")
	require.Zero(t, testing.AllocsPerRun(100, func() { astNodeBoundArgument(nil) }))
}

func TestTheTypedBoundInspectAdapterAllocatesLessThanItsMakeFuncWrapper(t *testing.T) {
	if raceDetectorOn {
		t.Skip("the race detector changes allocation counts")
	}
	maker, bound := boundInspectFixture(t, returnsConstant(reflect.ValueOf(true)))
	typed, viaMakeFunc := boundInspectWrappers(t, maker, bound)
	node := ast.Node(&ast.Ident{Name: "x"})
	typedAllocations := testing.AllocsPerRun(200, func() { typed(node) })
	makeFuncAllocations := testing.AllocsPerRun(200, func() { viaMakeFunc(node) })
	t.Logf("typed %v, MakeFunc %v objects per call", typedAllocations, makeFuncAllocations)
	require.Less(t, typedAllocations, makeFuncAllocations,
		"reflect.callReflect's argument slice is not needed by the typed adapter")
}

func intClosureFixture(t *testing.T, body func(builder *bytecodeBuilder)) (*VM, *RuntimeClosure) {
	t.Helper()
	maker, closure := inspectClosureFixture(t, body)
	closure.Function.ParameterKinds = []isa.RegisterKind{isa.RegisterInt64}
	closure.Function.NumRegisters[isa.RegisterInt64] = 1
	return maker, closure
}

func returnsNothing(builder *bytecodeBuilder) {
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
}

func callIntCallback(callback func(int), value int) (panicked any) {
	defer func() { panicked = recover() }()
	callback(value)
	return nil
}

func TestTheTypedIntAdapterBehavesAsItsMakeFuncWrapper(t *testing.T) {
	tests := []struct {
		name string
		body func(builder *bytecodeBuilder)
	}{
		{name: "nothing returned", body: returnsNothing},
		{name: "a value returned, which reflect refuses", body: returnsConstant(reflect.ValueOf(true))},
		{name: "a guest panic", body: func(builder *bytecodeBuilder) {
			constant := builder.addGeneralConst(reflect.ValueOf("stop"))
			builder.Emit(isa.OpLoadGeneralConstant, 0, constant, 0)
			panicsWithItsArgument(builder)
		}}, {name: "a guest panic with its argument", body: func(builder *bytecodeBuilder) {
			builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpBoxInt64), 0, 0)
			panicsWithItsArgument(builder)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maker, closure := intClosureFixture(t, tt.body)
			typed, ok := reflect.TypeAssert[func(int)](makeClosureWrapperFunc(maker, closure, intCallbackType))
			require.True(t, ok)
			named, ok := reflect.TypeAssert[intCallback](makeClosureWrapperFunc(maker, closure, reflect.TypeFor[intCallback]()))
			require.True(t, ok, "a named callback type keeps the reflect.MakeFunc wrapper")
			for _, value := range []int{0, 7, -300} {
				require.Equal(t, describePanic(callIntCallback(named, value)), describePanic(callIntCallback(typed, value)))
			}
		})
	}
}

func TestTheTypedIntAdapterAllocatesLessThanItsMakeFuncWrapper(t *testing.T) {
	if raceDetectorOn {
		t.Skip("the race detector changes allocation counts")
	}
	maker, closure := intClosureFixture(t, returnsNothing)
	typed, ok := reflect.TypeAssert[func(int)](makeClosureWrapperFunc(maker, closure, intCallbackType))
	require.True(t, ok)
	named, ok := reflect.TypeAssert[intCallback](makeClosureWrapperFunc(maker, closure, reflect.TypeFor[intCallback]()))
	require.True(t, ok)
	typedAllocations := testing.AllocsPerRun(200, func() { typed(1000) })
	makeFuncAllocations := testing.AllocsPerRun(200, func() { named(1000) })
	require.Less(t, typedAllocations, makeFuncAllocations)
}

func callVarPredicate(predicate func(*types.Var) bool, variable *types.Var) (outcome inspectOutcome) {
	defer func() {
		if recovered := recover(); recovered != nil {
			outcome.panicked = recovered
		}
	}()
	outcome.result = predicate(variable)
	return outcome
}

func TestTheTypedTypesVarPredicateAdapterBehavesAsItsMakeFuncWrapper(t *testing.T) {
	variable := types.NewVar(0, nil, "x", types.Typ[types.Int])
	tests := []struct {
		name string
		body func(builder *bytecodeBuilder)
	}{
		{name: "true", body: returnsConstant(reflect.ValueOf(true))},
		{name: "false", body: returnsConstant(reflect.ValueOf(false))},
		{name: "a named bool", body: returnsConstant(reflect.ValueOf(namedTruth(true)))},
		{name: "no value", body: returnsConstant(reflect.Value{})},
		{name: "a string, which reflect refuses", body: returnsConstant(reflect.ValueOf("yes"))},
		{name: "its argument, which reflect refuses", body: returnsItsArgument},
		{name: "a guest panic", body: panicsWithItsArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maker, closure := inspectClosureFixture(t, tt.body)
			typed, ok := reflect.TypeAssert[func(*types.Var) bool](makeClosureWrapperFunc(maker, closure, typesVarPredicateType))
			require.True(t, ok)
			named, ok := reflect.TypeAssert[varPredicate](makeClosureWrapperFunc(maker, closure, reflect.TypeFor[varPredicate]()))
			require.True(t, ok, "a named predicate type keeps the reflect.MakeFunc wrapper")
			for _, argument := range []*types.Var{variable, nil} {
				want, got := callVarPredicate(named, argument), callVarPredicate(typed, argument)
				require.Equal(t, want.result, got.result)
				require.Equal(t, describePanic(want.panicked), describePanic(got.panicked))
			}
		})
	}
}

func TestTheTypedTypesVarPredicateAdapterPlacesTheArgumentReflectWould(t *testing.T) {
	var captured reflect.Value
	capture, ok := reflect.TypeAssert[func(*types.Var) bool](reflect.MakeFunc(typesVarPredicateType, func(arguments []reflect.Value) []reflect.Value {
		captured = arguments[0]
		return []reflect.Value{reflect.ValueOf(true)}
	}))
	require.True(t, ok)
	for _, variable := range []*types.Var{types.NewVar(0, nil, "x", types.Typ[types.Int]), nil} {
		capture(variable)
		place := func(argument reflect.Value) reflect.Value {
			registers := NewRegisters([isa.NumRegisterKinds]uint32{isa.RegisterGeneral: 1})
			assignReflectParams(&registers, []isa.RegisterKind{isa.RegisterGeneral}, nil, []reflect.Value{argument})
			return registers.General[0]
		}
		want, got := place(captured), place(typesVarClosureArgument(variable))
		require.Equal(t, want.Type(), got.Type())
		require.Equal(t, want.Kind(), got.Kind())
		require.Equal(t, want.CanAddr(), got.CanAddr())
		require.Equal(t, want.CanSet(), got.CanSet())
		require.Equal(t, want.CanInterface(), got.CanInterface())
		require.Equal(t, want.Interface(), got.Interface())
	}
}
