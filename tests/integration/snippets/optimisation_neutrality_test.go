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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/sdk/stdlib"
)

func TestOptimisationIsObservationallyNeutral(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping optimisation-neutrality tests in short mode")
	}

	entries, err := os.ReadDir("testdata")
	require.NoError(t, err, "reading testdata directory")

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		directory := filepath.Join("testdata", name)

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			evalPath := filepath.Join(directory, "eval.go")
			requireFileExists(t, evalPath)

			if data, err := os.ReadFile(filepath.Join(directory, "testspec.json")); err == nil {
				var spec snippetSpec
				require.NoError(t, json.Unmarshal(data, &spec), "parsing spec for %s", name)
				if spec.KnownBug != "" {
					t.Skipf("known bug: %s", spec.KnownBug)
				}
				if spec.RequiresUnsafe && !unsafeLaneAvailable {
					t.Skip("requires the pointer-reinterpreting lane")
				}
			}

			snippet := readFile(t, evalPath)
			expected := parityExpectedOutput(t, name, snippet)

			service := app.NewService(app.WithOptimisations(passes.Options{}))
			service.UseSymbolProviders(stdlib.Providers()...)
			result, evalErr := service.EvalFile(context.Background(), snippet, "run")
			require.NoError(t, evalErr, "EvalFile failed for %s with optimisations disabled", name)

			require.Equal(t, expected, strings.TrimSpace(fmt.Sprint(result)),
				"optimisation is observable for %s: the unoptimised build disagrees with go run"+
					"\nsnippet:\n%s", name, snippet)
		})
	}
}
