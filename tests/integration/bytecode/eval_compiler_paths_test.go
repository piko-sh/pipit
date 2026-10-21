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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
)

func TestCompilerAssignmentForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want any
		name string
		code string
	}{
		{
			name: "compound_assign_selector",
			code: `
type S struct { X int }
s := S{X: 1}
s.X += 5
s.X`,
			want: 6,
		},
		{
			name: "compound_assign_selector_mul",
			code: `
type S struct { X int }
s := S{X: 3}
s.X *= 4
s.X`,
			want: 12,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.Eval(t.Context(), tt.code)
			require.NoError(t, err, "code: %s", tt.code)
			require.Equal(t, tt.want, result, "code: %s", tt.code)
		})
	}
}

func TestCompilerControlFlow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want any
		name string
		code string
	}{
		{
			name: "switch_multi_case",
			code: `
x := 3
y := 0
switch x {
case 1, 2, 3:
    y = 10
case 4, 5:
    y = 20
}
y`,
			want: 10,
		},

		{
			name: "type_switch_default",
			code: `
var x any = 3.14
result := ""
switch x.(type) {
case int:
    result = "int"
case string:
    result = "string"
default:
    result = "other"
}
result`,
			want: "other",
		},

		{
			name: "type_switch_string",
			code: `
var x any = "hello"
result := ""
switch v := x.(type) {
case int:
    result = "int"
case string:
    result = v
default:
    result = "other"
}
result`,
			want: "hello",
		},

		{
			name: "inc_selector",
			code: `
type S struct { X int }
s := S{X: 10}
s.X++
s.X`,
			want: 11,
		},
		{
			name: "dec_selector",
			code: `
type S struct { X int }
s := S{X: 10}
s.X--
s.X`,
			want: 9,
		},

		{
			name: "multi_return",
			code: `
func swap(a, b int) (int, int) { return b, a }
x, y := swap(1, 2)
x*10 + y`,
			want: 21,
		},

		{
			name: "map_comma_ok_found",
			code: `
m := map[string]int{"a": 42}
v, ok := m["a"]
result := -1
if ok { result = v }
result`,
			want: 42,
		},
		{
			name: "map_comma_ok_missing",
			code: `
m := map[string]int{"a": 42}
_, ok := m["z"]
ok`,
			want: false,
		},

		{
			name: "select_recv_discard",
			code: `
ch := make(chan int, 1)
ch <- 42
select {
case <-ch:
}
1`,
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.Eval(t.Context(), tt.code)
			require.NoError(t, err, "code: %s", tt.code)
			require.Equal(t, tt.want, result, "code: %s", tt.code)
		})
	}
}

func TestCompilerExpressions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want any
		name string
		code string
	}{
		{name: "complex_sub", code: "a := 3+4i; b := 1+2i; a - b", want: complex128(2 + 2i)},
		{name: "complex_div", code: "a := 6+8i; b := 2+0i; a / b", want: complex128(3 + 4i)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.Eval(t.Context(), tt.code)
			require.NoError(t, err, "code: %s", tt.code)
			require.Equal(t, tt.want, result, "code: %s", tt.code)
		})
	}
}

func TestCompilerClosures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want any
		name string
		code string
	}{
		{
			name: "addr_of_field",
			code: `
type S struct { X int }
s := S{X: 42}
p := &s.X
*p`,
			want: 42,
		},

		{
			name: "method_value",
			code: `
type Counter struct { N int }
func (c *Counter) Inc() { c.N++ }
c := &Counter{N: 0}
f := c.Inc
f()
f()
c.N`,
			want: 2,
		},

		{
			name: "range_define_both",
			code: `
sum := 0
index := 0
for i, v := range []int{10, 20, 30} {
    sum += v
    index = i
}
sum + index`,
			want: 62,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.Eval(t.Context(), tt.code)
			require.NoError(t, err, "code: %s", tt.code)
			require.Equal(t, tt.want, result, "code: %s", tt.code)
		})
	}
}

