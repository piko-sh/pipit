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

package astpattern

import (
	"context"
	"errors"
	"go/ast"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/compile/patterns"
	"pipit.sh/pipit/internal/compile/scope"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

var errBodyFailed = errors.New("body failed")

type recordingEmitter struct {
	function        *program.CompiledFunction
	scopes          *scope.ScopeStack
	expressionKinds map[string]isa.RegisterKind
	statementErr    error
	statements      int
	fallbacks       int
}

func (e *recordingEmitter) CurrentFunction() *program.CompiledFunction { return e.function }

func (e *recordingEmitter) ScopeStack() *scope.ScopeStack { return e.scopes }

func (*recordingEmitter) SetDebugPosition(context.Context, token.Pos) {}

func (e *recordingEmitter) CompileExpression(_ context.Context, expression ast.Expr) (program.VarLocation, error) {
	identifier, ok := expression.(*ast.Ident)
	if !ok {
		return program.VarLocation{}, errBodyFailed
	}
	kind, known := e.expressionKinds[identifier.Name]
	if !known {
		return program.VarLocation{}, errBodyFailed
	}
	return program.VarLocation{Register: e.scopes.Alloc.Alloc(kind), Kind: kind}, nil
}

func (e *recordingEmitter) CompileStmt(context.Context, ast.Stmt) (program.VarLocation, error) {
	e.statements++
	return program.VarLocation{}, e.statementErr
}

func (e *recordingEmitter) CompileForFallback(context.Context, *ast.ForStmt) (program.VarLocation, error) {
	e.fallbacks++
	return program.VarLocation{}, nil
}

func newRecordingEmitter(kinds map[string]isa.RegisterKind) *recordingEmitter {
	scopes := scope.NewScopeStack("f")
	scopes.PushScope()
	for name, kind := range kinds {
		scopes.DeclareVar(name, kind)
	}
	return &recordingEmitter{
		function:        program.NewNamedFunction("f"),
		scopes:          scopes,
		expressionKinds: map[string]isa.RegisterKind{"k": isa.RegisterFloat64, "n": isa.RegisterInt64},
		statementErr:    nil,
		statements:      0,
		fallbacks:       0,
	}
}

func typedSlices() map[string]isa.RegisterKind {
	return map[string]isa.RegisterKind{
		"a": isa.RegisterSliceFloat64, "b": isa.RegisterSliceFloat64, "d": isa.RegisterSliceFloat64,
		"s": isa.RegisterFloat64, "i": isa.RegisterInt64, "n": isa.RegisterInt64,
	}
}

func emitMatched(t *testing.T, emitter *recordingEmitter, body string) (program.VarLocation, error) {
	t.Helper()
	loop, info := checkedLoop(t, body)
	matched, matchToken, ok := DefaultRegistry().TryRecogniseForStmt(patterns.RecogniseContext{Info: info, Options: bothOptions()}, loop)
	require.True(t, ok, "the fixture loop is recognised")
	return matched.Emit(t.Context(), emitter, loop, matchToken)
}

func emittedSubOperations(function *program.CompiledFunction) []isa.SubOpcode {
	var found []isa.SubOpcode
	for _, word := range function.Body {
		if word.Op == isa.OpDrillTier1 {
			found = append(found, isa.SubOpcode(word.A))
		}
	}
	return found
}

func TestEachKernelEmitsItsVectorOperation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want isa.SubOpcode
	}{
		{name: "dot product", body: "for i := 0; i < len(a); i++ { s += a[i] * b[i] }", want: isa.SubOpSimdDotProductFloat64},
		{name: "dot product over a constant", body: "for i := 0; i < 300; i++ { s += a[i] * b[i] }", want: isa.SubOpSimdDotProductFloat64},
		{name: "sum", body: "for i := 0; i < len(a); i++ { s += a[i] }", want: isa.SubOpSimdSumFloat64},
		{name: "add", body: "for i := 0; i < len(a); i++ { d[i] = a[i] + b[i] }", want: isa.SubOpSimdAddFloat64},
		{name: "scale", body: "for i := 0; i < len(a); i++ { a[i] *= k }", want: isa.SubOpSimdScaleFloat64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			emitter := newRecordingEmitter(typedSlices())
			_, err := emitMatched(t, emitter, tt.body)
			require.NoError(t, err)
			require.Zero(t, emitter.fallbacks)
			require.Contains(t, emittedSubOperations(emitter.function), tt.want)
		})
	}
}

func TestAKernelOverBoxedSlicesUnboxesThem(t *testing.T) {
	t.Parallel()

	kinds := typedSlices()
	kinds["a"], kinds["b"] = isa.RegisterGeneral, isa.RegisterGeneral
	emitter := newRecordingEmitter(kinds)
	_, err := emitMatched(t, emitter, "for i := 0; i < len(a); i++ { s += a[i] * b[i] }")
	require.NoError(t, err)
	operations := emittedSubOperations(emitter.function)
	require.Contains(t, operations, isa.SubOpUnboxSliceFloat64)
	require.Contains(t, operations, isa.SubOpSimdDotProductFloat64)
}

