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

package language_test

import (
	"context"
	"reflect"
	"testing"

	"pipit.sh/pipit/internal/symtab"

	"github.com/stretchr/testify/require"
)

func TestUintIncDec(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, []evalTestCase{
		{expect: uint(6), name: "uint_inc", code: `var a uint = 5; a++; a`},
		{expect: uint(4), name: "uint_dec", code: `var a uint = 5; a--; a`},
	})
}

func TestUintBitNot(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, []evalTestCase{
		{expect: uint(0xFFFFFFFFFFFFFF00), name: "uint_bitnot", code: `var a uint = 0xFF; ^a`},
	})
}

func TestMapCommaOkAssign(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 1, name: "comma_ok_assign_found", code: `
m := map[string]int{"a": 1}
v := 0
ok := false
v, ok = m["a"]
_ = ok
v`},
		{expect: true, name: "comma_ok_assign_ok", code: `
m := map[string]int{"a": 1}
v := 0
ok := false
v, ok = m["a"]
_ = v
ok`},
		{expect: 0, name: "comma_ok_assign_missing", code: `
m := map[string]int{"a": 1}
v := 0
ok := false
v, ok = m["b"]
_ = ok
v`},
		{expect: false, name: "comma_ok_assign_missing_ok", code: `
m := map[string]int{"a": 1}
v := 0
ok := false
v, ok = m["b"]
_ = v
ok`},
	}

	runEvalTable(t, nil, tests)
}

func TestRuneStringConcat(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: "Hello", name: "rune_concat_prefix", code: `r := 'H'; string(r) + "ello"`},
		{expect: "aX", name: "rune_concat_suffix", code: `s := "a"; r := 'X'; s + string(r)`},
	}

	runEvalTable(t, nil, tests)
}

func TestSliceUintSetGet(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: uint(10), name: "uint_slice_set", code: `s := []uint{1, 2, 3}; s[0] = 10; s[0]`},
		{expect: uint(3), name: "uint_slice_get", code: `s := []uint{1, 2, 3}; s[2]`},
	}

	runEvalTable(t, nil, tests)
}

func TestCompoundAssignOnMapElement(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 15, name: "compound_map_int_key", code: `m := map[int]int{1: 10}; m[1] += 5; m[1]`},
		{expect: "helloworld", name: "compound_map_string_val", code: `m := map[string]string{"a": "hello"}; m["a"] += "world"; m["a"]`},
	}

	runEvalTable(t, nil, tests)
}

func TestZeroValuePerDeclaredType(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 0, name: "zero_int", code: `var x int; x`},
		{expect: float64(0), name: "zero_float", code: `var x float64; x`},
		{expect: "", name: "zero_string", code: `var x string; x`},
		{expect: false, name: "zero_bool", code: `var x bool; x`},
		{expect: uint(0), name: "zero_uint", code: `var x uint; x`},
	}

	runEvalTable(t, nil, tests)
}

func TestMultiCaseSwitch(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 10, name: "multi_case_1", code: `x := 1; switch x { case 1, 2, 3: x = 10; case 4, 5: x = 20 }; x`},
		{expect: 10, name: "multi_case_2", code: `x := 2; switch x { case 1, 2, 3: x = 10; case 4, 5: x = 20 }; x`},
		{expect: 20, name: "multi_case_4", code: `x := 4; switch x { case 1, 2, 3: x = 10; case 4, 5: x = 20 }; x`},
		{expect: 99, name: "multi_case_default", code: `x := 99; switch x { case 1, 2, 3: x = 10; default: x = 99 }; x`},
	}

	runEvalTable(t, nil, tests)
}

func TestIncDecSelector(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 6, name: "inc_selector", code: `
type S struct { X int }
s := S{X: 5}
s.X++
s.X`},
		{expect: 4, name: "dec_selector", code: `
type S struct { X int }
s := S{X: 5}
s.X--
s.X`},
	}

	runEvalTable(t, nil, tests)
}

func TestCompoundAssignOnSliceElement(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 15, name: "slice_add_assign", code: `s := []int{10, 20}; s[0] += 5; s[0]`},
		{expect: 5, name: "slice_sub_assign", code: `s := []int{10, 20}; s[0] -= 5; s[0]`},
		{expect: 50, name: "slice_mul_assign", code: `s := []int{10, 20}; s[0] *= 5; s[0]`},
	}

	runEvalTable(t, nil, tests)
}

