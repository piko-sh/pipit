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

package bytecode_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/adapters"
	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/rootfs"
	"pipit.sh/pipit/sdk/stdlib"
)

const receiverMethodSource = `package main

import (
	"fmt"
	"sort"
)

type node[K comparable, V any] struct {
	k    K
	v    V
	next *node[K, V]
}

type List[K comparable, V any] struct{ head *node[K, V] }

func (l *List[K, V]) Push(k K, v V) { l.head = &node[K, V]{k: k, v: v, next: l.head} }
func (l *List[K, V]) Zero() V      { var zero V; return zero }

type Pusher[K comparable, V any] interface {
	Push(K, V)
	Zero() V
}

type ByLen[T any] struct{ items [][]T }

func (b ByLen[T]) Len() int           { return len(b.items) }
func (b ByLen[T]) Less(i, j int) bool { return len(b.items[i]) < len(b.items[j]) }
func (b ByLen[T]) Swap(i, j int)      { b.items[i], b.items[j] = b.items[j], b.items[i] }

func Through() string {
	var p Pusher[string, int] = &List[string, int]{}
	p.Push("a", 1)
	push := p.Push
	push("b", 2)
	sorted := ByLen[byte]{items: [][]byte{[]byte("ccc"), []byte("a"), []byte("bb")}}
	sort.Sort(sorted)
	return fmt.Sprintf("%v %T %s %s", p.Zero(), p.Zero(), p.(*List[string, int]).head.k, sorted.items)
}
`

const receiverMethodWant = "0 int b [a bb ccc]"

func TestAGenericMethodFoundByNameRunsTheBodyForItsReceiver(t *testing.T) {
	forEachFinaliserDispatch(t, func(t *testing.T, service *app.Service) {
		compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": receiverMethodSource})
		require.NoError(t, err)
		result, err := service.CallFunction(t.Context(), compiled, "Through")
		require.NoError(t, err)
		require.Equal(t, receiverMethodWant, result)
	})
}

func TestTheReceiverMethodTableSurvivesSavedBytecode(t *testing.T) {
	service := app.NewService(app.WithBytecodeStore(adapters.NewBytecodeStore(rootfs.NewMemoryStore())))
	service.UseSymbolProviders(stdlib.Providers()...)
	ctx := t.Context()
	compiled, err := service.CompileFileSet(ctx, map[string]string{"main.go": receiverMethodSource})
	require.NoError(t, err)
	require.NoError(t, service.SaveCompiled(ctx, "receiver-methods", compiled))
	loaded, err := service.LoadCompiled(ctx, "receiver-methods")
	require.NoError(t, err)

	require.Len(t, loaded.Root().ReceiverMethods(), len(compiled.Root().ReceiverMethods()))
	result, err := service.CallFunction(ctx, loaded, "Through")
	require.NoError(t, err)
	require.Equal(t, receiverMethodWant, result)
}

func pastTheCapSource(instantiations int) string {
	var source strings.Builder
	source.WriteString("package main\n\ntype Box[T any] struct{ v T }\n\n")
	source.WriteString("func (b *Box[T]) Fresh() any { return new(T) }\n\n")
	source.WriteString("type Freshener interface{ Fresh() any }\n\nfunc All() int {\n\tvar all []Freshener\n")
	for index := 1; index <= instantiations; index++ {
		fmt.Fprintf(&source, "\tall = append(all, &Box[[%d]int]{})\n", index)
	}
	source.WriteString("\tfor _, f := range all {\n\t\t_ = f.Fresh()\n\t}\n\treturn len(all)\n}\n")
	return source.String()
}

func TestAGenericMethodWithNoBodyForItsReceiverIsRefused(t *testing.T) {
	forEachFinaliserDispatch(t, func(t *testing.T, service *app.Service) {
		compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": pastTheCapSource(program.MaxSpecialisationsPerFunction + 1)})
		require.NoError(t, err)
		_, err = service.CallFunction(t.Context(), compiled, "All")
		require.ErrorIs(t, err, fault.ErrNoMethodBodyForReceiver,
			"a receiver past the specialisation cap is refused rather than given the shared body")

		withinCap, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": pastTheCapSource(program.MaxSpecialisationsPerFunction)})
		require.NoError(t, err)
		count, err := service.CallFunction(t.Context(), withinCap, "All")
		require.NoError(t, err)
		require.Equal(t, program.MaxSpecialisationsPerFunction, count)
	})
}
