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

package gosource

import (
	"go/ast"
	"go/token"
)

// layoutSlack reserves lines for added braces and gaps.
const layoutSlack = 8

// lineMark marks composite literals and parameter lists for layout.
const lineMark = token.Pos(1)

// unplacedLine is the line-1 position reserved for unplaced nodes.
const unplacedLine = token.Pos(1)

// lineWidth is the number of position offsets reserved for each line.
const lineWidth = 1 << 12

// importDeclaration wraps an import declaration with its group boundaries.
type importDeclaration struct {
	// node is the declaration.
	node *ast.GenDecl

	// groupStarts holds the index of each group's first spec.
	groupStarts []int

	// lone prints a single import without parentheses.
	lone bool
}

// layout gives positions to the parts of a node that must start lines of their own.
type layout struct {
	// fileSet holds file.
	fileSet *token.FileSet

	// file is the file whose lines the positions lie on.
	file *token.File

	// line is the last line handed out.
	line int
}

// Pos returns the position of the declaration's first token.
//
// Returns token.Pos which is the declaration's own.
func (d *importDeclaration) Pos() token.Pos { return d.node.Pos() }

// End returns the position just past the declaration.
//
// Returns token.Pos which is the declaration's own.
func (d *importDeclaration) End() token.Pos { return d.node.End() }

// next hands out the start of the next line.
//
// Returns token.Pos which is that line's position.
func (l *layout) next() token.Pos {
	l.line++
	return l.file.LineStart(l.line)
}

// skip leaves the next line empty, which go/printer prints as one blank line.
func (l *layout) skip() {
	l.line++
}

// place gives positions to the parts within a node that need a layout of their own, in
// the order go/printer prints them.
//
// Takes node (ast.Node) which is the node to lay out.
func (l *layout) place(node ast.Node) {
	ast.Inspect(node, func(child ast.Node) bool {
		return !l.placeChild(child)
	})
}

// placeChild lays out one node when it needs a layout of its own.
//
// Takes child (ast.Node) which is the node.
//
// Returns bool which is true when the node and everything inside it are laid out.
func (l *layout) placeChild(child ast.Node) bool {
	switch typed := child.(type) {
	case *ast.FuncDecl:
		if typed.Body == nil {
			return false
		}
		l.placeFunction(typed)
	case *ast.FuncType:
		if typed.Params == nil || typed.Params.Opening != lineMark {
			return false
		}
		l.placeParameters(typed.Params)
	case *ast.StructType:
		return l.placeStruct(typed)
	case *ast.CallExpr:
		if typed.Lparen != lineMark {
			return false
		}
		l.placeArguments(typed)
	case *ast.CompositeLit:
		if typed.Lbrace == token.NoPos {
			return false
		}
		l.placeLiteral(typed, token.NoPos)
	default:
		return false
	}
	return true
}

// placeStruct keeps an empty struct's braces on one line and lays out a struct whose
// fields carry doc comments.
//
// Takes structure (*ast.StructType) which is the struct.
//
// Returns bool which is true when the struct is laid out.
func (l *layout) placeStruct(structure *ast.StructType) bool {
	switch {
	case len(structure.Fields.List) == 0:
		structure.Fields.Opening = unplacedLine
		structure.Fields.Closing = unplacedLine
	case fieldsDocumented(structure.Fields):
		l.placeDocumentedFields(structure.Fields)
	default:
		return false
	}
	return true
}

// placeDocumentedFields puts each field of a struct on a line of its own below its doc
// comment, with a blank line between fields.
//
// Takes fields (*ast.FieldList) which is the struct's field list.
func (l *layout) placeDocumentedFields(fields *ast.FieldList) {
	fields.Opening = l.next()
	for index, field := range fields.List {
		if index > 0 {
			l.skip()
		}
		if field.Doc != nil {
			for _, comment := range field.Doc.List {
				comment.Slash = l.next()
			}
		}
		start := l.next()
		for _, name := range field.Names {
			name.NamePos = start
		}
		placeStart(field.Type, start)
	}
	fields.Closing = l.next()
}

// fieldsDocumented reports whether any field of a list carries a doc comment.
//
// Takes fields (*ast.FieldList) which is the list.
//
// Returns bool which is true when a field has one.
func fieldsDocumented(fields *ast.FieldList) bool {
	for _, field := range fields.List {
		if field.Doc != nil {
			return true
		}
	}
	return false
}

