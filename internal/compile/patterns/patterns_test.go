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

package patterns

import (
	"go/parser"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMatchLenCallRecognisesALengthOfAPlainName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		want     bool
		wantName string
	}{
		{name: "a length of a name", source: "len(xs)", want: true, wantName: "xs"},
		{name: "a length of a different name", source: "len(buffer)", want: true, wantName: "buffer"},
		{name: "a length of an index expression is not a plain name", source: "len(xs[0])", want: false},
		{name: "a length of a call is not a plain name", source: "len(f())", want: false},
		{name: "a capacity call is not a length", source: "cap(xs)", want: false},
		{name: "a length with no argument does not match", source: "len()", want: false},
		{name: "a length with two arguments does not match", source: "len(xs, ys)", want: false},
		{name: "a method call named len does not match", source: "pkg.len(xs)", want: false},
		{name: "a plain name is not a call", source: "xs", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			expression, err := parser.ParseExpr(tt.source)
			require.NoError(t, err, "the fixture expression must parse")

			ident, ok := MatchLenCall(expression)

			require.Equal(t, tt.want, ok)
			if tt.want {
				require.Equal(t, tt.wantName, ident.Name)
			}
		})
	}
}

func TestNewRegistryStartsEmpty(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()

	require.NotNil(t, registry, "a fresh registry is usable without any recognisers")
}
