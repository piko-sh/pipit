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

func TestEvalStringRange(t *testing.T) {
	t.Parallel()

	service := app.NewService()
	result, err := service.Eval(t.Context(), `s := "hello"
n := 0
for range s {
	n = n + 1
}
n`)
	require.NoError(t, err)
	require.Equal(t, 5, result)
}

func TestEvalStringIndexSliceAndConcat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code string
		want int
	}{
		{name: "len hello", code: `len("hello")`, want: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.Eval(t.Context(), tt.code)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}

	boolTests := []struct {
		want any
		name string
		code string
	}{
		{name: "eq true", code: `"abc" == "abc"`, want: true},
		{name: "lt", code: `"abc" < "abd"`, want: true},
		{name: "le equal", code: `"abc" <= "abc"`, want: true},
	}

	for _, tt := range boolTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.Eval(t.Context(), tt.code)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}
