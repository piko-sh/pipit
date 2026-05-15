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
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/app"
)

func TestMinInt64DivideOverflow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		code   string
		expect int64
	}{
		{name: "div_overflow", code: "func d(a, b int64) int64 { return a / b }\nd(-9223372036854775808, -1)", expect: math.MinInt64},
		{name: "rem_overflow", code: "func m(a, b int64) int64 { return a % b }\nm(-9223372036854775808, -1)", expect: 0},
		{name: "div_normal", code: "func d(a, b int64) int64 { return a / b }\nd(20, 3)", expect: 6},
		{name: "rem_normal", code: "func m(a, b int64) int64 { return a % b }\nm(20, 3)", expect: 2},
		{name: "div_negative", code: "func d(a, b int64) int64 { return a / b }\nd(-20, 3)", expect: -6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := app.NewService().Eval(context.Background(), tt.code)
			require.NoError(t, err)
			require.EqualValues(t, tt.expect, result)
		})
	}
}
