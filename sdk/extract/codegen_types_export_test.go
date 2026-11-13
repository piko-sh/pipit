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

package extract

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

func TestLoadManifestTypesExportVariants(t *testing.T) {
	t.Parallel()

	tmpFile := t.TempDir() + "/variants.yaml"
	require.NoError(t, writeTestFile(tmpFile, `
package: test
output: out
types_export_variants:
  - safe
  - other_tag
packages:
  - fmt
`))
	manifest, err := LoadManifest(tmpFile)
	require.NoError(t, err)
	require.Equal(t, []string{"safe", "other_tag"}, manifest.TypesExportVariants)

	badFile := t.TempDir() + "/bad.yaml"
	require.NoError(t, writeTestFile(badFile, `
package: test
output: out
types_export_variants:
  - "safe || x"
packages:
  - fmt
`))
	_, err = LoadManifest(badFile)
	require.ErrorContains(t, err, "is not a build tag")
	require.ErrorIs(t, err, errManifestVariantNotBuildTag)
}

func TestVariantPackageErrorsReportsEveryError(t *testing.T) {
	t.Parallel()

	require.NoError(t, variantPackageErrors(&packages.Package{PkgPath: "example.com/clean"}, "safe"))

	broken := &packages.Package{PkgPath: "example.com/broken", Errors: []packages.Error{
		{Pos: "a.go:1:1", Msg: "undefined: first", Kind: packages.TypeError},
		{Pos: "b.go:2:2", Msg: "undefined: second", Kind: packages.TypeError},
	}}
	err := variantPackageErrors(broken, "safe")

	require.ErrorIs(t, err, errTypesExportVariantPackage)
	require.ErrorContains(t, err, "example.com/broken with -tags=safe")
	require.ErrorContains(t, err, "undefined: first")
	require.ErrorContains(t, err, "undefined: second")
}

func TestTypesExportManifestConstraintSelectsOneManifestPerBuild(t *testing.T) {
	t.Parallel()

	variants := []string{"safe", "other"}
	require.Equal(t, "//go:build (!js || !wasm) && !safe && !other", typesExportManifestConstraint(variants, ""))
	require.Equal(t, "//go:build (!js || !wasm) && safe", typesExportManifestConstraint(variants, "safe"))
	require.Equal(t, "//go:build (!js || !wasm) && !safe && other", typesExportManifestConstraint(variants, "other"))
	require.Equal(t, nativeTypesLoaderBuildTag, typesExportManifestConstraint(nil, ""), "a manifest without variants is generated as before")
	require.Equal(t, "gen_types_export/safe/net_http.bin.gz", typesExportVariantBlobFileName("safe", "net/http"))
	require.Equal(t, "gen_types_manifest_safe.go", typesExportVariantManifestFileName("safe"))
}

func TestTypesExportVariantBlobsMatchTheDefaultBuildWhereTheTagChangesNothing(t *testing.T) {
	t.Parallel()

	paths := []string{"pipit.sh/pipit/internal/compile/astpattern", "pipit.sh/pipit/internal/compile/scope",
		"pipit.sh/pipit/internal/hostmem", "pipit.sh/pipit/internal/symtab/typemodel"}
	extracted, err := Extract(t.Context(), paths, nil)
	if err != nil {
		t.Skipf("pipit's packages do not load outside the repository's workspace: %v", err)
	}
	defaults, err := GenerateTypesExportBlobs(extracted)
	require.NoError(t, err)
	want := make(map[string][]byte, len(defaults))
	for _, blob := range defaults {
		want[blob.ImportPath] = blob.Data
	}

	ec := emitContext{manifest: &Manifest{Package: "test", TypesExportVariants: []string{"pipit_tag_no_file_names"}}}
	for round := range 3 {
		files, err := typesExportVariantFiles(t.Context(), extracted, "pipit_tag_no_file_names", ec)
		require.NoError(t, err)
		compared := 0
		for _, file := range files {
			for importPath, data := range want {
				if file.name == typesExportVariantBlobFileName("pipit_tag_no_file_names", importPath) {
					require.Equalf(t, data, file.data, "round %d: the variant blob of %s differs from the default build's", round, importPath)
					compared++
				}
			}
		}
		require.Equal(t, len(want), compared, "round %d: every package has a variant blob", round)
	}
}
