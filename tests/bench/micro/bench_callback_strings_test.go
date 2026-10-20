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
	"reflect"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"
)

var callbackHostNames = func() []string {
	names := make([]string, 256)
	for index := range names {
		names[index] = "host-name-" + strconv.Itoa(index)
	}
	return names
}()

func callbackSymbols() *symtab.SymbolRegistry {
	return symtab.NewSymbolRegistry(symtab.SymbolExports{
		"host": {
			"Each": reflect.ValueOf(func(n int, fn func(int)) {
				for index := range n {
					fn(index)
				}
			}),
			"EachWith": reflect.ValueOf(func(s string, n int, fn func(int, string)) {
				for index := range n {
					fn(index, s)
				}
			}),
			"Names": reflect.ValueOf(func() []string { return callbackHostNames }),
		},
	})
}

func runCallbackBenchmarks(b *testing.B, benchmarks []struct {
	name   string
	source string
}) {
	b.Helper()
	symbols := callbackSymbols()
	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			service := app.NewService()
			service.UseSymbols(symbols)
			cfs, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": bm.source})
			require.NoError(b, err)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, err = service.ExecuteEntrypoint(context.Background(), cfs, "run")
				if err != nil {
					require.NoError(b, err)
				}
			}
		})
	}
}

func BenchmarkCallbackRender(b *testing.B) {
	runCallbackBenchmarks(b, []struct {
		name   string
		source string
	}{
		{
			name: "rows",
			source: `package main

import "host"

func run() int {
	names := host.Names()
	var out []string
	host.Each(len(names), func(index int) {
		out = append(out, "<li class=\"row\">")
		out = append(out, names[index])
		if index%2 == 0 {
			out = append(out, " (even)")
		}
		out = append(out, "</li>")
	})
	return len(out)
}
`,
		},
	})
}

func BenchmarkCallbackStringStores(b *testing.B) {
	const prelude = `package main

import "host"

func run() int {
	names := host.Names()
	local := make([]string, 256)
	for index := range local {
		local[index] = "p" + string(rune('a'+index%26))
	}
	out := make([]string, 256)
	host.Each(256, func(index int) {
		for step := 0; step < 16; step++ {
			slot := (index*16 + step) & 255
`
	const epilogue = `
		}
	})
	return len(out[0]) + len(local) + len(names)
}
`
	runCallbackBenchmarks(b, []struct {
		name   string
		source string
	}{
		{name: "constants", source: prelude + "\t\t\tout[slot] = \"constant\"" + epilogue},
		{name: "host", source: prelude + "\t\t\tout[slot] = names[slot]" + epilogue},
		{name: "parent_slice", source: prelude + "\t\t\tout[slot] = local[slot]" + epilogue},
		{name: "mixed", source: prelude + `			switch slot % 3 {
			case 0:
				out[slot] = "constant"
			case 1:
				out[slot] = names[slot]
			default:
				out[slot] = local[slot]
			}` + epilogue},
		{name: "parent_arena", source: `package main

import "host"

func run() int {
	key := "parent-" + string(rune('a'+len(host.Names())%26))
	out := make([]string, 256)
	host.EachWith(key, 256, func(index int, s string) {
		for step := 0; step < 16; step++ {
			out[(index*16+step)&255] = s
		}
	})
	return len(out[0])
}
`},
	})
}
