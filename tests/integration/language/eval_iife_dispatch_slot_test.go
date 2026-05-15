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
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/app"
)

type slotDispatchMode struct {
	name    string
	options []app.Option
}

func slotDispatchModes() []slotDispatchMode {
	return []slotDispatchMode{
		{name: "asm", options: nil},
		{name: "go", options: []app.Option{app.WithForceGoDispatch()}},
	}
}

func TestEvalDispatchSaveSlotForGoPushedFrames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source string
		expect any
	}{
		{
			name: "read-only int capture from a non-root frame",
			source: `func f() int {
	x := 1
	y := func() int { return x + 1 }()
	return y
}
f()`,
			expect: 2,
		},
		{
			name: "string capture takes the Go upvalue path",
			source: `func f() string {
	s := "abc"
	y := func() string { return s + "!" }()
	return y
}
f()`,
			expect: "abc!",
		},
		{
			name: "two-value IIFE result in a non-root frame",
			source: `func f() int {
	x := 1
	a, b := func() (int, int) { return x + 1, x + 2 }()
	return a*10 + b
}
f()`,
			expect: 23,
		},
		{
			name: "continuation after the IIFE keeps running",
			source: `func f() int {
	x := 1
	y := func() int { return x + 1 }()
	z := x + 100
	return y*1000 + z
}
f()`,
			expect: 2101,
		},
		{
			name: "IIFE two inline calls deep",
			source: `func g() int {
	x := 5
	y := func() int { return x * 2 }()
	return y + 1
}
func f() int { return g() + 100 }
f()`,
			expect: 111,
		},
		{
			name: "frame that made an inline call before the IIFE",
			source: `func h(n int) int { return n + 1 }
func f() int {
	x := h(1)
	y := func() int { return x + 1 }()
	return y
}
f()`,
			expect: 3,
		},
		{
			name: "frame holding a general register",
			source: `func f() int {
	s := []int{1, 2, 3}
	x := len(s)
	y := func() int { return x + 1 }()
	return y + s[0]
}
f()`,
			expect: 5,
		},
		{
			name: "deferred named function in a typed-only frame",
			source: `var trace int
func g() { trace = 7 }
func f() int {
	defer g()
	x := 1
	return x + 1
}
r := f()
r*100 + trace`,
			expect: 207,
		},
		{
			name: "method call on a named scalar from a typed-only frame",
			source: `type num int
func (n num) double() int { return int(n) * 2 }
func f() int {
	v := num(21)
	return v.double() + 1
}
f()`,
			expect: 43,
		},
		{
			name: "IIFE inside a method body",
			source: `type acc struct{ base int }
func (a acc) calc() int {
	b := a.base
	y := func() int { return b + 1 }()
	return y
}
func f() int { return acc{base: 41}.calc() }
f()`,
			expect: 42,
		},
	}

	for _, mode := range slotDispatchModes() {
		for _, testCase := range cases {
			t.Run(mode.name+"/"+testCase.name, func(t *testing.T) {
				t.Parallel()
				result, err := app.NewService(mode.options...).Eval(context.Background(), testCase.source)
				require.NoError(t, err)
				require.Equal(t, testCase.expect, result)
			})
		}
	}
}
