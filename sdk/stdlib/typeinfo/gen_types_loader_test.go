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

//go:build !js || !wasm

package typeinfo

import (
	"encoding/json"
	"go/types"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	typesExportDirectory = "gen_types_export"
)

func readDescriptorPaths(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("gen_types_descriptor.json")
	require.NoError(t, err)
	var descriptor struct {
		ImportPaths []string `json:"import_paths"`
	}
	require.NoError(t, json.Unmarshal(data, &descriptor))
	return descriptor.ImportPaths
}

func TestTypesPackagesNonEmptyAndComplete(t *testing.T) {
	t.Parallel()
	packages := TypesPackages()
	require.NotEmpty(t, packages)

	httpPackage, ok := packages["net/http"]
	require.True(t, ok, "net/http must be embedded")
	require.True(t, httpPackage.Complete())
	require.Equal(t, "net/http", httpPackage.Path())

	handler := httpPackage.Scope().Lookup("Handler")
	require.NotNil(t, handler)
	iface, isInterface := handler.Type().Underlying().(*types.Interface)
	require.True(t, isInterface)
	require.Equal(t, 1, iface.NumMethods())
	require.Equal(t, "ServeHTTP", iface.Method(0).Name())

	for path, pkg := range packages {
		require.True(t, pkg.Complete(), "%s must decode as a complete package", path)
		require.Equal(t, path, pkg.Path())
	}
}

func TestTypesPackagesShareDependencyInstances(t *testing.T) {
	t.Parallel()
	packages := TypesPackages()
	ioPackage := packages["io"]
	require.NotNil(t, ioPackage)

	bufioPackage := packages["bufio"]
	require.NotNil(t, bufioPackage)
	newReader := bufioPackage.Scope().Lookup("NewReader")
	require.NotNil(t, newReader)
	signature, ok := newReader.Type().(*types.Signature)
	require.True(t, ok)
	require.Equal(t, 1, signature.Params().Len())
	named, ok := signature.Params().At(0).Type().(*types.Named)
	require.True(t, ok)
	require.Same(t, ioPackage, named.Obj().Pkg(), "bufio must reference the embedded io package instance")
}

func TestTypesPackagesLoadErrorNil(t *testing.T) {
	t.Parallel()
	require.NoError(t, TypesPackagesLoadError())
}

func TestTypesExportManifestMatchesDescriptor(t *testing.T) {
	t.Parallel()
	descriptorPaths := readDescriptorPaths(t)

	manifestPaths := make([]string, 0, len(typesExportManifest))
	for _, entry := range typesExportManifest {
		manifestPaths = append(manifestPaths, entry.Path)
		require.Equal(t, typesExportDirectory+"/"+strings.ReplaceAll(entry.Path, "/", "_")+".bin.gz", entry.File)
		for _, dependency := range entry.Dependencies {
			require.Contains(t, descriptorPaths, dependency, "%s depends on %s which is not in the set", entry.Path, dependency)
		}
	}
	slices.Sort(descriptorPaths)
	require.Equal(t, descriptorPaths, manifestPaths)

	embedded, err := fs.ReadDir(typesExportFS, typesExportDirectory)
	require.NoError(t, err)
	embeddedFiles := make([]string, 0, len(embedded))
	for _, entry := range embedded {
		embeddedFiles = append(embeddedFiles, typesExportDirectory+"/"+entry.Name())
	}
	manifestFiles := make([]string, 0, len(typesExportManifest))
	for _, entry := range typesExportManifest {
		manifestFiles = append(manifestFiles, entry.File)
	}
	slices.Sort(embeddedFiles)
	slices.Sort(manifestFiles)
	require.Equal(t, manifestFiles, embeddedFiles, "every embedded blob must be listed and vice versa")

	require.True(t, strings.HasPrefix(typesExportGoVersion, "go"), typesExportGoVersion)
	require.Equal(t, len(TypesPackages()), len(typesExportManifest))
}
