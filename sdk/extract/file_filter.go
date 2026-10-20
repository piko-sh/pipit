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
	"bytes"
	"errors"
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
	// documentationPackage is the package name go/build leaves out, used by files that only
	// hold package documentation.
	documentationPackage = "documentation"

	// gcCompiler names the compiler whose build tag the interpreter sets.
	gcCompiler = "gc"
)

var (
	// errCgoInTest is returned for a test file that imports "C", which go/build refuses.
	errCgoInTest = errors.New("use of cgo in a test file is not supported")

	// errMalformedConstraint is returned when a file's build constraints cannot be read.
	errMalformedConstraint = errors.New("malformed build constraint")
)

// FileFilter reports whether the interpreter compiles a Go file.
//
// Takes name (string) which is the file's name; only the base name counts.
// Takes src ([]byte) which is the file's content.
//
// Returns bool which is true when the file is compiled.
// Returns error when the file's build constraints are malformed.
type FileFilter func(name string, src []byte) (bool, error)

// DefaultFileFilter returns the filter discovery uses when none is set.
//
// Takes buildTags ([]string) which are build tags to set besides interp.
//
// Returns FileFilter which is safe for concurrent use, and whose errors wrap
// errMalformedConstraint or errCgoInTest.
func DefaultFileFilter(buildTags []string) FileFilter {
	settings := build.Default
	settings.GOOS = runtime.GOOS
	settings.GOARCH = runtime.GOARCH
	settings.Compiler = gcCompiler
	settings.CgoEnabled = false
	settings.UseAllFiles = false
	settings.BuildTags = append([]string{interpBuildTag}, buildTags...)
	settings.ToolTags = slices.Clone(build.Default.ToolTags)
	settings.ReleaseTags = slices.Clone(build.Default.ReleaseTags)
	return func(name string, src []byte) (bool, error) {
		current := settings
		current.OpenFile = func(string) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(src)), nil
		}
		base := path.Base(filepath.ToSlash(name))
		matched, err := current.MatchFile(".", base)
		if err != nil {
			return false, fmt.Errorf("%w: %w", errMalformedConstraint, err)
		}
		if !matched {
			return false, nil
		}
		return compiledByInterpreter(base, src)
	}
}

// compiledByInterpreter excludes package documentation and cgo files after their names
// and build constraints match.
//
// Takes name (string) which is the file's base name.
// Takes src ([]byte) which is the file's content.
//
// Returns bool which is true when the file is compiled; a header that does not parse is
// kept, so the compiler can report it.
// Returns error which wraps errCgoInTest when a test file uses cgo.
func compiledByInterpreter(name string, src []byte) (bool, error) {
	file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly|parser.SkipObjectResolution)
	if err != nil {
		return true, nil
	}
	if file.Name.Name == documentationPackage {
		return false, nil
	}
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || importPath != cgoPseudoPackage {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			return false, fmt.Errorf("%w: %s", errCgoInTest, name)
		}
		return false, nil
	}
	return true, nil
}

// keepsFile reports whether discovery reads a file. A file whose build constraints are
// malformed is kept, as the interpreter's loaders keep it, so compiling it reports the
// error.
//
// Takes filter (FileFilter) which decides Go files.
// Takes name (string) which is the file's path.
// Takes src ([]byte) which is the file's content.
//
// Returns bool which is true when the file is read.
func keepsFile(filter FileFilter, name string, src []byte) bool {
	matched, err := filter(name, src)
	return err != nil || matched
}
