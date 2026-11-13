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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/debug"
)

func compileSourceForOpcodeCheck(t *testing.T, source string) string {
	t.Helper()
	service := app.NewService()
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.NoErrorf(t, err, "compile failed")
	return debug.DisassembleAssembly(compiled)
}

func TestSelectDirectCallOpcodeGate(t *testing.T) {
	t.Parallel()
	runDisassemblyExpectations(t, compileSourceForOpcodeCheck, []disassemblyExpectation{
		{
			name: "accepts scalar call",
			source: `package main
func fib(n int) int {
	if n < 2 { return n }
	return fib(n-1) + fib(n-2)
}
func EntrypointRun() int { return fib(10) }`,
			contains: []string{"CALL_SCALAR"},
			absent:   []string{":CALL "},
		},
		{
			name: "rejects variadic",
			source: `package main
func sumAll(values ...int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}
func EntrypointRun() int { return sumAll(1, 2, 3, 4, 5) }`,
			absent: []string{"CALL_SCALAR"},
		},
		{
			name: "rejects struct param",
			source: `package main
type Pair struct { Left int; Right int }
func add(pair Pair) int { return pair.Left + pair.Right }
func EntrypointRun() int { return add(Pair{Left: 3, Right: 4}) }`,
			absent: []string{"CALL_SCALAR"},
		},
		{
			name: "rejects struct result",
			source: `package main
type Point struct { X int; Y int }
func origin() Point { return Point{X: 0, Y: 0} }
func EntrypointRun() int {
	point := origin()
	return point.X + point.Y
}`,
			absent: []string{"CALL_SCALAR"},
		},
		{
			name: "rejects slice param",
			source: `package main
func sumSlice(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}
func EntrypointRun() int {
	values := []int{1, 2, 3, 4, 5}
	return sumSlice(values)
}`,
			absent: []string{"CALL_SCALAR"},
		},
		{
			name: "rejects closure call",
			source: `package main
func makeAdd(delta int) func(int) int {
	return func(value int) int { return value + delta }
}
func EntrypointRun() int {
	addOne := makeAdd(1)
	return addOne(41)
}`,
			contains: []string{":CALL "},
		},
		{
			name: "accepts mixed scalar kinds",
			source: `package main
func mix(intValue int, uintValue uint, floatValue float64, boolValue bool, textValue string) int {
	total := 0
	for step := 0; step < intValue; step++ {
		if boolValue {
			total += intValue + int(uintValue) + int(floatValue) + len(textValue)
		}
	}
	return total
}
func EntrypointRun() int {
	value := mix(1, 2, 3.0, true, "abcd")
	return value + 1
}`,
			contains: []string{"CALL_SCALAR"},
		},
	})
}

func TestStructLiteralEmitsTier0FieldSets(t *testing.T) {
	t.Parallel()
	dump := compileSourceForOpcodeCheck(t, `package main
type token struct {
	kind  int
	value string
}
func EntrypointRun(n int) int {
	tokens := []token{}
	for i := 0; i < n; i++ {
		tokens = append(tokens, token{kind: i, value: "v"})
	}
	return len(tokens)
}`)

	require.Positivef(t, strings.Count(dump, "SET_STRUCT_FIELD_INT64"),
		"expected the int field of the struct literal to use the tier-0 field set; disasm:\n%s", dump)
	require.Zerof(t, strings.Count(dump, "SET_FIELD_INT64"),
		"expected no reflect SET_FIELD_INT64 for a layout-resolved struct literal; disasm:\n%s", dump)
}
