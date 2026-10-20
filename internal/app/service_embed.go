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
	"fmt"
	"go/ast"
	"go/scanner"
	"go/token"
	"strings"

	"pipit.sh/pipit/internal/fault"
)

const (
	// goEmbedDirective is the comment prefix that requests build-time file embedding.
	goEmbedDirective = "//go:embed"

	// goDirectiveTool is the tool part of the go toolchain's //go:name directives.
	goDirectiveTool = "go"

	// embedDirectiveName is the name part of the //go:embed directive.
	embedDirectiveName = "embed"

	// embedBuildTagHint describes the build-constraint pattern that keeps an embedding file
	// out of the interpreter while the compiled binary still embeds.
	embedBuildTagHint = "hint: move the //go:embed variable into a file guarded by " +
		"//go:build !interp, and declare it again in a //go:build interp file that reads the " +
		"files at run time, for example var content fs.FS = os.DirFS(\"content\"). The " +
		"interpreter sets the interp build tag, and resolves a relative os.DirFS path against " +
		"the working directory."
)

// rejectGoEmbed reports every //go:embed directive in the filtered files as one compile
// error.
//
// Takes fileSet (*token.FileSet) which positions the diagnostic.
// Takes packagePath (string) which names the package in the message; empty for a file
// set.
// Takes files ([]*ast.File) which are the parsed, filtered sources, with comments.
//
// Returns error which is nil when no file embeds anything.
func rejectGoEmbed(fileSet *token.FileSet, packagePath string, files []*ast.File) error {
	var positions []token.Pos
	for _, file := range files {
		for _, group := range file.Comments {
			for _, comment := range group.List {
				if isEmbedDirective(comment.Slash, comment.Text) {
					positions = append(positions, comment.Slash)
				}
			}
		}
	}
	if len(positions) == 0 {
		return nil
	}
	return embedError(fileSet, packagePath, positions)
}

// rejectGoEmbedSource checks for //go:embed directives by tokenising source, including
// incomplete snippets parsed without comments.
//
// Takes name (string) which names the source in positions.
// Takes source (string) which is the source text.
//
// Returns error which is nil when nothing embeds.
func rejectGoEmbedSource(name, source string) error {
	if !strings.Contains(source, goEmbedDirective) {
		return nil
	}
	fileSet := token.NewFileSet()
	file := fileSet.AddFile(name, -1, len(source))
	var tokens scanner.Scanner
	tokens.Init(file, []byte(source), nil, scanner.ScanComments)
	var positions []token.Pos
	for {
		position, kind, literal := tokens.Scan()
		if kind == token.EOF {
			break
		}
		if kind == token.COMMENT && isEmbedDirective(position, literal) {
			positions = append(positions, position)
		}
	}
	if len(positions) == 0 {
		return nil
	}
	return embedError(fileSet, "", positions)
}

// isEmbedDirective reports whether a comment is a //go:embed directive.
//
// Takes position (token.Pos) which is where the comment starts.
// Takes text (string) which is the comment's text, including its marker.
//
// Returns bool which is true for the directive, with or without arguments.
func isEmbedDirective(position token.Pos, text string) bool {
	directive, ok := ast.ParseDirective(position, text)
	return ok && directive.Tool == goDirectiveTool && directive.Name == embedDirectiveName
}

// embedError formats the package name, directive positions and build-tag hint as a
// compile error.
//
// Takes fileSet (*token.FileSet) which resolves the positions.
// Takes packagePath (string) which names the package; empty for a file set.
// Takes positions ([]token.Pos) which are the directives' positions, at least one.
//
// Returns error which wraps fault.ErrCompilation and fault.ErrCompileEmbedUnsupported.
func embedError(fileSet *token.FileSet, packagePath string, positions []token.Pos) error {
	locations := make([]string, len(positions))
	for index, position := range positions {
		locations[index] = fileSet.Position(position).String()
	}
	where := strings.Join(locations, ", ")
	if packagePath == "" {
		return fmt.Errorf("%w: %w at %s\n\n%s", fault.ErrCompilation, fault.ErrCompileEmbedUnsupported, where, embedBuildTagHint)
	}
	return fmt.Errorf("%w: %s: %w at %s\n\n%s",
		fault.ErrCompilation, packagePath, fault.ErrCompileEmbedUnsupported, where, embedBuildTagHint)
}
