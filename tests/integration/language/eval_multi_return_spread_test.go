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

func TestEvalMultiReturnSpreadIntoFixedParams(t *testing.T) {
	t.Parallel()

	const program = `package main

type accumulator struct {
	total int
}

func (a *accumulator) addPair(x, y int) int {
	a.total += x + y
	return a.total
}

func pair() (int, int) {
	return 10, 20
}

func triple() (int, int, int) {
	return 1, 2, 3
}

func sumTwo(a, b int) int {
	return a + b
}

func sumThree(a, b, c int) int {
	return a + b + c
}

func run() int {
	acc := &accumulator{}
	return sumTwo(pair())*1000000 + sumThree(triple())*1000 + acc.addPair(pair())
}
`
	ctx := context.Background()
	service := app.NewService()
	compiled, err := service.CompileFileSet(ctx, map[string]string{"main.go": program})
	require.NoError(t, err)

	result, err := service.ExecuteEntrypoint(ctx, compiled, "run")
	require.NoError(t, err)
	require.Equal(t, 30006030, result,
		"multi-return spread produced %v; want sumTwo=30, sumThree=6, method=30", result)
}
