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

var handOffMicroPrograms = []struct{ name, source string }{
	{"chan_ptr_from_goroutine", `package main

type Node struct{ Name string; N int; Next *Node }

func run() int {
	ch := make(chan *Node, 16)
	go func() {
		for i := 0; i < 16; i++ {
			ch <- &Node{Name: "n", N: i}
		}
		close(ch)
	}()
	s := 0
	for n := range ch {
		s += n.N + len(n.Name)
	}
	return s
}
`},
	{"chan_string_from_goroutine", `package main

func run() int {
	ch := make(chan string, 16)
	go func() {
		for i := 0; i < 16; i++ {
			b := []byte{'a', byte('a' + i)}
			ch <- string(b) + "xyz"
		}
		close(ch)
	}()
	s := 0
	for v := range ch {
		s += len(v)
	}
	return s
}
`},
	{"chan_strslice_from_goroutine", `package main

func run() int {
	ch := make(chan []string, 16)
	go func() {
		for i := 0; i < 16; i++ {
			ch <- []string{"a", "b", "c"}
		}
		close(ch)
	}()
	s := 0
	for v := range ch {
		s += len(v)
	}
	return s
}
`},
	{"chan_struct_value_no_goroutine", `package main

type P struct{ A string; B int }

func run() int {
	ch := make(chan P, 64)
	for i := 0; i < 64; i++ {
		b := []byte{'a', byte('a' + i%26)}
		ch <- P{A: string(b), B: i}
	}
	close(ch)
	s := 0
	for v := range ch {
		s += v.B + len(v.A)
	}
	return s
}
`},
	{"chan_string_no_goroutine", `package main

func run() int {
	ch := make(chan string, 64)
	for i := 0; i < 64; i++ {
		b := []byte{'a', byte('a' + i%26)}
		ch <- string(b) + "q"
	}
	close(ch)
	s := 0
	for v := range ch {
		s += len(v)
	}
	return s
}
`},
	{"go_string_args", `package main

import "sync"

func run() int {
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0
	for i := 0; i < 8; i++ {
		b := []byte{'k', byte('a' + i)}
		key := string(b) + "-suffix"
		wg.Add(1)
		go func(k string, n int) {
			defer wg.Done()
			mu.Lock()
			total += len(k) + n
			mu.Unlock()
		}(key, i)
	}
	wg.Wait()
	return total
}
`},
	{"go_closure_capture_only", `package main

import "sync"

func run() int {
	var wg sync.WaitGroup
	results := make([]int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = i * 2
		}()
	}
	wg.Wait()
	s := 0
	for _, v := range results {
		s += v
	}
	return s
}
`},
	{"go_none_baseline", `package main

func run() int {
	s := 0
	for i := 0; i < 8; i++ {
		b := []byte{'k', byte('a' + i)}
		s += len(string(b) + "-suffix") + i
	}
	return s
}
`},
	{"defer_string_args", `package main

var acc int

func note(s string, n int) { acc += len(s) + n }

func work(i int) int {
	b := []byte{'d', byte('a' + i%26)}
	s := string(b) + "zz"
	defer note(s, i)
	return i
}

func run() int {
	acc = 0
	t := 0
	for i := 0; i < 256; i++ {
		t += work(i)
	}
	return t + acc
}
`},
	{"defer_struct_args", `package main

type R struct{ Name string; A, B int }

var acc int

func note(r R) { acc += len(r.Name) + r.A }

func work(i int) int {
	b := []byte{'d', byte('a' + i%26)}
	r := R{Name: string(b), A: i, B: 2}
	defer note(r)
	return i
}

func run() int {
	acc = 0
	t := 0
	for i := 0; i < 256; i++ {
		t += work(i)
	}
	return t + acc
}
`},
	{"closure_capture_string", `package main

func mk(s string) func() int { return func() int { return len(s) } }

func run() int {
	t := 0
	for i := 0; i < 256; i++ {
		b := []byte{'c', byte('a' + i%26)}
		f := mk(string(b) + "x")
		t += f()
	}
	return t
}
`},
	{"closure_capture_int", `package main

func mk(n int) func() int { return func() int { return n } }

func run() int {
	t := 0
	for i := 0; i < 256; i++ {
		f := mk(i)
		t += f()
	}
	return t
}
`},
	{"panic_recover_string", `package main

func risky(i int) (out int) {
	defer func() {
		if r := recover(); r != nil {
			out = len(r.(string))
		}
	}()
	b := []byte{'p', byte('a' + i%26)}
	panic(string(b) + "boom")
}

func run() int {
	t := 0
	for i := 0; i < 64; i++ {
		t += risky(i)
	}
	return t
}
`},
	{"panic_recover_error", `package main

import "errors"

func risky(i int) (out int) {
	defer func() {
		if r := recover(); r != nil {
			out = len(r.(error).Error())
		}
	}()
	panic(errors.New("boom"))
}

func run() int {
	t := 0
	for i := 0; i < 64; i++ {
		t += risky(i)
	}
	return t
}
`},
	{"workers_results_map", `package main

import "sync"

type Item struct{ K string; V int }

func run() int {
	var wg sync.WaitGroup
	var mu sync.Mutex
	m := map[string]int{}
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 8; i++ {
				b := []byte{'w', byte('a' + w), byte('a' + i)}
				it := &Item{K: string(b), V: i}
				mu.Lock()
				m[it.K] = it.V
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()
	return len(m)
}
`},
}

func BenchmarkHandOffMicros(b *testing.B) {
	symbols := symtab.NewSymbolRegistry(stdlib.Exports())
	for _, p := range handOffMicroPrograms {
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