func TestCompilerStatements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want any
		name string
		code string
	}{
		{
			name: "range_string_slice",
			code: `
result := ""
for _, s := range []string{"hello", " ", "world"} {
    result += s
}
result`,
			want: "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.Eval(t.Context(), tt.code)
			require.NoError(t, err, "code: %s", tt.code)
			require.Equal(t, tt.want, result, "code: %s", tt.code)
		})
	}
}

func TestCompilerCommaOkForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want any
		name string
		code string
	}{
		{
			name: "map_comma_ok_short_var_decl",
			code: `m := map[string]int{"a": 1}; v, ok := m["a"]; _ = v; ok`,
			want: true,
		},
		{
			name: "map_comma_ok_missing_key",
			code: `m := map[string]int{"a": 1}; v, ok := m["b"]; _ = v; ok`,
			want: false,
		},

		{
			name: "type_assert_comma_ok_success",
			code: `var i interface{} = 42; v, ok := i.(int); _ = v; ok`,
			want: true,
		},
		{
			name: "type_assert_comma_ok_failure",
			code: `var i interface{} = "hello"; v, ok := i.(int); _ = v; ok`,
			want: false,
		},

		{
			name: "chan_recv_comma_ok_open",
			code: `ch := make(chan int, 1); ch <- 42; v, ok := <-ch; _ = v; ok`,
			want: true,
		},
		{
			name: "chan_recv_comma_ok_closed",
			code: `ch := make(chan int); close(ch); v, ok := <-ch; _ = v; ok`,
			want: false,
		},

		{
			name: "type_assert_comma_ok_assign",
			code: `var i interface{} = 42; var v int; var ok bool; v, ok = i.(int); _ = v; ok`,
			want: true,
		},
		{
			name: "chan_recv_comma_ok_assign",
			code: `ch := make(chan int, 1); ch <- 7; var v int; var ok bool; v, ok = <-ch; _ = v; ok`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.Eval(t.Context(), tt.code)
			require.NoError(t, err, "code: %s", tt.code)
			require.Equal(t, tt.want, result, "code: %s", tt.code)
		})
	}
}

func TestCompilerPackageLevelVars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want       any
		name       string
		source     string
		entrypoint string
	}{
		{

			name: "blank_identifier_init",
			source: `package main

var _ = 42
var x = 10

func run() int { return x }
func main() {}
`,
			entrypoint: "run",
			want:       10,
		},
		{

			name: "global_var_assignment",
			source: `package main

var counter int

func run() int { counter = 42; return counter }
func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
		{

			name: "expression_init",
			source: `package main

var result = 7 * 6

func run() int { return result }
func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.EvalFile(t.Context(), tt.source, tt.entrypoint)
			require.NoError(t, err, "source: %s", tt.source)
			require.Equal(t, tt.want, result, "source: %s", tt.source)
		})
	}
}

func TestCompilerUintArithmetic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want any
		name string
		code string
	}{
		{
			name: "uint_less_than",
			code: `var a uint = 5; var b uint = 10; a < b`,
			want: true,
		},
		{
			name: "uint_greater_than",
			code: `var a uint = 10; var b uint = 5; a > b`,
			want: true,
		},
		{
			name: "uint_less_equal",
			code: `var a uint = 5; var b uint = 5; a <= b`,
			want: true,
		},
		{
			name: "uint_greater_equal",
			code: `var a uint = 10; var b uint = 5; a >= b`,
			want: true,
		},
		{
			name: "uint_equal",
			code: `var a uint = 5; var b uint = 5; a == b`,
			want: true,
		},
		{
			name: "uint_not_equal",
			code: `var a uint = 5; var b uint = 10; a != b`,
			want: true,
		},

		{
			name: "uint_subtract",
			code: `var a uint = 10; var b uint = 3; a - b`,
			want: uint(7),
		},
		{
			name: "uint_multiply",
			code: `var a uint = 6; var b uint = 7; a * b`,
			want: uint(42),
		},
		{
			name: "uint_divide",
			code: `var a uint = 42; var b uint = 6; a / b`,
			want: uint(7),
		},
		{
			name: "uint_remainder",
			code: `var a uint = 10; var b uint = 3; a % b`,
			want: uint(1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.Eval(t.Context(), tt.code)
			require.NoError(t, err, "code: %s", tt.code)
			require.Equal(t, tt.want, result, "code: %s", tt.code)
		})
	}
}

