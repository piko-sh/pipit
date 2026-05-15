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

package language_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/app"
)

func TestEmbeddedFieldSelectorLowering(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		expect any
	}{
		{
			name: "value_embedded_assign_compound_incdec",
			code: `type Inner struct{ Value int }
type Outer struct{ Inner }
o := Outer{}
o.Value = 5
o.Value += 3
o.Value++
o.Value`,
			expect: 9,
		},
		{
			name: "pointer_embedded_assign_compound_incdec",
			code: `type PInner struct{ Count int }
type POuter struct{ *PInner }
p := POuter{PInner: &PInner{}}
p.Count = 10
p.Count += 5
p.Count++
p.Count`,
			expect: 16,
		},
		{
			name: "string_leaf_promoted_assign",
			code: `type SInner struct{ Name string }
type SOuter struct{ SInner }
s := SOuter{}
s.Name = "hello"
s.Name`,
			expect: "hello",
		},
		{
			name: "address_of_promoted_field",
			code: `type QInner struct{ N int }
type QOuter struct{ QInner }
q := &QOuter{}
ptr := &q.N
*ptr = 42
q.N`,
			expect: 42,
		},
		{
			name: "two_level_value_embedding",
			code: `type A struct{ X int }
type B struct{ A }
type C struct{ B }
c := C{}
c.X = 7
c.X *= 6
c.X`,
			expect: 42,
		},
		{
			name: "float_promoted_field",
			code: `type FInner struct{ Ratio float64 }
type FOuter struct{ FInner }
f := FOuter{}
f.Ratio = 1.5
f.Ratio += 0.5
f.Ratio`,
			expect: 2.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := app.NewService()
			result, err := service.Eval(context.Background(), tt.code)
			require.NoError(t, err)
			require.Equal(t, tt.expect, result)
		})
	}
}
