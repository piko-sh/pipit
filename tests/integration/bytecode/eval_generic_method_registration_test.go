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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
)

func TestGenericMethodSpecialisationsStayOutOfMethodTable(t *testing.T) {
	t.Parallel()

	const source = `
type Holder[A any] struct{ a A }

func (h Holder[A]) convert[B any](b B) string { return "" }
func (h Holder[A]) plain() string             { return "" }

func run() string {
	h := Holder[int]{}
	_ = h.convert[string]("x")
	_ = h.convert[bool](true)
	_ = Holder[int8]{}.convert[float64](1)
	return h.plain()
}
run()
`

	root := compileExpression(t, source)
	table := root.MethodTable()

	specialised := map[uint16]string{}
	for _, function := range root.Functions {
		if function.SpecialisationOrigin != nil {
			specialised[indexOfFunction(t, root, function)] = function.Name
		}
	}
	require.NotEmpty(t, specialised, "expected the generic method to have specialised at least once")
	require.Contains(t, table, "Holder.plain", "expected the non-generic method to be registered by name")

	for name, index := range table {
		_, isSpecialisation := specialised[index]
		require.False(t, isSpecialisation,
			"method table entry %q resolves to specialised function index %d", name, index)
		require.False(t, strings.Contains(name, "[spec:"),
			"method table contains a specialisation name %q", name)
	}
}

func indexOfFunction(t *testing.T, root, function *program.CompiledFunction) uint16 {
	t.Helper()
	for index, candidate := range root.Functions {
		if candidate == function {

			return uint16(index)
		}
	}
	require.Fail(t, "function not present in root.Functions")
	return 0
}
