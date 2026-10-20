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

package app_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/fault"
)

func TestCompileFileSetRejectsGoEmbed(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"group": "package main\n\nimport \"embed\"\n\n//go:embed hello.txt\nvar (\n\tf embed.FS\n)\n\nfunc main() {}\n",
		"spec":  "package main\n\nimport \"embed\"\n\nvar (\n\t//go:embed hello.txt\n\tf embed.FS\n)\n\nfunc main() {}\n",
		"plain": "package main\n\n//go:embed hello.txt\nvar data string\n\nfunc main() {}\n",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			_, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
			require.ErrorIs(t, err, fault.ErrCompileEmbedUnsupported)
			require.ErrorIs(t, err, fault.ErrCompilation)
			require.NotContains(t, err.Error(), "PIPIT_SCRIPT_DIR", "the library does not know how its host finds files")
			require.Contains(t, err.Error(), "//go:build !interp")
			require.Contains(t, err.Error(), "working directory")
			require.Contains(t, err.Error(), "main.go:")
		})
	}
	t.Run("mention", func(t *testing.T) {
		t.Parallel()
		service := app.NewService()
		source := "package main\n\n// The go:embed directive is not used here.\nvar data = \"x\"\n\nfunc main() {}\n"
		_, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
		require.NoError(t, err)
	})
}

const (
	embedNativeSource = "//go:build !interp\n\npackage site\n\nimport _ \"embed\"\n\n" +
		"//go:embed hello.txt\nvar content string\n"
	embedInterpSource = "//go:build interp\n\npackage site\n\nvar content = \"read at run time\"\n"
	embedReaderSource = "package site\n\nfunc Hello() string { return content }\n"
)

func TestCompileFileSetSkipsGoEmbedBehindNotInterp(t *testing.T) {
	t.Parallel()
	service := app.NewService()
	ctx := t.Context()
	compiled, err := service.CompileFileSet(ctx, map[string]string{
		"content_native.go": strings.Replace(embedNativeSource, "package site", "package main", 1),
		"content_interp.go": strings.Replace(embedInterpSource, "package site", "package main", 1),
		"main.go": strings.Replace(embedReaderSource, "package site", "package main", 1) +
			"\nfunc main() {}\n",
	})
	require.NoError(t, err)
	require.NoError(t, service.ExecuteInits(ctx, compiled))
	result, err := service.ExecuteEntrypoint(ctx, compiled, "Hello")
	require.NoError(t, err)
	require.Equal(t, "read at run time", result)
}

func TestCompileProgramSkipsGoEmbedBehindNotInterp(t *testing.T) {
	t.Parallel()
	service := app.NewService()
	ctx := t.Context()
	compiled, err := service.CompileProgram(ctx, "example.com/m", map[string]map[string]string{
		"site": {
			"content_native.go": embedNativeSource,
			"content_interp.go": embedInterpSource,
			"reader.go":         embedReaderSource,
		},
		"": {"main.go": "package main\n\nimport \"example.com/m/site\"\n\nfunc Result() string { return site.Hello() }\n\nfunc main() {}\n"},
	})
	require.NoError(t, err)
	require.NoError(t, service.ExecuteInits(ctx, compiled))
	result, err := service.ExecuteEntrypoint(ctx, compiled, "Result")
	require.NoError(t, err)
	require.Equal(t, "read at run time", result)
}

func TestGoEmbedErrorListsEveryDirectiveAndHint(t *testing.T) {
	t.Parallel()
	service := app.NewService()
	_, err := service.CompileProgram(t.Context(), "example.com/site", map[string]map[string]string{
		"": {
			"a.go": "package main\n\n//go:embed a.txt\nvar a string\n\nfunc main() {}\n",
			"b.go": "package main\n\n//go:embed b.txt\nvar b string\n",
		},
	})
	require.ErrorIs(t, err, fault.ErrCompileEmbedUnsupported)
	firstLine, hint, found := strings.Cut(err.Error(), "\n\n")
	require.True(t, found, "a blank line separates the hint: %q", err.Error())
	require.Equal(t, "compiling program: compilation failed: example.com/site: //go:embed is not supported at a.go:3:1, b.go:3:1", firstLine)
	require.Contains(t, hint, "//go:build !interp")
}

func TestEvalAndSessionsRejectGoEmbed(t *testing.T) {
	t.Parallel()

	const snippet = "import \"embed\"\n\n//go:embed x.txt\nvar files embed.FS\n\nfiles\n"
	_, err := app.NewService().Eval(t.Context(), snippet)
	require.ErrorIs(t, err, fault.ErrCompileEmbedUnsupported)

	_, err = app.NewService().NewSession().Submit(t.Context(), snippet)
	require.ErrorIs(t, err, fault.ErrCompileEmbedUnsupported)
}

func TestEvalAndCompileRejectGoEmbedInStatements(t *testing.T) {
	t.Parallel()

	const statements = "//go:embed x.txt\nvar content string\n_ = content\n"
	_, err := app.NewService().Eval(t.Context(), statements)
	require.ErrorIs(t, err, fault.ErrCompileEmbedUnsupported)
	require.ErrorContains(t, err, "at eval.go:1:1")

	_, err = app.NewService().Compile(t.Context(), statements)
	require.ErrorIs(t, err, fault.ErrCompileEmbedUnsupported)
}

func TestEvalIgnoresGoEmbedOutsideComments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   any
	}{
		{name: "in a string", source: "\"//go:embed x.txt\"", want: "//go:embed x.txt"},
		{name: "in a block comment", source: "/* //go:embed x.txt */ 1", want: 1},
		{name: "a longer directive name", source: "//go:embedded x.txt\n2", want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := app.NewService().Eval(t.Context(), tt.source)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}
