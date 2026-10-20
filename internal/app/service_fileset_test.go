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

package app

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/symtab"
)

func TestEvalFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		source     string
		entrypoint string
		want       string
		wantErr    error
	}{
		{
			name: "runs an entrypoint",
			source: `package main

func entrypoint() int {
	return 42
}

func main() {}
`,
			entrypoint: "entrypoint",
			want:       "42",
		},
		{
			name: "calls a helper",
			source: `package main

func double(x int) int {
	return x * 2
}

func entrypoint() int {
	return double(21)
}

func main() {}
`,
			entrypoint: "entrypoint",
			want:       "42",
		},
		{
			name: "returns a string",
			source: `package main

func entrypoint() string {
	return "hello world"
}

func main() {}
`,
			entrypoint: "entrypoint",
			want:       "hello world",
		},
		{
			name: "a variadic entrypoint call",
			source: `package main

func sum(nums ...int) int {
	total := 0
	for _, n := range nums {
		total += n
	}
	return total
}

func run() int {
	return sum(10, 20, 12)
}
`,
			entrypoint: "run",
			want:       "42",
		},
		{
			name: "an empty variadic call",
			source: `package main

func count(arguments ...int) int {
	return len(arguments)
}

func run() int {
	return count()
}
`,
			entrypoint: "run",
			want:       "0",
		},
		{
			name: "a spread variadic call",
			source: `package main

func sum(nums ...int) int {
	total := 0
	for _, n := range nums {
		total += n
	}
	return total
}

func run() int {
	s := []int{1, 2, 3, 4, 5}
	return sum(s...)
}
`,
			entrypoint: "run",
			want:       "15",
		},
		{
			name: "a variadic call after a fixed parameter",
			source: `package main

func first(prefix string, nums ...int) int {
	r := len(prefix)
	for _, n := range nums {
		r += n
	}
	return r
}

func run() int {
	return first("abc", 10, 20)
}
`,
			entrypoint: "run",
			want:       "33",
		},
		{
			name: "a missing entrypoint",
			source: `package main

func other() int {
	return 1
}

func main() {}
`,
			entrypoint: "nonexistent",
			wantErr:    fault.ErrEntrypointNotFound,
		},
		{name: "a parse error", source: "not valid go {{{", entrypoint: "entrypoint", wantErr: fault.ErrParse},
		{
			name: "a type error",
			source: `package main

func entrypoint() int {
	return "not an int"
}

func main() {}
`,
			entrypoint: "entrypoint",
			wantErr:    fault.ErrTypeCheck,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := NewService().EvalFile(t.Context(), tt.source, tt.entrypoint)
			require.ErrorIs(t, err, tt.wantErr)
			if tt.wantErr != nil {
				return
			}
			require.Equal(t, tt.want, fmt.Sprint(result))
		})
	}
}

