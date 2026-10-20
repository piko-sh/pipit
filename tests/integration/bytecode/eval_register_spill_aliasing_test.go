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
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/fault"
)

const spillAliasingLocals = 300

func writeLiveIntLocals(b *strings.Builder, n, skip int) (sumStatements string, sum int) {
	var tail strings.Builder
	for i := range n {
		fmt.Fprintf(b, "\tv%d := %d\n", i, i)
		if i == skip {
			continue
		}
		fmt.Fprintf(&tail, "\ttotal += v%d\n", i)
		sum += i
	}
	return tail.String(), sum
}

func TestRegisterSpillDeadSpilledLocalDoesNotAliasRegisterZero(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString("package main\n\nfunc run() int {\n\tkeep := 7\n")
	tail, sum := writeLiveIntLocals(&b, spillAliasingLocals, 260)
	b.WriteString("\tsink := v260 * 2\n\tw := 99\n\ttotal := keep + w + sink\n")
	b.WriteString(tail)
	b.WriteString("\treturn total\n}\n")

	result, err := app.NewService().EvalFile(t.Context(), b.String(), "run")
	require.NoError(t, err)
	require.Equal(t, 7+99+520+sum, result)
}

func TestRegisterSpillDeadSpilledStringDoesNotAliasRegisterZero(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString("package main\n\nfunc run() string {\n\tkeep := \"k\"\n")
	for i := range spillAliasingLocals {
		fmt.Fprintf(&b, "\ts%d := \"%d\"\n", i, i)
	}
	b.WriteString("\tsink := s260 + \"!\"\n\tw := \"w\"\n\ttotal := keep + w + sink\n")
	for i := range spillAliasingLocals {
		if i != 260 {
			fmt.Fprintf(&b, "\ttotal += s%d[:0]\n", i)
		}
	}
	b.WriteString("\treturn total\n}\n")

	result, err := app.NewService().EvalFile(t.Context(), b.String(), "run")
	require.NoError(t, err)
	require.Equal(t, "kw260!", result)
}

func TestRegisterSpillMultiReturnDefine(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		prelude string
		define  string
		use     string
		want    int
	}{
		"both spilled": {
			define: "\ta, b := pair()\n",
			use:    "a*1000 + b",
			want:   11*1000 + 13,
		},
		"redeclared spilled name": {
			define: "\ta := 1\n\ta, b := pair()\n",
			use:    "a*1000 + b",
			want:   11*1000 + 13,
		},
		"blank and spilled": {
			define: "\t_, b := pair()\n",
			use:    "b",
			want:   13,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var b strings.Builder
			b.WriteString("package main\n\nfunc pair() (int, int) { return 11, 13 }\n\nfunc run() int {\n\tkeep := 7\n")
			tail, sum := writeLiveIntLocals(&b, spillAliasingLocals, -1)
			b.WriteString(tc.define)
			fmt.Fprintf(&b, "\ttotal := keep*1000000 + %s\n", tc.use)
			b.WriteString(tail)
			b.WriteString("\treturn total\n}\n")

			result, err := app.NewService().EvalFile(t.Context(), b.String(), "run")
			require.NoError(t, err)
			require.Equal(t, 7*1000000+tc.want+sum, result)
		})
	}
}

func TestRegisterSpillMultiReturnDefineStringBank(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString("package main\n\nfunc pair() (string, string) { return \"x\", \"y\" }\n\nfunc run() string {\n\tkeep := \"k\"\n")
	for i := range spillAliasingLocals {
		fmt.Fprintf(&b, "\ts%d := \"%d\"\n", i, i)
	}
	b.WriteString("\ta, c := pair()\n\ttotal := keep + a + c\n")
	for i := range spillAliasingLocals {
		fmt.Fprintf(&b, "\ttotal += s%d[:0]\n", i)
	}
	b.WriteString("\treturn total\n}\n")

	result, err := app.NewService().EvalFile(t.Context(), b.String(), "run")
	require.NoError(t, err)
	require.Equal(t, "kxy", result)
}

