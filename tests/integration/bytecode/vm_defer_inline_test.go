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

//go:build integration

package bytecode_test

import (
	"context"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"

	"github.com/stretchr/testify/require"
)

func runDeferScript(t *testing.T, source string) (any, *engine.VM) {
	t.Helper()
	compiled := compileExpression(t, source)
	vm := newTestVM(t)
	result, err := vm.Execute(compiled)
	require.NoError(t, err)
	return result, vm
}

func TestDeferDoesNotNestDispatch(t *testing.T) {
	t.Parallel()
	result, vm := runDeferScript(t, `
total := 0
work := func(n int) {
	defer func(v int) { total += v }(n)
}
for i := 0; i < 50; i++ { work(i) }
total`)
	require.EqualValues(t, 50*49/2, result)
	require.Equal(t, 1, vm.DispatchEntries, "a deferred closure runs in the returning frame's own dispatch loop")
}

func TestDeferWithExplicitReturnDoesNotNestDispatch(t *testing.T) {
	t.Parallel()
	result, vm := runDeferScript(t, `
total := 0
work := func(n int) int {
	defer func(v int) { total += v }(n)
	return n * 2
}
sum := 0
for i := 0; i < 50; i++ { sum += work(i) }
sum + total`)
	require.EqualValues(t, 50*49/2*3, result)
	require.Equal(t, 1, vm.DispatchEntries)
}

func TestDeferStackDoesNotNestDispatch(t *testing.T) {
	t.Parallel()
	result, vm := runDeferScript(t, `
func f() (result string) {
	defer func() { result += "a" }()
	defer func() { result += "b" }()
	defer func() { result += "c" }()
	return ""
}
f() + f()`)
	require.Equal(t, "cbacba", result)
	require.Equal(t, 1, vm.DispatchEntries, "every deferred closure of the frame runs inline, last in first out")
}

func TestDeferWithRecoverKeepsNestedDispatch(t *testing.T) {
	t.Parallel()
	result, vm := runDeferScript(t, `
func f() (r string) {
	defer func() {
		if v := recover(); v != nil { r = "recovered" }
	}()
	panic("boom")
}
f()`)
	require.Equal(t, "recovered", result)
	require.Equal(t, 2, vm.DispatchEntries, "a deferred closure that recovers keeps the nested dispatch that scopes recover")
}

func TestPanicInInlineDeferUnwindsRemainingDefersInOrder(t *testing.T) {
	t.Parallel()
	result, err := app.NewService().Eval(context.Background(), `
var log []string
func f() {
	defer func() { log = append(log, "a") }()
	defer func() { panic("boom") }()
	defer func() { log = append(log, "c") }()
}
func outer() (r string) {
	defer func() {
		if v := recover(); v != nil { r = v.(string) }
	}()
	f()
	return "no panic"
}
outer() + ":" + log[0] + log[1]`)
	require.NoError(t, err)
	require.Equal(t, "boom:ca", result)
}

func TestPanicInTrivialInlineDeferPropagates(t *testing.T) {
	t.Parallel()
	_, err := app.NewService().Eval(context.Background(), `
func f() int {
	defer func() { panic("deferred boom") }()
	return 1
}
f()`)
	require.ErrorContains(t, err, "deferred boom")
}

func TestInlineDeferNamedResultsObserveDeferredWrites(t *testing.T) {
	t.Parallel()
	result, err := app.NewService().Eval(context.Background(), `
func f() (x int) {
	defer func() { x *= 2 }()
	x = 21
	return x
}
func g() (a int, b int) {
	a, b = 1, 2
	defer func() { a += 10; b += 20 }()
	return
}
a, b := g()
f() + a + b`)
	require.NoError(t, err)
	require.EqualValues(t, 42+11+22, result)
}
