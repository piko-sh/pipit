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
	"fmt"
	"go/build"
	"go/parser"
	"go/token"
	"io"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

const (
	// InterpTag is the build tag that identifies interpreted source files.
	InterpTag = "interp"

	// labelFileName represents sources without a .go filename, whose build constraints alone
	// determine selection.
	labelFileName = "label.go"

	// documentationPackage is the package name go/build ignores, used for files that only
	// hold package documentation.
	documentationPackage = "documentation"

	// cgoImportPath is the import that marks a file as using cgo.
	cgoImportPath = "C"

	// gcCompiler names the compiler whose build tag the interpreter sets.
	gcCompiler = "gc"
)

// Matcher decides which files the interpreter compiles. It is safe for concurrent use.
type Matcher struct {
	// settings are the build settings; each match works on a copy.
	settings build.Context
}

// New returns a Matcher for the host's GOOS and GOARCH.
//
// Takes tags ([]string) which are build tags to set besides InterpTag.
//
// Returns *Matcher which is ready for use.
func New(tags []string) *Matcher {
	return newFor(runtime.GOOS, runtime.GOARCH, tags)
}

// newFor returns a Matcher for goos and goarch.
//
// Takes goos (string) which is the target operating system.
// Takes goarch (string) which is the target architecture.
// Takes tags ([]string) which are build tags to set besides InterpTag.
//
// Returns *Matcher which is ready for use.
func newFor(goos, goarch string, tags []string) *Matcher {
	settings := build.Default
	settings.GOOS = goos
	settings.GOARCH = goarch
	settings.Compiler = gcCompiler
	settings.CgoEnabled = false
	settings.UseAllFiles = false
	settings.BuildTags = append([]string{InterpTag}, tags...)
	settings.ToolTags = slices.Clone(build.Default.ToolTags)
	settings.ReleaseTags = slices.Clone(build.Default.ReleaseTags)
	return &Matcher{settings: settings}
}

// Match reports whether the interpreter compiles the file name with content src.
//
// Only the base name counts. A name that does not end in .go labels a source, such as an
// evaluated snippet, and only its build constraints apply. A file that passes the name
// and constraint rules is still left out when it is package documentation or uses cgo, as
// go/build.Import does; a header that does not parse is kept, so the compiler can report
// it.
//
// Takes name (string) which is the file's name or label.
// Takes src ([]byte) which is the file's content.
//
// Returns bool which is true when the file is compiled.
// Returns error which names the file when its build constraints are malformed, its header
// cannot be read (for example it holds a NUL byte), or a test file uses cgo.
func (m *Matcher) Match(name string, src []byte) (bool, error) {
	base := path.Base(filepath.ToSlash(name))
	matchName := base
	if !strings.HasSuffix(base, ".go") {
		matchName = labelFileName
	}
	settings := m.settings
	settings.OpenFile = func(string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(src)), nil
	}
	matched, err := settings.MatchFile(".", matchName)
	if err != nil {
		if matchName != base {
			return false, relabel(err, name)
		}
		return false, err
	}
	if !matched {
		return false, nil
	}
	return compiledByGo(base, src)
}

// MatchString is Match for content held in a string.
//
// Takes name (string) which is the file's name or label.
// Takes src (string) which is the file's content.
//
// Returns bool which is true when the file is compiled.
// Returns error which reports malformed build constraints, unreadable headers, or cgo in
// test files, as Match does.
func (m *Matcher) MatchString(name, src string) (bool, error) {
	return m.Match(name, []byte(src))
}

// labelError is an error from go/build about a labelled source, reported under the
// source's own label instead of the file name it was matched as.
type labelError struct {
	// err is the error go/build returned.
	err error

	// message is err's text with the label in place of the file name.
	message string
}

// Error returns the message with the source's label.
//
// Returns string which is the message.
func (e *labelError) Error() string {
	return e.message
}

// Unwrap returns the error go/build returned.
//
// Returns error which is the original error.
func (e *labelError) Unwrap() error {
	return e.err
}

// compiledByGo excludes package documentation and cgo files after their names and build
// constraints match.
//
// Takes name (string) which is the file's base name.
// Takes src ([]byte) which is the file's content.
//
// Returns bool which is true when the file is compiled.
// Returns error when a test file uses cgo.
func compiledByGo(name string, src []byte) (bool, error) {
	file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly|parser.SkipObjectResolution)
	if err != nil {
		return true, nil
	}
	if file.Name.Name == documentationPackage {
		return false, nil
	}
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || importPath != cgoImportPath {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			return false, fmt.Errorf("use of cgo in test %s not supported", name)
		}
		return false, nil
	}
	return true, nil
}

// relabel puts a source's label in place of labelFileName in an error from go/build,
// which names the file it read in every message it builds.
//
// Takes err (error) which is the error go/build returned.
// Takes label (string) which is the source's label.
//
// Returns error which reads as if go/build had been given the label.
func relabel(err error, label string) error {
	return &labelError{err: err, message: strings.ReplaceAll(err.Error(), labelFileName, label)}
}
