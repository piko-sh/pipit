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
	"pipit.sh/pipit/internal/compile/scope"
	"pipit.sh/pipit/internal/isa"
)

func TestRegisterWatermarkManyFieldAssignments(t *testing.T) {
	t.Parallel()

	const assignmentCount = 300

	var source strings.Builder
	source.WriteString("package main\n\n")
	source.WriteString("type Item struct {\n\tValue string\n}\n\n")
	source.WriteString("func run() string {\n")
	source.WriteString("\tresult := make([]Item, 0, 300)\n")
	for i := range assignmentCount {
		fmt.Fprintf(&source, "\tresult = append(result, Item{Value: \"v%d\"})\n", i)
	}
	source.WriteString("\treturn result[0].Value + result[299].Value\n")
	source.WriteString("}\n\nfunc main() {}\n")

	service := app.NewService()
	result, err := service.EvalFile(t.Context(), source.String(), "run")
	require.NoError(t, err)
	require.Equal(t, "v0v299", result)
}

func TestRegisterWatermarkManyExpressionStatements(t *testing.T) {
	t.Parallel()

	const callCount = 300

	var source strings.Builder
	source.WriteString("package main\n\n")
	source.WriteString("var counter int\n\n")
	source.WriteString("func inc() int {\n\tcounter++\n\treturn counter\n}\n\n")
	source.WriteString("func run() int {\n")
	for range callCount {
		source.WriteString("\tinc()\n")
	}
	source.WriteString("\treturn counter\n")
	source.WriteString("}\n\nfunc main() {}\n")

	service := app.NewService()
	result, err := service.EvalFile(t.Context(), source.String(), "run")
	require.NoError(t, err)
	require.Equal(t, int(callCount), result)
}

func TestRegisterWatermarkPreservesLocalVariables(t *testing.T) {
	t.Parallel()

	service := app.NewService()
	result, err := service.EvalFile(t.Context(), `package main

func noop(x int) {}

func run() int {
	a := 1
	noop(a + 2)
	b := 3
	noop(b + 4)
	c := 5
	noop(c + 6)
	return a + b + c
}

func main() {}
`, "run")
	require.NoError(t, err)
	require.Equal(t, 9, result)
}

func TestRegisterWatermarkPreservesVarDecl(t *testing.T) {
	t.Parallel()

	service := app.NewService()
	result, err := service.EvalFile(t.Context(), `package main

func noop(x int) {}

func run() int {
	var a int = 10
	noop(a + 1)
	var b int = 20
	noop(b + 2)
	return a + b
}

func main() {}
`, "run")
	require.NoError(t, err)
	require.Equal(t, 30, result)
}

func TestRegisterWatermarkMultiReturnDefine(t *testing.T) {
	t.Parallel()

	const extraStatements = 300

	var source strings.Builder
	source.WriteString("package main\n\n")
	source.WriteString("var counter int\n\n")
	source.WriteString("func twoValues() (int, int) { return 10, 20 }\n")
	source.WriteString("func inc() int { counter++; return counter }\n\n")
	source.WriteString("func run() int {\n")
	source.WriteString("\ta, b := twoValues()\n")
	for range extraStatements {
		source.WriteString("\tinc()\n")
	}
	source.WriteString("\treturn a + b\n")
	source.WriteString("}\n\nfunc main() {}\n")

	service := app.NewService()
	result, err := service.EvalFile(t.Context(), source.String(), "run")
	require.NoError(t, err)
	require.Equal(t, 30, result)
}

func TestRegisterWatermarkForLoopWithDefine(t *testing.T) {
	t.Parallel()

	const extraStatements = 200

	var source strings.Builder
	source.WriteString("package main\n\n")
	source.WriteString("var counter int\n\n")
	source.WriteString("func inc() int { counter++; return counter }\n\n")
	source.WriteString("func run() int {\n")
	for range extraStatements {
		source.WriteString("\tinc()\n")
	}
	source.WriteString("\tsum := 0\n")
	source.WriteString("\tfor i := 0; i < 5; i++ {\n")
	source.WriteString("\t\tsum += i\n")
	source.WriteString("\t}\n")
	for range extraStatements {
		source.WriteString("\tinc()\n")
	}
	source.WriteString("\treturn sum\n")
	source.WriteString("}\n\nfunc main() {}\n")

	service := app.NewService()
	result, err := service.EvalFile(t.Context(), source.String(), "run")
	require.NoError(t, err)
	require.Equal(t, 10, result)
}

