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
	"go/token"
	"strings"

	"pipit.sh/pipit/internal/fault"
)

// goEmbedDirective is the comment prefix that requests build-time file embedding.
const goEmbedDirective = "//go:embed"

// rejectGoEmbed reports the first //go:embed directive among files as a compile error.
//
// Takes fileSet (*token.FileSet) which positions the diagnostic.
// Takes files ([]*ast.File) which are the parsed sources.
//
// Returns error which is nil when no file embeds anything.
func rejectGoEmbed(fileSet *token.FileSet, files []*ast.File) error {
	for _, file := range files {
		for _, declaration := range file.Decls {
			if position, found := declEmbedDirective(declaration); found {
				return embedError(fileSet, position)
			}
		}
	}
	return nil
}

// declEmbedDirective returns the position of the first //go:embed line on a var
// declaration group or on any of its specs.
//
// Takes declaration (ast.Decl) which is inspected when it is a var group.
//
// Returns the directive's position and true, or false when the declaration has none.
func declEmbedDirective(declaration ast.Decl) (token.Pos, bool) {
	genDecl, ok := declaration.(*ast.GenDecl)
	if !ok || genDecl.Tok != token.VAR {
		return token.NoPos, false
	}
	if position, found := embedDirectivePosition(genDecl.Doc); found {
		return position, true
	}
	for _, spec := range genDecl.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		if position, found := embedDirectivePosition(valueSpec.Doc); found {
			return position, true
		}
	}
	return token.NoPos, false
}

// embedDirectivePosition returns the position of the first //go:embed line in group.
//
// Takes group (*ast.CommentGroup) which may be nil.
//
// Returns the directive's position and true, or false when the group has none.
func embedDirectivePosition(group *ast.CommentGroup) (token.Pos, bool) {
	if group == nil {
		return token.NoPos, false
	}
	for _, comment := range group.List {
		text := comment.Text
		if text == goEmbedDirective || strings.HasPrefix(text, goEmbedDirective+" ") || strings.HasPrefix(text, goEmbedDirective+"\t") {
			return comment.Pos(), true
		}
	}
	return token.NoPos, false
}

// embedError builds the diagnostic for a directive at position.
//
// Takes fileSet (*token.FileSet) which resolves the position.
// Takes position (token.Pos) which is the directive's position.
//
// Returns error which wraps fault.ErrCompilation and fault.ErrCompileEmbedUnsupported.
func embedError(fileSet *token.FileSet, position token.Pos) error {
	return fmt.Errorf("%w: %w at %s", fault.ErrCompilation, fault.ErrCompileEmbedUnsupported, fileSet.Position(position))
}
