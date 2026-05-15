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
	typeSwitchBenchmarkSource   = "../../../tests/bench/cross_language/benchmarks/19_type_switches/go/pipit_source.go"
	typeSwitchBenchmarkExpected = "3783227424"
	typeSwitchBenchmarkCycles   = 3
)

func TestTypeSwitchBenchmarkStableAcrossExecutions(t *testing.T) {
	source, err := os.ReadFile(filepath.Clean(typeSwitchBenchmarkSource))
	require.NoError(t, err, "vendored benchmark source must be readable")
	wrapper := "package main\nfunc EntrypointRun() string { return Run() }\n"
	for cycle := range typeSwitchBenchmarkCycles {
		service := app.NewService()
		service.UseSymbols(symtab.NewSymbolRegistry(stdlib.Exports()))
		compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": string(source), "entry.go": wrapper})
		require.NoError(t, err)
		result, err := service.ExecuteEntrypoint(context.Background(), compiled, "EntrypointRun")
		require.NoError(t, err)
		require.Equalf(t, typeSwitchBenchmarkExpected, result, "cycle %d diverged from the native result", cycle)
	}
}