func TestCompoundAssignOnStructField(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 15, name: "selector_add_assign", code: `
type S struct { X int }
s := S{X: 10}
s.X += 5
s.X`},
		{expect: 5, name: "selector_sub_assign", code: `
type S struct { X int }
s := S{X: 10}
s.X -= 5
s.X`},
		{expect: 50, name: "selector_mul_assign", code: `
type S struct { X int }
s := S{X: 10}
s.X *= 5
s.X`},
	}

	runEvalTable(t, nil, tests)
}

func TestForLoopRangeString(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 5, name: "range_string_count", code: `
count := 0
for range "hello" { count++ }
count`},
		{expect: 532, name: "range_string_rune_sum", code: `
sum := 0
for _, r := range "hello" { sum += int(r) }
sum`},
	}

	runEvalTable(t, nil, tests)
}

func TestAddressOfSelector(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 42, name: "addr_of_field", code: `
type S struct { X int }
s := S{X: 42}
p := &s.X
*p`},
		{expect: 99, name: "write_through_addr", code: `
type S struct { X int }
s := S{X: 42}
p := &s.X
*p = 99
s.X`},
	}

	runEvalTable(t, nil, tests)
}

func TestMultiReturnFunctions(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 3, name: "multi_return_first", code: `
func f() (int, string) { return 3, "hello" }
x, _ := f()
x`},
		{expect: "hello", name: "multi_return_second", code: `
func f() (int, string) { return 3, "hello" }
_, s := f()
s`},
		{expect: 10, name: "multi_return_sum", code: `
func f() (int, int) { return 3, 7 }
a, b := f()
a + b`},
	}

	runEvalTable(t, nil, tests)
}

func TestShortVarRedeclare(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 5, name: "redecl_first", code: `
x := 1
x, y := 5, 10
_ = y
x`},
		{expect: 10, name: "redecl_second", code: `
x := 1
x, y := 5, 10
_ = x
y`},
	}

	runEvalTable(t, nil, tests)
}

func TestForLoopWithBreakContinue(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 5, name: "for_break", code: `
sum := 0
for i := 0; i < 100; i++ {
	if i >= 5 { break }
	sum++
}
sum`},
		{expect: 50, name: "for_continue", code: `
sum := 0
for i := 0; i < 100; i++ {
	if i%2 != 0 { continue }
	sum++
}
sum`},
		{expect: 3, name: "for_range_break", code: `
count := 0
for _, v := range []int{1, 2, 3, 4, 5} {
	if v > 3 { break }
	count++
}
count`},
	}

	runEvalTable(t, nil, tests)
}

func TestClosureCaptureAndCall(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 15, name: "closure_capture", code: `
func make() func() int {
	x := 15
	return func() int { return x }
}
f := make()
f()`},
		{expect: 10, name: "closure_counter", code: `
func counter() func() int {
	n := 0
	return func() int { n++; return n }
}
c := counter()
sum := 0
for i := 0; i < 4; i++ { sum += c() }
sum`},
		{expect: 120, name: "recursive_closure", code: `
func factorial(n int) int {
	if n <= 1 { return 1 }
	return n * factorial(n-1)
}
factorial(5)`},
	}

	runEvalTable(t, nil, tests)
}

func TestInterfaceTypeSwitch(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 1, name: "type_switch_int", code: `
var x any = 42
result := 0
switch x.(type) {
case int: result = 1
case string: result = 2
}
result`},
		{expect: 2, name: "type_switch_string", code: `
var x any = "hello"
result := 0
switch x.(type) {
case int: result = 1
case string: result = 2
}
result`},
		{expect: 3, name: "type_switch_default", code: `
var x any = 3.14
result := 0
switch x.(type) {
case int: result = 1
case string: result = 2
default: result = 3
}
result`},
	}

	runEvalTable(t, nil, tests)
}

