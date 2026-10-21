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
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/sdk/stdlib"
)

var typedStorePrograms = []struct{ name, source string }{
	{"deref_pointer_free", `package main

type pt struct {
	a, b, c int
}

func run() int {
	p := new(pt)
	q := &pt{}
	for i := 0; i < 1000; i++ {
		*p = pt{a: i, b: i + 1, c: i + 2}
		*q = *p
	}
	return p.a + q.c
}
`},
	{"deref_pointer_free_global", `package main

type pt struct {
	a, b, c int
}

var g pt

func run() int {
	p := &g
	for i := 0; i < 1000; i++ {
		*p = pt{a: i, b: i + 1, c: i + 2}
	}
	return g.a + g.c
}
`},
	{"deref_pointer_free_array", `package main

func run() int {
	p := new([4]int)
	for i := 0; i < 1000; i++ {
		*p = [4]int{i, i + 1, i + 2, i + 3}
	}
	return p[0] + p[3]
}
`},
	{"deref_string_struct_local", `package main

type node struct {
	name string
	n    int
}

func run() int {
	var v node
	p := &v
	for i := 0; i < 1000; i++ {
		*p = node{name: "n", n: i}
	}
	return p.n + len(v.name)
}
`},
	{"deref_string_struct_new", `package main

type node struct {
	name string
	n    int
}

func run() int {
	p := new(node)
	q := new(node)
	for i := 0; i < 1000; i++ {
		*p = node{name: "n", n: i}
		*q = *p
	}
	return p.n + q.n + len(q.name)
}
`},
	{"element_pointer_free", `package main

type pt struct {
	a, b, c int
}

func run() int {
	xs := make([]pt, 8)
	for i := 0; i < 1000; i++ {
		xs[i%8] = pt{a: i, b: i + 1, c: i + 2}
	}
	return xs[0].a + xs[7].c
}
`},
}

var stringIntrinsicPrograms = []struct{ name, source string }{
	{"join", `package main

import "strings"

func run() int {
	parts := []string{"a", "bb", "ccc", "dddd"}
	n := 0
	for i := 0; i < 100; i++ {
		n += len(strings.Join(parts, ","))
	}
	return n
}
`},
	{"upper", `package main

import "strings"

func run() int {
	n := 0
	for i := 0; i < 100; i++ {
		n += len(strings.ToUpper("hello world"))
	}
	return n
}
`},
	{"lower", `package main

import "strings"

func run() int {
	n := 0
	for i := 0; i < 100; i++ {
		n += len(strings.ToLower("Hello World"))
	}
	return n
}
`},
	{"repeat", `package main

import "strings"

func run() int {
	n := 0
	for i := 0; i < 100; i++ {
		n += len(strings.Repeat("ab", 8))
	}
	return n
}
`},
	{"repl", `package main

import "strings"

func run() int {
	n := 0
	for i := 0; i < 100; i++ {
		n += len(strings.ReplaceAll("a-b-c-d-e", "-", "+"))
	}
	return n
}
`},
	{"retstr", `package main

import "strings"

func run() string {
	return strings.Repeat("ab", 8) + strings.ToUpper("x") + strings.Join([]string{"p", "q"}, "-")
}
`},
	{"keepstr", `package main

import "strings"

func run() int {
	kept := make(map[int]string, 32)
	for i := 0; i < 16; i++ {
		kept[2*i] = strings.ToUpper("row")
		kept[2*i+1] = strings.Repeat("-", i)
	}
	return len(kept)
}
`},
	{"tobytes", `package main

func run() int {
	s := "hello, world, this is a string"
	n := 0
	for i := 0; i < 100; i++ {
		b := []byte(s)
		n += len(b) + int(b[3])
	}
	return n
}
`},
	{"bytesborrow", `package main

func run() int {
	s := "hello, world, this is a string"
	n := 0
	for i := 0; i < 100; i++ {
		for _, c := range []byte(s) {
			n += int(c)
		}
	}
	return n
}
`},
	{"bytes_written", `package main

func run() int {
	s := "hello, world, this is a string"
	n := 0
	for i := 0; i < 100; i++ {
		b := []byte(s)
		b[0] = byte(i)
		n += int(b[0]) + len(b)
	}
	return n
}
`},
	{"retbytes", `package main

func run() []byte {
	return []byte("hello world")
}
`},
}

func runEntrypointPrograms(b *testing.B, programs []struct{ name, source string }) {
	b.Helper()
	symbols := symtab.NewSymbolRegistry(stdlib.Exports())
	for _, p := range programs {
		b.Run(p.name, func(b *testing.B) {
			service := app.NewService()
			service.UseSymbols(symbols)
			ctx := context.WithoutCancel(b.Context())
			cfs, err := service.CompileFileSet(ctx, map[string]string{"main.go": p.source})
			require.NoError(b, err)
			b.ReportAllocs()
			for b.Loop() {
				_, err = service.ExecuteEntrypoint(ctx, cfs, "run")
				require.NoError(b, err)
			}
		})
	}
}

func BenchmarkTypedStore(b *testing.B)       { runEntrypointPrograms(b, typedStorePrograms) }
func BenchmarkStringIntrinsics(b *testing.B) { runEntrypointPrograms(b, stringIntrinsicPrograms) }
