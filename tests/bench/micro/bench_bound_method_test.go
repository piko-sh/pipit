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
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
)

const boundMethodSource = `package main

import "host"

type item struct{ name string; n int }

func (i item) String() string { return i.name + "!" }

type byN []int

func (b byN) Len() int           { return len(b) }
func (b byN) Less(i, j int) bool { return b[i] < b[j] }
func (b byN) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }

func stringer() int {
	it := item{name: "abc", n: 1}
	t := 0
	for k := 0; k < 16; k++ {
		t += len(host.Stringify(it))
	}
	return t
}

func sorter() int {
	xs := make(byN, 64)
	for i := range xs {
		xs[i] = (i * 7919) % 64
	}
	host.SortIt(xs)
	return xs[0]
}
`

func newBoundMethodInterpreter(b *testing.B) (*pipit.Interpreter, *pipit.CompiledFileSet) {
	b.Helper()
	in := pipit.NewInterpreterWithSymbols(pipit.SymbolExports{"host": {
		"Stringify": reflect.ValueOf(func(s fmt.Stringer) string { return s.String() }),
		"Stringer":  reflect.ValueOf((*fmt.Stringer)(nil)),
		"SortIt":    reflect.ValueOf(func(x sort.Interface) { sort.Sort(x) }),
		"Interface": reflect.ValueOf((*sort.Interface)(nil)),
	}})
	cfs, err := in.CompileFileSet(b.Context(), map[string]string{"main.go": boundMethodSource})
	require.NoError(b, err)
	return in, cfs
}

func benchBoundMethod(b *testing.B, entrypoint string) {
	b.Helper()
	in, cfs := newBoundMethodInterpreter(b)
	ctx := context.WithoutCancel(b.Context())
	b.ReportAllocs()
	for b.Loop() {
		_, err := in.CallFunction(ctx, cfs, entrypoint)
		require.NoError(b, err)
	}
}

func BenchmarkBoundStringer16(b *testing.B) { benchBoundMethod(b, "stringer") }
func BenchmarkBoundSort64(b *testing.B)     { benchBoundMethod(b, "sorter") }
