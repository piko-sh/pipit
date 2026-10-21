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

//go:build integration && !(js && wasm)

package bytecode_test

import (
	"fmt"
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/sdk/stdlib"
)

const deferClosureCaptureSource = `package main

import (
	"deferhost"
	"strconv"
)

func keepLocal(f func() []int) { deferhost.Keep(f()) }

func Slice() int {
	var xs []int
	defer deferhost.KeepSlice(func() []int { return xs })
	xs = []int{1, 2, 3}
	return len(xs)
}

func SliceLocal() int {
	xs := []int{9}
	defer keepLocal(func() []int { return xs })
	xs = append(xs, 8, 7)
	return len(xs)
}

func Int() int {
	n := 1
	defer deferhost.KeepInt(func() int { return n })
	n = 42
	return n
}

func String() int {
	s := "before"
	defer deferhost.KeepString(func() string { return s })
	s = "after-" + strconv.Itoa(7)
	return len(s)
}

func Struct() int {
	type pair struct{ a, b int }
	p := pair{1, 2}
	defer deferhost.KeepString(func() string { return strconv.Itoa(p.a) + "," + strconv.Itoa(p.b) })
	p = pair{3, 4}
	p.b = 5
	return p.a
}

func Map() int {
	m := map[string]int{"a": 1}
	defer deferhost.KeepString(func() string { return strconv.Itoa(len(m)) })
	m = map[string]int{"a": 1, "b": 2, "c": 3}
	return len(m)
}

func Named() (result int) {
	defer deferhost.KeepInt(func() int { return result })
	result = 11
	return result + 1
}

func ints(n int) []int {
	xs := make([]int, 3)
	for i := range xs {
		xs[i] = n*10 + i
	}
	return xs
}

func Returning(n int) int {
	xs := ints(n)
	defer deferhost.KeepInts(func() []int { return xs })
	xs = ints(n + 600)
	return len(xs)
}

func NotReturning(n int) int {
	xs := ints(n)
	defer deferhost.KeepSlice(func() []int { return xs })
	xs = ints(n + 600)
	return len(xs)
}

func Stored(n int) int {
	xs := ints(n)
	defer deferhost.Store(func() []int { return xs })
	xs = ints(n + 600)
	return len(xs)
}

func StoredElement(n int) int {
	xs := ints(n)
	defer deferhost.Store(func() []int { return xs })
	xs[0] = n + 500
	return len(xs)
}

func StoredNow(n int) int {
	xs := ints(n)
	deferhost.Store(func() []int { return xs })
	xs = ints(n + 600)
	return len(xs)
}

func Churn(n int) int {
	t := 0
	for i := 0; i < 16; i++ {
		a := ints(n + 7000 + i)
		f := func() int { return len(a) }
		a = ints(n + 9000 + i)
		t += f() + a[0]
	}
	return t
}

func Loop() int {
	total := 0
	for i := 0; i < 3; i++ {
		defer deferhost.KeepInt(func() int { return total })
		total += i
	}
	total = 100
	return total
}
`

func TestDeferredCallClosureArgumentsSeeLaterAssignments(t *testing.T) {
	var kept []string
	var stored []func() []int
	keep := func(v any) { kept = append(kept, fmt.Sprint(v)) }
	exports := maps.Clone(stdlib.Exports())
	exports["deferhost"] = map[string]reflect.Value{
		"Keep":       reflect.ValueOf(func(v any) { keep(v) }),
		"KeepSlice":  reflect.ValueOf(func(f func() []int) { keep(f()) }),
		"KeepInt":    reflect.ValueOf(func(f func() int) { keep(f()) }),
		"KeepString": reflect.ValueOf(func(f func() string) { keep(f()) }),
		"Store": reflect.ValueOf(func(f func() []int) int {
			stored = append(stored, f)
			return len(stored)
		}),
		"KeepInts": reflect.ValueOf(func(f func() []int) int {
			xs := f()
			keep(xs)
			return len(xs)
		}),
	}
	service := app.NewService()
	service.UseSymbols(symtab.NewSymbolRegistry(exports))
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": deferClosureCaptureSource})
	require.NoError(t, err)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))

	cases := []struct {
		entry string
		want  string
	}{
		{"Slice", "[1 2 3]"},
		{"SliceLocal", "[9 8 7]"},
		{"Int", "42"},
		{"String", "after-7"},
		{"Struct", "3,5"},
		{"Map", "3"},
		{"Named", "12"},
		{"Loop", "100;100;100"},
		{"Returning", "[6010 6011 6012]"},
		{"NotReturning", "[6010 6011 6012]"},
		{"Stored", "[6010 6011 6012]"},
		{"StoredElement", "[501 11 12]"},
		{"StoredNow", "[6010 6011 6012]"},
	}
	var failures []string
	for _, testCase := range cases {
		kept = kept[:0]
		var arguments []any
		if testCase.entry == "Returning" || testCase.entry == "NotReturning" || strings.HasPrefix(testCase.entry, "Stored") {
			arguments = []any{1}
		}
		_, err := service.CallFunction(t.Context(), compiled, testCase.entry, arguments...)
		require.NoError(t, err, "calling %s", testCase.entry)
		for round := range 8 {
			_, err := service.CallFunction(t.Context(), compiled, "Churn", round)
			require.NoError(t, err)
		}
		for _, f := range stored {
			kept = append(kept, fmt.Sprint(f()))
		}
		stored = nil
		if got := strings.Join(kept, ";"); got != testCase.want {
			failures = append(failures, fmt.Sprintf("%s: the deferred call's closure read %q, Go reads %q", testCase.entry, got, testCase.want))
		}
	}
	require.Empty(t, failures)
}
