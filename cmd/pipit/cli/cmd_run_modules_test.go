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

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/modloader"
)

func TestPackageCacheBindsResolvedDependencies(t *testing.T) {
	t.Parallel()
	cache := modloader.NewBytecodeCache(t.TempDir())
	resolved := []modloader.ResolvedModule{
		{Path: "example.com/dep", Version: "v1", Packages: map[string]map[string]string{"": {"dep.go": "package dep; const Value = 1"}}, PackageOrder: []string{""}},
		{Path: "example.com/lib", Version: "v1", Packages: map[string]map[string]string{"": {"lib.go": "package lib; import \"example.com/dep\"; func Answer() int { return dep.Value }"}}, PackageOrder: []string{""}},
	}
	phases := []struct {
		name string
		want string
		hit  bool
	}{
		{name: "cold", want: "1", hit: false},
		{name: "warm", want: "1", hit: true},
		{name: "changed dependency", want: "2", hit: false},
	}
	for _, phase := range phases {
		if phase.want == "2" {
			resolved[0].Packages[""]["dep.go"] = "package dep; const Value = 2"
		}
		interpreter := pipit.NewInterpreter()
		var diagnostics bytes.Buffer
		require.NoError(t, loadResolvedModules(t.Context(), interpreter, cache, resolved, &diagnostics), "phase %s", phase.name)
		require.Equal(t, phase.hit, strings.Contains(diagnostics.String(), "bytecode cache hit"), "phase %s cache reuse: %s", phase.name, diagnostics.String())
		result, err := interpreter.EvalFile(t.Context(), "package main; import \"example.com/lib\"; func answer() int { return lib.Answer() }", "answer")
		require.NoError(t, err, "phase %s", phase.name)
		require.Equal(t, phase.want, formatResult(result), "phase %s: the dependency constant is current", phase.name)
	}
}
