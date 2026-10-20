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

func TestUintConstPoolAcrossFrames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code string
		want any
	}{
		{
			name: "single call",
			code: `bump := func(v uint) uint {
	v += 5
	return v
}
bump(10)`,
			want: uint(15),
		},
		{
			name: "nested calls",
			code: `bump := func(v uint) uint {
	v += 5
	return v
}
bump(bump(10))`,
			want: uint(20),
		},
		{
			name: "uint div and rem across a call",
			code: `mix := func(v uint) uint {
	return v/3 + v%3
}
mix(uint(20))`,
			want: uint(8),
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
