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

package buildfilter

import (
	"bytes"
	"go/build"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type platform struct {
	goos   string
	goarch string
}

var (
	linuxAMD64   = platform{goos: "linux", goarch: "amd64"}
	linuxARM64   = platform{goos: "linux", goarch: "arm64"}
	androidARM64 = platform{goos: "android", goarch: "arm64"}
	darwinARM64  = platform{goos: "darwin", goarch: "arm64"}
	iosARM64     = platform{goos: "ios", goarch: "arm64"}
	illumosAMD64 = platform{goos: "illumos", goarch: "amd64"}
	windowsAMD64 = platform{goos: "windows", goarch: "amd64"}
	jsWasm       = platform{goos: "js", goarch: "wasm"}
	wasip1Wasm   = platform{goos: "wasip1", goarch: "wasm"}
	plan9AMD64   = platform{goos: "plan9", goarch: "amd64"}
)

const plain = "package p\n"

type matchCase struct {
	name     string
	file     string
	src      string
	platform platform
	tags     []string
	want     bool
	wantErr  string
}

type virtualFile struct {
	name string
	size int64
}

func (f virtualFile) Name() string       { return f.name }
func (f virtualFile) Size() int64        { return f.size }
func (f virtualFile) Mode() fs.FileMode  { return 0o644 }
func (f virtualFile) ModTime() time.Time { return time.Time{} }
func (f virtualFile) IsDir() bool        { return false }
func (f virtualFile) Sys() any           { return nil }

var matchCases = []matchCase{
	{name: "an unconstrained file", file: "a.go", src: plain, platform: linuxAMD64, want: true},
	{name: "a matching OS suffix", file: "a_linux.go", src: plain, platform: linuxAMD64, want: true},
	{name: "another OS suffix", file: "a_windows.go", src: plain, platform: linuxAMD64, want: false},
	{name: "an OS suffix on its OS", file: "a_windows.go", src: plain, platform: windowsAMD64, want: true},
	{name: "a matching architecture suffix", file: "a_amd64.go", src: plain, platform: linuxAMD64, want: true},
	{name: "another architecture suffix", file: "a_amd64.go", src: plain, platform: linuxARM64, want: false},
	{name: "an OS and architecture suffix", file: "a_linux_amd64.go", src: plain, platform: linuxAMD64, want: true},
	{name: "an OS and other architecture suffix", file: "a_linux_amd64.go", src: plain, platform: linuxARM64, want: false},
	{name: "a suffix before a second dot", file: "a_windows.pb.go", src: plain, platform: linuxAMD64, want: false},
	{name: "a suffix before a second dot on its OS", file: "a_windows.pb.go", src: plain, platform: windowsAMD64, want: true},
	{name: "a test file", file: "a_test.go", src: plain, platform: linuxAMD64, want: true},
	{name: "a test file with another OS suffix", file: "a_windows_test.go", src: plain, platform: linuxAMD64, want: false},
	{name: "an unknown suffix", file: "a_foo.go", src: plain, platform: linuxAMD64, want: true},
	{name: "unix is not a file suffix", file: "a_unix.go", src: plain, platform: windowsAMD64, want: true},
	{name: "an underscore prefix", file: "_a.go", src: plain, platform: linuxAMD64, want: false},
	{name: "a dot prefix", file: ".a.go", src: plain, platform: linuxAMD64, want: false},
	{name: "only the base name counts", file: "x_windows/a.go", src: plain, platform: linuxAMD64, want: true},
	{name: "a suffix in a path", file: "dir/a_windows.go", src: plain, platform: linuxAMD64, want: false},
	{name: "android counts as linux", file: "a_linux.go", src: plain, platform: androidARM64, want: true},
	{name: "linux does not count as android", file: "a_android.go", src: plain, platform: linuxARM64, want: false},
	{name: "ios counts as darwin", file: "a_darwin.go", src: plain, platform: iosARM64, want: true},
	{name: "darwin does not count as ios", file: "a_ios.go", src: plain, platform: darwinARM64, want: false},
	{name: "illumos counts as solaris", file: "a_solaris.go", src: plain, platform: illumosAMD64, want: true},
	{name: "js and wasm suffixes", file: "a_js_wasm.go", src: plain, platform: jsWasm, want: true},
	{name: "a wasip1 suffix", file: "a_wasip1.go", src: plain, platform: wasip1Wasm, want: true},
	{name: "a plan9 suffix", file: "a_plan9.go", src: plain, platform: plan9AMD64, want: true},
	{name: "the interp tag", file: "a.go", src: constrained("interp"), platform: linuxAMD64, want: true},
	{name: "not interp", file: "a.go", src: constrained("!interp"), platform: linuxAMD64, want: false},
	{name: "cgo is off", file: "a.go", src: constrained("cgo"), platform: linuxAMD64, want: false},
	{name: "not cgo", file: "a.go", src: constrained("!cgo"), platform: linuxAMD64, want: true},
	{name: "the gc compiler", file: "a.go", src: constrained("gc"), platform: linuxAMD64, want: true},
	{name: "not gccgo", file: "a.go", src: constrained("gccgo"), platform: linuxAMD64, want: false},
	{name: "a released Go version", file: "a.go", src: constrained("go1.1"), platform: linuxAMD64, want: true},
	{name: "a future Go version", file: "a.go", src: constrained("go1.999"), platform: linuxAMD64, want: false},
	{name: "unix on linux", file: "a.go", src: constrained("unix"), platform: linuxAMD64, want: true},
	{name: "unix on windows", file: "a.go", src: constrained("unix"), platform: windowsAMD64, want: false},
	{name: "unix on js", file: "a.go", src: constrained("unix"), platform: jsWasm, want: false},
	{name: "an OS expression", file: "a.go", src: constrained("linux && amd64"), platform: linuxAMD64, want: true},
	{name: "an OS expression for another OS", file: "a.go", src: constrained("linux && amd64"), platform: linuxARM64, want: false},
	{name: "an old +build line", file: "a.go", src: "// +build windows\n\npackage p\n", platform: linuxAMD64, want: false},
	{name: "//go:build beats +build", file: "a.go", src: "//go:build linux\n// +build windows\n\npackage p\n", platform: linuxAMD64, want: true},
	{name: "a constraint after the package clause", file: "a.go", src: "package p\n\n//go:build windows\n", platform: linuxAMD64, want: true},
	{name: "a constraint right above the package clause", file: "a.go", src: "//go:build windows\npackage p\n", platform: linuxAMD64, want: false},
	{name: "a constraint after a block comment", file: "a.go", src: "/* licence */\n\n//go:build windows\n\npackage p\n", platform: linuxAMD64, want: false},
	{name: "a constraint after line comments", file: "a.go", src: "// licence\n\n//go:build windows\n\npackage p\n", platform: linuxAMD64, want: false},
	{name: "a user tag", file: "a.go", src: constrained("pro"), platform: linuxAMD64, tags: []string{"pro"}, want: true},
	{name: "a user tag not set", file: "a.go", src: constrained("pro"), platform: linuxAMD64, want: false},
	{name: "a cgo file", file: "a.go", src: "package p\n\nimport \"C\"\n", platform: linuxAMD64, want: false},
	{name: "a cgo file in a group", file: "a.go", src: "package p\n\nimport (\n\t\"fmt\"\n\t\"C\"\n)\n", platform: linuxAMD64, want: false},
	{name: "package documentation", file: "doc.go", src: "// Package p does things.\npackage documentation\n", platform: linuxAMD64, want: false},
	{name: "an external test package", file: "a_test.go", src: "package p_test\n", platform: linuxAMD64, want: true},
	{name: "a malformed //go:build line", file: "a.go", src: constrained("linux &&"), platform: linuxAMD64, wantErr: "a.go: parsing //go:build line"},
	{name: "two //go:build lines", file: "a.go", src: "//go:build linux\n//go:build amd64\n\npackage p\n", platform: linuxAMD64, wantErr: "a.go: multiple //go:build comments"},
	{name: "a NUL byte", file: "a.go", src: "package p\x00\n", platform: linuxAMD64, wantErr: "read a.go: unexpected NUL in input"},
	{name: "cgo in a test file", file: "a_test.go", src: "package p\n\nimport \"C\"\n", platform: linuxAMD64, wantErr: "use of cgo in test a_test.go not supported"},
	{name: "a header that does not parse is kept", file: "a.go", src: "package p\n\nimport (\n", platform: linuxAMD64, want: true},
	{name: "a label has no name rules", file: "snippet_windows", src: plain, platform: linuxAMD64, want: true},
	{name: "a label with a constraint", file: "<eval>", src: constrained("!interp"), platform: linuxAMD64, want: false},
	{name: "a label that is not a file", file: "<eval>", src: "x := 1\nx + 1", platform: linuxAMD64, want: true},
	{name: "a label with a malformed constraint", file: "<eval>", src: constrained("("), platform: linuxAMD64, wantErr: "<eval>: parsing //go:build line"},
	{name: "a label with a NUL byte", file: "<eval>", src: "package p\x00\n", platform: linuxAMD64, wantErr: "read <eval>: unexpected NUL in input"},
}

func TestMatch(t *testing.T) {
	t.Parallel()

	for _, tt := range matchCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			matched, err := newFor(tt.platform.goos, tt.platform.goarch, tt.tags).MatchString(tt.file, tt.src)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.NotContains(t, err.Error(), labelFileName, "the error names the source, not the stand-in file")
				require.False(t, matched)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, matched)
		})
	}
}

