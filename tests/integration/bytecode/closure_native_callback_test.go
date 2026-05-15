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

package bytecode_test

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"

	"github.com/stretchr/testify/require"
)

func concurrentCallbackSymbols(fanOut int) *symtab.SymbolRegistry {
	return symtab.NewSymbolRegistry(symtab.SymbolExports{
		"concurrentcb": {
			"ApplyConcurrently": reflect.ValueOf(func(callback func(int) int) int {
				results := make([]int, fanOut)
				var waitGroup sync.WaitGroup
				waitGroup.Add(fanOut)
				for index := range fanOut {
					go func() {
						defer waitGroup.Done()
						results[index] = callback(index)
					}()
				}
				waitGroup.Wait()
				total := 0
				for _, value := range results {
					total += value
				}
				return total
			}),
		},
	})
}

func TestInterpretedClosureConcurrentNativeCallback(t *testing.T) {
	t.Parallel()
	const fanOut = 64
	const base = 1000

	service := app.NewService()
	service.UseSymbols(concurrentCallbackSymbols(fanOut))
	ctx := context.Background()

	source := `package main

import "concurrentcb"

func run() int {
	base := 1000
	adder := func(x int) int { return base + x }
	return concurrentcb.ApplyConcurrently(adder)
}
`
	compiled, err := service.CompileFileSet(ctx, map[string]string{"main.go": source})
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(ctx, compiled, "run")
	require.NoError(t, err)

	expected := 0
	for index := range fanOut {
		expected += base + index
	}
	require.EqualValues(t, expected, result)
}