func TestAKernelFallsBackWhenAnOperandIsNotInATypedBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kinds func(map[string]isa.RegisterKind)
		name  string
		body  string
	}{
		{name: "an integer destination", body: "for i := 0; i < len(a); i++ { s += a[i] }",
			kinds: func(kinds map[string]isa.RegisterKind) { kinds["s"] = isa.RegisterInt64 }},
		{name: "an undeclared slice", body: "for i := 0; i < len(a); i++ { s += a[i] * b[i] }",
			kinds: func(kinds map[string]isa.RegisterKind) { delete(kinds, "b") }},
		{name: "a slice in another bank", body: "for i := 0; i < len(a); i++ { d[i] = a[i] + b[i] }",
			kinds: func(kinds map[string]isa.RegisterKind) { kinds["d"] = isa.RegisterInt64 }},
		{name: "an outer counter in another bank", body: "i := 0\n_ = i\nfor i = 0; i < len(a); i++ { s += a[i] }",
			kinds: func(kinds map[string]isa.RegisterKind) { kinds["i"] = isa.RegisterFloat64 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			kinds := typedSlices()
			tt.kinds(kinds)
			emitter := newRecordingEmitter(kinds)
			_, err := emitMatched(t, emitter, tt.body)
			require.NoError(t, err)
			require.Equal(t, 1, emitter.fallbacks, "the loop is compiled as written")
		})
	}
}

func TestAKernelOnAnOuterCounterWritesTheCounterBack(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		"i := 0\n_ = i\nfor i = 0; i < len(a); i++ { s += a[i] }",
		"i := 0\n_ = i\nfor i = 0; i < 400; i++ { s += a[i] }",
		"i := 0\n_ = i\nfor i = 0; i < 4; i++ { s += a[i] }",
	} {
		emitter := newRecordingEmitter(typedSlices())
		_, err := emitMatched(t, emitter, body)
		require.NoError(t, err, body)
		require.Zero(t, emitter.fallbacks, body)
		require.Contains(t, emittedSubOperations(emitter.function), isa.SubOpSimdSumFloat64, body)
	}
}

func TestTheUnrollerEmitsTheBodyOncePerIteration(t *testing.T) {
	t.Parallel()

	emitter := newRecordingEmitter(typedSlices())
	_, err := emitMatched(t, emitter, "for i := 0; i < 3; i++ { ints[i] = i }")
	require.NoError(t, err)
	require.Equal(t, 3, emitter.statements)
	require.Zero(t, emitter.fallbacks)

	failing := newRecordingEmitter(typedSlices())
	failing.statementErr = errBodyFailed
	_, err = emitMatched(t, failing, "for i := 0; i < 3; i++ { ints[i] = i }")
	require.ErrorIs(t, err, errBodyFailed)

	outer := newRecordingEmitter(typedSlices())
	_, err = emitMatched(t, outer, "i := 0\n_ = i\nfor i = 0; i < 3; i++ { ints[i] = i }")
	require.NoError(t, err)
	require.Equal(t, 3, outer.statements, "an outer counter is set to the trip count afterwards")
}

func TestAnEmitGivenAnotherRecognisersTokenFallsBack(t *testing.T) {
	t.Parallel()

	loop, _ := checkedLoop(t, "for i := 0; i < 3; i++ { ints[i] = i }")
	for _, recogniser := range []patterns.Recogniser{&loopUnrollRecogniser{}, &simdKernelRecogniser{}} {
		emitter := newRecordingEmitter(typedSlices())
		_, err := recogniser.Emit(t.Context(), emitter, loop, "not a match")
		require.NoError(t, err)
		require.Equal(t, 1, emitter.fallbacks)
	}
	emitter := newRecordingEmitter(typedSlices())
	location, emitted, err := emitSimdKernelByKind(t.Context(), emitter, simdKernelMatch{})
	require.NoError(t, err)
	require.False(t, emitted)
	require.Equal(t, program.VarLocation{}, location)
}

func TestTheRecognisersRefuseLoopsTheyCannotRewrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		options bool
	}{
		{name: "a dot product into an integer", body: "for i := 0; i < len(a); i++ { n += int(a[i] * b[i]) }", options: true},
		{name: "a dot product subtracting", body: "for i := 0; i < len(a); i++ { s -= a[i] * b[i] }", options: true},
		{name: "a sum into an integer", body: "for i := 0; i < len(ints); i++ { n += ints[i] }", options: true},
		{name: "a sum bounded by another slice", body: "for i := 0; i < len(d); i++ { s += a[i] }", options: true},
		{name: "a scale of integers", body: "for i := 0; i < len(ints); i++ { ints[i] *= n }", options: true},
		{name: "a scale bounded by another slice", body: "for i := 0; i < len(d); i++ { a[i] *= k }", options: true},
		{name: "a scale that is not in place", body: "for i := 0; i < len(a); i++ { a[n] *= k }", options: true},
		{name: "an unrolled body that breaks", body: "for i := 0; i < 3; i++ { if ok { break } }", options: true},
		{name: "an unrolled body that continues", body: "for i := 0; i < 3; i++ { if ok { continue } }", options: true},
		{name: "an unrolled body that takes the counter's address", body: "for i := 0; i < 3; i++ { _ = &i }", options: true},
		{name: "an unrolled body that changes the counter", body: "for i := 0; i < 3; i++ { i++ }", options: true},
		{name: "a counter declared without :=", body: "var i int\nfor i = 1; i < 3; i++ { ints[i] = i }", options: true},
		{name: "a loop with the passes off", body: "for i := 0; i < 3; i++ { ints[i] = i }", options: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loop, info := checkedLoop(t, tt.body)
			options := passes.Options{SIMDKernels: tt.options, LoopUnroll: tt.options}
			matched, _, ok := DefaultRegistry().TryRecogniseForStmt(patterns.RecogniseContext{Info: info, Options: options}, loop)
			require.False(t, ok, "%s is compiled as written", tt.name)
			require.Nil(t, matched)
		})
	}
}