// placeArguments puts each argument of a marked call on a line of its own, and its
// closing parenthesis on the line after them.
//
// Takes call (*ast.CallExpr) which is the marked call.
func (l *layout) placeArguments(call *ast.CallExpr) {
	l.place(call.Fun)
	call.Lparen = l.next()
	for _, argument := range call.Args {
		l.placeElement(argument, l.next())
	}
	call.Rparen = l.next()
}

// placeParameters puts a marked parameter list's parameters on one line of their own,
// between lines holding its parentheses.
//
// Takes parameters (*ast.FieldList) which is the marked list.
func (l *layout) placeParameters(parameters *ast.FieldList) {
	parameters.Opening = l.next()
	start := l.next()
	for _, parameter := range parameters.List {
		for _, name := range parameter.Names {
			name.NamePos = start
		}
		placeStart(parameter.Type, start)
	}
	parameters.Closing = l.next()
}

// placeFunction puts a function body's braces on lines of their own.
//
// Takes function (*ast.FuncDecl) which is a function with a body.
func (l *layout) placeFunction(function *ast.FuncDecl) {
	l.place(function.Type)
	function.Body.Lbrace = l.next()
	for _, statement := range function.Body.List {
		returned, ok := statement.(*ast.ReturnStmt)
		if !ok {
			l.place(statement)
			continue
		}
		returned.Return = l.next()
		for _, result := range returned.Results {
			if literal, isLiteral := result.(*ast.CompositeLit); isLiteral && literal.Lbrace != token.NoPos {
				l.placeLiteral(literal, returned.Return)
				continue
			}
			l.place(result)
		}
	}
	function.Body.Rbrace = l.next()
}

// placeLiteral puts a marked composite literal's closing brace and each of its elements
// on a line of its own, and lays out the literals the elements hold. The opening brace
// stays on the line the literal starts on.
//
// Takes literal (*ast.CompositeLit) which is the marked literal.
// Takes start (token.Pos) which is the line the literal starts on, or token.NoPos to
// start it on a new one.
func (l *layout) placeLiteral(literal *ast.CompositeLit, start token.Pos) {
	if start == token.NoPos {
		start = l.next()
	}
	if literal.Type != nil {
		placeStart(literal.Type, start)
	}
	literal.Lbrace = start
	for _, element := range literal.Elts {
		l.placeElement(element, l.next())
	}
	literal.Rbrace = l.next()
}

// placeElement starts one element of a marked literal on the given line and lays out the
// literals it holds, keeping a literal that opens the element, or the value of a keyed
// element, on that same line.
//
// Takes element (ast.Expr) which is the element.
// Takes start (token.Pos) which is the element's line.
func (l *layout) placeElement(element ast.Expr, start token.Pos) {
	if nested, ok := element.(*ast.CompositeLit); ok && nested.Lbrace != token.NoPos {
		l.placeLiteral(nested, start)
		return
	}
	placeStart(element, start)
	if inline, ok := element.(*ast.CompositeLit); ok {
		for _, part := range inline.Elts {
			l.place(part)
		}
		return
	}
	if keyed, ok := element.(*ast.KeyValueExpr); ok {
		l.place(keyed.Key)
		if nested, isLiteral := keyed.Value.(*ast.CompositeLit); isLiteral && nested.Lbrace != token.NoPos {
			l.placeLiteral(nested, start)
			return
		}
		l.place(keyed.Value)
		return
	}
	l.place(element)
}

// placeImports puts each import on a line of its own and a blank line between groups.
//
// Takes declaration (*importDeclaration) which is the file's imports.
func (l *layout) placeImports(declaration *importDeclaration) {
	if declaration.lone && len(declaration.node.Specs) == 1 {
		return
	}
	declaration.node.Lparen = l.next()
	groupIndex := 0
	for index, spec := range declaration.node.Specs {
		if groupIndex < len(declaration.groupStarts) && declaration.groupStarts[groupIndex] == index {
			if index > 0 {
				l.skip()
			}
			groupIndex++
		}
		imported, ok := spec.(*ast.ImportSpec)
		if !ok {
			continue
		}
		position := l.next()
		imported.Path.ValuePos = position
		if imported.Name != nil {
			imported.Name.NamePos = position
		}
	}
	declaration.node.Rparen = l.next()
}

