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
	"strings"
	"testing"

	"pipit.sh/pipit/internal/app"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/debug"
)

const (
	typeSwitchJumpTableProgram = `package main

type point struct{ x, y int }

type box struct{ label string }

func classify(v any) string {
	switch t := v.(type) {
	case int:
		return "int:" + itoa(t)
	case string:
		return "string:" + t
	case float64:
		return "float"
	case point:
		return "point:" + itoa(t.x+t.y)
	case *box:
		return "boxptr:" + t.label
	case []int:
		return "ints:" + itoa(len(t))
	default:
		return "default"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func EntrypointRun() string {
	var nothing any
	values := []any{7, "s", 2.5, point{x: 3, y: 4}, &box{label: "b"}, []int{1, 2, 3}, true, nothing, box{label: "value"}}
	out := ""
	for _, v := range values {
		out += classify(v) + ";"
	}
	return out
}
`
)

const (
	typeSwitchJumpTableExpected = "int:7;string:s;float;point:7;boxptr:b;ints:3;default;default;default;"
)

func TestTypeSwitchJumpTableEmittedForFiveOrMoreCases(t *testing.T) {
	t.Parallel()
	dump := compileSourceForOpcodeCheck(t, typeSwitchJumpTableProgram)
	require.Positivef(t, strings.Count(dump, "TYPE_SWITCH_JUMP"), "expected a jump-table head; disasm:\n%s", dump)
	require.Equalf(t, 6, strings.Count(dump, "TYPE_SWITCH_CASE"), "expected one row per clause; disasm:\n%s", dump)
	require.Zerof(t, strings.Count(dump, "TYPE_ASSERT"), "expected no per-clause probes; disasm:\n%s", dump)
}

func TestTypeSwitchJumpTableNotEmittedForFourCases(t *testing.T) {
	t.Parallel()
	dump := compileSourceForOpcodeCheck(t, `package main

func classify(v any) int {
	switch v.(type) {
	case int:
		return 1
	case string:
		return 2
	case float64:
		return 3
	case bool:
		return 4
	}
	return 0
}

func EntrypointRun() int { return classify(1) + classify("s") }`)
	require.Zerof(t, strings.Count(dump, "TYPE_SWITCH_JUMP"), "four clauses must keep the probe chain; disasm:\n%s", dump)
	require.Positivef(t, strings.Count(dump, "TYPE_ASSERT"), "expected per-clause probes; disasm:\n%s", dump)
}

func TestTypeSwitchJumpTableNotEmittedForInterfaceOrNilClauses(t *testing.T) {
	t.Parallel()
	dump := compileSourceForOpcodeCheck(t, `package main

type stringer interface{ String() string }

func classify(v any) int {
	switch v.(type) {
	case int:
		return 1
	case string:
		return 2
	case float64:
		return 3
	case bool:
		return 4
	case stringer:
		return 5
	case nil:
		return 6
	}
	return 0
}

func EntrypointRun() int { return classify(1) }`)
	require.Zerof(t, strings.Count(dump, "TYPE_SWITCH_JUMP"), "interface and nil clauses must keep the probe chain; disasm:\n%s", dump)
}

func TestTypeSwitchJumpTableSemantics(t *testing.T) {
	t.Parallel()
	service := app.NewService()
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": typeSwitchJumpTableProgram})
	require.NoError(t, err)
	require.Contains(t, debug.DisassembleAssembly(compiled), "TYPE_SWITCH_JUMP")
	result, err := service.ExecuteEntrypoint(context.Background(), compiled, "EntrypointRun")
	require.NoError(t, err)
	require.Equal(t, typeSwitchJumpTableExpected, result)
}

func TestTypeSwitchJumpTableMatchesProbeChain(t *testing.T) {
	t.Parallel()
	chainProgram := strings.Replace(typeSwitchJumpTableProgram, "\tcase []int:\n\t\treturn \"ints:\" + itoa(len(t))\n", "", 1)
	chainProgram = strings.Replace(chainProgram, "\tcase float64:\n\t\treturn \"float\"\n", "", 1)
	service := app.NewService()
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": chainProgram})
	require.NoError(t, err)
	require.NotContains(t, debug.DisassembleAssembly(compiled), "TYPE_SWITCH_JUMP")
	result, err := service.ExecuteEntrypoint(context.Background(), compiled, "EntrypointRun")
	require.NoError(t, err)
	require.Equal(t, "int:7;string:s;default;point:7;boxptr:b;default;default;default;default;", result)
}

func TestTypeSwitchJumpTableBoundVariableNarrowsPerClause(t *testing.T) {
	t.Parallel()
	service := app.NewService()
	result, err := service.Eval(context.Background(), `

type a struct{ n int }

type b struct{ n int }

type c struct{ n int }

type d struct{ n int }

type e struct{ n int }
sum := 0
for _, v := range []any{a{1}, b{2}, c{3}, d{4}, e{5}, &e{6}} {
	switch t := v.(type) {
	case a:
		sum += t.n * 1
	case b:
		sum += t.n * 10
	case c:
		sum += t.n * 100
	case d:
		sum += t.n * 1000
	case e:
		sum += t.n * 10000
	default:
		sum += 1000000
	}
}
sum`)
	require.NoError(t, err)
	require.EqualValues(t, 1054321, result)
}

func TestTypeSwitchJumpTableBreakAndNoFallthrough(t *testing.T) {
	t.Parallel()
	service := app.NewService()
	result, err := service.Eval(context.Background(), `

func f(v any) int {
	out := 0
	switch v.(type) {
	case int:
		out = 1
		break
	case int8:
		out = 2
	case int16:
		out = 3
	case int32:
		out = 4
	case int64:
		out = 5
	}
	out *= 10
	return out
}
f(1) + f(int16(1)) + f(int64(1)) + f("x")`)
	require.NoError(t, err)
	require.EqualValues(t, 90, result)
}