func TestDeferWithPanic(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 42, name: "defer_recover", code: `
func f() int {
	defer func() { recover() }()
	panic("test")
	return 0
}
func g() int {
	defer func() {}()
	return 42
}
g()`},
		{expect: "caught", name: "recover_message", code: `
func f() string {
	defer func() {
		if r := recover(); r != nil {
			_ = r
		}
	}()
	panic("caught")
	return ""
}
f()
"caught"`},
	}

	runEvalTable(t, nil, tests)
}

func TestChannelSendReceiveAndSelect(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 42, name: "chan_send_recv", code: `
ch := make(chan int, 1)
ch <- 42
<-ch`},
		{expect: 10, name: "chan_buffered", code: `
ch := make(chan int, 3)
ch <- 10
ch <- 20
ch <- 30
<-ch`},
	}

	runEvalTable(t, nil, tests)
}

func TestNativeFunctionCaching(t *testing.T) {
	t.Parallel()

	service := newSuiteServiceWithSymbols(t, symtab.SymbolExports{
		"fp": {
			"StringInt":       reflect.ValueOf(fpStringInt),
			"Float64Float64":  reflect.ValueOf(fpFloat64Float64),
			"IntBool":         reflect.ValueOf(fpIntBool),
			"StringBool":      reflect.ValueOf(fpStringBool),
			"Float642Float64": reflect.ValueOf(fpFloat642Float64),
		},
	})

	tests := []struct {
		expect any
		name   string
		code   string
	}{

		{name: "string_int_cached", code: `import "fp"
a := fp.StringInt("hello")
b := fp.StringInt("world")
a + b`, expect: 10},
		{name: "float_cached", code: `import "fp"
a := fp.Float64Float64(2.0)
b := fp.Float64Float64(3.0)
a + b`, expect: float64(10.0)},
		{name: "bool_cached", code: `import "fp"
a := fp.IntBool(5)
b := fp.IntBool(-1)
a && !b`, expect: true},
		{name: "string_bool_cached", code: `import "fp"
a := fp.StringBool("hi")
b := fp.StringBool("")
a && !b`, expect: true},
		{name: "float2_cached", code: `import "fp"
a := fp.Float642Float64(1.0, 2.0)
b := fp.Float642Float64(3.0, 4.0)
a + b`, expect: float64(10.0)},

		{name: "loop_cached", code: `import "fp"
sum := 0
for i := 0; i < 10; i++ { sum += fp.StringInt("ab") }
sum`, expect: 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			localService := service.Clone()
			result, err := localService.Eval(context.Background(), tt.code)
			require.NoError(t, err)
			require.Equal(t, tt.expect, result)
		})
	}
}

func TestNarrowIntTypes(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 127, name: "int8_max", code: `var x int8 = 127; int(x)`},
		{expect: 32767, name: "int16_max", code: `var x int16 = 32767; int(x)`},
		{expect: 42, name: "int32_val", code: `var x int32 = 42; int(x)`},
		{expect: 100, name: "int64_val", code: `var x int64 = 100; int(x)`},
		{expect: uint(255), name: "uint8_val", code: `var x uint8 = 255; uint(x)`},
		{expect: uint(65535), name: "uint16_val", code: `var x uint16 = 65535; uint(x)`},
		{expect: uint(42), name: "uint32_val", code: `var x uint32 = 42; uint(x)`},
		{expect: uint(100), name: "uint64_val", code: `var x uint64 = 100; uint(x)`},
		{expect: 3, name: "byte_slice_len", code: `s := []byte{1, 2, 3}; len(s)`},
		{expect: 65, name: "byte_to_int", code: `var b byte = 'A'; int(b)`},
	}

	runEvalTable(t, nil, tests)
}

func TestMapInsertLookupAndDelete(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 3, name: "map_len", code: `m := map[string]int{"a": 1, "b": 2, "c": 3}; len(m)`},
		{expect: 0, name: "map_empty_len", code: `m := map[string]int{}; len(m)`},
		{expect: 2, name: "map_after_delete", code: `m := map[string]int{"a": 1, "b": 2, "c": 3}; delete(m, "b"); len(m)`},
		{expect: true, name: "map_check_after_set", code: `m := map[string]bool{}; m["key"] = true; m["key"]`},
		{expect: 6, name: "map_range_sum", code: `
m := map[string]int{"a": 1, "b": 2, "c": 3}
sum := 0
for _, v := range m { sum += v }
sum`},
	}

	runEvalTable(t, nil, tests)
}