func TestCompilerComplexArithmetic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want any
		name string
		code string
	}{
		{
			name: "complex_add",
			code: `var c1 complex128 = complex(1.0, 2.0); var c2 complex128 = complex(3.0, 4.0); c1 + c2`,
			want: complex128(4 + 6i),
		},
		{
			name: "complex_subtract",
			code: `var c1 complex128 = complex(5.0, 10.0); var c2 complex128 = complex(2.0, 3.0); c1 - c2`,
			want: complex128(3 + 7i),
		},
		{
			name: "complex_multiply",
			code: `var c1 complex128 = complex(1.0, 2.0); var c2 complex128 = complex(3.0, 4.0); c1 * c2`,
			want: complex128(-5 + 10i),
		},
		{
			name: "complex_divide",
			code: `var c1 complex128 = complex(10.0, 0.0); var c2 complex128 = complex(2.0, 0.0); c1 / c2`,
			want: complex128(5 + 0i),
		},
		{
			name: "complex_equal",
			code: `var c1 complex128 = complex(1.0, 2.0); var c2 complex128 = complex(1.0, 2.0); c1 == c2`,
			want: true,
		},
		{
			name: "complex_not_equal",
			code: `var c1 complex128 = complex(1.0, 2.0); var c2 complex128 = complex(3.0, 4.0); c1 != c2`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.Eval(t.Context(), tt.code)
			require.NoError(t, err, "code: %s", tt.code)
			require.Equal(t, tt.want, result, "code: %s", tt.code)
		})
	}
}

func TestCompilerStructFieldIncDec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want       any
		name       string
		source     string
		entrypoint string
	}{
		{
			name: "int_field_inc_dec",
			source: `package main

type Counter struct {
	IntField   int
	FloatField float64
	UintField  uint
}

func run() int {
	c := Counter{IntField: 10, FloatField: 2.5, UintField: 100}
	c.IntField++
	c.IntField--
	c.IntField++
	return c.IntField
}

func main() {}
`,
			entrypoint: "run",
			want:       11,
		},
		{
			name: "float_field_inc",
			source: `package main

type Counter struct {
	FloatField float64
}

func run() float64 {
	c := Counter{FloatField: 2.5}
	c.FloatField++
	return c.FloatField
}

func main() {}
`,
			entrypoint: "run",
			want:       float64(3.5),
		},
		{
			name: "uint_field_inc",
			source: `package main

type Counter struct {
	UintField uint
}

func run() uint {
	c := Counter{UintField: 100}
	c.UintField++
	return c.UintField
}

func main() {}
`,
			entrypoint: "run",
			want:       uint(101),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.EvalFile(t.Context(), tt.source, tt.entrypoint)
			require.NoError(t, err, "source: %s", tt.source)
			require.Equal(t, tt.want, result, "source: %s", tt.source)
		})
	}
}

