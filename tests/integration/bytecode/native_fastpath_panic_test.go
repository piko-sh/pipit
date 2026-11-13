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
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/sdk/stdlib"
)

const fastPathPanicSource = `package main

import (
	"fmt"
	"strings"

	"hostpanic"
)

func Function(values []int) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result += fmt.Sprint("|recovered: ", r)
		}
	}()
	for _, v := range values {
		result += fmt.Sprint(hostpanic.Check(v), ",")
	}
	return result
}

func Method(values []int) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result += fmt.Sprint("|recovered: ", r)
		}
	}()
	var b strings.Builder
	for _, v := range values {
		b.Grow(v)
		result += "g,"
	}
	return result
}
`

func hostPanicCheck(n int) int {
	if n < 0 {
		panic(fmt.Sprintf("negative %d", n))
	}
	return n
}

func TestNativeFastPathPanicPropagatesFromACachedCall(t *testing.T) {
	exports := stdlib.Exports()
	exports["hostpanic"] = map[string]reflect.Value{"Check": reflect.ValueOf(hostPanicCheck)}
	symbols := symtab.NewSymbolRegistry(exports)
	tests := []struct {
		entry  string
		values []int
		want   string
	}{
		{entry: "Function", values: []int{-1}, want: "|recovered: negative -1"},
		{entry: "Function", values: []int{1, 2, -3, 4}, want: "1,2,|recovered: negative -3"},
		{entry: "Method", values: []int{-1}, want: "|recovered: strings.Builder.Grow: negative count"},
		{entry: "Method", values: []int{1, 2, -3, 4}, want: "g,g,|recovered: strings.Builder.Grow: negative count"},
	}
	configurations := map[string][]app.Option{
		"assembly": nil,
		"go-loop":  {app.WithForceGoDispatch()},
	}
	for name, options := range configurations {
		for _, tt := range tests {
			t.Run(fmt.Sprintf("%s/%s/%v", name, tt.entry, tt.values), func(t *testing.T) {
				service := app.NewService(options...)
				service.UseSymbols(symbols)
				ctx := t.Context()
				cfs, err := service.CompileFileSet(ctx, map[string]string{"main.go": fastPathPanicSource})
				require.NoError(t, err)
				for run := range 3 {
					got, callErr := service.CallFunction(ctx, cfs, tt.entry, tt.values)
					require.NoError(t, callErr, "run %d", run)
					require.Equal(t, tt.want, got, "run %d", run)
				}
			})
		}
	}
}
