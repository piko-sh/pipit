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
	"errors"
	"fmt"
	"go/ast"
	"go/token"

	"pipit.sh/pipit/internal/fault"
)

const (
	// maxFunctionValueArity is the most parameters and results, receiver included, that
	// reflect.FuncOf builds a function type for.
	maxFunctionValueArity = 128

	// maxSignatureErrorsPerPackage caps the oversize signatures reported for one package.
	maxSignatureErrorsPerPackage = 10
)

// errTooManySignatureErrors ends a package's oversize-signature errors when more than
// maxSignatureErrorsPerPackage were found.
var errTooManySignatureErrors = errors.New("too many oversize signatures")

// signatureChecker collects the function types of a package that are too large for a
// run-time function value.
type signatureChecker struct {
	// fileSet positions the problems.
	fileSet *token.FileSet

	// methods marks method signatures so their receivers count towards the arity limit.
	methods map[*ast.FuncType]bool

	// problems holds one error for each function type that is too large.
	problems *cappedErrors
}

// visit marks method signatures and checks function types during ast.Inspect.
//
// Takes node (ast.Node) which is the node visited.
//
// Returns bool which is always true, so the walk continues.
func (c *signatureChecker) visit(node ast.Node) bool {
	switch typed := node.(type) {
	case *ast.FuncDecl:
		if typed.Recv != nil {
			c.methods[typed.Type] = true
		}
	case *ast.InterfaceType:
		for _, method := range typed.Methods.List {
			if signature, ok := method.Type.(*ast.FuncType); ok {
				c.methods[signature] = true
			}
		}
	case *ast.FuncType:
		c.check(typed)
	}
	return true
}

// check records a problem when a function type has more parameters and results, its
// receiver included, than a run-time function value can carry.
//
// Takes signature (*ast.FuncType) which is the function type to check.
func (c *signatureChecker) check(signature *ast.FuncType) {
	arity := fieldCount(signature.Params) + fieldCount(signature.Results)
	if c.methods[signature] {
		arity++
	}
	if arity > maxFunctionValueArity {
		c.problems.add(fmt.Errorf("%w at %s: %d, at most %d",
			fault.ErrCompileSignatureTooLarge, c.fileSet.Position(signature.Pos()), arity, maxFunctionValueArity))
	}
}

// rejectOversizeSignatures reports every function type in files with more parameters and
// results than a function value can carry at run time. Go accepts such functions, but the
// interpreter represents every function as a reflect function type, which reflect.FuncOf
// caps; without this check the compiler would panic.
//
// Takes fileSet (*token.FileSet) which positions the files.
// Takes files ([]*ast.File) which are the files to check.
//
// Returns error which joins one error per oversize signature, up to
// maxSignatureErrorsPerPackage, or nil.
func rejectOversizeSignatures(fileSet *token.FileSet, files []*ast.File) error {
	checker := signatureChecker{
		fileSet:  fileSet,
		methods:  make(map[*ast.FuncType]bool),
		problems: newCappedErrors(maxSignatureErrorsPerPackage),
	}
	for _, file := range files {
		ast.Inspect(file, checker.visit)
	}
	return checker.problems.join(errTooManySignatureErrors)
}

// fieldCount counts the entries a parameter or result list declares; an unnamed field
// counts once and a field with several names once per name.
//
// Takes fields (*ast.FieldList) which may be nil.
//
// Returns int which is the number of entries.
func fieldCount(fields *ast.FieldList) int {
	if fields == nil {
		return 0
	}
	count := 0
	for _, field := range fields.List {
		count += max(len(field.Names), 1)
	}
	return count
}