func TestCompilerPaths(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		source     string
		entrypoint string
		want       any
	}{
		{
			name: "embedded fields",
			source: `package main

type Inner struct {
	Value int
}

type Outer struct {
	Inner
}

func run() int {
	o := Outer{Inner: Inner{Value: 42}}
	return o.Value
}

func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
		{
			name: "address of selector",
			source: `package main

type Point struct {
	X, Y int
}

func run() int {
	p := Point{X: 10, Y: 20}
	px := &p.X
	*px = 42
	return p.X
}

func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
		{
			name: "method expression",
			source: `package main

type Adder struct {
	Base int
}

func (a Adder) Add(x int) int {
	return a.Base + x
}

func run() int {
	f := Adder.Add
	return f(Adder{Base: 10}, 32)
}

func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
		{
			name: "struct into collection",
			source: `package main

type Point struct {
	X, Y int
}

func run() int {
	points := make([]Point, 3)
	points[0] = Point{X: 1, Y: 2}
	points[1] = Point{X: 3, Y: 4}
	points[2] = Point{X: 5, Y: 6}
	return points[0].X + points[1].Y + points[2].X
}

func main() {}
`,
			entrypoint: "run",
			want:       10,
		},
		{
			name: "bound method on embedded",
			source: `package main

type Inner struct {
	Value int
}

func (i Inner) Double() int {
	return i.Value * 2
}

type Outer struct {
	Inner
}

func run() int {
	o := Outer{Inner: Inner{Value: 21}}
	f := o.Double
	return f()
}

func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
		{
			name: "tagless switch multi expr",
			source: `package main

func classify(x int) string {
	switch {
	case x < 0, x > 100:
		return "out_of_range"
	case x == 0:
		return "zero"
	case x > 50:
		return "high"
	default:
		return "normal"
	}
}

func run() string {
	return classify(-1) + "," + classify(0) + "," + classify(75) + "," + classify(25) + "," + classify(200)
}

func main() {}
`,
			entrypoint: "run",
			want:       "out_of_range,zero,high,normal,out_of_range",
		},
		{
			name: "upvalue call",
			source: `package main

func run() int {
	add := func(a, b int) int { return a + b }
	result := func() int {
		return add(20, 22)
	}()
	return result
}

func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
		{
			name: "compound assign upvalue",
			source: `package main

func run() int {
	x := 10
	f := func() {
		x += 32
	}
	f()
	return x
}

func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
		{
			name: "multi return to upvalue",
			source: `package main

func twoValues() (int, bool) { return 42, true }

func run() int {
	var val int
	var ok bool
	f := func() {
		val, ok = twoValues()
	}
	f()
	if ok {
		return val
	}
	return 0
}

func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
		{
			name: "compound assign map int int",
			source: `package main

func run() int {
	m := map[int]int{1: 10}
	m[1] += 5
	m[2] += 3
	return m[1] + m[2]
}

func main() {}
`,
			entrypoint: "run",
			want:       18,
		},
		{
			name: "global var set kind mismatch",
			source: `package main

var g interface{}

func run() interface{} {
	g = 42
	return g
}

func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
		{
			name: "pointer deref assign",
			source: `package main

func run() int {
	x := 10
	p := &x
	*p = 42
	return x
}

func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
		{
			name: "map string assign",
			source: `package main

func run() string {
	m := map[string]string{}
	m["key"] = "hello"
	return m["key"]
}

func main() {}
`,
			entrypoint: "run",
			want:       "hello",
		},
		{
			name: "range over string runes",
			source: `package main

func run() int {
	count := 0
	for _, r := range "hello" {
		_ = r
		count++
	}
	return count
}

func main() {}
`,
			entrypoint: "run",
			want:       5,
		},
		{
			name: "compound assign global",
			source: `package main

var counter int

func run() int {
	counter = 10
	counter += 32
	return counter
}

func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
		{
			name: "switch fallthrough",
			source: `package main

func classify(x int) int {
	result := 0
	switch x {
	case 1:
		result += 10
		fallthrough
	case 2:
		result += 20
	case 3:
		result += 30
	}
	return result
}

func run() int {
	return classify(1)*1000 + classify(2)*100 + classify(3)
}

func main() {}
`,
			entrypoint: "run",
			want:       int(30*1000 + 20*100 + 30),
		},
		{
			name: "type switch",
			source: `package main

func classify(x interface{}) string {
	switch v := x.(type) {
	case int:
		_ = v
		return "int"
	case string:
		return "string"
	case bool:
		return "bool"
	default:
		return "other"
	}
}

func run() string {
	return classify(42) + "," + classify("hi") + "," + classify(true) + "," + classify(3.14)
}

func main() {}
`,
			entrypoint: "run",
			want:       "int,string,bool,other",
		},
		{
			name: "labelled break",
			source: `package main

func run() int {
	sum := 0
outer:
	for i := 0; i < 10; i++ {
		for j := 0; j < 10; j++ {
			if j == 3 {
				break outer
			}
			sum += 1
		}
	}
	return sum
}

func main() {}
`,
			entrypoint: "run",
			want:       3,
		},
		{
			name: "labelled continue",
			source: `package main

func run() int {
	sum := 0
outer:
	for i := 0; i < 5; i++ {
		for j := 0; j < 5; j++ {
			if j == 2 {
				continue outer
			}
			sum += 1
		}
	}
	return sum
}

func main() {}
`,
			entrypoint: "run",
			want:       10,
		},
		{
			name: "builtin copy",
			source: `package main

func run() int {
	source := []int{1, 2, 3, 4, 5}
	destination := make([]int, 3)
	n := copy(destination, source)
	return n + destination[0] + destination[1] + destination[2]
}

func main() {}
`,
			entrypoint: "run",
			want:       9,
		},
		{
			name: "builtin clear",
			source: `package main

func run() int {
	m := map[string]int{"a": 1, "b": 2}
	clear(m)
	return len(m)
}

func main() {}
`,
			entrypoint: "run",
			want:       0,
		},
		{
			name: "range over integer",
			source: `package main

func run() int {
	sum := 0
	for i := range 5 {
		sum += i
	}
	return sum
}

func main() {}
`,
			entrypoint: "run",
			want:       10,
		},
		{
			name: "compound assign slice",
			source: `package main

func run() int {
	s := []int{10, 20, 30}
	s[1] += 5
	return s[1]
}

func main() {}
`,
			entrypoint: "run",
			want:       25,
		},
		{
			name: "deep embedded method expression",
			source: `package main

type Inner struct { V int }
type Middle struct { Inner }
type Outer struct { Middle }

func (i Inner) Get() int { return i.V }

func run() int {
	o := Outer{Middle: Middle{Inner: Inner{V: 42}}}
	return o.Get()
}

func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := app.NewService().EvalFile(t.Context(), tt.source, tt.entrypoint)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}

func TestCompilerMultiReturnCall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want       any
		name       string
		source     string
		entrypoint string
	}{
		{

			name: "compiled_multi_return",
			source: `package main

func divide(a, b int) (int, int) {
	return a / b, a % b
}

func run() int {
	q, r := divide(17, 5)
	return q*10 + r
}

func main() {}
`,
			entrypoint: "run",
			want:       32,
		},
		{

			name: "closure_multi_return",
			source: `package main

func run() int {
	f := func() (int, string) { return 42, "hello" }
	v, s := f()
	_ = s
	return v
}

func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.EvalFile(t.Context(), tt.source, tt.entrypoint)
			require.NoError(t, err, "source: %s", tt.source)
			require.Equal(t, tt.want, result, "source: %s", tt.source)
		})
	}
}

func TestCompilerUnsafeConversions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want       any
		name       string
		source     string
		entrypoint string
	}{
		{

			name: "unsafe_sizeof",
			source: `package main

import (
	"unsafe"
)

func run() int { return int(unsafe.Sizeof(int(0))) }
func main() {}
`,
			entrypoint: "run",
			want:       8,
		},
		{

			name: "unsafe_alignof",
			source: `package main

import (
	"unsafe"
)

func run() int { return int(unsafe.Alignof(int(0))) }
func main() {}
`,
			entrypoint: "run",
			want:       8,
		},
		{

			name: "unsafe_add",
			source: `package main

import (
	"unsafe"
)

func run() bool {
	x := 42
	p := unsafe.Pointer(&x)
	p2 := unsafe.Add(p, 0)
	return p == p2
}

func main() {}
`,
			entrypoint: "run",
			want:       true,
		},
		{

			name: "unsafe_slice",
			source: `package main

import (
	"unsafe"
)

func run() int {
	arr := [3]int{10, 20, 30}
	p := unsafe.Pointer(&arr[0])
	s := unsafe.Slice((*int)(p), 3)
	return s[1]
}

func main() {}
`,
			entrypoint: "run",
			want:       20,
		},
		{

			name: "unsafe_string",
			source: `package main

import (
	"unsafe"
)

func run() string {
	b := []byte("hello")
	p := unsafe.Pointer(&b[0])
	s := unsafe.String((*byte)(p), 5)
	return s
}

func main() {}
`,
			entrypoint: "run",
			want:       "hello",
		},
		{

			name: "unsafe_string_data",
			source: `package main

import (
	"unsafe"
)

func run() bool {
	s := "hello"
	p := unsafe.StringData(s)
	return p != nil
}

func main() {}
`,
			entrypoint: "run",
			want:       true,
		},
		{

			name: "unsafe_slice_data",
			source: `package main

import (
	"unsafe"
)

func run() bool {
	s := []int{1, 2, 3}
	p := unsafe.SliceData(s)
	return p != nil
}

func main() {}
`,
			entrypoint: "run",
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.EvalFile(t.Context(), tt.source, tt.entrypoint)
			require.NoError(t, err, "source: %s", tt.source)
			require.Equal(t, tt.want, result, "source: %s", tt.source)
		})
	}
}

