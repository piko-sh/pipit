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
	"maps"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/sdk/stdlib"
)

var entrypointGlobalsPrograms = []struct{ name, source string }{
	{"tables", `package main

import (
	"strconv"
	"strings"
)

var table = map[string]int{"alpha": 1, "beta": 2, "gamma": 3, "delta": 4, "epsilon": 5, "zeta": 6, "eta": 7, "theta": 8}
var names = []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
var banner = strings.Repeat("=", 16)
var limits = struct{ Lo, Hi int }{Lo: 2, Hi: 9}
var labels = func() []string {
	out := make([]string, 0, len(names))
	for i, n := range names {
		out = append(out, strings.ToUpper(n)+"-"+strconv.Itoa(i))
	}
	return out
}()

func run() int {
	total := 0
	for _, n := range names {
		total += table[n]
	}
	return total + len(banner) + limits.Hi - limits.Lo + len(labels[3])
}
`},
	{"scalar_written", `package main

import "strings"

var hits = 0
var names = []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
var banner = strings.Repeat("=", 16)

func run() int {
	hits++
	return hits + len(names) + len(banner)
}
`},
	{"sorted_by_native", `package main

import "sort"

var names = []string{"theta", "eta", "zeta", "epsilon", "delta", "gamma", "beta", "alpha"}

func run() int {
	sort.Strings(names)
	return len(names[0])
}
`},
}

var byteConversionPrograms = []struct{ name, source string }{
	{"reads_only", `package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"unicode/utf8"
)

func run() int {
	total := 0
	for i := 0; i < 8; i++ {
		s := "conversion-" + strconv.Itoa(i) + "-payload-with-some-length"
		if bytes.Contains([]byte(s), []byte("payload")) {
			total++
		}
		if utf8.Valid([]byte(s)) {
			total++
		}
		h := sha256.Sum256([]byte(s))
		total += int(h[0])
		total += len(hex.EncodeToString([]byte(s)))
	}
	return total
}
`},
	{"writer_copies", `package main

import (
	"declhost"
	"strconv"
)

func run() int {
	total := 0
	for i := 0; i < 32; i++ {
		s := "conversion-" + strconv.Itoa(i) + "-payload-with-some-length"
		total += declhost.Scribble([]byte(s))
	}
	return total
}
`},
}

const hostDeclaredBorrowsSource = `package main

import (
	"declhost"
	"strconv"
)

func run() int {
	total := 0
	for i := 0; i < 32; i++ {
		s := "borrowed-" + strconv.Itoa(i) + "-string"
		xs := []string{s + "-a", s + "-b"}
		total += declhost.Score(s, xs)
	}
	return total
}
`

func BenchmarkEntrypointGlobals(b *testing.B) {
	runEntrypointPrograms(b, entrypointGlobalsPrograms)
}

func benchScribble(b []byte) int {
	if len(b) > 0 {
		b[0] = 'X'
	}
	return len(b)
}

func benchScore(s string, xs []string) int {
	total := len(s)
	for _, x := range xs {
		total += len(x)
	}
	return total
}

func declarationBenchSymbols() *symtab.SymbolRegistry {
	exports := maps.Clone(stdlib.Exports())
	exports["declhost"] = map[string]reflect.Value{
		"Scribble": reflect.ValueOf(benchScribble),
		"Score":    reflect.ValueOf(benchScore),
	}
	return symtab.NewSymbolRegistry(exports)
}

func BenchmarkByteConversions(b *testing.B) {
	symbols := declarationBenchSymbols()
	for _, p := range byteConversionPrograms {
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

func BenchmarkHostDeclaredBorrows(b *testing.B) {
	symbols := declarationBenchSymbols()
	for _, declared := range []bool{true, false} {
		name := "undeclared"
		if declared {
			name = "declared"
		}
		b.Run(name, func(b *testing.B) {
			service := app.NewService()
			service.UseSymbols(symbols)
			if declared {
				require.NoError(b, service.DeclareNativeBorrowClass(benchScore, engine.BorrowBorrows))
			}
			ctx := context.WithoutCancel(b.Context())
			cfs, err := service.CompileFileSet(ctx, map[string]string{"main.go": hostDeclaredBorrowsSource})
			require.NoError(b, err)
			b.ReportAllocs()
			for b.Loop() {
				_, err = service.ExecuteEntrypoint(ctx, cfs, "run")
				require.NoError(b, err)
			}
		})
	}
}
