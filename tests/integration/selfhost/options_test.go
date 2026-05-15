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

package selfhost_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSplitList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "empty", raw: "", want: nil},
		{name: "one", raw: "0957", want: []string{"0957"}},
		{name: "several with spaces", raw: "0957, 1634 ,,0958", want: []string{"0957", "1634", "0958"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, splitList(tc.raw))
		})
	}
}

func TestMatchesFilter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		program   string
		fragments []string
		want      bool
	}{
		{name: "no filter admits everything", program: "0001_hello", fragments: nil, want: true},
		{name: "number prefix", program: "0957_destructured_slice_elem", fragments: []string{"0957"}, want: true},
		{name: "word", program: "1634_fmt_type_verbs", fragments: []string{"type_verbs"}, want: true},
		{name: "second fragment", program: "0958_destructured_elem_edges", fragments: []string{"0957", "0958"}, want: true},
		{name: "no match", program: "0001_hello", fragments: []string{"0957"}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, matchesFilter(tc.program, tc.fragments))
		})
	}
}

func TestReportFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "default", args: nil, want: reportPath},
		{name: "separate value", args: []string{"-filter", "0957", "-report", "/tmp/r.md"}, want: "/tmp/r.md"},
		{name: "joined value", args: []string{"-report=/tmp/r.md"}, want: "/tmp/r.md"},
		{name: "dangling flag keeps the default", args: []string{"-report"}, want: reportPath},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, reportFor(tc.args))
		})
	}
}

func TestInnermostCompilerFunction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		stack string
		want  string
	}{
		{name: "empty", stack: "", want: ""},
		{name: "method frame", stack: "pipit.sh/pipit/internal/compile.(*Compiler).tryShortVarDeclLowerings(...)\n\tcompiler_lowerings.go:140 +0x8\n", want: "tryShortVarDeclLowerings"},
		{name: "function frame after a driver frame", stack: "main.CompilePackage(...)\n\tdriver.go:12\npipit.sh/pipit/internal/compile.CompileParsedPackage(...)\n\tcompiler.go:1\n", want: "CompileParsedPackage"},
		{name: "no compiler frame", stack: "main.CompilePackage(...)\n\tdriver.go:12\n", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, innermostCompilerFunction(tc.stack))
		})
	}
}

func TestParseOptionsDefaults(t *testing.T) {
	t.Parallel()
	var stderr discardWriter
	opts, err := parseOptions([]string{"-filter", "0957,0958", "-no-gc", "-no-arena-promotion", "-limit", "3"}, stderr)
	require.NoError(t, err)
	require.Equal(t, "0957,0958", opts.filter)
	require.True(t, opts.noGC)
	require.True(t, opts.noArenaPromotion)
	require.False(t, opts.noPasses)
	require.True(t, opts.goDispatch)
	require.Equal(t, 3, opts.limit)
	require.Equal(t, reportPath, opts.report)

	_, err = parseOptions([]string{"-no-such-flag"}, stderr)
	require.Error(t, err)
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
