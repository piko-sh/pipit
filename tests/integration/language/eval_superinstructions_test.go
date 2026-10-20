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
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"
)

type evalTestCase struct {
	want any
	name string
	code string
}

func runEvalTable(t *testing.T, opts []app.Option, tests []evalTestCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService(opts...)
			result, err := service.Eval(t.Context(), tt.code)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}

var (
	forLoopTests = []evalTestCase{
		{want: 4950, name: "for_inc_sum", code: `sum := 0; for i := 0; i < 100; i++ { sum += i }; sum`},
		{want: 3628800, name: "for_inc_product", code: `p := 1; for i := 1; i <= 10; i++ { p *= i }; p`},
		{want: 0, name: "for_dec_countdown", code: `x := 10; for x > 0 { x-- }; x`},
		{want: 90, name: "for_step_2", code: `sum := 0; for i := 0; i < 20; i += 2 { sum += i }; sum`},
		{want: 25, name: "nested_for", code: `sum := 0; for i := 0; i < 5; i++ { for j := 0; j < 5; j++ { sum++ } }; sum`},
	}
)

var (
	switchTests = []evalTestCase{
		{want: 10, name: "switch_case_1", code: `x := 1; switch x { case 1: x = 10; case 2: x = 20 }; x`},
		{want: 20, name: "switch_case_2", code: `x := 2; switch x { case 1: x = 10; case 2: x = 20; case 3: x = 30 }; x`},
		{want: 30, name: "switch_case_3", code: `x := 3; switch x { case 1: x = 10; case 2: x = 20; case 3: x = 30 }; x`},
		{want: -1, name: "switch_default", code: `x := 99; switch x { case 1: x = 10; default: x = -1 }; x`},
		{want: 0, name: "switch_no_match", code: `x := 5; y := 0; switch x { case 1: y = 10; case 2: y = 20 }; y`},
	}
)

var (
	arithmeticTests = []evalTestCase{
		{want: 7, name: "sub_int", code: `a := 10; b := 3; a - b`},
		{want: 42, name: "mul_int", code: `a := 6; b := 7; a * b`},
		{want: 3, name: "div_int", code: `a := 10; b := 3; a / b`},
		{want: 1, name: "rem_int", code: `a := 10; b := 3; a % b`},
		{want: -5, name: "neg_int", code: `a := 5; -a`},
		{want: 0x0A, name: "bit_and", code: `a := 0x0F; b := 0x0A; a & b`},
		{want: 0x05, name: "bit_xor", code: `a := 0x0F; b := 0x0A; a ^ b`},
		{want: 8, name: "shift_left", code: `a := 1; a << 3`},
		{want: 4, name: "shift_right", code: `a := 32; a >> 3`},
		{want: 5, name: "sub_int_const", code: `a := 10; a - 5`},
		{want: float64(4.0), name: "add_float", code: `a := 1.5; b := 2.5; a + b`},
		{want: float64(3.0), name: "sub_float", code: `a := 5.0; b := 2.0; a - b`},
		{want: float64(12.0), name: "mul_float", code: `a := 3.0; b := 4.0; a * b`},
		{want: float64(2.5), name: "div_float", code: `a := 10.0; b := 4.0; a / b`},
		{want: float64(-3.0), name: "neg_float", code: `a := 3.0; -a`},
		{want: 0xF0, name: "bit_andnot", code: `a := 0xFF; b := 0x0F; a &^ b`},
		{want: -1, name: "bit_not", code: `a := 0; ^a`},
		{want: "helloworld", name: "concat_string", code: `a := "hello"; b := "world"; a + b`},
		{want: 5, name: "len_string", code: `a := "hello"; len(a)`},
	}
)

