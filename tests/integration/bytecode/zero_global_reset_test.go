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

//go:build integration && !safe && !(js && wasm)

package bytecode_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/sdk/stdlib"
)

var zeroGlobalCases = []struct {
	id, decls, body, want string
}{
	{"int-before-initialised", "var hits int\nvar greeting = \"hi\"", `hits++; greeting += "!"; return greeting + strconv.Itoa(hits)`, "hi!1"},
	{"int-after-initialised", "var greeting = \"hi\"\nvar hits int", `hits++; greeting += "!"; return greeting + strconv.Itoa(hits)`, "hi!1"},
	{"every-bank", "var n int\nvar f float64\nvar u uint8\nvar b bool\nvar s string\nvar tags []string\nvar m map[string]int\nvar label = \"x\"",
		`n++; f += 0.5; u++; b = !b; s += "s"; tags = append(tags, label); if m == nil { m = map[string]int{} }; m[label]++; return fmt.Sprint(n, f, u, b, s, len(tags), m[label])`, fmt.Sprint(1, 0.5, uint8(1), true, "s", 1, 1)},
	{"nil-able", "var p *int\nvar e error\nvar f func() int\nvar ch chan int\nvar label = \"x\"",
		`r := fmt.Sprint(p == nil, e == nil, f == nil, ch == nil); v := len(label); p = &v; e = fmt.Errorf("e"); f = func() int { return v }; ch = make(chan int); return r`, "true true true true"},
	{"only-valueless", "var hits int\nvar greeting string", `hits++; greeting += "!"; return greeting + strconv.Itoa(hits)`, "!1"},
}

func TestValuelessGlobalsStartAtZeroOnEveryEntrypointRun(t *testing.T) {
	for _, testCase := range zeroGlobalCases {
		t.Run(testCase.id, func(t *testing.T) {
			service := app.NewService()
			service.UseSymbolProviders(stdlib.Providers()...)
			source := "package main\n\nimport (\n\t\"fmt\"\n\t\"strconv\"\n)\n\nvar _ = fmt.Sprint\nvar _ = strconv.Itoa\n\n" + testCase.decls + "\n\nfunc run() string { " + testCase.body + " }\n"
			compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
			require.NoError(t, err)
			for round := range 6 {
				result, err := service.ExecuteEntrypoint(t.Context(), compiled, "run")
				require.NoError(t, err)
				require.Equal(t, testCase.want, fmt.Sprint(result), "run %d starts from the declared values", round)
			}
		})
	}
}

func TestGlobalReuseStillSkipsWhenValuelessGlobalsAreOnlyRead(t *testing.T) {
	for _, testCase := range []struct {
		id, body, want string
		skips          bool
	}{
		{"read-only", "return hits + len(table)", "3", true},
		{"written", "hits++; return hits + len(table)", "4", false},
	} {
		t.Run(testCase.id, func(t *testing.T) {
			service := app.NewService()
			service.UseSymbolProviders(stdlib.Providers()...)
			source := "package main\n\nvar hits int\nvar table = []int{1, 2, 3}\n\nfunc run() int { " + testCase.body + " }\n"
			compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
			require.NoError(t, err)
			before := service.VarInitReuseStatistics()
			for round := range 6 {
				result, err := service.ExecuteEntrypoint(t.Context(), compiled, "run")
				require.NoError(t, err)
				require.Equal(t, testCase.want, fmt.Sprint(result), "run %d", round)
			}
			skips := service.VarInitReuseStatistics().Skips - before.Skips
			plan := service.VarInitReusePlanFor(compiled)
			t.Logf("global reuse plan eligible=%v reason=%q skips=%d", plan.Eligible, plan.Reason, skips)
			if testCase.skips && plan.Eligible {
				require.Equal(t, int64(5), skips, "global reuse skips every run after the first")
			}
			if !testCase.skips {
				require.Zero(t, skips, "a written global stops every skip")
			}
		})
	}
}