func TestRegisterWatermarkIfWithInit(t *testing.T) {
	t.Parallel()

	const extraStatements = 200

	var source strings.Builder
	source.WriteString("package main\n\n")
	source.WriteString("var counter int\n\n")
	source.WriteString("func inc() int { counter++; return counter }\n")
	source.WriteString("func getErr() error { return nil }\n\n")
	source.WriteString("func run() int {\n")
	for range extraStatements {
		source.WriteString("\tinc()\n")
	}
	source.WriteString("\tresult := 42\n")
	source.WriteString("\tif err := getErr(); err != nil {\n")
	source.WriteString("\t\tresult = -1\n")
	source.WriteString("\t}\n")
	for range extraStatements {
		source.WriteString("\tinc()\n")
	}
	source.WriteString("\treturn result\n")
	source.WriteString("}\n\nfunc main() {}\n")

	service := app.NewService()
	result, err := service.EvalFile(t.Context(), source.String(), "run")
	require.NoError(t, err)
	require.Equal(t, 42, result)
}

func TestVarInitFunctionRegisterOverflow(t *testing.T) {
	t.Parallel()

	const globalCount = 300

	var source strings.Builder
	source.WriteString("package main\n\n")
	source.WriteString("func compute(n int) int { return n * 2 }\n\n")
	for i := range globalCount {
		fmt.Fprintf(&source, "var g%d = compute(%d)\n", i, i)
	}
	source.WriteString("\nfunc run() int { return g0 + g299 }\n")
	source.WriteString("func main() {}\n")

	service := app.NewService()
	result, err := service.EvalFile(t.Context(), source.String(), "run")
	require.NoError(t, err)
	require.Equal(t, 598, result)
}

func TestRegisterWatermarkShortVarDeclDoesNotLeak(t *testing.T) {
	t.Parallel()

	const declCount = 200

	var source strings.Builder
	source.WriteString("package main\n\n")
	source.WriteString("func compute(n int) int { return n * 2 }\n\n")
	source.WriteString("func run() int {\n")
	source.WriteString("\tsum := 0\n")
	for i := range declCount {
		fmt.Fprintf(&source, "\tv%d := compute(%d)\n", i, i)
		fmt.Fprintf(&source, "\tsum += v%d\n", i)
	}
	source.WriteString("\treturn sum\n")
	source.WriteString("}\n\nfunc main() {}\n")

	var expectedSum int
	for i := range declCount {
		expectedSum += i * 2
	}

	service := app.NewService()
	result, err := service.EvalFile(t.Context(), source.String(), "run")
	require.NoError(t, err)
	require.Equal(t, expectedSum, result)
}

func TestRegisterWatermarkShortVarRedeclDoesNotLeak(t *testing.T) {
	t.Parallel()

	const reassignCount = 200

	var source strings.Builder
	source.WriteString("package main\n\n")
	source.WriteString("func compute(n int) int { return n * 2 }\n\n")
	source.WriteString("func run() int {\n")
	source.WriteString("\tx := 0\n")
	source.WriteString("\tsum := 0\n")
	for i := range reassignCount {
		fmt.Fprintf(&source, "\tx, v%d := compute(%d), compute(%d)\n", i, i, i+1)
		fmt.Fprintf(&source, "\tsum += v%d\n", i)
	}
	source.WriteString("\tsum += x\n")
	source.WriteString("\treturn sum\n")
	source.WriteString("}\n\nfunc main() {}\n")

	var expectedSum int
	for i := range reassignCount {
		expectedSum += (i + 1) * 2
	}
	expectedSum += (reassignCount - 1) * 2

	service := app.NewService()
	result, err := service.EvalFile(t.Context(), source.String(), "run")
	require.NoError(t, err)
	require.Equal(t, expectedSum, result)
}

func TestRegisterWatermarkShortVarDeclPreservesValues(t *testing.T) {
	t.Parallel()

	service := app.NewService()
	result, err := service.EvalFile(t.Context(), `package main

func compute(n int) int { return n * 10 }

func run() int {
	a := compute(1)
	b := compute(2)
	c := compute(3)
	d := compute(4)
	e := compute(5)
	return a + b + c + d + e
}

func main() {}
`, "run")
	require.NoError(t, err)
	require.Equal(t, 150, result)
}

