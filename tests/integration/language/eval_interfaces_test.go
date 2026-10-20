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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
)

func TestEvalInterfaceBoxing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want any
		name string
		code string
	}{
		{
			name: "box and unbox int",
			code: "func wrap(x any) any { return x }\nwrap(42)",
			want: 42,
		},
		{
			name: "box and unbox float",
			code: "func wrap(x any) any { return x }\ny := 3.14\nwrap(y)",
			want: 3.14,
		},
		{
			name: "box and unbox bool",
			code: "func wrap(x any) any { return x }\nb := true\nwrap(b)",
			want: true,
		},
		{
			name: "box and unbox string",
			code: "func wrap(x any) any { return x }\ns := \"hello\"\nwrap(s)",
			want: "hello",
		},
		{
			name: "box and unbox uint",
			code: "func wrap(x any) any { return x }\nvar u uint = 42\nwrap(u)",
			want: uint(42),
		},
		{
			name: "unbox to typed int",
			code: `func box() any { return 42 }
func use() int { v := box(); return v.(int) }
use()`,
			want: 42,
		},
		{
			name: "unbox to typed string",
			code: `func box() any { return "hello" }
func use() string { v := box(); return v.(string) }
use()`,
			want: "hello",
		},
		{
			name: "interface function param float add",
			code: `func add(a any, b any) float64 {
	return a.(float64) + b.(float64)
}
x := 1.5
y := 2.5
add(x, y)`,
			want: 4.0,
		},
		{
			name: "interface function param bool",
			code: `func negate(x any) bool {
	return !x.(bool)
}
b := true
negate(b)`,
			want: false,
		},
		{
			name: "type assertion comma ok success",
			code: `func check(x any) bool {
	_, ok := x.(int)
	return ok
}
check(42)`,
			want: true,
		},
		{
			name: "type assertion comma ok fail",
			code: `func check(x any) bool {
	_, ok := x.(string)
	return ok
}
check(42)`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.Eval(t.Context(), tt.code)
			require.NoError(t, err, "code: %s", tt.code)
			require.Equal(t, tt.want, result, "code: %s", tt.code)
		})
	}
}
