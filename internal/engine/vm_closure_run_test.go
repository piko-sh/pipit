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
	"errors"
	"go/ast"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab"
)

type resultPoint struct {
	X    int
	Y    int
	Name string
}

func resultClosureFixture(t *testing.T, body func(builder *bytecodeBuilder)) (*VM, *RuntimeClosure) {
	t.Helper()
	builder := newBytecodeBuilder()
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

func returningInt(value int64) func(builder *bytecodeBuilder) {
	return func(builder *bytecodeBuilder) {
		constant := builder.addIntConst(value)
		builder.intRegisters(1).returnInt()
		builder.Emit(isa.OpLoadInt64Constant, 0, constant, 0)
		builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	}
}

func returningFloat(value float64) func(builder *bytecodeBuilder) {
	return func(builder *bytecodeBuilder) {
		constant := builder.addFloatConst(value)
		builder.floatRegisters(1).returnFloat()
		builder.Emit(isa.OpLoadFloat64Constant, 0, constant, 0)
		builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	}
}

func returningString(value string) func(builder *bytecodeBuilder) {
	return func(builder *bytecodeBuilder) {
		constant := builder.addStringConst(value)
		builder.stringRegisters(1).returnString()
		builder.Emit(isa.OpLoadStringConstant, 0, constant, 0)
		builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	}
}

func returningGeneral(value reflect.Value) func(builder *bytecodeBuilder) {
	return func(builder *bytecodeBuilder) {
		constant := builder.addGeneralConst(value)
		builder.generalRegisters(1).returnGeneral()
		builder.Emit(isa.OpLoadGeneralConstant, 0, constant, 0)
		builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	}
}

func returningIntAndString(number int64, text string) func(builder *bytecodeBuilder) {
	return func(builder *bytecodeBuilder) {
		numberConstant := builder.addIntConst(number)
		textConstant := builder.addStringConst(text)
		builder.intRegisters(1).stringRegisters(1)
		builder.resultKinds = []isa.RegisterKind{isa.RegisterInt64, isa.RegisterString}
		builder.Emit(isa.OpLoadInt64Constant, 0, numberConstant, 0)
		builder.Emit(isa.OpLoadStringConstant, 0, textConstant, 0)
		builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 2)
	}
}

func panicking(value reflect.Value) func(builder *bytecodeBuilder) {
	return func(builder *bytecodeBuilder) {
		constant := builder.addGeneralConst(value)
		builder.generalRegisters(1).returnGeneral()
		builder.Emit(isa.OpLoadGeneralConstant, 0, constant, 0)
		builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Panic), 0)
		builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	}
}

func callWrapper(t *testing.T, functionType reflect.Type, body func(builder *bytecodeBuilder), arguments ...reflect.Value) []any {
	t.Helper()
	maker, closure := resultClosureFixture(t, body)
	results := makeClosureWrapperFunc(maker, closure, functionType).Call(arguments)
	values := make([]any, len(results))
	for i, result := range results {
		values[i] = result.Interface()
	}
	return values
}

