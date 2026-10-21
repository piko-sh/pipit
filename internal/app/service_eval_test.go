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

package app

import (
	"context"
	"errors"
	"fmt"
	"go/build"
	"maps"
	"reflect"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/symtab"
)

func TestCompileFileSetFollowsGoFileRules(t *testing.T) {
	t.Parallel()

	otherOS := "windows"
	if runtime.GOOS == otherOS {
		otherOS = "linux"
	}
	const (
		run    = "package main\n\nfunc Run() int { return helper() }\n"
		helper = "package main\n\nfunc helper() int { return 1 }\n"

		clash = "package main\n\nfunc helper() int { return 2 }\n"
	)
	tests := []struct {
		name    string
		extra   map[string]string
		wantErr string
	}{
		{name: "an excluded file need not parse", extra: map[string]string{"broken.go": "//go:build ignore\n\npackage main\n\nfunc {\n"}},
		{name: "a malformed //go:build line fails", extra: map[string]string{"bad.go": "//go:build linux &&\n\npackage main\n"}, wantErr: "bad.go: parsing //go:build line"},
		{name: "//go:build beats +build", extra: map[string]string{"helper.go": "//go:build interp\n// +build ignore\n\n" + helper}},
		{name: "a suffix before a second dot", extra: map[string]string{"helper_" + otherOS + ".pb.go": clash}},
		{name: "underscore and dot files", extra: map[string]string{"_helper.go": clash, ".helper.go": clash}},
		{name: "package documentation", extra: map[string]string{"doc.go": "// Package main runs.\npackage documentation\n"}},
		{name: "a cgo file", extra: map[string]string{"cgo.go": "package main\n\nimport \"C\"\n\nfunc helper() int { return 2 }\n"}},
		{name: "cgo is off", extra: map[string]string{"cgo_on.go": "//go:build cgo\n\n" + clash}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sources := map[string]string{"main.go": run, "helper.go": helper}
			maps.Copy(sources, tt.extra)
			service := NewService()
			cfs, err := service.CompileFileSet(t.Context(), sources)
			if tt.wantErr != "" {
				require.ErrorIs(t, err, fault.ErrParse)
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			result, err := service.ExecuteEntrypoint(t.Context(), cfs, "Run")
			require.NoError(t, err)
			require.Equal(t, "1", fmt.Sprint(result))
		})
	}
}

func TestCompileFileSetHonoursToolTags(t *testing.T) {
	t.Parallel()

	if len(build.Default.ToolTags) == 0 {
		t.Skip("the toolchain sets no tool tags")
	}
	sources := map[string]string{
		"main.go":  "package main\n\nfunc Run() int { return helper() }\n",
		"tool.go":  "//go:build " + build.Default.ToolTags[0] + "\n\npackage main\n\nfunc helper() int { return 1 }\n",
		"other.go": "//go:build !" + build.Default.ToolTags[0] + "\n\npackage main\n\nfunc helper() int { return 2 }\n",
	}
	service := NewService()
	cfs, err := service.CompileFileSet(t.Context(), sources)
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(t.Context(), cfs, "Run")
	require.NoError(t, err)
	require.Equal(t, "1", fmt.Sprint(result))
}

func TestServiceMatchFile(t *testing.T) {
	t.Parallel()

	service := NewService(WithBuildTags("pro"))
	tests := []struct {
		name    string
		file    string
		src     string
		want    bool
		wantErr bool
	}{
		{name: "the interp tag", file: "a.go", src: "//go:build interp\n\npackage p\n", want: true},
		{name: "a tag from WithBuildTags", file: "a.go", src: "//go:build pro\n\npackage p\n", want: true},
		{name: "a tag not set", file: "a.go", src: "//go:build enterprise\n\npackage p\n", want: false},
		{name: "only the base name counts", file: "x_windows/a.go", src: "package p\n", want: true},
		{name: "a malformed constraint", file: "a.go", src: "//go:build (\n\npackage p\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			matched, err := service.MatchFile(tt.file, []byte(tt.src))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, matched)
		})
	}
}

func TestParseSelectedFilesCapsTheFilesReported(t *testing.T) {
	t.Parallel()

	sources := make(map[string]string, maxTypeErrorsPerPackage+2)
	for index := range maxTypeErrorsPerPackage + 2 {
		sources[fmt.Sprintf("f%02d.go", index)] = "package main\n\nfunc (\n"
	}
	_, err := NewService().parseSelectedFiles(t.Context(), sources)
	require.ErrorIs(t, err, errTooManyFileErrors)
	require.Contains(t, err.Error(), "f00.go:")
	require.NotContains(t, err.Error(), "f11.go:")
	require.Contains(t, err.Error(), "(2 more not shown)")
}

func TestParseSelectedFilesStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancelCause(t.Context())
	cause := errors.New("shutting down")
	cancel(cause)

	_, err := NewService().parseSelectedFiles(ctx, map[string]string{"main.go": "package main\n"})
	require.ErrorIs(t, err, fault.ErrExecutionCancelled)
	require.ErrorIs(t, err, cause)
}

const garbageInInitSource = `package main

import "runtime"

var kept []byte

func init() {
	const chunks = 400
	const megabyte = 1 << 20
	buffer := make([]byte, megabyte)
	for i := range buffer {
		buffer[i] = byte(i%10 + '0')
	}
	text := string(buffer)

	stats := new(runtime.MemStats)
	runtime.ReadMemStats(stats)
	before := stats.Sys
	for range chunks {
		kept = []byte(text)
	}
	runtime.ReadMemStats(stats)
	if grown := stats.Sys - before; grown >= 256*megabyte {
		println("an init function's garbage grew the process by", grown>>20, "MB")
		panic("init garbage was not collected")
	}
}

func main() {}
`

func TestAnInitFunctionsGarbageIsCollectedAsItRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("makes 400 MB of garbage")
	}
	service := NewService()
	service.UseSymbols(symtab.NewSymbolRegistry(symtab.SymbolExports{"runtime": {
		"MemStats":     reflect.ValueOf((*runtime.MemStats)(nil)),
		"ReadMemStats": reflect.ValueOf(runtime.ReadMemStats),
	}}))
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": garbageInInitSource})
	require.NoError(t, err)

	_, err = service.ExecuteEntrypoint(t.Context(), compiled, "main")
	require.NoError(t, err, "the init function's arena collects while it runs instead of growing")
}
