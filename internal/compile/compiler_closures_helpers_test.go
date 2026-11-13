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
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/compile/inline"
)

func TestAdmitsUnsafePointerSeesThroughNamesAndConstraints(t *testing.T) {
	t.Parallel()

	unsafePointer := types.Typ[types.UnsafePointer]
	named := types.NewNamed(types.NewTypeName(token.NoPos, nil, "handle", nil), unsafePointer, nil)
	parameter := func(terms ...*types.Term) *types.TypeParam {
		constraint := types.NewInterfaceType(nil, []types.Type{types.NewUnion(terms)})
		return types.NewTypeParam(types.NewTypeName(token.NoPos, nil, "T", nil), constraint)
	}

	tests := []struct {
		name  string
		typ   types.Type
		admit bool
	}{
		{name: "unsafe.Pointer itself", typ: unsafePointer, admit: true},
		{name: "a type defined over it", typ: named, admit: true},
		{name: "an integer", typ: types.Typ[types.Int], admit: false},
		{
			name:  "a type parameter whose union holds it",
			typ:   parameter(types.NewTerm(true, types.Typ[types.Int]), types.NewTerm(false, unsafePointer)),
			admit: true,
		},
		{
			name:  "a type parameter whose union does not",
			typ:   parameter(types.NewTerm(true, types.Typ[types.Int]), types.NewTerm(false, types.Typ[types.String])),
			admit: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.admit, admitsUnsafePointer(test.typ))
		})
	}
}

func TestAProgramNamingUnsafePointerReachesMemoryThroughUnsafe(t *testing.T) {
	t.Parallel()

	plain := compileSnippet(t, "package main\n\nfunc main() { values := []int{1}; _ = values[0] }\n")
	require.False(t, reachesMemoryThroughUnsafe(inline.CollectReachableFunctions(plain)))

	generic := compileSnippet(t, `package main

import "unsafe"

func first[T ~uintptr | unsafe.Pointer](values []T) T { return values[0] }

func main() { _ = first([]uintptr{1}) }
`)
	require.True(t, reachesMemoryThroughUnsafe(inline.CollectReachableFunctions(generic)),
		"a constraint that admits unsafe.Pointer is enough, though no value of it is made")
}