// Lines marks a composite literal to be printed with its braces and each element on a
// line of its own, as gofmt prints a literal written that way.
//
// Takes literal (*ast.CompositeLit) which is the literal to mark.
//
// Returns *ast.CompositeLit which is the same literal.
func Lines(literal *ast.CompositeLit) *ast.CompositeLit {
	literal.Lbrace = lineMark
	return literal
}

// ArgumentLines marks a call to print each argument on its own line.
//
// Takes call (*ast.CallExpr) which is the call to mark.
//
// Returns *ast.CallExpr which is the same call.
func ArgumentLines(call *ast.CallExpr) *ast.CallExpr {
	call.Lparen = lineMark
	return call
}

// ParameterLine marks a parameter list to print its parameters on one line between the
// parentheses.
//
// Takes parameters (*ast.FieldList) which is the list to mark.
//
// Returns *ast.FieldList which is the same list.
func ParameterLine(parameters *ast.FieldList) *ast.FieldList {
	parameters.Opening = lineMark
	return parameters
}

// newLayout builds the layout of one node and gives positions to its marked parts.
//
// Takes node (ast.Node) which is a declaration or an *importDeclaration.
//
// Returns *layout which holds the file set to print the node through.
func newLayout(node ast.Node) *layout {
	size := layoutSlack
	countTarget := node
	if imports, ok := node.(*importDeclaration); ok {
		countTarget = imports.node
	}
	ast.Inspect(countTarget, func(child ast.Node) bool {
		if child != nil {
			size += 2
		}
		return true
	})
	fileSet := token.NewFileSet()
	file := fileSet.AddFile("generated.go", -1, size*lineWidth)
	lineOffsets := make([]int, size)
	for index := range lineOffsets {
		lineOffsets[index] = index * lineWidth
	}
	file.SetLines(lineOffsets)
	lines := &layout{fileSet: fileSet, file: file, line: 1}
	if imports, ok := node.(*importDeclaration); ok {
		lines.placeImports(imports)
		return lines
	}
	lines.place(node)
	return lines
}

// placeStart gives position to the first token of an expression, so that the expression
// starts a line.
//
// Takes expression (ast.Expr) which is the expression.
// Takes position (token.Pos) which is the position to give it.
func placeStart(expression ast.Expr, position token.Pos) {
	switch typed := expression.(type) {
	case *ast.Ident:
		typed.NamePos = position
	case *ast.BasicLit:
		typed.ValuePos = position
	case *ast.CompositeLit:
		if typed.Type != nil {
			placeStart(typed.Type, position)
			return
		}
		typed.Lbrace = position
	case *ast.KeyValueExpr:
		placeStart(typed.Key, position)
	case *ast.SelectorExpr:
		placeStart(typed.X, position)
	case *ast.CallExpr:
		placeStart(typed.Fun, position)
	case *ast.IndexExpr:
		placeStart(typed.X, position)
	case *ast.IndexListExpr:
		placeStart(typed.X, position)
	case *ast.SliceExpr:
		placeStart(typed.X, position)
	case *ast.TypeAssertExpr:
		placeStart(typed.X, position)
	case *ast.BinaryExpr:
		placeStart(typed.X, position)
	case *ast.UnaryExpr:
		typed.OpPos = position
	case *ast.StarExpr:
		typed.Star = position
	case *ast.ParenExpr:
		typed.Lparen = position
	case *ast.FuncLit:
		typed.Type.Func = position
	default:
		placeTypeStart(expression, position)
	}
}

// placeTypeStart gives position to the first token of a type expression.
//
// Takes expression (ast.Expr) which is the type.
// Takes position (token.Pos) which is the position to give it.
func placeTypeStart(expression ast.Expr, position token.Pos) {
	switch typed := expression.(type) {
	case *ast.ArrayType:
		typed.Lbrack = position
	case *ast.MapType:
		typed.Map = position
	case *ast.FuncType:
		typed.Func = position
	case *ast.StructType:
		typed.Struct = position
	case *ast.InterfaceType:
		typed.Interface = position
	case *ast.ChanType:
		typed.Begin = position
	}
}