var (
	comparisonTests = []evalTestCase{
		{want: true, name: "ne_int_true", code: `a := 1; b := 2; a != b`},
		{want: false, name: "ne_int_false", code: `a := 1; b := 1; a != b`},
		{want: true, name: "le_int_true", code: `a := 3; b := 5; a <= b`},
		{want: true, name: "le_int_eq", code: `a := 5; b := 5; a <= b`},
		{want: false, name: "le_int_false", code: `a := 6; b := 5; a <= b`},
		{want: true, name: "ge_int_true", code: `a := 5; b := 3; a >= b`},
		{want: false, name: "ge_int_false", code: `a := 3; b := 5; a >= b`},
		{want: true, name: "gt_int_true", code: `a := 5; b := 3; a > b`},
		{want: false, name: "gt_int_false", code: `a := 3; b := 5; a > b`},
		{want: true, name: "eq_float_true", code: `a := 1.0; b := 1.0; a == b`},
		{want: false, name: "eq_float_false", code: `a := 1.0; b := 2.0; a == b`},
		{want: true, name: "ne_float_true", code: `a := 1.0; b := 2.0; a != b`},
		{want: true, name: "lt_float_true", code: `a := 1.0; b := 2.0; a < b`},
		{want: true, name: "le_float_true", code: `a := 1.0; b := 1.0; a <= b`},
		{want: true, name: "gt_float_true", code: `a := 2.0; b := 1.0; a > b`},
		{want: true, name: "ge_float_true", code: `a := 2.0; b := 1.0; a >= b`},
		{want: true, name: "ge_float_eq", code: `a := 1.0; b := 1.0; a >= b`},

		{want: true, name: "eq_string_true", code: `a := "abc"; b := "abc"; a == b`},
		{want: false, name: "eq_string_false", code: `a := "abc"; b := "xyz"; a == b`},
		{want: true, name: "ne_string_true", code: `a := "abc"; b := "xyz"; a != b`},
		{want: true, name: "lt_string_true", code: `a := "abc"; b := "xyz"; a < b`},
		{want: true, name: "le_string_true", code: `a := "abc"; b := "abc"; a <= b`},
		{want: true, name: "gt_string_true", code: `a := "xyz"; b := "abc"; a > b`},
		{want: true, name: "ge_string_true", code: `a := "abc"; b := "abc"; a >= b`},
	}
)

var (
	boolAndJumpTests = []evalTestCase{
		{want: 42, name: "jump_if_true", code: `x := true; y := 0; if x { y = 42 }; y`},
		{want: true, name: "load_bool_true", code: `b := true; b`},
		{want: false, name: "load_bool_false", code: `b := false; b`},
		{want: true, name: "not_false", code: `a := false; !a`},
		{want: false, name: "not_true", code: `a := true; !a`},
	}
)

var (
	typeConversionTests = []evalTestCase{
		{want: float64(5), name: "int_to_float", code: `a := 5; float64(a)`},
		{want: 3, name: "float_to_int", code: `a := 3.9; int(a)`},
	}
)

var (
	switchStringTests = []evalTestCase{
		{want: "hello", name: "switch_string_case1", code: `s := "a"; switch s { case "a": s = "hello"; case "b": s = "world" }; s`},
		{want: "world", name: "switch_string_case2", code: `s := "b"; switch s { case "a": s = "hello"; case "b": s = "world" }; s`},
		{want: "default", name: "switch_string_default", code: `s := "z"; switch s { case "a": s = "hello"; default: s = "default" }; s`},
	}
)

var (
	uintArithTests = []evalTestCase{
		{want: uint(7), name: "uint_add", code: `var a uint = 3; var b uint = 4; a + b`},
		{want: uint(2), name: "uint_sub", code: `var a uint = 5; var b uint = 3; a - b`},
		{want: uint(12), name: "uint_mul", code: `var a uint = 3; var b uint = 4; a * b`},
		{want: uint(3), name: "uint_div", code: `var a uint = 10; var b uint = 3; a / b`},
		{want: uint(1), name: "uint_rem", code: `var a uint = 10; var b uint = 3; a % b`},
		{want: uint(0x0A), name: "uint_bit_and", code: `var a uint = 0x0F; var b uint = 0x0A; a & b`},
		{want: uint(0x0F), name: "uint_bit_or", code: `var a uint = 0x0A; var b uint = 0x05; a | b`},
		{want: uint(0x05), name: "uint_bit_xor", code: `var a uint = 0x0F; var b uint = 0x0A; a ^ b`},
		{want: uint(0xF0), name: "uint_bit_andnot", code: `var a uint = 0xFF; var b uint = 0x0F; a &^ b`},
		{want: uint(8), name: "uint_shift_left", code: `var a uint = 1; a << 3`},
		{want: uint(4), name: "uint_shift_right", code: `var a uint = 32; a >> 3`},
		{want: true, name: "uint_eq", code: `var a uint = 5; var b uint = 5; a == b`},
		{want: true, name: "uint_ne", code: `var a uint = 5; var b uint = 3; a != b`},
		{want: true, name: "uint_lt", code: `var a uint = 3; var b uint = 5; a < b`},
		{want: true, name: "uint_le", code: `var a uint = 5; var b uint = 5; a <= b`},
		{want: true, name: "uint_gt", code: `var a uint = 5; var b uint = 3; a > b`},
		{want: true, name: "uint_ge", code: `var a uint = 5; var b uint = 5; a >= b`},
	}
)