func TestStructMethodCalls(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 42, name: "value_method", code: `
type S struct { X int }
func (s S) Get() int { return s.X }
s := S{X: 42}
s.Get()`},
		{expect: 99, name: "pointer_method", code: `
type S struct { X int }
func (s *S) Set(v int) { s.X = v }
func (s S) Get() int { return s.X }
s := S{X: 0}
s.Set(99)
s.Get()`},
		{expect: 15, name: "method_chain", code: `
type Acc struct { Total int }
func (a *Acc) Add(n int) *Acc { a.Total += n; return a }
a := &Acc{}
a.Add(5).Add(10)
a.Total`},
	}

	runEvalTable(t, nil, tests)
}

func TestNestedStructs(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 42, name: "nested_field", code: `
type Inner struct { X int }
type Outer struct { I Inner }
o := Outer{I: Inner{X: 42}}
o.I.X`},
		{expect: 99, name: "nested_set", code: `
type Inner struct { X int }
type Outer struct { I Inner }
o := Outer{I: Inner{X: 0}}
o.I.X = 99
o.I.X`},
	}

	runEvalTable(t, nil, tests)
}

func TestEmbeddedStructs(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 42, name: "embedded_field", code: `
type Base struct { X int }
type Derived struct { Base }
d := Derived{Base: Base{X: 42}}
d.X`},
		{expect: 99, name: "embedded_method", code: `
type Base struct { X int }
func (b Base) Value() int { return b.X }
type Derived struct { Base }
d := Derived{Base: Base{X: 99}}
d.Value()`},
	}

	runEvalTable(t, nil, tests)
}

func TestCompoundAssignSliceFloat(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, []evalTestCase{
		{expect: float64(15.5), name: "float_slice_add_assign", code: `s := []float64{10.5}; s[0] += 5.0; s[0]`},
	})
}

func TestCompoundAssignSelectorFloat(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, []evalTestCase{
		{expect: float64(15.0), name: "float_selector_add", code: `
type S struct { X float64 }
s := S{X: 10.0}
s.X += 5.0
s.X`},
	})
}

func TestCompoundAssignSelectorString(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, []evalTestCase{
		{expect: "hello world", name: "string_selector_add", code: `
type S struct { X string }
s := S{X: "hello"}
s.X += " world"
s.X`},
	})
}

func TestZeroValueComposite(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, []evalTestCase{
		{expect: 0, name: "zero_array", code: `var a [3]int; a[0]`},
		{expect: 0, name: "zero_struct", code: `
type S struct { X int; Y string }
var s S
s.X`},
		{expect: "", name: "zero_struct_string", code: `
type S struct { X int; Y string }
var s S
s.Y`},
	})
}

func TestForLoopClauseForms(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, []evalTestCase{

		{expect: 10, name: "for_ge_limit", code: `
sum := 0
for i := 10; i >= 1; i-- { sum++ }
sum`},

		{expect: 5, name: "for_step_3", code: `
count := 0
for i := 0; i < 15; i += 3 { count++ }
count`},

		{expect: 10, name: "infinite_break", code: `
i := 0
for { if i >= 10 { break }; i++ }
i`},
	})
}

func TestInterfaceConversions(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, []evalTestCase{
		{expect: int(42), name: "type_assert_int", code: `var x any = 42; x.(int)`},
		{expect: "hello", name: "type_assert_string", code: `var x any = "hello"; x.(string)`},
		{expect: float64(3.14), name: "type_assert_float", code: `var x any = 3.14; x.(float64)`},
		{expect: true, name: "type_assert_bool", code: `var x any = true; x.(bool)`},
	})
}

func TestSliceOfSlice(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: 42, name: "nested_slice", code: `
s := [][]int{{1, 2}, {3, 4}, {42}}
s[2][0]`},
		{expect: 6, name: "slice_slice", code: `s := []int{1, 2, 3, 4, 5}; t := s[1:3]; len(s) - len(t) + t[1]`},
	}

	runEvalTable(t, nil, tests)
}

