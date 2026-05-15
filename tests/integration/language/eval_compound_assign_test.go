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

func TestEvalCompoundAssignAcrossOperators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expect any
		name   string
		code   string
	}{
		{name: "string_concat_assign", code: `s := "hello"` + "\ns += \" world\"\ns", expect: "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.Eval(context.Background(), tt.code)
			require.NoError(t, err, "code: %s", tt.code)
			require.Equal(t, tt.expect, result, "code: %s", tt.code)
		})
	}
}

func TestEvalCompoundAssignMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expect any
		name   string
		code   string
	}{
		{
			name:   "map_add_assign",
			code:   "m := map[string]int{\"a\": 1}\nm[\"a\"] += 5\nm[\"a\"]",
			expect: 6,
		},
		{
			name:   "map_sub_assign",
			code:   "m := map[string]int{\"x\": 10}\nm[\"x\"] -= 3\nm[\"x\"]",
			expect: 7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.Eval(context.Background(), tt.code)
			require.NoError(t, err, "code: %s", tt.code)
			require.Equal(t, tt.expect, result, "code: %s", tt.code)
		})
	}
}

func TestEvalCompoundAssignInterfaceSlice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expect     any
		name       string
		source     string
		entrypoint string
	}{
		{
			name: "interface_slice_add_assign",
			source: `package main

func run() interface{} {
	s := make([]interface{}, 3)
	s[0] = 10
	s[1] = 20
	s[2] = 30
	return s[1]
}

func main() {}
`,
			entrypoint: "run",
			expect:     int(20),
		},
		{
			name: "interface_slice_set_string",
			source: `package main

func run() interface{} {
	s := make([]interface{}, 2)
	s[0] = "hello"
	s[1] = "world"
	return s[0]
}

func main() {}
`,
			entrypoint: "run",
			expect:     "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.EvalFile(context.Background(), tt.source, tt.entrypoint)
			require.NoError(t, err)
			require.Equal(t, tt.expect, result)
		})
	}
}

func TestEvalSwitchAcrossClauseForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expect any
		name   string
		code   string
	}{
		{
			name: "switch_with_return_in_function",
			code: `func classify(x int) string {
	switch x {
	case 1:
		return "one"
	case 2:
		return "two"
	default:
		return "other"
	}
}
classify(2)`,
			expect: "two",
		},
		{
			name: "tagless_switch_with_return",
			code: `func bracket(x int) string {
	switch {
	case x < 10:
		return "low"
	case x < 20:
		return "mid"
	default:
		return "high"
	}
}
bracket(15)`,
			expect: "mid",
		},
		{
			name: "multi_value_case_with_return",
			code: `func group(x int) string {
	switch x {
	case 1, 2, 3:
		return "low"
	case 4, 5, 6:
		return "high"
	default:
		return "other"
	}
}
group(3)`,
			expect: "low",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			result, err := service.Eval(context.Background(), tt.code)
			require.NoError(t, err, "code: %s", tt.code)
			require.Equal(t, tt.expect, result, "code: %s", tt.code)
		})
	}
}