func TestRegisterRecyclingPreventsOverflow(t *testing.T) {
	t.Parallel()

	const declCount = 400

	var source strings.Builder
	source.WriteString("package main\n\n")
	source.WriteString("func compute(n int) int { return n * 2 }\n\n")
	source.WriteString("func run() int {\n")
	source.WriteString("\tsum := 0\n")
	for i := range declCount {
		fmt.Fprintf(&source, "\tv%d := compute(%d)\n", i, i)
		fmt.Fprintf(&source, "\tsum += v%d\n", i)
	}
	source.WriteString("\treturn sum\n")
	source.WriteString("}\n\nfunc main() {}\n")

	var expectedSum int
	for i := range declCount {
		expectedSum += i * 2
	}

	service := app.NewService()
	result, err := service.EvalFile(t.Context(), source.String(), "run")
	require.NoError(t, err)
	require.Equal(t, expectedSum, result)
}

func TestRegisterRecyclingWithGeneralBank(t *testing.T) {
	t.Parallel()

	const declCount = 400

	var source strings.Builder
	source.WriteString("package main\n\n")
	source.WriteString("type Node struct { Value int }\n\n")
	source.WriteString("func makeNode(n int) *Node { return &Node{Value: n} }\n\n")
	source.WriteString("func run() int {\n")
	source.WriteString("\tsum := 0\n")
	for i := range declCount {
		fmt.Fprintf(&source, "\tn%d := makeNode(%d)\n", i, i)
		fmt.Fprintf(&source, "\tsum += n%d.Value\n", i)
	}
	source.WriteString("\treturn sum\n")
	source.WriteString("}\n\nfunc main() {}\n")

	var expectedSum int
	for i := range declCount {
		expectedSum += i
	}

	service := app.NewService()
	result, err := service.EvalFile(t.Context(), source.String(), "run")
	require.NoError(t, err)
	require.Equal(t, expectedSum, result)
}

func TestRegisterRecyclingClosureSafety(t *testing.T) {
	t.Parallel()

	service := app.NewService()
	result, err := service.EvalFile(t.Context(), `package main

func run() int {
	x := 10
	f := func() int { return x }
	y := 20
	_ = y
	return f()
}

func main() {}
`, "run")
	require.NoError(t, err)
	require.Equal(t, 10, result)
}

func TestRegisterOverflowErrorIncludesFunctionName(t *testing.T) {
	t.Parallel()

	allocator := &scope.RegisterAllocator{FunctionName: "heavyFunction"}
	for range 257 {
		allocator.Alloc(isa.RegisterGeneral)
	}

	require.ErrorContains(t, allocator.OverflowErr, "heavyFunction", "the overflow error names the function")
	require.ErrorContains(t, allocator.OverflowErr, "general", "the overflow error names the register bank")
}

func TestRegisterAllocatorRestorePanicsOnInvalidSnapshot(t *testing.T) {
	t.Parallel()

	allocator := &scope.RegisterAllocator{FunctionName: "testFunc"}
	for range 5 {
		allocator.Alloc(isa.RegisterInt64)
	}

	var invalidSnapshot [isa.NumRegisterKinds]uint32
	invalidSnapshot[isa.RegisterInt64] = 10

	defer func() {
		recovered := recover()
		require.NotNil(t, recovered, "restoring an invalid snapshot panics")
		message, ok := recovered.(string)
		require.True(t, ok, "the panic value is a string: %T %v", recovered, recovered)
		require.Contains(t, message, "exceeds", "the panic message says the snapshot exceeds the allocator")
	}()

	allocator.Restore(invalidSnapshot)
}

func TestRegisterBlankMultiReturnResultsDoNotAliasLaterDeclarations(t *testing.T) {
	t.Parallel()

	service := app.NewService()
	result, err := service.EvalFile(t.Context(), `package main

func triple() (int, int, int) { return 1, 2, 3 }

func run() int {
	a := 1
	_, _, _ = triple()
	b, a := a, 5
	return a*10 + b
}
`, "run")
	require.NoError(t, err)
	require.Equal(t, 51, result)
}

func TestRegisterWatermarkManyBlankMultiReturnAssignments(t *testing.T) {
	t.Parallel()

	const statements = 450

	var source strings.Builder
	source.WriteString("package main\n\nfunc pair(n int) (int, int) { return n * 2, n + 1 }\n\n")
	source.WriteString("func run() int {\n\tx := 0\n\ttotal := 0\n")
	for i := range statements {
		fmt.Fprintf(&source, "\t_, x = pair(%d)\n\ttotal += x\n", i)
	}
	source.WriteString("\tsteps := 0\n\tfor i := 0; i < 5; _, i = pair(i) {\n\t\tsteps++\n\t}\n")
	source.WriteString("\treturn total*10 + steps\n}\n\nfunc main() {}\n")

	expected := 0
	for i := range statements {
		expected += i + 1
	}
	expected = expected*10 + 5

	service := app.NewService()
	result, err := service.EvalFile(t.Context(), source.String(), "run")
	require.NoError(t, err)
	require.Equal(t, expected, result)
}