func TestCompilerEmitsDebugInfoForEachStatement(t *testing.T) {
	t.Parallel()

	source := `package main

func helper() int { return 42 }
func run() int { return helper() }
func main() {}
`

	service := app.NewService(app.WithDebugInfo())
	result, err := service.EvalFile(t.Context(), source, "run")
	require.NoError(t, err)
	require.Equal(t, 42, result)

	cfs, err := service.CompileFileSet(t.Context(), map[string]string{
		"main.go": source,
	})
	require.NoError(t, err)

	fn, err := cfs.FindFunction("run")
	require.NoError(t, err)
	require.True(t, fn.HasDebugSourceMap())
}

func TestCompilerUnaryNegateComplex128(t *testing.T) {
	t.Parallel()

	code := `var c complex128 = complex(3.0, 4.0); -c`
	service := app.NewService()
	result, err := service.Eval(t.Context(), code)
	require.NoError(t, err)
	require.Equal(t, complex128(-3-4i), result)
}

func TestCompilerBitwiseComplementUint(t *testing.T) {
	t.Parallel()

	code := `var x uint = 0x0F; ^x`
	service := app.NewService()
	result, err := service.Eval(t.Context(), code)
	require.NoError(t, err)
	require.Equal(t, ^uint(0x0F), result)
}