func TestMatchAgreesWithImportDir(t *testing.T) {
	t.Parallel()

	for _, tt := range matchCases {
		if tt.wantErr != "" || !strings.HasSuffix(tt.file, ".go") || !headerParses(tt.src) {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			matcher := newFor(tt.platform.goos, tt.platform.goarch, tt.tags)
			matched, err := matcher.MatchString(tt.file, tt.src)
			require.NoError(t, err)
			require.Equal(t, importDirIncludes(t, matcher.settings, path.Base(tt.file), tt.src), matched)
		})
	}
}

func TestNewSetsTheInterpTag(t *testing.T) {
	t.Parallel()

	matcher := New([]string{"extra"})
	require.Equal(t, []string{InterpTag, "extra"}, matcher.settings.BuildTags)
	require.False(t, matcher.settings.CgoEnabled)
	matched, err := matcher.Match("a.go", []byte(constrained("interp && extra")))
	require.NoError(t, err)
	require.True(t, matched)
}

func headerParses(src string) bool {
	_, err := parser.ParseFile(token.NewFileSet(), "a.go", src, parser.ImportsOnly)
	return err == nil
}

func importDirIncludes(t *testing.T, settings build.Context, name, src string) bool {
	t.Helper()

	settings.IsDir = func(string) bool { return true }
	settings.HasSubdir = func(string, string) (string, bool) { return "", false }
	settings.ReadDir = func(string) ([]fs.FileInfo, error) {
		return []fs.FileInfo{virtualFile{name: name, size: int64(len(src))}}, nil
	}
	settings.OpenFile = func(string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader([]byte(src))), nil
	}
	pkg, err := settings.ImportDir("/virtual", 0)
	if pkg == nil {
		require.NoError(t, err)
	}
	return slices.Contains(pkg.GoFiles, name) || slices.Contains(pkg.TestGoFiles, name) || slices.Contains(pkg.XTestGoFiles, name)
}

func constrained(expression string) string {
	return "//go:build " + expression + "\n\npackage p\n"
}
