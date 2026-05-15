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
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"
)

func TestClosureCallbackStringsSurviveReuse(t *testing.T) {
	t.Parallel()
	const walkLength = 1000
	var results []string
	symbols := symtab.NewSymbolRegistry(symtab.SymbolExports{
		"walker": {
			"Walk": reflect.ValueOf(func(fn func(int) string) int {
				results = results[:0]
				for index := range walkLength {
					results = append(results, fn(index))
				}
				return len(results)
			}),
		},
	})
	service := app.NewService()
	service.UseSymbols(symbols)

	source := `package main

import "walker"

func run() int {
	prefix := "item-"
	return walker.Walk(func(index int) string {
		s := prefix
		for step := 0; step < index%7; step++ {
			s += "x"
		}
		return s + "-" + string(rune('a'+index%26))
	})
}
`
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(context.Background(), compiled, "run")
	require.NoError(t, err)
	require.EqualValues(t, walkLength, result)

	require.Len(t, results, walkLength)
	for index, got := range results {
		want := "item-"
		for step := 0; step < index%7; step++ {
			want += "x"
		}
		want += "-" + string(rune('a'+index%26))
		require.Equal(t, want, got, "result %d", index)
	}
}

func TestClosureCallbackParentReleaseThenLateCall(t *testing.T) {
	t.Parallel()
	var stored func(int) string
	symbols := symtab.NewSymbolRegistry(symtab.SymbolExports{
		"registry": {
			"Register": reflect.ValueOf(func(fn func(int) string) { stored = fn }),
		},
	})
	service := app.NewService()
	service.UseSymbols(symbols)

	source := `package main

import "registry"

func init() {
	label := "late"
	registry.Register(func(n int) string { return label + "-" + string(rune('0'+n%10)) })
}

func run() int { return 1 }
`
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
	require.NoError(t, err)
	_, err = service.ExecuteEntrypoint(context.Background(), compiled, "run")
	require.NoError(t, err)
	require.NotNil(t, stored)

	const callsPerGoroutine = 50
	var waitGroup sync.WaitGroup
	failures := make(chan string, 2*callsPerGoroutine)
	for goroutine := range 2 {
		waitGroup.Go(func() {
			for call := range callsPerGoroutine {
				n := goroutine*callsPerGoroutine + call
				if got, want := stored(n), fmt.Sprintf("late-%d", n%10); got != want {
					failures <- fmt.Sprintf("call %d: got %q, want %q", n, got, want)
				}
			}
		})
	}
	waitGroup.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
}