func TestCompileFileSet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		options    []Option
		sources    map[string]string
		entrypoint string
		want       string
	}{
		{
			name: "one file",
			sources: map[string]string{
				"main.go": `package main

func entrypoint() int {
	return 100
}

func main() {}
`,
			},
			entrypoint: "entrypoint",
			want:       "100",
		},
		{
			name: "several files",
			sources: map[string]string{
				"helpers.go": `package main

func triple(x int) int {
	return x * 3
}
`,
				"main.go": `package main

func entrypoint() int {
	return triple(14)
}

func main() {}
`,
			},
			entrypoint: "entrypoint",
			want:       "42",
		},
		{
			name: "calls in both directions between files",
			sources: map[string]string{
				"a.go": `package main

func addOne(x int) int {
	return x + 1
}

func entrypoint() int {
	return doubleAndAdd(20)
}
`,
				"b.go": `package main

func doubleAndAdd(x int) int {
	return addOne(x * 2)
}
`,
			},
			entrypoint: "entrypoint",
			want:       "41",
		},
		{
			name:    "a build tag excludes a file",
			options: []Option{WithBuildTags("custom")},
			sources: map[string]string{
				"main.go": `package main

func Run() int { return 1 }
`,
				"extra.go": "//go:build !custom\n\npackage main\n\nfunc Run() int { return 2 }\n",
			},
			entrypoint: "Run",
			want:       "1",
		},
		{
			name:    "a build tag includes a file",
			options: []Option{WithBuildTags("custom")},
			sources: map[string]string{
				"main.go": `package main

func Entry() int { return helper() }
`,
				"impl_custom.go":  "//go:build custom\n\npackage main\n\nfunc helper() int { return 42 }\n",
				"impl_default.go": "//go:build !custom\n\npackage main\n\nfunc helper() int { return 0 }\n",
			},
			entrypoint: "Entry",
			want:       "42",
		},
		{
			name: "the ignore tag excludes a file",
			sources: map[string]string{
				"main.go": `package main

func Run() int { return 1 }
`,
				"ignored.go": "//go:build ignore\n\npackage main\n\nfunc Run() int { return 999 }\n",
			},
			entrypoint: "Run",
			want:       "1",
		},
		{
			name: "the host GOOS selects a file",
			sources: map[string]string{
				"main.go": `package main

func Run() string { return "base" }
`,
				"platform.go": "//go:build " + runtime.GOOS + "\n\npackage main\n\nfunc Platform() string { return \"" + runtime.GOOS + "\" }\n",
				"other.go":    "//go:build fakeos\n\npackage main\n\nfunc Platform() string { return \"fake\" }\n",
			},
			entrypoint: "Platform",
			want:       runtime.GOOS,
		},
		{
			name:    "boolean build constraints",
			options: []Option{WithBuildTags("tagA")},
			sources: map[string]string{
				"main.go": `package main

func Run() int { return helper() }
`,
				"impl_ab.go":   "//go:build tagA && tagB\n\npackage main\n\nfunc helper() int { return 1 }\n",
				"impl_a.go":    "//go:build tagA && !tagB\n\npackage main\n\nfunc helper() int { return 2 }\n",
				"impl_none.go": "//go:build !tagA && !tagB\n\npackage main\n\nfunc helper() int { return 3 }\n",
			},
			entrypoint: "Run",
			want:       "2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := NewService(tt.options...)
			cfs, err := service.CompileFileSet(t.Context(), tt.sources)
			require.NoError(t, err)
			result, err := service.ExecuteEntrypoint(t.Context(), cfs, tt.entrypoint)
			require.NoError(t, err)
			require.Equal(t, tt.want, fmt.Sprint(result))
		})
	}
}

func TestCompileFileSet_ReExecute(t *testing.T) {
	t.Parallel()
	sources := map[string]string{
		"main.go": `package main

func entrypoint() int {
	return 7 * 6
}

func main() {}
`,
	}

	service := NewService()
	cfs, err := service.CompileFileSet(t.Context(), sources)
	require.NoError(t, err)

	for range 3 {
		result, err := service.ExecuteEntrypoint(t.Context(), cfs, "entrypoint")
		require.NoError(t, err)
		require.Equal(t, "42", fmt.Sprint(result))
	}
}

func TestCompileFileSet_DifferentEntrypoints(t *testing.T) {
	t.Parallel()
	sources := map[string]string{
		"main.go": `package main

func alpha() string {
	return "alpha"
}

func beta() string {
	return "beta"
}

func main() {}
`,
	}

	service := NewService()
	cfs, err := service.CompileFileSet(t.Context(), sources)
	require.NoError(t, err)

	r1, err := service.ExecuteEntrypoint(t.Context(), cfs, "alpha")
	require.NoError(t, err)
	require.Equal(t, "alpha", fmt.Sprint(r1))

	r2, err := service.ExecuteEntrypoint(t.Context(), cfs, "beta")
	require.NoError(t, err)
	require.Equal(t, "beta", fmt.Sprint(r2))
}

func TestEvalFile_PackageLevelVars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "global int var",
			source: `package main

var counter int

func inc() { counter++ }

func Main() int {
	inc()
	inc()
	inc()
	return counter
}
`,
			want: "3",
		},
		{
			name: "global with initialiser",
			source: `package main

var base int = 100

func add(n int) int { return base + n }

func Main() int {
	return add(42)
}
`,
			want: "142",
		},
		{
			name: "multiple global types",
			source: `package main

var x int = 10
var y float64 = 2.5
var name string = "pipit"

func Main() string {
	return name
}
`,
			want: "pipit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := NewService()
			result, err := service.EvalFile(t.Context(), tt.source, "Main")
			require.NoError(t, err)
			require.Equal(t, tt.want, fmt.Sprint(result))
		})
	}
}