var (
	complexTests = []evalTestCase{
		{want: complex128(3 + 4i), name: "complex_add", code: `var a complex128 = 1+2i; var b complex128 = 2+2i; a + b`},
		{want: complex128(-1 + 0i), name: "complex_sub", code: `var a complex128 = 1+2i; var b complex128 = 2+2i; a - b`},
		{want: complex128(-3 + 4i), name: "complex_mul", code: `var a complex128 = 1+2i; var b complex128 = 1+2i; a * b`},
		{want: complex128(1 + 0i), name: "complex_div", code: `var a complex128 = 2+4i; var b complex128 = 2+4i; a / b`},
		{want: complex128(-1 - 2i), name: "complex_neg", code: `var a complex128 = 1+2i; -a`},
		{want: true, name: "complex_eq", code: `var a complex128 = 1+2i; var b complex128 = 1+2i; a == b`},
		{want: true, name: "complex_ne", code: `var a complex128 = 1+2i; var b complex128 = 3+4i; a != b`},
		{want: float64(3), name: "complex_real", code: `var a complex128 = 3+4i; real(a)`},
		{want: float64(4), name: "complex_imag", code: `var a complex128 = 3+4i; imag(a)`},
		{want: complex128(3 + 4i), name: "complex_build", code: `complex(3.0, 4.0)`},
	}
)

var (
	crossTypeConvTests = []evalTestCase{
		{want: uint(5), name: "int_to_uint", code: `a := 5; uint(a)`},
		{want: 5, name: "uint_to_int", code: `var a uint = 5; int(a)`},
		{want: float64(5), name: "uint_to_float", code: `var a uint = 5; float64(a)`},
		{want: uint(3), name: "float_to_uint", code: `a := 3.9; uint(a)`},
	}
)

func TestSuperinstructionForLoops(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, forLoopTests)
}

func TestSuperinstructionSwitch(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, switchTests)
}

func TestArithmeticHandlersAcrossOperandKinds(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, arithmeticTests)
}

func TestComparisonHandlersAcrossOperandKinds(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, comparisonTests)
}

func TestBoolAndConditionalJumpHandlers(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, boolAndJumpTests)
}

func TestTypeConversions(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, typeConversionTests)
}

func TestSwitchString(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, switchStringTests)
}

func TestUintArithmetic(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, uintArithTests)
}

func TestComplexArithmetic(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, complexTests)
}

func TestCrossTypeConversions(t *testing.T) {
	t.Parallel()
	runEvalTable(t, nil, crossTypeConvTests)
}

func fpStringInt(s string) int   { return len(s) }
func fpStringBool(s string) bool { return s != "" }
func fpStringRuneBool(s string, r int32) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}
func fpStringRuneInt(s string, r int32) int {
	for i, c := range s {
		if c == r {
			return i
		}
	}
	return -1
}
func fpString2Int(a, b string) int {
	if a == b {
		return 0
	}
	return 1
}
func fpString3String(a, b, c string) string { return a + b + c }
func fpIntBool(n int) bool                  { return n > 0 }
func fpInt2Bool(a, b int) bool              { return a == b }
func fpInt2String(a, b int) string {
	if a > b {
		return "gt"
	}
	return "le"
}
func fpFloat64Float64(x float64) float64     { return x * 2 }
func fpFloat642Float64(x, y float64) float64 { return x + y }
func fpRetFloat64() float64                  { return 3.14 }
func fpRetError() error                      { return nil }
func fpRetErrorNonNil() error                { return errors.New("oops") }
func fpVoidInt(_ int)                        {}
func fpVoidBool(_ bool)                      {}
func fpReadFloat(x float64) float64          { return x }
func fpReadBool(b bool) bool                 { return !b }

