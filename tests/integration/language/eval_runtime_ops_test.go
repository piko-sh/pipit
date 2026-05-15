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

func TestEvalRuntimeGeneralComparisons(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expect any
		name   string
		code   string
	}{
		{
			name: "eq via interface func",
			code: `func eq(a, b any) bool { return a == b }
eq(1, 1)`,
			expect: true,
		},
		{
			name: "ne via interface func",
			code: `func eq(a, b any) bool { return a == b }
eq(1, 2)`,
			expect: false,
		},
		{
			name: "lt via interface func",
			code: `func lt(a, b any) bool { return a.(int) < b.(int) }
lt(1, 2)`,
			expect: true,
		},
		{
			name: "lt false via interface",
			code: `func lt(a, b any) bool { return a.(int) < b.(int) }
lt(2, 1)`,
			expect: false,
		},
		{
			name: "le via interface",
			code: `func le(a, b any) bool { return a.(int) <= b.(int) }
le(1, 1)`,
			expect: true,
		},
		{
			name: "gt via interface",
			code: `func gt(a, b any) bool { return a.(int) > b.(int) }
gt(3, 1)`,
			expect: true,
		},
		{
			name: "ge via interface",
			code: `func ge(a, b any) bool { return a.(int) >= b.(int) }
ge(1, 2)`,
			expect: false,
		},
		{
			name: "string eq via interface",
			code: `func eq(a, b any) bool { return a.(string) == b.(string) }
eq("hello", "hello")`,
			expect: true,
		},
		{
			name: "string lt via interface",
			code: `func lt(a, b any) bool { return a.(string) < b.(string) }
lt("a", "b")`,
			expect: true,
		},
		{
			name: "direct any eq string",
			code: `func eq(a, b any) bool { return a == b }
eq("hello", "hello")`,
			expect: true,
		},
		{
			name: "direct any eq nil",
			code: `func eq(a, b any) bool { return a == b }
eq(nil, nil)`,
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.Eval(context.Background(), tt.code)
			require.NoError(t, err)
			require.Equal(t, tt.expect, result)
		})
	}
}
