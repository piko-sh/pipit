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

package compile

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
)

const receiverInstancesSource = `package main

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
`

func receiverEntries(root *program.CompiledFunction) map[string][]string {
	entries := make(map[string][]string)
	for key, target := range root.ReceiverMethods() {
		shared := root.Functions[key.Method].Name
		entries[shared] = append(entries[shared], root.Functions[target].Name)
	}
	return entries
}

func TestAnInstantiationUsedThroughAnInterfaceGetsItsMethodsEntered(t *testing.T) {
	t.Parallel()

	root := compileSnippet(t, receiverInstancesSource+`
func f() int {
	var p Pusher[string, int] = &List[string, int]{}
	p.Push("a", 1)
	return p.Zero()
}
`)
	entries := receiverEntries(root)

	require.Contains(t, entries["List.Push"], "List.Push[spec:string,int]")
	require.Contains(t, entries["List.Zero"], "List.Zero[spec:string,int]",
		"a method never called directly still gets a body for the instantiation")
}

func TestAnInstantiationOverTypeParametersMapsToTheSharedBody(t *testing.T) {
	t.Parallel()

	root := compileSnippet(t, receiverInstancesSource+`
func f() int {
	var p Pusher[string, int] = &List[string, int]{}
	return p.Zero()
}
`)
	entries := receiverEntries(root)

	require.Contains(t, entries["List.Push"], "List.Push",
		"the receiver type the shared body builds values of keeps the shared body")
}

func TestNothingIsEnteredForAProgramWithoutGenericTypes(t *testing.T) {
	t.Parallel()

	root := compileSnippet(t, "package main\n\ntype T struct{}\n\nfunc (T) M() int { return 1 }\n\nfunc f() int { var i interface{ M() int } = T{}; return i.M() }\n")

	require.Empty(t, root.ReceiverMethods())
}

func TestADirectCallPastTheSpecialisationCapFails(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	source.WriteString("package main\n\ntype Box[T any] struct{ v T }\n\nfunc (b *Box[T]) Fresh() any { return new(T) }\n\nfunc f() {\n")
	for index := 1; index <= program.MaxSpecialisationsPerFunction+1; index++ {
		fmt.Fprintf(&source, "\t_ = (&Box[[%d]int]{}).Fresh()\n", index)
	}
	source.WriteString("}\n")

	_, err := tryCompileSnippet(t, source.String())

	require.ErrorIs(t, err, fault.ErrCompileGenericMethodSpecialisationLimit,
		"a direct call the cap declines fails rather than running the shared body")
}