func TestRegisterSpillDeclaringFormsKeepTheirValues(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		prelude string
		body    string
		want    int
	}{
		"map comma-ok": {
			prelude: "\tm := map[string]int{\"a\": 5}\n",
			body:    "\tv, ok := m[\"a\"]\n\tgot := v\n\tif ok {\n\t\tgot += 100\n\t}\n",
			want:    105,
		},
		"type assertion comma-ok": {
			prelude: "\tvar iface any = 5\n",
			body:    "\tv, ok := iface.(int)\n\tgot := v\n\tif ok {\n\t\tgot += 100\n\t}\n",
			want:    105,
		},
		"channel receive comma-ok": {
			prelude: "\tch := make(chan int, 1)\n\tch <- 5\n",
			body:    "\tv, ok := <-ch\n\tgot := v\n\tif ok {\n\t\tgot += 100\n\t}\n",
			want:    105,
		},
		"range over slice": {
			body: "\tgot := 0\n\tfor k, v := range []int{10, 20, 30} {\n\t\tgot += k*100 + v\n\t}\n",
			want: 360,
		},
		"range over map": {
			body: "\tgot := 0\n\tfor k, v := range map[int]int{4: 6} {\n\t\tgot += k*100 + v\n\t}\n",
			want: 406,
		},
		"range over int": {
			body: "\tgot := 0\n\tfor i := range 4 {\n\t\tgot += i\n\t}\n",
			want: 6,
		},
		"range over string": {
			body: "\tgot := 0\n\tfor i, r := range \"ab\" {\n\t\tgot += i*1000 + int(r)\n\t}\n",
			want: 1000 + 97 + 98,
		},
		"range over channel": {
			prelude: "\tch := make(chan int, 2)\n\tch <- 3\n\tch <- 4\n\tclose(ch)\n",
			body:    "\tgot := 0\n\tfor v := range ch {\n\t\tgot += v\n\t}\n",
			want:    7,
		},
		"type switch binding": {
			prelude: "\tvar iface any = 5\n",
			body:    "\tgot := 0\n\tswitch v := iface.(type) {\n\tcase int:\n\t\tgot = v + 100\n\t}\n",
			want:    105,
		},
		"select receive": {
			prelude: "\tch := make(chan int, 1)\n\tch <- 5\n",
			body:    "\tgot := 0\n\tselect {\n\tcase v, ok := <-ch:\n\t\tgot = v\n\t\tif ok {\n\t\t\tgot += 100\n\t\t}\n\t}\n",
			want:    105,
		},
		"range assigning existing variables": {
			body: "\tvar k, v int\n\tgot := 0\n\tfor k, v = range map[int]int{4: 6} {\n\t\tgot += k*100 + v\n\t}\n\tgot += k*10000 + v*1000\n",
			want: 406 + 40000 + 6000,
		},
		"range assigning a package variable": {
			body: "\tgot := 0\n\tfor global = range \"ab\" {\n\t\tgot += global + 1\n\t}\n\tgot += global * 100\n",
			want: 3 + 100,
		},
		"map comma-ok into a package variable": {
			prelude: "\tm := map[string]int{\"a\": 5}\n",
			body:    "\tvar ok bool\n\tglobal, ok = m[\"a\"]\n\tgot := global\n\tif ok {\n\t\tgot += 100\n\t}\n",
			want:    105,
		},
		"type assertion comma-ok into a package variable": {
			prelude: "\tvar iface any = 5\n",
			body:    "\tvar ok bool\n\tglobal, ok = iface.(int)\n\tgot := global\n\tif ok {\n\t\tgot += 100\n\t}\n",
			want:    105,
		},
		"channel receive comma-ok into a package variable": {
			prelude: "\tch := make(chan int, 1)\n\tch <- 5\n",
			body:    "\tvar ok bool\n\tglobal, ok = <-ch\n\tgot := global\n\tif ok {\n\t\tgot += 100\n\t}\n",
			want:    105,
		},
		"zero-valued var declarations": {
			body: "\tvar a int\n\tvar s string\n\tvar f float64\n\tvar p *int\n\tgot := a + len(s) + int(f)\n\tif p == nil {\n\t\tgot += 100\n\t}\n",
			want: 100,
		},
		"call of a closure held in a spilled local": {
			body: "\tadd := func(n int) int { return n + 100 }\n\tgot := add(5)\n",
			want: 105,
		},
		"multi-value call of a closure held in a spilled local": {
			body: "\tsplit := func(n int) (int, int) { return n, 100 }\n\tx, y := split(5)\n\tgot := x + y\n",
			want: 105,
		},
		"capture inside a branch that does not run": {
			body: "\tlabel := 5\n\tgot := 0\n\tif keep < 0 {\n\t\tfunc() { got = label }()\n\t}\n\tgot += label + 100\n",
			want: 105,
		},
		"range over a typed slice": {
			prelude: "\tnums := make([]int, 3)\n\tnums[0], nums[1], nums[2] = 10, 20, 30\n",
			body:    "\tgot := 0\n\tfor _, x := range nums {\n\t\tgot += x\n\t}\n",
			want:    60,
		},
		"var declaration with call": {
			body: "\tvar got = pair1() + 100\n",
			want: 111,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var b strings.Builder
			b.WriteString("package main\n\nvar global int\n\nfunc pair1() int { return 11 }\n\nfunc run() int {\n\tkeep := 7\n")
			b.WriteString(tc.prelude)
			tail, sum := writeLiveIntLocals(&b, spillAliasingLocals, -1)
			b.WriteString(tc.body)
			b.WriteString("\ttotal := keep*1000000 + got\n")
			b.WriteString(tail)
			b.WriteString("\treturn total\n}\n")

			result, err := app.NewService().EvalFile(t.Context(), b.String(), "run")
			require.NoError(t, err)
			require.Equal(t, 7*1000000+tc.want+sum, result)
		})
	}
}