func TestCompileFileSet_ErrorInSource(t *testing.T) {
	t.Parallel()
	sources := map[string]string{
		"main.go": `package main

func entrypoint() int {
	return "not an int"
}
`,
	}

	service := NewService()
	_, err := service.CompileFileSet(t.Context(), sources)
	require.ErrorIs(t, err, fault.ErrTypeCheck)
}

func TestCompileProgram(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		options    []Option
		sources    map[string]map[string]string
		entrypoint string
		want       string
	}{
		{
			name: "a cross-package call",
			sources: map[string]map[string]string{
				"": {
					"main.go": `package main

import (
	"testmod/lib"
)

func entrypoint() int {
	return lib.Add(1, 2)
}

func main() {}
`,
				},
				"lib": {
					"lib.go": `package lib

func Add(a, b int) int {
	return a + b
}
`,
				},
			},
			entrypoint: "entrypoint",
			want:       "3",
		},
		{
			name: "a cross-package constant",
			sources: map[string]map[string]string{
				"": {
					"main.go": `package main

import (
	"testmod/lib"
)

func entrypoint() int {
	return lib.Answer
}

func main() {}
`,
				},
				"lib": {
					"lib.go": `package lib

const Answer = 42
`,
				},
			},
			entrypoint: "entrypoint",
			want:       "42",
		},
		{
			name: "a cross-package float constant",
			sources: map[string]map[string]string{
				"": {
					"main.go": `package main

import (
	"testmod/lib"
)

func entrypoint() float64 {
	return lib.Pi
}

func main() {}
`,
				},
				"lib": {
					"lib.go": `package lib

const Pi = 3.14
`,
				},
			},
			entrypoint: "entrypoint",
			want:       "3.14",
		},
		{
			name: "a cross-package bool constant",
			sources: map[string]map[string]string{
				"": {
					"main.go": `package main

import (
	"testmod/lib"
)

func entrypoint() bool {
	return lib.Flag
}

func main() {}
`,
				},
				"lib": {
					"lib.go": `package lib

const Flag = true
`,
				},
			},
			entrypoint: "entrypoint",
			want:       "true",
		},
		{
			name: "a cross-package string constant",
			sources: map[string]map[string]string{
				"": {
					"main.go": `package main

import (
	"testmod/lib"
)

func entrypoint() string {
	return lib.Greeting
}

func main() {}
`,
				},
				"lib": {
					"lib.go": `package lib

const Greeting = "hello"
`,
				},
			},
			entrypoint: "entrypoint",
			want:       "hello",
		},
		{
			name: "a dependency chain",
			sources: map[string]map[string]string{
				"": {
					"main.go": `package main

import (
	"testmod/b"
)

func entrypoint() int {
	return b.Double(21)
}

func main() {}
`,
				},
				"a": {
					"a.go": `package a

func Add(x, y int) int {
	return x + y
}
`,
				},
				"b": {
					"b.go": `package b

import (
	"testmod/a"
)

func Double(x int) int {
	return a.Add(x, x)
}
`,
				},
			},
			entrypoint: "entrypoint",
			want:       "42",
		},
		{
			name: "a blank import",
			sources: map[string]map[string]string{
				"": {
					"main.go": `package main

import (
	_ "testmod/sideeffect"
)

func entrypoint() int {
	return 1
}

func main() {}
`,
				},
				"sideeffect": {
					"sideeffect.go": `package sideeffect

func helper() int { return 42 }

func init() {
	_ = helper()
}
`,
				},
			},
			entrypoint: "entrypoint",
			want:       "1",
		},
		{
			name: "a cross-package method call",
			sources: map[string]map[string]string{
				"": {
					"main.go": `package main

import (
	"testmod/lib"
)

func entrypoint() int {
	c := lib.NewCounter(10)
	c = c.Add(5)
	return c.Value()
}

func main() {}
`,
				},
				"lib": {
					"lib.go": `package lib

type Counter struct { V int }

func NewCounter(v int) Counter { return Counter{V: v} }

func (c Counter) Add(x int) Counter { return Counter{V: c.V + x} }

func (c Counter) Value() int { return c.V }
`,
				},
			},
			entrypoint: "entrypoint",
			want:       "15",
		},
		{
			name:    "build tags select files in imported packages",
			options: []Option{WithBuildTags("interp_test")},
			sources: map[string]map[string]string{
				"": {
					"main.go": `package main

import (
	"testmod/util"
)

func Run() int { return util.Val() }

func main() {}
`,
				},
				"util": {
					"val_tagged.go":  "//go:build interp_test\n\npackage util\n\nfunc Val() int { return 100 }\n",
					"val_default.go": "//go:build !interp_test\n\npackage util\n\nfunc Val() int { return 0 }\n",
				},
			},
			entrypoint: "Run",
			want:       "100",
		},
		{
			name: "a cross-package float constant in an expression",
			sources: map[string]map[string]string{
				"": {
					"main.go": `package main

import (
	"testmod/lib"
)

func entrypoint() float64 {
	return lib.Pi * 2
}

func main() {}
`,
				},
				"lib": {
					"lib.go": `package lib

const Pi = 3.14
`,
				},
			},
			entrypoint: "entrypoint",
			want:       "6.28",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := NewService(tt.options...)
			cfs, err := service.CompileProgram(t.Context(), "testmod", tt.sources)
			require.NoError(t, err)
			result, err := service.ExecuteEntrypoint(t.Context(), cfs, tt.entrypoint)
			require.NoError(t, err)
			require.Equal(t, tt.want, fmt.Sprint(result))
		})
	}
}

