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
	"context"
	"strings"
	"testing"

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
	for _, phase := range []struct {
		name string
		want string
		hit  bool
	}{
		{name: "cold", want: "1", hit: false},
		{name: "warm", want: "1", hit: true},
		{name: "changed dependency", want: "2", hit: false},
	} {
		t.Run(phase.name, func(t *testing.T) {
			if phase.want == "2" {
				resolved[0].Packages[""]["dep.go"] = "package dep; const Value = 2"
			}
			interpreter := pipit.NewInterpreter()
			var diagnostics bytes.Buffer
			if err := loadResolvedModules(context.Background(), interpreter, cache, resolved, &diagnostics); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(diagnostics.String(), "bytecode cache hit") != phase.hit {
				t.Fatalf("unexpected cache reuse: %s", diagnostics.String())
			}
			result, err := interpreter.EvalFile(context.Background(), "package main; import \"example.com/lib\"; func answer() int { return lib.Answer() }", "answer")
			if err != nil || formatResult(result) != phase.want {
				t.Fatalf("stale dependency constant: result=%v error=%v", result, err)
			}
		})
	}
}
