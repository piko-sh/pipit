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
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"
)

func newIntrinsicsService(t *testing.T) *app.Service {
	t.Helper()
	return newTestServiceWithSymbols(t, symtab.SymbolExports{
		"strings": {
			"Contains":     reflect.ValueOf(strings.Contains),
			"ContainsRune": reflect.ValueOf(strings.ContainsRune),
			"Count":        reflect.ValueOf(strings.Count),
			"EqualFold":    reflect.ValueOf(strings.EqualFold),
			"HasPrefix":    reflect.ValueOf(strings.HasPrefix),
			"HasSuffix":    reflect.ValueOf(strings.HasSuffix),
			"Index":        reflect.ValueOf(strings.Index),
			"IndexRune":    reflect.ValueOf(strings.IndexRune),
			"Join":         reflect.ValueOf(strings.Join),
			"LastIndex":    reflect.ValueOf(strings.LastIndex),
			"Repeat":       reflect.ValueOf(strings.Repeat),
			"ReplaceAll":   reflect.ValueOf(strings.ReplaceAll),
			"Split":        reflect.ValueOf(strings.Split),
			"ToLower":      reflect.ValueOf(strings.ToLower),
			"ToUpper":      reflect.ValueOf(strings.ToUpper),
			"Trim":         reflect.ValueOf(strings.Trim),
			"TrimPrefix":   reflect.ValueOf(strings.TrimPrefix),
			"TrimSpace":    reflect.ValueOf(strings.TrimSpace),
			"TrimSuffix":   reflect.ValueOf(strings.TrimSuffix),
		},
		"math": {
			"Abs":   reflect.ValueOf(math.Abs),
			"Ceil":  reflect.ValueOf(math.Ceil),
			"Cos":   reflect.ValueOf(math.Cos),
			"Exp":   reflect.ValueOf(math.Exp),
			"Floor": reflect.ValueOf(math.Floor),
			"Max":   reflect.ValueOf(math.Max),
			"Min":   reflect.ValueOf(math.Min),
			"Mod":   reflect.ValueOf(math.Mod),
			"Pow":   reflect.ValueOf(math.Pow),
			"Round": reflect.ValueOf(math.Round),
			"Sin":   reflect.ValueOf(math.Sin),
			"Sqrt":  reflect.ValueOf(math.Sqrt),
			"Tan":   reflect.ValueOf(math.Tan),
			"Trunc": reflect.ValueOf(math.Trunc),
		},
		"strconv": {
			"FormatBool": reflect.ValueOf(strconv.FormatBool),
			"FormatInt":  reflect.ValueOf(strconv.FormatInt),
			"Itoa":       reflect.ValueOf(strconv.Itoa),
		},
	})
}

