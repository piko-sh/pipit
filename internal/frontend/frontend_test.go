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

package frontend

import (
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
)

func TestNewTypesInfoAllocatesEveryConsultedMap(t *testing.T) {
	t.Parallel()

	info := NewTypesInfo()

	require.NotNil(t, info.Types, "a nil map makes the checker skip recording expression types")
	require.NotNil(t, info.Defs)
	require.NotNil(t, info.Uses)
	require.NotNil(t, info.Instances)
	require.NotNil(t, info.Selections)
}

func TestNewTypesConfigPinsTheInterpretedLanguageVersion(t *testing.T) {
	t.Parallel()

	t.Run("the size model and language version are pinned", func(t *testing.T) {
		t.Parallel()
		config := NewTypesConfig(nil)

		require.NotNil(t, config.Sizes, "the value layout must not vary with the host")
		require.NotEmpty(t, config.GoVersion)
		require.Nil(t, config.Importer, "without an importer every import is refused")
	})

	t.Run("an importer is carried through", func(t *testing.T) {
		t.Parallel()
		importer := types.Importer(nil)
		config := NewTypesConfig(importer)

		require.Nil(t, config.Importer, "a nil importer stays nil rather than being wrapped")
	})

	t.Run("the pinned size model lays out a pointer as eight bytes", func(t *testing.T) {
		t.Parallel()
		config := NewTypesConfig(nil)

		require.Equal(t, int64(8), config.Sizes.Sizeof(types.Typ[types.Int64]))
	})
}

func TestParseSourcesReadsFilesInSortedOrder(t *testing.T) {
	t.Parallel()

	t.Run("files are parsed by sorted filename", func(t *testing.T) {
		t.Parallel()
		fileSet := token.NewFileSet()
		sources := map[string]string{
			"zeta.go":  "package main\n",
			"alpha.go": "package main\n",
			"mid.go":   "package main\n",
		}

		files, err := ParseSources(fileSet, sources)

		require.NoError(t, err)
		require.Len(t, files, 3)
		require.Equal(t, "alpha.go", fileSet.Position(files[0].Package).Filename)
		require.Equal(t, "mid.go", fileSet.Position(files[1].Package).Filename)
		require.Equal(t, "zeta.go", fileSet.Position(files[2].Package).Filename,
			"a map's iteration order must not reach the recorded positions")
	})

	t.Run("the order is stable across repeated runs", func(t *testing.T) {
		t.Parallel()
		sources := map[string]string{
			"b.go": "package main\n",
			"a.go": "package main\n",
			"c.go": "package main\n",
			"d.go": "package main\n",
		}

		var firstOrder []string
		for range 5 {
			fileSet := token.NewFileSet()
			files, err := ParseSources(fileSet, sources)
			require.NoError(t, err)

			order := make([]string, 0, len(files))
			for _, file := range files {
				order = append(order, fileSet.Position(file.Package).Filename)
			}
			if firstOrder == nil {
				firstOrder = order
				continue
			}
			require.Equal(t, firstOrder, order, "parse order must be deterministic")
		}
	})

	t.Run("no sources parse to no files", func(t *testing.T) {
		t.Parallel()
		files, err := ParseSources(token.NewFileSet(), nil)

		require.NoError(t, err)
		require.Empty(t, files)
	})

	t.Run("a syntax error names the offending file", func(t *testing.T) {
		t.Parallel()
		_, err := ParseSources(token.NewFileSet(), map[string]string{"broken.go": "package main\nfunc ("})

		require.ErrorIs(t, err, fault.ErrParse)
		require.Contains(t, err.Error(), "broken.go")
	})
}

func TestTypecheckReturnsTheInformationTheCompilerNeeds(t *testing.T) {
	t.Parallel()

	t.Run("a well-formed package type-checks", func(t *testing.T) {
		t.Parallel()
		result, err := Typecheck("main", map[string]string{
			"main.go": "package main\n\nfunc add(a, b int) int { return a + b }\n",
		}, nil)

		require.NoError(t, err)
		require.NotNil(t, result.FileSet)
		require.NotNil(t, result.Info)
		require.NotNil(t, result.Package)
		require.Len(t, result.Files, 1)
		require.NotEmpty(t, result.Info.Defs, "definitions must be recorded for the compiler to read")
	})

	t.Run("several files check as one package", func(t *testing.T) {
		t.Parallel()
		result, err := Typecheck("main", map[string]string{
			"a.go": "package main\n\nfunc helper() int { return 1 }\n",
			"b.go": "package main\n\nfunc caller() int { return helper() }\n",
		}, nil)

		require.NoError(t, err)
		require.Len(t, result.Files, 2)
	})

	t.Run("expression types are recorded", func(t *testing.T) {
		t.Parallel()
		result, err := Typecheck("main", map[string]string{
			"main.go": "package main\n\nfunc f() int { return 1 + 2 }\n",
		}, nil)

		require.NoError(t, err)
		require.NotEmpty(t, result.Info.Types)
	})

	t.Run("a type error is reported rather than returned as a result", func(t *testing.T) {
		t.Parallel()
		_, err := Typecheck("main", map[string]string{
			"main.go": "package main\n\nfunc f() int { return \"not an int\" }\n",
		}, nil)

		require.ErrorIs(t, err, fault.ErrTypeCheck)
	})

	t.Run("a syntax error is reported before type checking", func(t *testing.T) {
		t.Parallel()
		_, err := Typecheck("main", map[string]string{"main.go": "package main\nfunc ("}, nil)

		require.ErrorIs(t, err, fault.ErrParse)
	})

	t.Run("no sources are refused", func(t *testing.T) {
		t.Parallel()
		_, err := Typecheck("main", nil, nil)

		require.ErrorIs(t, err, fault.ErrParse)
	})

	t.Run("an import with no importer is refused", func(t *testing.T) {
		t.Parallel()
		_, err := Typecheck("main", map[string]string{
			"main.go": "package main\n\nimport \"fmt\"\n\nfunc f() { fmt.Println() }\n",
		}, nil)

		require.ErrorIs(t, err, fault.ErrTypeCheck,
			"without an importer an import cannot resolve")
	})
}
