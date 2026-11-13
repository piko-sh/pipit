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

package adapters_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/adapters"
	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/rootfs"
	"pipit.sh/pipit/internal/symtab"
)

func fmtService(t *testing.T, store program.BytecodeStorePort) *app.Service {
	t.Helper()
	service := app.NewService(app.WithBytecodeStore(store))
	service.UseSymbols(symtab.NewSymbolRegistry(symtab.SymbolExports{
		"fmt": {"Sprint": reflect.ValueOf(fmt.Sprint), "Sprintf": reflect.ValueOf(fmt.Sprintf)},
	}))
	return service
}

const richProgramSource = `package main

import "fmt"

var (
	count    = 3
	ratio    = 1.5
	label    = "x"
	enabled  = true
	mask     uint64 = 0xF0
	wave     = complex(1, 2)
	register = map[string]int{"a": 1}
)

type Pair[K comparable, V any] struct {
	Key   K
	Value V
}

func (p Pair[K, V]) Describe() string { return fmt.Sprint(p.Key, "=", p.Value) }

type Shape interface{ Area() float64 }

type Square struct {
	Side float64 ` + "`json:\"side\"`" + `
}

func (s Square) Area() float64 { return s.Side * s.Side }

type Labelled struct {
	Square
	Name string
}

func first() string {
	type T struct{ A int }
	return fmt.Sprintf("%T", T{1})
}

func second() string {
	type T struct{ B string }
	return fmt.Sprintf("%T", T{"b"})
}

func split(total int) (half, rest int) {
	half = total / 2
	rest = total - half
	return
}

func counter() func() int {
	n := 0
	return func() int {
		n++
		return n
	}
}

func EntrypointRun() string {
	next := counter()
	next()
	var shape Shape = Labelled{Square: Square{Side: 2}, Name: "sq"}
	half, rest := split(count)
	z := wave * complex(2, 0)
	p := Pair[string, int]{Key: label, Value: count}
	q := Pair[int, bool]{Key: 1, Value: enabled}
	return fmt.Sprint(next(), shape.Area(), half, rest, real(z), imag(z), mask>>4, ratio, register["a"],
		p.Describe(), q.Describe(), first() != second())
}
`

func TestARichProgramSurvivesSaveAndLoad(t *testing.T) {
	t.Parallel()

	store := adapters.NewBytecodeStore(rootfs.NewMemoryStore())
	compiler := fmtService(t, store)
	compiled, err := compiler.CompileFileSet(t.Context(), map[string]string{"main.go": richProgramSource})
	require.NoError(t, err)
	direct, err := compiler.ExecuteEntrypoint(t.Context(), compiled, "EntrypointRun")
	require.NoError(t, err)
	require.NoError(t, compiler.SaveCompiled(t.Context(), "rich", compiled))

	fresh := fmtService(t, store)
	loaded, err := fresh.LoadCompiled(t.Context(), "rich")
	require.NoError(t, err)
	result, err := fresh.ExecuteEntrypoint(t.Context(), loaded, "EntrypointRun")
	require.NoError(t, err)
	require.Equal(t, direct, result, "the loaded program behaves as the compiled one")

	payload, err := adapters.PackCompiledFileSetToBytes(t.Context(), compiled)
	require.NoError(t, err)
	again, err := adapters.PackCompiledFileSetToBytes(t.Context(), compiled)
	require.NoError(t, err)
	require.Equal(t, payload, again, "packing is deterministic")
}
