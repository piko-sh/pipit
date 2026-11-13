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

package snippets_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/adapters"
	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/rootfs"
	"pipit.sh/pipit/sdk/stdlib"
)

const firstRoundTripSnippet = 1753

var roundTripExclusions = map[string]string{}

func roundTripSnippets(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("testdata")
	require.NoError(t, err)
	var names []string
	for _, entry := range entries {
		number, _, found := strings.Cut(entry.Name(), "_")
		if !entry.IsDir() || !found {
			continue
		}
		value, err := strconv.Atoi(number)
		if err != nil || value < firstRoundTripSnippet {
			continue
		}
		if _, excluded := roundTripExclusions[entry.Name()]; !excluded {
			names = append(names, entry.Name())
		}
	}
	return names
}

func TestRoundTripExclusionsNameRoundTripSnippets(t *testing.T) {
	t.Parallel()
	for name, reason := range roundTripExclusions {
		require.NotEmpty(t, reason, "%s needs a reason", name)
		require.DirExists(t, filepath.Join("testdata", name))
		number, _, _ := strings.Cut(name, "_")
		value, err := strconv.Atoi(number)
		require.NoError(t, err)
		require.GreaterOrEqual(t, value, firstRoundTripSnippet, "%s is not in the round-trip range", name)
	}
	require.NotEmpty(t, roundTripSnippets(t))
}

func TestParityAfterABytecodeRoundTrip(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping parity tests in short mode")
	}
	for _, name := range roundTripSnippets(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if spec, err := os.ReadFile(filepath.Join("testdata", name, "testspec.json")); err == nil {
				var parsed snippetSpec
				require.NoError(t, json.Unmarshal(spec, &parsed), "parsing spec for %s", name)
				if parsed.RequiresUnsafe && !unsafeLaneAvailable {
					t.Skip("requires the pointer-reinterpreting lane")
				}
			}
			data, err := os.ReadFile(filepath.Join("testdata", name, "eval.go"))
			require.NoError(t, err)
			snippet := string(data)
			expected := parityExpectedOutput(t, name, snippet)
			for _, mode := range dispatchModes() {
				t.Run(mode.name, func(t *testing.T) {
					options := append([]app.Option{app.WithBytecodeStore(adapters.NewBytecodeStore(rootfs.NewMemoryStore()))}, mode.options...)
					compiler := app.NewService(options...)
					compiler.UseSymbolProviders(stdlib.Providers()...)
					ctx := t.Context()
					compiled, err := compiler.CompileFileSet(ctx, map[string]string{"main.go": snippet})
					require.NoError(t, err, "compiling %s", name)
					require.NoError(t, compiler.SaveCompiled(ctx, name, compiled))
					service := app.NewService(options...)
					service.UseSymbolProviders(stdlib.Providers()...)
					loaded, err := service.LoadCompiled(ctx, name)
					require.NoError(t, err, "loading %s", name)
					result, err := service.ExecuteEntrypoint(ctx, loaded, "run")
					require.NoError(t, err, "running %s from bytecode in %s mode", name, mode.name)
					actual := strings.TrimSpace(fmt.Sprint(result))
					require.Equal(t, expected, actual,
						"round-trip parity mismatch for %s in %s mode\ngo run: %q\nbytecode: %q", name, mode.name, expected, actual)
				})
			}
		})
	}
}