func TestCompileProgram_CyclicImport(t *testing.T) {
	t.Parallel()

	sources := map[string]map[string]string{
		"": {
			"main.go": `package main

import (
	"testmod/a"
)

func entrypoint() int { return a.X() }
func main() {}
`,
		},
		"a": {
			"a.go": `package a

import (
	"testmod/b"
)

func X() int { return b.Y() }
`,
		},
		"b": {
			"b.go": `package b

import (
	"testmod/a"
)

func Y() int { return a.X() }
`,
		},
	}

	service := NewService()
	_, err := service.CompileProgram(t.Context(), "testmod", sources)
	require.ErrorIs(t, err, fault.ErrCyclicImport)
}

func TestCompileProgram_NoMainPackage(t *testing.T) {
	t.Parallel()

	sources := map[string]map[string]string{
		"lib": {
			"lib.go": `package lib

func Hello() string { return "hello" }
`,
		},
	}

	service := NewService()
	cfs, err := service.CompileProgram(t.Context(), "testmod", sources)
	require.NoError(t, err)

	err = service.ExecuteInits(t.Context(), cfs)
	require.NoError(t, err)
}

func TestExecuteEntrypoint_NotFound(t *testing.T) {
	t.Parallel()
	sources := map[string]string{
		"main.go": `package main

func other() int { return 1 }
func main() {}
`,
	}

	service := NewService()
	cfs, err := service.CompileFileSet(t.Context(), sources)
	require.NoError(t, err)

	_, err = service.ExecuteEntrypoint(t.Context(), cfs, "nonexistent")
	require.ErrorIs(t, err, fault.ErrEntrypointNotFound)
}

func TestEvalFile_NativeVariadic(t *testing.T) {
	t.Parallel()
	source := `package main

import (
	"fmt"
)

func run() string {
	return fmt.Sprintf("hello %s, you are %d", "world", 42)
}
`
	symbols := symtab.NewSymbolRegistry(symtab.SymbolExports{
		"fmt": {
			"Sprintf": reflect.ValueOf(fmt.Sprintf),
		},
	})
	service := NewService()
	service.UseSymbols(symbols)
	cfs, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(t.Context(), cfs, "run")
	require.NoError(t, err)
	require.Equal(t, "hello world, you are 42", result)
}

func TestCompileFileSet_BuildTagAllExcluded(t *testing.T) {
	t.Parallel()

	sources := map[string]string{
		"a.go": "//go:build ignore\n\npackage main\n\nfunc Run() int { return 1 }\n",
	}

	service := NewService()
	_, err := service.CompileFileSet(t.Context(), sources)
	require.ErrorIs(t, err, fault.ErrParse)
	require.ErrorContains(t, err, "all files excluded")
}

func TestReflectedVariableRetainsUnexportedTypeMethods(t *testing.T) {
	t.Parallel()
	service := NewService()
	service.UseSymbols(symtab.NewSymbolRegistry(symtab.SymbolExports{"encoding/binary": {
		"LittleEndian": reflect.ValueOf(&binary.LittleEndian).Elem(),
	}}))
	result, err := service.EvalFile(t.Context(), `package main
 import "encoding/binary"
 func run() uint16 { return binary.LittleEndian.Uint16([]byte{42, 1}) }
 `, "run")
	require.NoError(t, err)
	require.Equal(t, "298", fmt.Sprint(result))
}
