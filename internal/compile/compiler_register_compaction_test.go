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
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

const leakElements = 300

const rightDeepLevels = 70

const leakPeakBound = 24

func repeatJoined(n int, separator string, element func(int) string) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = element(i)
	}
	return strings.Join(parts, separator)
}

func requireRegisterPeak(t *testing.T, source, function string, kind isa.RegisterKind, bound uint32) {
	t.Helper()
	root, err := tryCompileSnippet(t, source)
	require.NoError(t, err)
	if function != "" {
		peak := findCompiledFunction(t, root, function).NumRegisters[kind]
		require.LessOrEqual(t, peak, bound, "%s needs %d %s registers", function, peak, kind)
		return
	}
	for _, compiled := range root.SubFunctions() {
		peak := compiled.NumRegisters[kind]
		require.LessOrEqual(t, peak, bound, "%s needs %d %s registers", compiled.FunctionName(), peak, kind)
	}
}

func TestRegisterLeaksAreReclaimedPerElement(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		source   string
		function string
		kind     isa.RegisterKind
		bound    uint32
	}{
		"variadic bytecode call": {
			source: "package main\n\nfunc count(values ...int) int { return len(values) }\n\n" +
				"func run(x int) int {\n\treturn count(" + repeatJoined(leakElements, ", ", func(i int) string { return fmt.Sprintf("x+%d", i) }) + ")\n}\n",
			function: "run",
			kind:     isa.RegisterInt64,
		},
		"multi-value call spread into a variadic call": {
			source: "package main\n\nfunc count(values ...int) int { return len(values) }\n\n" +
				"func many() (" + repeatJoined(120, ", ", func(int) string { return "int" }) + ") {\n\treturn " +
				repeatJoined(120, ", ", func(i int) string { return fmt.Sprintf("%d", i) }) + "\n}\n\n" +
				"func run() int {\n\treturn count(many())\n}\n",
			function: "run",
			kind:     isa.RegisterGeneral,
		},
		"min and max": {
			source: "package main\n\nfunc run(x int, y float64) float64 {\n\treturn float64(max(" +
				repeatJoined(leakElements, ", ", func(i int) string { return fmt.Sprintf("x+%d", i) }) + ")) + min(" +
				repeatJoined(leakElements, ", ", func(i int) string { return fmt.Sprintf("y+%d", i) }) + ")\n}\n",
			function: "run",
			kind:     isa.RegisterInt64,
		},
		"return inside a range-over-func body": {
			source: "package main\n\nfunc h(a, b, c, d, e, f, g, i int) int { return a }\n\n" +
				"func seq(yield func(int) bool) { yield(1) }\n\n" +
				"func run(x int) (" + repeatJoined(30, ", ", func(int) string { return "int" }) + ") {\n" +
				"\tfor v := range seq {\n\t\treturn " +
				repeatJoined(30, ", ", func(int) string { return "h(x+v, x+1, x+2, x+3, x+4, x+5, x+6, x+7) + 1" }) +
				"\n\t}\n\treturn " + repeatJoined(30, ", ", func(int) string { return "0" }) + "\n}\n",
			kind:  isa.RegisterInt64,
			bound: 2*30 + leakPeakBound,
		},
		"struct literal stored into a collection": {
			source: "package main\n\ntype big struct {\n" +
				repeatJoined(120, "", func(i int) string { return fmt.Sprintf("\tF%d int\n", i) }) + "}\n\n" +
				"func f(n int) int { return n }\n\n" +
				"func run(items []big, i, x int) {\n\titems[i] = big{" +
				repeatJoined(120, ", ", func(i int) string { return fmt.Sprintf("F%d: f(x+%d)", i, i) }) + "}\n}\n",
			function: "run",
			kind:     isa.RegisterInt64,
		},
		"append of many values to an int slice": {
			source: "package main\n\nfunc run(s []int, x int) []int {\n\treturn append(s, " +
				repeatJoined(leakElements, ", ", func(i int) string { return fmt.Sprintf("x+%d", i) }) + ")\n}\n",
			function: "run",
			kind:     isa.RegisterInt64,
		},
		"append of many values to a string slice": {
			source: "package main\n\nfunc run(s []string, x string) []string {\n\treturn append(s, " +
				repeatJoined(leakElements, ", ", func(i int) string { return fmt.Sprintf("x+\"%d\"", i) }) + ")\n}\n",
			function: "run",
			kind:     isa.RegisterGeneral,
		},
		"arithmetic chain of calls": {
			source: "package main\n\nfunc f(n int) int { return n }\n\nfunc run(x int) int {\n\treturn " +
				repeatJoined(leakElements, " + ", func(i int) string { return fmt.Sprintf("f(x+%d)", i) }) + "\n}\n",
			function: "run",
			kind:     isa.RegisterInt64,
		},
		"string concatenation": {
			source: "package main\n\nfunc s(n int) string { return \"\" }\n\nfunc run(x int) string {\n\treturn " +
				repeatJoined(leakElements, " + ", func(i int) string { return fmt.Sprintf("s(x+%d)", i) }) + "\n}\n",
			function: "run",
			kind:     isa.RegisterString,
		},
		"bitwise chain of shifts": {
			source: "package main\n\nfunc run(x int) int {\n\treturn " +
				repeatJoined(leakElements, " | ", func(i int) string { return fmt.Sprintf("(x+%d)<<%d", i, i%60) }) + "\n}\n",
			function: "run",
			kind:     isa.RegisterInt64,
		},
		"right-deep chain": {
			source: "package main\n\nfunc f(n int) int { return n }\n\nfunc run(x int) int {\n\treturn " +
				repeatJoined(rightDeepLevels, " + (", func(i int) string { return fmt.Sprintf("f(x+%d)", i) }) + strings.Repeat(")", rightDeepLevels-1) + "\n}\n",
			function: "run",
			kind:     isa.RegisterInt64,
			bound:    3*rightDeepLevels + leakPeakBound,
		},
		"short-circuit chains": {
			source: "package main\n\nfunc f(n int) bool { return n > 0 }\n\nfunc run(x int) bool {\n\treturn (" +
				repeatJoined(leakElements, " && ", func(i int) string { return fmt.Sprintf("f(x+%d)", i) }) + ") || (" +
				repeatJoined(leakElements, " || ", func(i int) string { return fmt.Sprintf("f(x-%d)", i) }) + ")\n}\n",
			function: "run",
			kind:     isa.RegisterInt64,
		},
		"multi-value return": {
			source: "package main\n\nfunc f(a, b, c, d int) int { return a }\n\nfunc run(x int) (" +
				repeatJoined(100, ", ", func(int) string { return "int" }) + ") {\n\treturn " +
				repeatJoined(100, ", ", func(i int) string { return fmt.Sprintf("f(x+%d, x+1, x+2, x+3)", i) }) + "\n}\n",
			function: "run",
			kind:     isa.RegisterInt64,
			bound:    2*100 + leakPeakBound,
		},
		"tuple assignment": {
			source: "package main\n\nfunc f(a, b, c, d int) int { return a }\n\nfunc run(x int) int {\n\tvar " +
				repeatJoined(100, ", ", func(i int) string { return fmt.Sprintf("t%d", i) }) + " int\n\t" +
				repeatJoined(100, ", ", func(i int) string { return fmt.Sprintf("t%d", i) }) + " = " +
				repeatJoined(100, ", ", func(i int) string { return fmt.Sprintf("f(x+%d, x+1, x+2, x+3)", i) }) + "\n\treturn " +
				repeatJoined(100, " + ", func(i int) string { return fmt.Sprintf("t%d", i) }) + "\n}\n",
			function: "run",
			kind:     isa.RegisterInt64,
			bound:    2*100 + leakPeakBound,
		},
		"expression switch clauses": {
			source: "package main\n\nfunc f(n int) int { return n }\n\nfunc run(x int) int {\n\ttotal := 0\n\tswitch x {\n" +
				repeatJoined(leakElements, "", func(i int) string { return fmt.Sprintf("\tcase f(x+%d):\n\t\ttotal += %d\n", i, i) }) +
				"\t}\n\treturn total\n}\n",
			function: "run",
			kind:     isa.RegisterInt64,
		},
		"tagless switch clauses": {
			source: "package main\n\nfunc f(n int) int { return n }\n\nfunc run(x int) int {\n\ttotal := 0\n\tswitch {\n" +
				repeatJoined(leakElements, "", func(i int) string { return fmt.Sprintf("\tcase x == f(%d)+1:\n\t\ttotal += %d\n", i, i) }) +
				"\t}\n\treturn total\n}\n",
			function: "run",
			kind:     isa.RegisterInt64,
		},
		"one clause with many values": {
			source: "package main\n\nfunc run(x int) int {\n\tswitch x {\n\tcase " +
				repeatJoined(leakElements, ", ", func(i int) string { return fmt.Sprintf("x*%d+1", i+2) }) +
				":\n\t\treturn 1\n\t}\n\treturn 0\n}\n",
			function: "run",
			kind:     isa.RegisterInt64,
		},
		"type switch chain": {
			source: "package main\n\n" + repeatJoined(leakElements, "", func(i int) string { return fmt.Sprintf("type t%d struct{ f int }\n", i) }) +
				"\nfunc run(v any) int {\n\tswitch v.(type) {\n" +
				repeatJoined(leakElements, "", func(i int) string { return fmt.Sprintf("\tcase t%d:\n\t\treturn %d\n", i, i) }) +
				"\t}\n\treturn -1\n}\n",
			function: "run",
			kind:     isa.RegisterGeneral,
		},
		"switch case body": {
			source: "package main\n\nfunc f(n int) int { return n }\n\nfunc run(x int) {\n\tswitch {\n\tcase x > 0:\n" +
				repeatJoined(leakElements, "", func(i int) string { return fmt.Sprintf("\t\t_ = f(x+%d) + f(x+%d)\n", i, i+1) }) + "\t}\n}\n",
			function: "run",
			kind:     isa.RegisterInt64,
		},
		"type switch case and default bodies": {
			source: "package main\n\nfunc f(n int) int { return n }\n\nfunc run(v any, x int) {\n\tswitch v.(type) {\n\tcase int:\n" +
				repeatJoined(leakElements, "", func(i int) string { return fmt.Sprintf("\t\t_ = f(x+%d) + f(x+%d)\n", i, i+1) }) + "\tdefault:\n" + repeatJoined(leakElements, "", func(i int) string { return fmt.Sprintf("\t\t_ = f(x+%d) + f(x+%d)\n", i, i+1) }) + "\t}\n}\n",
			function: "run",
			kind:     isa.RegisterInt64,
		},
		"select case body": {
			source: "package main\n\nfunc f(n int) int { return n }\n\nfunc run(ch chan int, x int) {\n\tselect {\n\tcase <-ch:\n" +
				repeatJoined(leakElements, "", func(i int) string { return fmt.Sprintf("\t\t_ = f(x+%d) + f(x+%d)\n", i, i+1) }) + "\tdefault:\n" + repeatJoined(leakElements, "", func(i int) string { return fmt.Sprintf("\t\t_ = f(x+%d) + f(x+%d)\n", i, i+1) }) + "\t}\n}\n",
			function: "run",
			kind:     isa.RegisterInt64,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			requireRegisterPeak(t, tc.source, tc.function, tc.kind, max(tc.bound, leakPeakBound))
		})
	}
}