func TestCompilerCrossBankConversions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want       any
		name       string
		source     string
		entrypoint string
	}{
		{
			name: "int_to_float",
			source: `package main
func run() float64 {
	x := 42
	return float64(x)
}
func main() {}
`,
			entrypoint: "run",
			want:       float64(42),
		},
		{
			name: "float_to_int",
			source: `package main
func run() int {
	f := 3.7
	return int(f)
}
func main() {}
`,
			entrypoint: "run",
			want:       3,
		},
		{
			name: "int_to_uint",
			source: `package main
func run() uint {
	x := 42
	return uint(x)
}
func main() {}
`,
			entrypoint: "run",
			want:       uint(42),
		},
		{
			name: "uint_to_int",
			source: `package main
func run() int {
	var u uint = 42
	return int(u)
}
func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
		{
			name: "uint_to_float",
			source: `package main
func run() float64 {
	var u uint = 42
	return float64(u)
}
func main() {}
`,
			entrypoint: "run",
			want:       float64(42),
		},
		{
			name: "float_to_uint",
			source: `package main
func run() uint {
	f := 42.9
	return uint(f)
}
func main() {}
`,
			entrypoint: "run",
			want:       uint(42),
		},
		{
			name: "bool_to_int_via_conversion",
			source: `package main
func boolToInt(b bool) int {
	if b { return 1 }
	return 0
}
func run() int {
	return boolToInt(true) + boolToInt(false)
}
func main() {}
`,
			entrypoint: "run",
			want:       1,
		},
		{
			name: "int_to_interface",
			source: `package main
func run() interface{} {
	x := 42
	var i interface{} = x
	return i
}
func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
		{
			name: "interface_to_int",
			source: `package main
func run() int {
	var i interface{} = 42
	return i.(int)
}
func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
		{
			name: "float_to_interface",
			source: `package main
func run() interface{} {
	f := 3.14
	var i interface{} = f
	return i
}
func main() {}
`,
			entrypoint: "run",
			want:       float64(3.14),
		},
		{
			name: "interface_to_float",
			source: `package main
func run() float64 {
	var i interface{} = 3.14
	return i.(float64)
}
func main() {}
`,
			entrypoint: "run",
			want:       float64(3.14),
		},
		{
			name: "bool_to_interface",
			source: `package main
func run() interface{} {
	b := true
	var i interface{} = b
	return i
}
func main() {}
`,
			entrypoint: "run",
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.EvalFile(t.Context(), tt.source, tt.entrypoint)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}

func TestCompilerCommaOkReassign(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want       any
		name       string
		source     string
		entrypoint string
	}{
		{
			name: "map_comma_ok_reassign",
			source: `package main
func run() int {
	m := map[string]int{"a": 42}
	var v int
	var ok bool
	v, ok = m["a"]
	if ok {
		return v
	}
	return 0
}
func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
		{
			name: "map_comma_ok_reassign_missing",
			source: `package main
func run() bool {
	m := map[string]int{"a": 42}
	var v int
	var ok bool
	v, ok = m["z"]
	_ = v
	return ok
}
func main() {}
`,
			entrypoint: "run",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.EvalFile(t.Context(), tt.source, tt.entrypoint)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}

func TestCompilerGlobalZeroNamedTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want       any
		name       string
		source     string
		entrypoint string
	}{
		{
			name: "named_int_type_zero",
			source: `package main
type Counter int
var c Counter
func run() int { return int(c) }
func main() {}
`,
			entrypoint: "run",
			want:       0,
		},
		{
			name: "slice_var_zero",
			source: `package main
var s []int
func run() int { return len(s) }
func main() {}
`,
			entrypoint: "run",
			want:       0,
		},
		{
			name: "map_var_zero",
			source: `package main
var m map[string]int
func run() int { return len(m) }
func main() {}
`,
			entrypoint: "run",
			want:       0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.EvalFile(t.Context(), tt.source, tt.entrypoint)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}

func TestCompilerTypeConversionAfterTypeAssert(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want       any
		name       string
		source     string
		entrypoint string
	}{
		{
			name: "int_from_uint_assertion",
			source: `package main

func identity(v interface{}) interface{} { return v }

func run() int {
	x := identity(uint(42))
	return int(x.(uint))
}

func main() {}
`,
			entrypoint: "run",
			want:       42,
		},
		{
			name: "float64_from_int_assertion",
			source: `package main

func identity(v interface{}) interface{} { return v }

func run() float64 {
	x := identity(42)
	return float64(x.(int))
}

func main() {}
`,
			entrypoint: "run",
			want:       float64(42),
		},
		{
			name: "uint_from_float64_assertion",
			source: `package main

func identity(v interface{}) interface{} { return v }

func run() uint {
	x := identity(42.0)
	return uint(x.(float64))
}

func main() {}
`,
			entrypoint: "run",
			want:       uint(42),
		},
		{
			name: "bool_roundtrip_through_interface",
			source: `package main

func identity(v interface{}) interface{} { return v }

func run() bool {
	x := identity(true)
	return x.(bool)
}

func main() {}
`,
			entrypoint: "run",
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.EvalFile(t.Context(), tt.source, tt.entrypoint)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}