func TestRegisterWatermarkCaseBodiesKeepControlFlow(t *testing.T) {
	t.Parallel()

	service := app.NewService()
	result, err := service.EvalFile(t.Context(), `package main

func f(n int) int { return n }

func run() int {
	trace := 0
	switch x := f(1); x {
	case 1:
		a := f(x) + 1
		trace = trace*10 + a
		fallthrough
	case 2:
		b := f(trace) + 1
		trace = trace*10 + b%10
		fallthrough
	default:
		trace = trace*10 + 9
	}
	count := 0
outer:
	for i := range 4 {
		switch {
		case i%2 == 0:
			doubled := f(i) * 2
			if doubled >= 0 {
				continue outer
			}
		default:
			count += f(i)
		}
	}
	return trace*100 + count
}
`, "run")
	require.NoError(t, err)
	require.Equal(t, 239*100+4, result)
}

func TestRegisterWatermarkAppendOfManyValues(t *testing.T) {
	t.Parallel()

	const values = 300

	join := func(element func(int) string) string {
		parts := make([]string, values)
		for i := range parts {
			parts[i] = element(i)
		}
		return strings.Join(parts, ", ")
	}
	cases := map[string]struct {
		source string
		want   any
	}{
		"int slice": {
			source: "func run() int {\n\tx := 1\n\tvar s []int\n\ts = append(s, " + join(func(i int) string { return fmt.Sprintf("x+%d", i) }) +
				")\n\treturn len(s)*100000 + s[0]*1000 + s[299]\n}\n",
			want: 300*100000 + 1*1000 + 300,
		},
		"typed int local": {
			source: "func run() int {\n\tx := 1\n\ts := make([]int, 0, 4)\n\ts = append(s, " + join(func(i int) string { return fmt.Sprintf("x+%d", i) }) +
				")\n\treturn len(s)*100000 + s[0]*1000 + s[299]\n}\n",
			want: 300*100000 + 1*1000 + 300,
		},
		"string slice": {
			source: "func run() string {\n\tx := \"v\"\n\tvar s []string\n\ts = append(s, " + join(func(i int) string { return fmt.Sprintf("x+\"%d\"", i) }) +
				")\n\treturn s[0] + s[299] + string(rune('0'+len(s)/100))\n}\n",
			want: "v0v2993",
		},
		"struct slice": {
			source: "type item struct{ N int }\n\nfunc run() int {\n\tx := 1\n\tvar s []item\n\ts = append(s, " + join(func(i int) string { return fmt.Sprintf("item{N: x+%d}", i) }) +
				")\n\treturn len(s)*100000 + s[0].N*1000 + s[299].N\n}\n",
			want: 300*100000 + 1*1000 + 300,
		},
		"byte slice": {
			source: "func run() int {\n\tx := byte(1)\n\tvar s []byte\n\ts = append(s, " + join(func(i int) string { return fmt.Sprintf("x+%d", i%200) }) +
				")\n\treturn len(s)*100000 + int(s[0])*1000 + int(s[299])\n}\n",
			want: 300*100000 + 1*1000 + 100,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			service := app.NewService()
			result, err := service.EvalFile(t.Context(), "package main\n\n"+tc.source+"\nfunc main() {}\n", "run")
			require.NoError(t, err)
			require.Equal(t, tc.want, result)
		})
	}
}

