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

package pipit

import (
	"fmt"

	"pipit.sh/pipit/internal/buildfilter"
	"pipit.sh/pipit/internal/fault"
)

// BuildTag is the build tag the interpreter sets when it decides which files to compile.
// Keep a file the interpreter cannot compile out with `//go:build !interp`, and give the
// interpreter its own version with `//go:build interp`.
const BuildTag = buildfilter.InterpTag

// FileFilter reports whether the interpreter compiles a file. It uses only the base name,
// applying just build constraints to sources without a .go suffix.
//
// Takes name (string) which is the file's name.
// Takes src ([]byte) which is the file's content.
//
// Returns bool which is true when the file is compiled.
// Returns error which wraps ErrParse when the file's build constraints are malformed, its
// header cannot be read (for example it holds a NUL byte), or a test file imports "C".
type FileFilter func(name string, src []byte) (bool, error)

// NewFileFilter returns the filter the interpreter applies to a package's files. It
// decides as go build does for the host's GOOS and GOARCH with BuildTag and tags set, the
// gc compiler and cgo off, and also leaves out package documentation files and files that
// import "C".
//
// Takes tags (...string) which are build tags to set besides BuildTag, as WithBuildTags
// adds them.
//
// Returns FileFilter which is safe for concurrent use.
func NewFileFilter(tags ...string) FileFilter {
	matcher := buildfilter.New(tags)
	return func(name string, src []byte) (bool, error) {
		matched, err := matcher.Match(name, src)
		if err != nil {
			return false, fmt.Errorf(fault.ErrChainFmt, ErrParse, err)
		}
		return matched, nil
	}
}

// MatchFile reports whether this interpreter compiles a file, with the build tags
// WithBuildTags added; NewFileFilter describes the rules.
//
// Takes name (string) which is the file's name.
// Takes src ([]byte) which is the file's content.
//
// Returns bool which is true when the file is compiled.
// Returns error which wraps ErrParse, as for FileFilter.
func (i *Interpreter) MatchFile(name string, src []byte) (bool, error) {
	return i.service.MatchFile(name, src)
}
