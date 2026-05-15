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

func TestGenericSpecialisationDistinctNamedTypes(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		expect string
	}{
		{
			name: "float_underlying_named_types",
			code: `type Uniter interface{ Unit() string }
type Celsius float64
func (c Celsius) Unit() string { return "C" }
type Fahrenheit float64
func (f Fahrenheit) Unit() string { return "F" }
func unitOf[T Uniter](v T) string { return v.Unit() }
unitOf(Celsius(1)) + unitOf(Fahrenheit(1))`,
			expect: "CF",
		},
		{
			name: "int_underlying_named_types_reverse_order",
			code: `type Ider interface{ Kind() string }
type UserID int
func (u UserID) Kind() string { return "user" }
type PostID int
func (p PostID) Kind() string { return "post" }
func kindOf[T Ider](v T) string { return v.Kind() }
kindOf(PostID(1)) + "-" + kindOf(UserID(1))`,
			expect: "post-user",
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