func TestACallbackHandsBackItsSingleResultOfEveryKind(t *testing.T) {
	failure := errors.New("refused")
	point := &resultPoint{X: 1, Y: 2, Name: "p"}
	tests := []struct {
		name         string
		functionType reflect.Type
		body         func(builder *bytecodeBuilder)
		want         any
	}{
		{name: "an int", functionType: reflect.TypeFor[func() int](), body: returningInt(42), want: 42},
		{name: "a narrower int", functionType: reflect.TypeFor[func() int8](), body: returningInt(-7), want: int8(-7)},
		{name: "a float", functionType: reflect.TypeFor[func() float64](), body: returningFloat(2.5), want: 2.5},
		{name: "a string", functionType: reflect.TypeFor[func() string](), body: returningString("text"), want: "text"},
		{name: "a bool", functionType: reflect.TypeFor[func() bool](), body: returningGeneral(reflect.ValueOf(true)), want: true},
		{name: "a named bool", functionType: reflect.TypeFor[func() namedTruth](), body: returningGeneral(reflect.ValueOf(namedTruth(true))), want: namedTruth(true)},
		{name: "an interface", functionType: reflect.TypeFor[func() error](), body: returningGeneral(reflect.ValueOf(failure)), want: failure},
		{name: "a nil interface", functionType: reflect.TypeFor[func() error](), body: returningGeneral(reflect.Value{}), want: error(nil)},
		{name: "an empty interface", functionType: reflect.TypeFor[func() any](), body: returningGeneral(reflect.ValueOf(7)), want: 7},
		{name: "a struct", functionType: reflect.TypeFor[func() resultPoint](), body: returningGeneral(reflect.ValueOf(*point)), want: *point},
		{name: "a pointer", functionType: reflect.TypeFor[func() *resultPoint](), body: returningGeneral(reflect.ValueOf(point)), want: point},
		{name: "a slice", functionType: reflect.TypeFor[func() []string](), body: returningGeneral(reflect.ValueOf([]string{"a", "b"})), want: []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, []any{tt.want}, callWrapper(t, tt.functionType, tt.body))
		})
	}
	t.Run("an ast.Inspect callback", func(t *testing.T) {
		maker, closure := inspectClosureFixture(t, returnsConstant(reflect.ValueOf(true)))
		typed, viaMakeFunc := inspectWrappers(t, maker, closure)
		require.True(t, typed(&ast.Ident{Name: "x"}))
		require.True(t, viaMakeFunc(nil))
	})
	t.Run("two results", func(t *testing.T) {
		require.Equal(t, []any{3, "three"}, callWrapper(t, reflect.TypeFor[func() (int, string)](), returningIntAndString(3, "three")))
	})
}

func TestACallbackVMReusedAfterAFailedRunHandsBackTheNextRunsResult(t *testing.T) {
	maker, failing := resultClosureFixture(t, panicking(reflect.ValueOf("boom")))
	wrapFailing := makeClosureWrapperFunc(maker, failing, reflect.TypeFor[func() string]())
	require.Equal(t, "", wrapFailing.Call(nil)[0].Interface(), "a failed run's callback returns the zero value")

	_, succeeding := resultClosureFixture(t, returningString("after"))
	succeeding.RootFunction = failing.RootFunction
	failing.RootFunction.Functions = append(failing.RootFunction.Functions, succeeding.Function)
	wrapSucceeding := makeClosureWrapperFunc(maker, succeeding, reflect.TypeFor[func() string]())
	require.Equal(t, "after", wrapSucceeding.Call(nil)[0].Interface())
	require.Equal(t, "", wrapFailing.Call(nil)[0].Interface())

	_, pair := resultClosureFixture(t, returningIntAndString(5, "five"))
	pair.RootFunction = failing.RootFunction
	failing.RootFunction.Functions = append(failing.RootFunction.Functions, pair.Function)
	wrapPair := makeClosureWrapperFunc(maker, pair, reflect.TypeFor[func() (int, string)]())
	results := wrapPair.Call(nil)
	require.Equal(t, 5, results[0].Interface())
	require.Equal(t, "five", results[1].Interface())
	require.Equal(t, "after", wrapSucceeding.Call(nil)[0].Interface())
}

func TestACallbackWithOneResultBuildsNoResultSlice(t *testing.T) {
	maker, closure := inspectClosureFixture(t, returnsConstant(reflect.ValueOf(true)))
	typed, _ := inspectWrappers(t, maker, closure)
	node := ast.Node(&ast.Ident{Name: "x"})
	allocations := testing.AllocsPerRun(200, func() {
		if !typed(node) {
			t.Fatal("the callback returns true")
		}
	})
	require.Zero(t, allocations,
		"a run with one result keeps it by value, and the typed adapter returns a plain bool without making a reflect Value of it")
}
