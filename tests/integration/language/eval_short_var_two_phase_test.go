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

func TestShortVarTwoPhaseEvaluation(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		expect int64
	}{
		{
			name:   "redeclared_var_read_on_rhs",
			code:   `x := 10; x, y := 2, x; y`,
			expect: 10,
		},
		{
			name:   "earlier_lhs_read_by_later_rhs",
			code:   `a := 1; a, c := 5, a; c`,
			expect: 1,
		},
		{
			name:   "swap_like_with_new_var",
			code:   `a := 1; b := 2; a, b, c := b, a, a+b; a*100 + b*10 + c`,
			expect: 213,
		},
		{
			name:   "independent_still_correct",
			code:   `x, y := 3, 4; x*10 + y`,
			expect: 34,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := app.NewService()
			result, err := service.Eval(context.Background(), tt.code)
			require.NoError(t, err)
			require.EqualValues(t, tt.expect, result)
		})
	}
}
