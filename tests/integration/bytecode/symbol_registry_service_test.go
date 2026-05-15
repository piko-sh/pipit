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
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/symtab"
)

type sectionLikeNode struct {
	Title    string
	Slug     string
	Children []sectionLikeNode
	Level    int
}

type mutualA struct {
	B     *mutualB
	Label string
}

type mutualB struct {
	A      *mutualA
	Marker int
}

func TestSymbolRegistrySelfReferentialStructTypeChecks(t *testing.T) {
	t.Parallel()

	service := newTestServiceWithSymbols(t, symtab.SymbolExports{
		"tree": {
			"Node": reflect.ValueOf((*sectionLikeNode)(nil)),
			"Make": reflect.ValueOf(func() sectionLikeNode {
				return sectionLikeNode{
					Title: "root",
					Slug:  "root",
					Level: 1,
					Children: []sectionLikeNode{
						{Title: "a", Slug: "a", Level: 2},
						{Title: "b", Slug: "b", Level: 2, Children: []sectionLikeNode{
							{Title: "b1", Slug: "b1", Level: 3},
						}},
					},
				}
			}),
		},
	})

	result, err := service.Eval(context.Background(), `
import (
	"tree"
)
func walk(node tree.Node) int {
	total := 1
	for _, child := range node.Children {
		total += walk(child)
	}
	return total
}
walk(tree.Make())
`)
	require.NoError(t, err, "eval should not fail on self-referential type")
	require.Equal(t, 4, result)
}

func TestSymbolRegistryMutuallyRecursiveTypeChecks(t *testing.T) {
	t.Parallel()

	service := newTestServiceWithSymbols(t, symtab.SymbolExports{
		"pair": {
			"A": reflect.ValueOf((*mutualA)(nil)),
			"B": reflect.ValueOf((*mutualB)(nil)),
			"Make": reflect.ValueOf(func() *mutualA {
				b := &mutualB{Marker: 7}
				a := &mutualA{Label: "root", B: b}
				b.A = a
				return a
			}),
		},
	})

	result, err := service.Eval(context.Background(), `
import (
	"pair"
)
a := pair.Make()
a.Label + "/" + a.B.A.Label
`)
	require.NoError(t, err)
	require.Equal(t, "root/root", result)
}
