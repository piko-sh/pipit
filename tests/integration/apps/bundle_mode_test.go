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
	"fmt"
	"go/parser"
	"go/token"
	"path"
	"slices"
	"strconv"
	"strings"
	"testing"

	"pipit.sh/pipit/internal/app"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/adapters"
	"pipit.sh/pipit/internal/module"
	"pipit.sh/pipit/sdk/stdlib"
)

func runBundled(t *testing.T, modulePath string, packages map[string]map[string]string) string {
	t.Helper()
	ctx := context.Background()

	builder := app.NewService()
	builder.UseSymbolProviders(stdlib.Providers()...)

	order := bundleOrder(t, modulePath, packages)
	bundles := make([]*module.Bundle, 0, len(order))
	for _, relPath := range order {
		importPath := modulePath + "/" + relPath
		bundle, err := builder.PackageModule(ctx,
			module.Descriptor{
				SchemaVersion: module.DescriptorVersion,
				Ref:           module.Ref{Path: importPath},
			},
			importPath,
			map[string]map[string]string{"": packages[relPath]},
			adapters.PackCompiledFileSetToBytes,
		)
		require.NoError(t, err, "packaging %s", importPath)
		loadBundle(t, builder, bundle)
		bundles = append(bundles, bundle)
	}

	consumer := app.NewService()
	consumer.UseSymbolProviders(stdlib.Providers()...)
	for _, bundle := range bundles {
		loadBundle(t, consumer, bundle)
	}

	mainCfs, err := consumer.CompileProgram(ctx, "main", map[string]map[string]string{"": packages[""]})
	require.NoError(t, err, "compiling main against the loaded bundles")

	result, err := consumer.ExecuteEntrypoint(ctx, mainCfs, "entrypoint")
	require.NoError(t, err, "executing entrypoint against the loaded bundles")
	return fmt.Sprint(result)
}

func loadBundle(t *testing.T, service *app.Service, bundle *module.Bundle) {
	t.Helper()
	reference := module.Ref{
		Path:    bundle.Descriptor.Ref.Path,
		Version: bundle.Descriptor.Ref.Version,
		Pin:     bundle.Descriptor.Ref.Pin,
	}
	_, err := service.LoadModule(context.Background(), bundle, reference, nil, adapters.LoadCompiledFromBytes)
	require.NoError(t, err, "loading bundle %s", reference.Path)
}

func bundleOrder(t *testing.T, modulePath string, packages map[string]map[string]string) []string {
	t.Helper()

	relPaths := make([]string, 0, len(packages))
	for relPath := range packages {
		if relPath != "" {
			relPaths = append(relPaths, relPath)
		}
	}
	slices.Sort(relPaths)

	order := make([]string, 0, len(relPaths))
	state := make(map[string]int, len(relPaths))
	const (
		visiting = 1
		done     = 2
	)
	var visit func(relPath string)
	visit = func(relPath string) {
		switch state[relPath] {
		case done:
			return
		case visiting:
			t.Fatalf("import cycle through package %s", relPath)
		}
		state[relPath] = visiting
		for _, dependency := range intraModuleImports(t, modulePath, packages[relPath]) {
			if _, ok := packages[dependency]; ok {
				visit(dependency)
			}
		}
		state[relPath] = done
		order = append(order, relPath)
	}
	for _, relPath := range relPaths {
		visit(relPath)
	}
	return order
}

func intraModuleImports(t *testing.T, modulePath string, files map[string]string) []string {
	t.Helper()

	prefix := modulePath + "/"
	fileSet := token.NewFileSet()
	seen := make(map[string]bool)
	for name, source := range files {
		file, err := parser.ParseFile(fileSet, name, source, parser.ImportsOnly)
		require.NoError(t, err, "parsing imports of %s", name)
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			require.NoError(t, err)
			if relPath, ok := strings.CutPrefix(importPath, prefix); ok {
				seen[path.Clean(relPath)] = true
			}
		}
	}

	imports := make([]string, 0, len(seen))
	for relPath := range seen {
		imports = append(imports, relPath)
	}
	slices.Sort(imports)
	return imports
}
