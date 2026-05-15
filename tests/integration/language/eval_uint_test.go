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

func TestEvalUintBitwise(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		code   string
		expect uint
	}{
		{name: "and", code: "var a uint = 0xFF; var b uint = 0x0F; a & b", expect: 0x0F},
		{name: "or", code: "var a uint = 0xF0; var b uint = 0x0F; a | b", expect: 0xFF},
		{name: "xor", code: "var a uint = 0xFF; var b uint = 0x0F; a ^ b", expect: 0xF0},
		{name: "and not", code: "var a uint = 0xFF; var b uint = 0x0F; a &^ b", expect: 0xF0},
		{name: "not", code: "var a uint = 0; ^a", expect: math.MaxUint64},
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

func TestEvalUintCompound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		code   string
		expect uint
	}{
		{name: "and assign", code: "var a uint = 0xFF; a &= 0x0F; a", expect: 0x0F},
		{name: "or assign", code: "var a uint = 0xF0; a |= 0x0F; a", expect: 0xFF},
		{name: "xor assign", code: "var a uint = 0xFF; a ^= 0x0F; a", expect: 0xF0},
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

func TestEvalUintGlobal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expect any
		name   string
		code   string
	}{
		{
			name:   "global uint set and get",
			code:   "var g uint = 0\nfunc set() { g = 99 }\nset()\ng",
			expect: uint(99),
		},
		{
			name:   "global uint default",
			code:   "var g uint\ng",
			expect: uint(0),
		},
		{
			name:   "global uint increment",
			code:   "var g uint = 5\nfunc inc() { g++ }\ninc()\ninc()\ng",
			expect: uint(7),
		},
		{
			name:   "global uint compound assign",
			code:   "var g uint = 10\nfunc add(n uint) { g += n }\nadd(5)\ng",
			expect: uint(15),
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
