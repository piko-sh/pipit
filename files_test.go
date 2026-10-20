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

package pipit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
)

func TestNewFileFilter(t *testing.T) {
	t.Parallel()

	filter := pipit.NewFileFilter("pro")
	tests := []struct {
		name    string
		file    string
		src     string
		want    bool
		wantErr string
	}{
		{name: "an ordinary file", file: "a.go", src: "package p\n", want: true},
		{name: "the interp tag", file: "a.go", src: "//go:build " + pipit.BuildTag + "\n\npackage p\n", want: true},
		{name: "not interp", file: "a.go", src: "//go:build !" + pipit.BuildTag + "\n\npackage p\n", want: false},
		{name: "a tag passed in", file: "a.go", src: "//go:build pro\n\npackage p\n", want: true},
		{name: "cgo is off", file: "a.go", src: "//go:build cgo\n\npackage p\n", want: false},
		{name: "a file that imports C", file: "a.go", src: "package p\n\nimport \"C\"\n", want: false},
		{name: "an underscore prefix", file: "_a.go", src: "package p\n", want: false},
		{name: "a malformed constraint", file: "a.go", src: "//go:build (\n\npackage p\n", wantErr: "a.go: parsing //go:build line"},
		{name: "a NUL byte", file: "a.go", src: "package p\x00\n", wantErr: "unexpected NUL in input"},
		{name: "cgo in a test file", file: "a_test.go", src: "package p\n\nimport \"C\"\n", wantErr: "use of cgo in test a_test.go not supported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			matched, err := filter(tt.file, []byte(tt.src))
			if tt.wantErr != "" {
				require.ErrorIs(t, err, pipit.ErrParse)
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, matched)
		})
	}
}

func TestInterpreterMatchFileUsesItsBuildTags(t *testing.T) {
	t.Parallel()

	src := []byte("//go:build pro\n\npackage p\n")
	matched, err := pipit.NewInterpreter(pipit.WithBuildTags("pro")).MatchFile("a.go", src)
	require.NoError(t, err)
	require.True(t, matched)
	matched, err = pipit.NewInterpreter().MatchFile("a.go", src)
	require.NoError(t, err)
	require.False(t, matched)

	_, err = pipit.NewInterpreter().MatchFile("a.go", []byte("//go:build (\n\npackage p\n"))
	require.ErrorIs(t, err, pipit.ErrParse)
}