func TestNativeFastpathDispatchers(t *testing.T) {
	t.Parallel()

	service := newSuiteServiceWithSymbols(t, symtab.SymbolExports{
		"fp": {
			"StringInt":       reflect.ValueOf(fpStringInt),
			"StringBool":      reflect.ValueOf(fpStringBool),
			"StringRuneBool":  reflect.ValueOf(fpStringRuneBool),
			"StringRuneInt":   reflect.ValueOf(fpStringRuneInt),
			"String2Int":      reflect.ValueOf(fpString2Int),
			"String3String":   reflect.ValueOf(fpString3String),
			"IntBool":         reflect.ValueOf(fpIntBool),
			"Int2Bool":        reflect.ValueOf(fpInt2Bool),
			"Int2String":      reflect.ValueOf(fpInt2String),
			"Float64Float64":  reflect.ValueOf(fpFloat64Float64),
			"Float642Float64": reflect.ValueOf(fpFloat642Float64),
			"RetFloat64":      reflect.ValueOf(fpRetFloat64),
			"RetError":        reflect.ValueOf(fpRetError),
			"RetErrorNonNil":  reflect.ValueOf(fpRetErrorNonNil),
			"VoidInt":         reflect.ValueOf(fpVoidInt),
			"VoidBool":        reflect.ValueOf(fpVoidBool),
			"ReadFloat":       reflect.ValueOf(fpReadFloat),
			"ReadBool":        reflect.ValueOf(fpReadBool),
		},
	})

	tests := []struct {
		want any
		name string
		code string
	}{
		{name: "string_int", code: "import \"fp\"\nfp.StringInt(\"hello\")", want: 5},

		{name: "string_bool_true", code: "import \"fp\"\nfp.StringBool(\"hi\")", want: true},
		{name: "string_bool_false", code: "import \"fp\"\nfp.StringBool(\"\")", want: false},

		{name: "string_rune_bool_true", code: "import \"fp\"\nfp.StringRuneBool(\"hello\", 'e')", want: true},
		{name: "string_rune_bool_false", code: "import \"fp\"\nfp.StringRuneBool(\"hello\", 'z')", want: false},

		{name: "string_rune_int_found", code: "import \"fp\"\nfp.StringRuneInt(\"hello\", 'l')", want: 2},
		{name: "string_rune_int_missing", code: "import \"fp\"\nfp.StringRuneInt(\"hello\", 'z')", want: -1},

		{name: "string2_int_eq", code: "import \"fp\"\nfp.String2Int(\"a\", \"a\")", want: 0},
		{name: "string2_int_ne", code: "import \"fp\"\nfp.String2Int(\"a\", \"b\")", want: 1},

		{name: "string3_string", code: "import \"fp\"\nfp.String3String(\"a\", \"b\", \"c\")", want: "abc"},

		{name: "int_bool_true", code: "import \"fp\"\nfp.IntBool(5)", want: true},
		{name: "int_bool_false", code: "import \"fp\"\nfp.IntBool(-1)", want: false},

		{name: "int2_bool_true", code: "import \"fp\"\nfp.Int2Bool(5, 5)", want: true},
		{name: "int2_bool_false", code: "import \"fp\"\nfp.Int2Bool(5, 3)", want: false},

		{name: "int2_string_gt", code: "import \"fp\"\nfp.Int2String(5, 3)", want: "gt"},
		{name: "int2_string_le", code: "import \"fp\"\nfp.Int2String(3, 5)", want: "le"},

		{name: "float64_float64", code: "import \"fp\"\nfp.Float64Float64(2.5)", want: float64(5.0)},

		{name: "float642_float64", code: "import \"fp\"\nfp.Float642Float64(1.5, 2.5)", want: float64(4.0)},

		{name: "ret_float64", code: "import \"fp\"\nfp.RetFloat64()", want: float64(3.14)},

		{name: "ret_error_nil", code: "import \"fp\"\nerr := fp.RetError()\nerr == nil", want: true},

		{name: "void_int", code: "import \"fp\"\nfp.VoidInt(42)\ntrue", want: true},

		{name: "void_bool", code: "import \"fp\"\nfp.VoidBool(true)\ntrue", want: true},

		{name: "read_bool", code: "import \"fp\"\nfp.ReadBool(true)", want: false},
		{name: "read_float", code: "import \"fp\"\nfp.ReadFloat(2.5)", want: float64(2.5)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			localService := service.Clone()
			result, err := localService.Eval(t.Context(), tt.code)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}
