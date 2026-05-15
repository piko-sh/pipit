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

func TestSimdKernelBoundCorrectSlice(t *testing.T) {
	t.Parallel()

	result, err := app.NewService().Eval(context.Background(), `a := []float64{1, 2, 3, 4}
b := []float64{1, 2}
sum := 0.0
for i := 0; i < len(b); i++ {
	sum += a[i] * b[i]
}
sum`)
	require.NoError(t, err)
	require.Equal(t, 5.0, result)
}

func TestSimdKernelBoundLongerSlicePanics(t *testing.T) {
	t.Parallel()

	_, err := app.NewService().Eval(context.Background(), `a := []float64{1, 2}
b := []float64{1, 2, 3, 4}
sum := 0.0
for i := 0; i < len(b); i++ {
	sum += a[i] * b[i]
}
sum`)
	require.Error(t, err)
}

func TestSimdAddShiftedOverlap(t *testing.T) {
	t.Parallel()

	result, err := app.NewService().Eval(context.Background(), `base := make([]float64, 10)
for i := 0; i < len(base); i++ {
	base[i] = float64(i + 1)
}
a := base[0:9]
d := base[1:10]
b := make([]float64, 9)
for i := 0; i < len(b); i++ {
	b[i] = 100.0
}
for i := 0; i < len(a); i++ {
	d[i] = a[i] + b[i]
}
d[8]`)
	require.NoError(t, err)
	require.Equal(t, 901.0, result)
}

func TestSimdPreDeclaredLoopVarWriteback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		code   string
		expect int64
	}{
		{
			name:   "unroller_const_bound",
			code:   "var i int\ntotal := 0\nfor i = 0; i < 4; i++ {\n\ttotal += i\n}\ni",
			expect: 4,
		},
		{
			name:   "kernel_len_bound",
			code:   "a := make([]float64, 4)\na[0]=1.0\na[1]=2.0\na[2]=3.0\na[3]=4.0\nvar i int\nsum := 0.0\nfor i = 0; i < len(a); i++ {\n\tsum += a[i]\n}\ni",
			expect: 4,
		},
		{
			name:   "kernel_const_bound",
			code:   "a := make([]float64, 8)\nvar i int\nsum := 0.0\nfor i = 0; i < 8; i++ {\n\tsum += a[i]\n}\ni",
			expect: 8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := app.NewService().Eval(context.Background(), tt.code)
			require.NoError(t, err)
			require.EqualValues(t, tt.expect, result)
		})
	}
}
