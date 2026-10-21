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

// Package gosource builds Go source files from go/ast nodes for Pipit's code generators,
// and prints them in gofmt's form.
package gosource

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"reflect"
	"strconv"
	"strings"
)

// printerTabWidth is the tab width go/printer lays out with, gofmt's own.
const printerTabWidth = 8

var (
	// errNoPackage reports a file rendered without a package name.
	errNoPackage = errors.New("gosource: the file has no package name")

	// errNoDeclarationNode reports a declaration without a node to print.
	errNoDeclarationNode = errors.New("gosource: a declaration has no node")
)

// File is one generated Go source file.
type File struct {
	// Header is the text before the build constraint, including comment markers.
	Header string

	// BuildConstraint is the build expression, or empty for none.
	BuildConstraint string

	// Package is the package name.
	Package string

	// ImportGroups holds the import paths, one group per blank-line-separated block.
	ImportGroups [][]Import

	// Declarations holds the top-level declarations in order.
	Declarations []Declaration

	// LoneImportUnparenthesised prints a file's only import without parentheses.
	LoneImportUnparenthesised bool
}

// Import is one import of a file.
type Import struct {
	// Path is the import path.
	Path string

	// Name is the import's local name, or empty for the package's own.
	Name string
}

// Declaration is one top-level declaration and its doc comment.
type Declaration struct {
	// Node is the declaration.
	Node ast.Decl

	// Doc holds the comment's lines without their markers.
	Doc []string

	// Directives holds the compiler directives that follow the comment, such as
	// "go:nosplit", without their markers.
	Directives []string
}

// Render prints the file in gofmt's form.
//
// Returns []byte which is the formatted source.
// Returns error when the file has no package name, a declaration has no node, a node
// cannot be printed, or the printed source does not parse.
func (f *File) Render() ([]byte, error) {
	if f.Package == "" {
		return nil, errNoPackage
	}
	var source bytes.Buffer
	if f.Header != "" {
		_, _ = source.WriteString(strings.TrimRight(f.Header, "\n"))
		_, _ = source.WriteString("\n\n")
	}
	if f.BuildConstraint != "" {
		_, _ = source.WriteString("//go:build " + f.BuildConstraint + "\n\n")
	}
	_, _ = source.WriteString("package " + f.Package + "\n")
	if imports := f.importDeclaration(); imports != nil {
		printed, err := printNode(imports)
		if err != nil {
			return nil, fmt.Errorf("printing the imports: %w", err)
		}
		_, _ = source.WriteString("\n" + printed + "\n")
	}
	for index, declaration := range f.Declarations {
		if err := declaration.write(&source); err != nil {
			return nil, fmt.Errorf("declaration %d: %w", index, err)
		}
	}
	formatted, err := format.Source(source.Bytes())
	if err != nil {
		return nil, fmt.Errorf("formatting the generated source: %w", err)
	}
	return formatted, nil
}

// Expression parses a Go expression into nodes with no positions.
//
// Takes source (string) which is the expression.
//
// Returns ast.Expr which is the expression's nodes.
// Returns error when source is not an expression.
func Expression(source string) (ast.Expr, error) {
	expression, err := parser.ParseExpr(source)
	if err != nil {
		return nil, fmt.Errorf("gosource: parsing %q: %w", source, err)
	}
	positionType := reflect.TypeFor[token.Pos]()
	ast.Inspect(expression, func(node ast.Node) bool {
		if node == nil {
			return false
		}
		for _, field := range reflect.ValueOf(node).Elem().Fields() {
			if field.Type() == positionType {
				field.SetInt(int64(token.NoPos))
			}
		}
		return true
	})
	return expression, nil
}

// write prints the declaration after a blank line, below its doc comment and directives.
//
// Takes source (*bytes.Buffer) which receives the text.
//
// Returns error when the declaration has no node or the node cannot be printed.
func (d *Declaration) write(source *bytes.Buffer) error {
	if d.Node == nil {
		return errNoDeclarationNode
	}
	printed, err := printNode(d.Node)
	if err != nil {
		return fmt.Errorf("printing: %w", err)
	}
	_, _ = source.WriteString("\n")
	for _, line := range d.Doc {
		_, _ = source.WriteString(strings.TrimRight("// "+line, " ") + "\n")
	}
	for _, directive := range d.Directives {
		_, _ = source.WriteString("//" + directive + "\n")
	}
	_, _ = source.WriteString(printed + "\n")
	return nil
}

// importDeclaration builds the file's import declaration.
//
// Returns *importDeclaration which is the declaration and where its groups start, or nil
// when the file imports nothing.
func (f *File) importDeclaration() *importDeclaration {
	declaration := &importDeclaration{
		node:        &ast.GenDecl{Doc: nil, TokPos: token.NoPos, Tok: token.IMPORT, Lparen: token.NoPos, Specs: nil, Rparen: token.NoPos},
		groupStarts: nil,
		lone:        f.LoneImportUnparenthesised,
	}
	for _, group := range f.ImportGroups {
		if len(group) == 0 {
			continue
		}
		declaration.groupStarts = append(declaration.groupStarts, len(declaration.node.Specs))
		for _, imported := range group {
			spec := &ast.ImportSpec{
				Doc: nil, Name: nil, Comment: nil, EndPos: token.NoPos,
				Path: &ast.BasicLit{ValuePos: token.NoPos, Kind: token.STRING, Value: strconv.Quote(imported.Path)},
			}
			if imported.Name != "" {
				spec.Name = ast.NewIdent(imported.Name)
			}
			declaration.node.Specs = append(declaration.node.Specs, spec)
		}
	}
	if len(declaration.node.Specs) == 0 {
		return nil
	}
	return declaration
}

// printNode prints one node through go/printer, with the line breaks its layout asks for.
//
// Takes node (ast.Node) which is a declaration or an import declaration.
//
// Returns string which is the printed node.
// Returns error when go/printer fails.
func printNode(node ast.Node) (string, error) {
	lines := newLayout(node)
	var printed bytes.Buffer
	config := printer.Config{Mode: printer.TabIndent | printer.UseSpaces, Tabwidth: printerTabWidth, Indent: 0}
	target := node
	if imports, ok := node.(*importDeclaration); ok {
		target = imports.node
	}
	var comments []*ast.CommentGroup
	ast.Inspect(target, func(child ast.Node) bool {
		if group, ok := child.(*ast.CommentGroup); ok {
			comments = append(comments, group)
		}
		return true
	})
	var printable any = target
	if len(comments) > 0 {
		printable = &printer.CommentedNode{Node: target, Comments: comments}
	}
	if err := config.Fprint(&printed, lines.fileSet, printable); err != nil {
		return "", fmt.Errorf("printing %T: %w", target, err)
	}
	return printed.String(), nil
}
