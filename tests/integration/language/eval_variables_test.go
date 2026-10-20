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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
)

func TestEvalVariableDeclaration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want any
		name string
		code string
	}{
		{
			name: "short var decl int",
			code: `x := 42
x`,
			want: 42,
		},
		{
			name: "short var decl string",
			code: `s := "hello"
s`,
			want: "hello",
		},
		{
			name: "short var decl float",
			code: `f := 3.14
f`,
			want: 3.14,
		},
		{
			name: "var decl with value",
			code: `var x int = 10
x`,
			want: 10,
		},
		{
			name: "multiple assignments",
			code: `x := 1
x = x + 1
x = x * 3
x`,
			want: 6,
		},
		{
			name: "variable in expression",
			code: `a := 10
b := 20
a + b`,
			want: 30,
		},
		{
			name: "variable reassignment",
			code: `x := "hello"
x = "world"
x`,
			want: "world",
		},
		{
			name: "string variable concatenation",
			code: `greeting := "hello"
name := "world"
greeting + " " + name`,
			want: "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.Eval(t.Context(), tt.code)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}

func TestEvalGlobalVariables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want any
		name string
		code string
	}{
		{
			name: "global int",
			code: "var g int = 0\nfunc set() { g = 42 }\nset()\ng",
			want: 42,
		},
		{
			name: "global int default",
			code: "var g int\ng",
			want: 0,
		},
		{
			name: "global float",
			code: "var g float64 = 0.0\nfunc set() { g = 3.14 }\nset()\ng",
			want: float64(3.14),
		},
		{
			name: "global string",
			code: "var g string\nfunc set() { g = \"hello\" }\nset()\ng",
			want: "hello",
		},
		{
			name: "global bool",
			code: "var g bool\nfunc set() { g = true }\nset()\ng",
			want: true,
		},
		{
			name: "global int increment",
			code: "var counter int\nfunc inc() { counter++ }\ninc()\ninc()\ninc()\ncounter",
			want: 3,
		},
		{
			name: "global int compound assign",
			code: "var total int = 10\nfunc add(n int) { total += n }\nadd(5)\nadd(3)\ntotal",
			want: 18,
		},
		{
			name: "global with initialiser",
			code: "var g int = 42\nfunc get() int { return g }\nget()",
			want: 42,
		},
		{
			name: "multiple globals",
			code: "var x int = 10\nvar y int = 20\nfunc sum() int { return x + y }\nsum()",
			want: 30,
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

func TestEvalControlFlow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want any
		name string
		code string
	}{
		{
			name: "if true branch",
			code: `x := 0
if true {
	x = 1
}
x`,
			want: 1,
		},
		{
			name: "if false branch",
			code: `x := 0
if false {
	x = 1
}
x`,
			want: 0,
		},
		{
			name: "if else true",
			code: `x := 0
if true {
	x = 1
} else {
	x = 2
}
x`,
			want: 1,
		},
		{
			name: "if else false",
			code: `x := 0
if false {
	x = 1
} else {
	x = 2
}
x`,
			want: 2,
		},
		{
			name: "if with condition",
			code: `x := 10
y := 0
if x > 5 {
	y = 1
} else {
	y = 2
}
y`,
			want: 1,
		},
		{
			name: "for loop sum",
			code: `sum := 0
for i := 0; i < 10; i++ {
	sum = sum + i
}
sum`,
			want: 45,
		},
		{
			name: "for loop countdown",
			code: `n := 10
for n > 0 {
	n = n - 1
}
n`,
			want: 0,
		},
		{
			name: "nested if",
			code: `x := 15
result := 0
if x > 10 {
	if x > 20 {
		result = 3
	} else {
		result = 2
	}
} else {
	result = 1
}
result`,
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.Eval(t.Context(), tt.code)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}