func TestIntrinsicStringFunctions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want any
		name string
		code string
	}{
		{name: "ContainsRune_found", code: "import \"strings\"\nstrings.ContainsRune(\"hello\", 'e')", want: true},
		{name: "ContainsRune_not_found", code: "import \"strings\"\nstrings.ContainsRune(\"hello\", 'z')", want: false},
		{name: "ContainsRune_empty", code: "import \"strings\"\nstrings.ContainsRune(\"\", 'a')", want: false},

		{name: "EqualFold_true", code: "import \"strings\"\nstrings.EqualFold(\"Go\", \"go\")", want: true},
		{name: "EqualFold_false", code: "import \"strings\"\nstrings.EqualFold(\"Go\", \"py\")", want: false},
		{name: "EqualFold_empty", code: "import \"strings\"\nstrings.EqualFold(\"\", \"\")", want: true},

		{name: "Trim_spaces", code: "import \"strings\"\nstrings.Trim(\"  hello  \", \" \")", want: "hello"},
		{name: "Trim_chars", code: "import \"strings\"\nstrings.Trim(\"!!hello!!\", \"!\")", want: "hello"},
		{name: "Trim_noop", code: "import \"strings\"\nstrings.Trim(\"hello\", \"!\")", want: "hello"},

		{name: "IndexRune_found", code: "import \"strings\"\nstrings.IndexRune(\"hello\", 'l')", want: 2},
		{name: "IndexRune_not_found", code: "import \"strings\"\nstrings.IndexRune(\"hello\", 'z')", want: -1},
		{name: "IndexRune_first", code: "import \"strings\"\nstrings.IndexRune(\"hello\", 'h')", want: 0},

		{name: "LastIndex_found", code: "import \"strings\"\nstrings.LastIndex(\"go gopher\", \"go\")", want: 3},
		{name: "LastIndex_not_found", code: "import \"strings\"\nstrings.LastIndex(\"hello\", \"xyz\")", want: -1},
		{name: "LastIndex_single", code: "import \"strings\"\nstrings.LastIndex(\"abcabc\", \"c\")", want: 5},

		{name: "Join_comma", code: "import \"strings\"\nstrings.Join([]string{\"a\", \"b\", \"c\"}, \",\")", want: "a,b,c"},
		{name: "Join_empty_sep", code: "import \"strings\"\nstrings.Join([]string{\"a\", \"b\"}, \"\")", want: "ab"},
		{name: "Join_single", code: "import \"strings\"\nstrings.Join([]string{\"only\"}, \",\")", want: "only"},

		{name: "Split_comma", code: "import \"strings\"\nlen(strings.Split(\"a,b,c\", \",\"))", want: 3},
		{name: "Split_no_sep", code: "import \"strings\"\nlen(strings.Split(\"hello\", \",\"))", want: 1},

		{name: "ReplaceAll_basic", code: "import \"strings\"\nstrings.ReplaceAll(\"aaa\", \"a\", \"b\")", want: "bbb"},
		{name: "ReplaceAll_no_match", code: "import \"strings\"\nstrings.ReplaceAll(\"hello\", \"z\", \"x\")", want: "hello"},
		{name: "ReplaceAll_empty_old", code: "import \"strings\"\nstrings.ReplaceAll(\"hi\", \"\", \"-\")", want: "-h-i-"},

		{name: "Repeat_3", code: "import \"strings\"\nstrings.Repeat(\"ab\", 3)", want: "ababab"},
		{name: "Repeat_0", code: "import \"strings\"\nstrings.Repeat(\"ab\", 0)", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := newIntrinsicsService(t)
			result, err := service.Eval(t.Context(), tt.code)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}

func TestIntrinsicMathFunctions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		code  string
		want  float64
		delta float64
	}{
		{name: "Abs_negative", code: "import \"math\"\nmath.Abs(-3.14)", want: 3.14, delta: 0},
		{name: "Abs_positive", code: "import \"math\"\nmath.Abs(3.14)", want: 3.14, delta: 0},
		{name: "Abs_zero", code: "import \"math\"\nmath.Abs(0.0)", want: 0.0, delta: 0},

		{name: "Sqrt_16", code: "import \"math\"\nmath.Sqrt(16.0)", want: 4.0, delta: 0},
		{name: "Sqrt_2", code: "import \"math\"\nmath.Sqrt(2.0)", want: math.Sqrt(2.0), delta: 1e-10},

		{name: "Floor_positive", code: "import \"math\"\nmath.Floor(3.7)", want: 3.0, delta: 0},
		{name: "Floor_negative", code: "import \"math\"\nmath.Floor(-3.2)", want: -4.0, delta: 0},

		{name: "Ceil_positive", code: "import \"math\"\nmath.Ceil(3.2)", want: 4.0, delta: 0},
		{name: "Ceil_negative", code: "import \"math\"\nmath.Ceil(-3.7)", want: -3.0, delta: 0},

		{name: "Trunc_positive", code: "import \"math\"\nmath.Trunc(3.9)", want: 3.0, delta: 0},
		{name: "Trunc_negative", code: "import \"math\"\nmath.Trunc(-3.9)", want: -3.0, delta: 0},

		{name: "Round_up", code: "import \"math\"\nmath.Round(3.5)", want: 4.0, delta: 0},
		{name: "Round_down", code: "import \"math\"\nmath.Round(3.4)", want: 3.0, delta: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := newIntrinsicsService(t)
			result, err := service.Eval(t.Context(), tt.code)
			require.NoError(t, err)
			require.InDelta(t, tt.want, result, tt.delta)
		})
	}
}

func TestIntrinsicStrconvFunctions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want any
		name string
		code string
	}{
		{name: "FormatBool_true", code: "import \"strconv\"\nstrconv.FormatBool(true)", want: "true"},
		{name: "FormatBool_false", code: "import \"strconv\"\nstrconv.FormatBool(false)", want: "false"},
		{name: "FormatInt_base10", code: "import \"strconv\"\nstrconv.FormatInt(42, 10)", want: "42"},
		{name: "FormatInt_base16", code: "import \"strconv\"\nstrconv.FormatInt(255, 16)", want: "ff"},
		{name: "FormatInt_base2", code: "import \"strconv\"\nstrconv.FormatInt(10, 2)", want: "1010"},
		{name: "FormatInt_negative", code: "import \"strconv\"\nstrconv.FormatInt(-42, 10)", want: "-42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := newIntrinsicsService(t)
			result, err := service.Eval(t.Context(), tt.code)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}
