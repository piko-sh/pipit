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

package compile

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
)

const genericTypeMethodSource = `package main

type item[T any] struct {
	value T
	next  *item[T]
}

type stack[T any] struct{ top *item[T] }

func (s *stack[T]) push(value T) { s.top = &item[T]{value: value, next: s.top} }

func (s *stack[T]) pushTwice(value T) {
	s.push(value)
	s.push(value)
}

type pusher interface{ push(int) }

type flags struct{ stack[bool] }

func main() {
	ints := &stack[int]{}
	ints.push(1)
	ints.pushTwice(2)
	push := ints.push
	push(3)
	(*stack[int]).push(ints, 4)
	strings := &stack[string]{}
	strings.push("a")
	var p pusher = ints
	p.push(5)
	held := &flags{}
	held.push(true)
}
`

func TestMethodsOfAGenericTypeAreSpecialisedForTheirReceiver(t *testing.T) {
	t.Parallel()

	root := compileSnippet(t, genericTypeMethodSource)
	names := make(map[string]bool)
	for _, function := range root.SubFunctions() {
		names[function.FunctionName()] = true
	}

	require.True(t, names["stack.push[spec:int]"], "a call, value and expression on *stack[int] share one body: %v", names)
	require.True(t, names["stack.push[spec:string]"], "each receiver instantiation gets its own body: %v", names)
	require.True(t, names["stack.pushTwice[spec:int]"], "%v", names)
	require.True(t, names["stack.push[spec:bool]"], "a method promoted from an embedded instantiation is specialised: %v", names)
	require.True(t, names["stack.push"], "the erased body stays for interface dispatch: %v", names)

	specialisations := 0
	for _, function := range root.SubFunctions() {
		if function.FunctionName() == "stack.push[spec:int]" {
			specialisations++
		}
	}
	require.Equal(t, 1, specialisations)
}

func TestASpecialisedGenericTypeMethodCallsTheSpecialisationOfItsPeers(t *testing.T) {
	t.Parallel()

	root := compileSnippet(t, genericTypeMethodSource)
	pushTwice := findCompiledFunction(t, root, "stack.pushTwice[spec:int]")
	pushInt := findCompiledFunction(t, root, "stack.push[spec:int]")
	erased := findCompiledFunction(t, root, "stack.push")

	callees := calleesOf(root, pushTwice)
	require.Contains(t, callees, pushInt, "a call on the instantiated receiver inside a specialised body is specialised")
	require.NotContains(t, callees, erased)

	erasedTwice := findCompiledFunction(t, root, "stack.pushTwice")
	require.Contains(t, calleesOf(root, erasedTwice), erased,
		"the erased body's receiver is still a type parameter, so it calls the erased body")
}

func calleesOf(root, caller *program.CompiledFunction) []*program.CompiledFunction {
	functions := root.SubFunctions()
	var callees []*program.CompiledFunction
	for _, site := range caller.CallSites {
		if int(site.FunctionIndex) < len(functions) {
			callees = append(callees, functions[site.FunctionIndex])
		}
	}
	return callees
}
