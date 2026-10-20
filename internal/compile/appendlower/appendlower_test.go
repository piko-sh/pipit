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

package appendlower

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMatchShapeRecognisesASingleElementAppendBackToItself(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		declarations string
		statement    string
		want         bool
	}{
		{name: "an append of one element", statement: "xs = append(xs, 1)", want: true},
		{name: "an append to a different name still matches the shape", statement: "ys = append(xs, 1)", want: true},
		{name: "a parenthesised append", statement: "xs = (append)(xs, 1)", want: true},
		{name: "an append of two elements is not the lowered shape", statement: "xs = append(xs, 1, 2)", want: false},
		{name: "an append of no elements is not the lowered shape", statement: "xs = append(xs)", want: false},
		{name: "another builtin is not an append", statement: "n = copy(xs, ys)", want: false},
		{name: "a non-call right-hand side is not an append", statement: "xs = ys", want: false},
		{name: "an index expression on the left is not the lowered shape", statement: "grid[0] = append(xs, 1)", want: false},
		{
			name:         "a function that shadows append is not the builtin",
			declarations: "func append(xs []int, x int) []int { return xs }\n",
			statement:    "xs = append(xs, 1)",
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			function, info := checkedFunction(t, "package main\n"+tt.declarations+
				"func f(xs, ys []int, grid [][]int, n int) {\n"+tt.statement+"\n_, _, _, _ = xs, ys, grid, n\n}\n")
			assignment := firstAssignment(t, function)

			ident, call, ok := MatchShape(info, assignment.Lhs[0], assignment.Rhs[0])

			require.Equal(t, tt.want, ok)
			if tt.want {
				require.NotNil(t, ident)
				require.NotNil(t, call)
			}
		})
	}

	t.Run("nothing matches without type information", func(t *testing.T) {
		t.Parallel()
		function, _ := checkedFunction(t, "package main\nfunc f(xs []int) { xs = append(xs, 1) }\n")
		assignment := firstAssignment(t, function)

		_, _, ok := MatchShape(nil, assignment.Lhs[0], assignment.Rhs[0])

		require.False(t, ok, "only type information can tell the builtin from a shadowing function")
	})
}