func TestRegisterWatermarkLongOperatorChains(t *testing.T) {
	t.Parallel()

	const terms = 300

	join := func(separator string, element func(int) string) string {
		parts := make([]string, terms)
		for i := range parts {
			parts[i] = element(i)
		}
		return strings.Join(parts, separator)
	}
	sum, shifted := 0, 0
	for i := range terms {
		sum += 1 + i
		shifted |= (1 + i) << (i % 60)
	}
	cases := map[string]struct {
		source string
		want   any
	}{
		"arithmetic chain": {
			source: "func f(n int) int { return n }\n\nfunc run() int {\n\tx := 1\n\treturn " + join(" + ", func(i int) string { return fmt.Sprintf("f(x+%d)", i) }) + "\n}\n",
			want:   sum,
		},
		"string concatenation": {
			source: "func s(n int) string { return string(rune('a' + n%26)) }\n\nfunc run() int {\n\tx := 0\n\treturn len(" + join(" + ", func(i int) string { return fmt.Sprintf("s(x+%d)", i) }) + ")\n}\n",
			want:   terms,
		},
		"bitwise chain of shifts": {
			source: "func run() int {\n\tx := 1\n\treturn " + join(" | ", func(i int) string { return fmt.Sprintf("(x+%d)<<%d", i, i%60) }) + "\n}\n",
			want:   shifted,
		},
		"mixed int and uint shifts": {
			source: "func run() uint {\n\tvar u uint = 3\n\tn := 2\n\treturn (u << n) | (u >> 1) ^ (u << uint(n+1)) &^ 1\n}\n",
			want:   uint(3<<2) | (3 >> 1) ^ (3<<3)&^1,
		},
		"chain with a captured local": {
			source: "func f(n int) int { return n }\n\nfunc run() int {\n\tx := 1\n\tg := func() int { return x }\n\treturn f(x) + g()*10 + f(x+1)*100 + g()\n}\n",
			want:   1 + 10 + 200 + 1,
		},
		"short-circuit keeps skipping side effects": {
			source: "var calls int\n\nfunc f(n int) bool { calls++; return n > 0 }\n\nfunc run() int {\n\tx := 1\n\ta := f(x) && f(x-1) && f(x+5) && f(x+6)\n\tb := f(-x) || f(x) || f(x+7)\n\tr := calls * 100\n\tif a {\n\t\tr++\n\t}\n\tif b {\n\t\tr += 10\n\t}\n\treturn r\n}\n",
			want:   4*100 + 10,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			service := app.NewService()
			result, err := service.EvalFile(t.Context(), "package main\n\n"+tc.source+"\nfunc main() {}\n", "run")
			require.NoError(t, err)
			require.Equal(t, tc.want, result)
		})
	}
}

func TestRegisterWatermarkMultiValueReturnsAndTuples(t *testing.T) {
	t.Parallel()

	const results = 100

	join := func(separator string, element func(int) string) string {
		parts := make([]string, results)
		for i := range parts {
			parts[i] = element(i)
		}
		return strings.Join(parts, separator)
	}
	cases := map[string]struct {
		source string
		want   any
	}{
		"many results": {
			source: "func f(n int) int { return n }\n\nfunc many(x int) (" + join(", ", func(int) string { return "int" }) + ") {\n\treturn " +
				join(", ", func(i int) string { return fmt.Sprintf("f(x+%d)", i) }) + "\n}\n\nfunc run() int {\n\t" +
				join(", ", func(i int) string { return fmt.Sprintf("r%d", i) }) + " := many(1)\n\treturn " +
				join(" + ", func(i int) string { return fmt.Sprintf("r%d", i) }) + " + r0*100000 + r99*1000\n}\n",
			want: 100*101/2 + 1*100000 + 100*1000,
		},
		"swapped parameters": {
			source: "func swap(a, b int) (int, int) { return b, a }\n\nfunc run() int {\n\tx, y := swap(1, 2)\n\treturn x*10 + y\n}\n",
			want:   21,
		},
		"results in different banks": {
			source: "func mixed(v any, x int) (int, string, int) { return v.(int), \"s\", x + 1 }\n\nfunc run() int {\n\ta, s, b := mixed(5, 7)\n\treturn a*100 + len(s)*10 + b\n}\n",
			want:   5*100 + 10 + 8,
		},
		"tuple of many targets": {
			source: "func f(n int) int { return n }\n\nfunc run() int {\n\tx := 1\n\tvar " + join(", ", func(i int) string { return fmt.Sprintf("t%d", i) }) + " int\n\t" +
				join(", ", func(i int) string { return fmt.Sprintf("t%d", i) }) + " = " +
				join(", ", func(i int) string { return fmt.Sprintf("f(x+%d)", i) }) + "\n\treturn " +
				join(" + ", func(i int) string { return fmt.Sprintf("t%d", i) }) + "\n}\n",
			want: 100 * 101 / 2,
		},
		"tuple swap": {
			source: "func run() int {\n\ta, b := 1, 2\n\ta, b = b, a\n\ts := []int{3, 4}\n\ts[0], s[1] = s[1], s[0]\n\treturn a*1000 + b*100 + s[0]*10 + s[1]\n}\n",
			want:   2*1000 + 1*100 + 4*10 + 3,
		},
		"two-phase define": {
			source: "func f(n int) int { return n }\n\nfunc run() int {\n\ta := 1\n\ta, b := f(a+1), f(a+2)\n\treturn a*10 + b\n}\n",
			want:   2*10 + 3,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			service := app.NewService()
			result, err := service.EvalFile(t.Context(), "package main\n\n"+tc.source+"\nfunc main() {}\n", "run")
			require.NoError(t, err)
			require.Equal(t, tc.want, result)
		})
	}
}
