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

//go:build bench

package bench

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"
)

func BenchmarkEval(b *testing.B) {
	benchmarks := []struct {
		name string
		code string
	}{
		{
			name: "arithmetic_simple",
			code: `1 + 2 * 3`,
		},
		{
			name: "arithmetic_complex",
			code: `((1+2)*3-4)/5 + 6*7 - 8`,
		},
		{
			name: "variable_assignment",
			code: `x := 10
y := 20
z := x + y
z * 2`,
		},
		{
			name: "function_call",
			code: `func add(a, b int) int { return a + b }
add(3, 4)`,
		},
		{
			name: "recursive_fibonacci",
			code: `func fib(n int) int {
	if n <= 1 {
		return n
	}
	return fib(n-1) + fib(n-2)
}
fib(20)`,
		},
		{
			name: "recursive_factorial",
			code: `func fact(n int) int {
	if n <= 1 {
		return 1
	}
	return n * fact(n-1)
}
fact(100)`,
		},
		{
			name: "closure_adder",
			code: `func makeAdder(x int) func(int) int {
	return func(y int) int {
		return x + y
	}
}
add5 := makeAdder(5)
add5(10)`,
		},
		{
			name: "for_loop_sum",
			code: `func sum() int {
	s := 0
	for i := 0; i < 1000; i++ {
		s += i
	}
	return s
}
sum()`,
		},
		{
			name: "string_concat",
			code: `func concat() string {
	s := ""
	for i := 0; i < 100; i++ {
		s += "x"
	}
	return s
}
concat()`,
		},
		{
			name: "slice_append",
			code: `func build() int {
	s := make([]int, 0, 100)
	for i := 0; i < 100; i++ {
		s = append(s, i)
	}
	return len(s)
}
build()`,
		},
		{
			name: "map_operations",
			code: `func mapOps() int {
	m := map[string]int{}
	m["a"] = 1
	m["b"] = 2
	m["c"] = 3
	return m["a"] + m["b"] + m["c"]
}
mapOps()`,
		},
		{
			name: "goroutine_channel",
			code: `func roundTrip() int {
	ch := make(chan int, 1)
	go func() {
		ch <- 42
	}()
	return <-ch
}
roundTrip()`,
		},
		{
			name: "type_switch",
			code: `func classify(v interface{}) string {
	switch v.(type) {
	case int:
		return "int"
	case string:
		return "string"
	case bool:
		return "bool"
	default:
		return "other"
	}
}
classify(42)`,
		},
		{
			name: "compound_assign_loop",
			code: `func sum() int {
	x := 0
	for i := 0; i < 1000; i++ {
		x += i
	}
	return x
}
sum()`,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				service := app.NewService()
				_, err := service.Eval(b.Context(), bm.code)
				if err != nil {
					require.NoError(b, err)
				}
			}
		})
	}
}

func BenchmarkNativeCall(b *testing.B) {
	symbols := symtab.NewSymbolRegistry(symtab.SymbolExports{
		"strings": {
			"ToUpper":    reflect.ValueOf(strings.ToUpper),
			"Contains":   reflect.ValueOf(strings.Contains),
			"HasPrefix":  reflect.ValueOf(strings.HasPrefix),
			"TrimSpace":  reflect.ValueOf(strings.TrimSpace),
			"ReplaceAll": reflect.ValueOf(strings.ReplaceAll),
		},
		"fmt": {
			"Sprintf": reflect.ValueOf(fmt.Sprintf),
		},
	})

	benchmarks := []struct {
		name string
		code string
	}{
		{
			name: "single_call",
			code: `import "strings"
strings.ToUpper("hello")`,
		},
		{
			name: "repeated_calls",
			code: `import "strings"
func transform() string {
	s := strings.ToUpper("hello")
	s = strings.TrimSpace(s)
	return s
}
transform()`,
		},
		{
			name: "call_in_loop",
			code: `import "strings"
func bench() string {
	s := "hello"
	for i := 0; i < 100; i++ {
		s = strings.ToUpper(s)
	}
	return s
}
bench()`,
		},
		{
			name: "multi_arg_call",
			code: `import "strings"
strings.Contains("hello world", "world")`,
		},
		{
			name: "sprintf",
			code: `import "fmt"
fmt.Sprintf("value: %d, name: %s", 42, "test")`,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				service := app.NewService()
				service.UseSymbols(symbols)
				_, err := service.Eval(b.Context(), bm.code)
				if err != nil {
					require.NoError(b, err)
				}
			}
		})
	}
}
