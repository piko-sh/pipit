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

func TestCompositeLiteralPreservesElementType(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		expect any
	}{
		{name: "slice_any_int_assert", code: `s := []any{1}; s[0].(int)`, expect: int(1)},
		{name: "map_any_int_assert", code: `m := map[string]any{"a": 1}; m["a"].(int)`, expect: int(1)},
		{name: "struct_any_int_assert", code: `type T struct{ X any }; t := T{X: 1}; t.X.(int)`, expect: int(1)},
		{name: "array_any_int_assert", code: `a := [1]any{1}; a[0].(int)`, expect: int(1)},
		{name: "slice_any_int32_assert", code: `s := []any{int32(1)}; s[0].(int32)`, expect: int32(1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := app.NewService()
			result, err := service.Eval(context.Background(), tt.code)
			require.NoError(t, err, "type assertion should succeed with precise element type")
			require.Equal(t, tt.expect, result)
		})
	}
}
