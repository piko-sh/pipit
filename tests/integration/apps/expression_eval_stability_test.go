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

package apps_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/sdk/stdlib"
)

const (
	expressionEvalBenchmarkSource   = "../../../tests/bench/cross_language/benchmarks/05_expression_eval_10k/go/pipit_source.go"
	expressionEvalBenchmarkExpected = "856361636"
	expressionEvalBenchmarkCycles   = 3
)

func TestExpressionEvalBenchmarkStableAcrossExecutions(t *testing.T) {
	source, err := os.ReadFile(filepath.Clean(expressionEvalBenchmarkSource))
	require.NoError(t, err, "vendored benchmark source must be readable")
	wrapper := "package main\nfunc EntrypointRun() string { return Run() }\n"
	service := app.NewService()
	service.UseSymbols(symtab.NewSymbolRegistry(stdlib.Exports()))
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": string(source), "entry.go": wrapper})
	require.NoError(t, err)
	for cycle := range expressionEvalBenchmarkCycles {
		result, err := service.ExecuteEntrypoint(context.Background(), compiled, "EntrypointRun")
		require.NoError(t, err)
		require.Equalf(t, expressionEvalBenchmarkExpected, result, "cycle %d on one Service diverged from the native result", cycle)
	}
	for cycle := range expressionEvalBenchmarkCycles {
		fresh := app.NewService()
		fresh.UseSymbols(symtab.NewSymbolRegistry(stdlib.Exports()))
		freshCompiled, err := fresh.CompileFileSet(context.Background(), map[string]string{"main.go": string(source), "entry.go": wrapper})
		require.NoError(t, err)
		result, err := fresh.ExecuteEntrypoint(context.Background(), freshCompiled, "EntrypointRun")
		require.NoError(t, err)
		require.Equalf(t, expressionEvalBenchmarkExpected, result, "cycle %d on a fresh Service diverged from the native result", cycle)
	}
}
