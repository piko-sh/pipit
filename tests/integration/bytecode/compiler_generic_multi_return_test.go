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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const genericMultiReturnSource = `package main

type number interface {
	int | int64 | float64
}

func pick[T number](value any) (T, bool) {
	switch typed := value.(type) {
	case T:
		return typed, true
	}
	return 0, false
}

func forward[T number](value any) (T, bool) {
	return pick[T](value)
}

func main() {
	n, ok := pick[int]("42")
	var f float64
	f, ok = pick[float64](2.5)
	w, okw := forward[int64](int64(9))
	println(n, ok, f, w, okw)
}
`

func TestGenericMultiReturnCallsSpecialise(t *testing.T) {
	cfs := compileFileSource(t, genericMultiReturnSource)
	functions := program.ExportFunctions(cfs.Root())

	tests := []struct {
		name          string
		caller        string
		wantCallee    string
		wantResult    isa.RegisterKind
		unwantedNames []string
	}{
		{name: "define", caller: "main", wantCallee: "pick[spec:int]", wantResult: isa.RegisterInt, unwantedNames: []string{"pick", "forward"}},
		{name: "forwarding return", caller: "forward[spec:int64]", wantCallee: "pick[spec:int64]", wantResult: isa.RegisterInt, unwantedNames: []string{"pick"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			caller := findCompiledFunctionByName(t, cfs, tc.caller)
			if caller == nil {
				caller = findExportedFunctionNamed(functions, tc.caller)
			}
			require.NotNil(t, caller, "caller %q missing", tc.caller)
			targets := calleeNamesOf(caller, functions)
			require.Contains(t, targets, tc.wantCallee)
			for _, unwanted := range tc.unwantedNames {
				require.NotContains(t, targets, unwanted, "erased template %q still called from %q", unwanted, tc.caller)
			}
			callee := findExportedFunctionNamed(functions, tc.wantCallee)
			require.NotNil(t, callee)
			require.Equal(t, tc.wantResult, callee.ResultKinds[0])
		})
	}
}

func calleeNamesOf(caller *program.CompiledFunction, functions []*program.CompiledFunction) []string {
	names := make([]string, 0, len(caller.CallSites))
	for _, site := range caller.CallSites {
		if site.IsNative || int(site.FunctionIndex) >= len(functions) || functions[site.FunctionIndex] == nil {
			continue
		}
		names = append(names, functions[site.FunctionIndex].Name)
	}
	return names
}

func findExportedFunctionNamed(functions []*program.CompiledFunction, name string) *program.CompiledFunction {
	for _, fn := range functions {
		if fn != nil && strings.EqualFold(fn.Name, name) {
			return fn
		}
	}
	return nil
}