func TestNilComparisonsAcrossKinds(t *testing.T) {
	t.Parallel()

	tests := []evalTestCase{
		{expect: true, name: "nil_test_nil", code: `var p *int; p == nil`},
		{expect: false, name: "nil_test_nonnull", code: `x := 42; p := &x; p == nil`},
		{expect: true, name: "nil_slice", code: `var s []int; s == nil`},
		{expect: false, name: "nil_map_false", code: `m := map[string]int{}; m == nil`},
	}

	runEvalTable(t, nil, tests)
}

func TestClosureCaptureTypes(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, []evalTestCase{
		{expect: float64(3.14), name: "capture_float", code: `
func make() func() float64 {
	x := 3.14
	return func() float64 { return x }
}
f := make()
f()`},
		{expect: "hello", name: "capture_string", code: `
func make() func() string {
	x := "hello"
	return func() string { return x }
}
f := make()
f()`},
		{expect: true, name: "capture_bool", code: `
func make() func() bool {
	x := true
	return func() bool { return x }
}
f := make()
f()`},
		{expect: 10, name: "capture_mutate", code: `
func make() func() int {
	x := 0
	return func() int { x += 10; return x }
}
f := make()
f()`},
		{expect: float64(5.0), name: "capture_mutate_float", code: `
func make() func() float64 {
	x := 0.0
	return func() float64 { x += 5.0; return x }
}
f := make()
f()`},
		{expect: "ab", name: "capture_mutate_string", code: `
func make() func() string {
	x := "a"
	return func() string { x += "b"; return x }
}
f := make()
f()`},
	})
}

func TestMethodValuesBindTheirReceiver(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, []evalTestCase{
		{expect: 42, name: "method_value", code: `
type S struct { X int }
func (s S) Get() int { return s.X }
s := S{X: 42}
f := s.Get
f()`},
		{expect: 99, name: "method_value_ptr", code: `
type S struct { X int }
func (s *S) Get() int { return s.X }
s := &S{X: 99}
f := s.Get
f()`},
	})
}

func TestRangeWithDifferentTypes(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, []evalTestCase{
		{expect: float64(6.0), name: "range_float_slice", code: `
sum := 0.0
for _, v := range []float64{1.0, 2.0, 3.0} { sum += v }
sum`},
		{expect: "abc", name: "range_string_slice", code: `
result := ""
for _, s := range []string{"a", "b", "c"} { result += s }
result`},
		{expect: 2, name: "range_bool_count", code: `
count := 0
for _, b := range []bool{true, false, true} { if b { count++ } }
count`},
	})
}

func TestCrossBankAssign(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, []evalTestCase{

		{expect: float64(5), name: "int_to_float_assign", code: `
func f() float64 { x := 5; return float64(x) }
f()`},

		{expect: 3, name: "float_to_int_assign", code: `
func f() int { x := 3.9; return int(x) }
f()`},

		{expect: 1, name: "bool_to_int", code: `
func f() int { b := true; if b { return 1 }; return 0 }
f()`},
	})
}

func TestMultiReturnWithDifferentTypes(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, []evalTestCase{
		{expect: float64(3.14), name: "multi_ret_float_bool", code: `
func f() (float64, bool) { return 3.14, true }
v, _ := f()
v`},
		{expect: true, name: "multi_ret_bool", code: `
func f() (float64, bool) { return 3.14, true }
_, b := f()
b`},
		{expect: "hello", name: "multi_ret_string_int", code: `
func f() (string, int) { return "hello", 42 }
s, _ := f()
s`},
		{expect: 42, name: "multi_ret_int_from_string_int", code: `
func f() (string, int) { return "hello", 42 }
_, n := f()
n`},
	})
}

func TestDeferOrder(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, []evalTestCase{
		{expect: "cba", name: "defer_lifo_order", code: `
func f() (result string) {
	defer func() { result += "a" }()
	defer func() { result += "b" }()
	defer func() { result += "c" }()
	return ""
}
f()`},
	})
}

func TestComplexConstants(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, []evalTestCase{
		{expect: complex128(0), name: "complex_zero", code: `var c complex128; c`},
		{expect: complex128(1i), name: "complex_imag_only", code: `c := 1i; c`},
		{expect: complex128(3.14), name: "complex_real_only", code: `c := complex128(3.14); c`},
	})
}