func TestRegisterSpillGeneralBankCopiesValues(t *testing.T) {
	t.Parallel()

	for locals := 238; locals <= 250; locals++ {
		var b strings.Builder
		b.WriteString("package main\n\ntype box struct{ N int }\n\nfunc run() int {\n")
		for i := range locals {
			fmt.Fprintf(&b, "\tg%d := []int{%d}\n", i, i)
		}
		b.WriteString("\ts1 := box{N: 7}\n\ts2 := s1\n\ts2.N = 99\n")
		b.WriteString("\ta1 := [2]int{1, 2}\n\ta2 := a1\n\ta2[0] = 50\n")
		b.WriteString("\ttotal := s1.N*100 + s2.N + a1[0]*1000 + a2[0]*10000\n")
		for i := range locals {
			fmt.Fprintf(&b, "\ttotal += len(g%d) - 1\n", i)
		}
		b.WriteString("\treturn total\n}\n")

		result, err := app.NewService().EvalFile(t.Context(), b.String(), "run")
		require.NoError(t, err, "%d locals", locals)
		require.Equal(t, 7*100+99+1000+500000, result, "%d locals", locals)
	}
}

func manyIntParameters(n int) string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("a%d", i)
	}
	return strings.Join(names, ", ") + " int"
}

func manyIntArguments(n int) string {
	values := make([]string, n)
	for i := range values {
		values[i] = fmt.Sprintf("%d", i)
	}
	return strings.Join(values, ", ")
}

func TestRegisterLargestSignatureKeepsEveryParameter(t *testing.T) {
	t.Parallel()

	const parameters = 127
	source := fmt.Sprintf("package main\n\nfunc wide(%s) int { return a0*1000 + a%d }\n\nfunc run() int { return wide(%s) }\n",
		manyIntParameters(parameters), parameters-1, manyIntArguments(parameters))

	result, err := app.NewService().EvalFile(t.Context(), source, "run")
	require.NoError(t, err)
	require.Equal(t, parameters-1, result)
}

func TestRegisterOversizeSignatureIsACompileError(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		source   string
		position string
	}{
		"function": {
			source:   fmt.Sprintf("func wide(%s) int { return a0 }\n\nfunc run() int { return wide(%s) }\n", manyIntParameters(300), manyIntArguments(300)),
			position: "main.go:3:1",
		},
		"closure": {
			source:   fmt.Sprintf("func run() int {\n\twide := func(%s) int { return a0 }\n\treturn wide(%s)\n}\n", manyIntParameters(129), manyIntArguments(129)),
			position: "main.go:4:10",
		},
		"method": {
			source:   fmt.Sprintf("type T struct{}\n\nfunc (T) wide(%s) int { return a0 }\n\nfunc run() int { return T{}.wide(%s) }\n", manyIntParameters(128), manyIntArguments(128)),
			position: "main.go:5:1",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := app.NewService().EvalFile(t.Context(), "package main\n\n"+tc.source, "run")
			require.ErrorIs(t, err, fault.ErrCompileSignatureTooLarge)
			require.Contains(t, err.Error(), tc.position)
		})
	}
}
