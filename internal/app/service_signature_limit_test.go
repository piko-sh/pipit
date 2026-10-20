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
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
)

func TestRejectOversizeSignaturesCapsTheErrors(t *testing.T) {
	t.Parallel()

	parameters := strings.Repeat("int, ", maxFunctionValueArity) + "int"
	var source strings.Builder
	source.WriteString("package p\n\n")
	for index := range maxSignatureErrorsPerPackage + 3 {
		fmt.Fprintf(&source, "func f%d(%s) {}\n", index, parameters)
	}
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "p.go", source.String(), 0)
	require.NoError(t, err)

	err = rejectOversizeSignatures(fileSet, []*ast.File{file})
	require.ErrorIs(t, err, fault.ErrCompileSignatureTooLarge)
	require.ErrorIs(t, err, errTooManySignatureErrors)
	require.Equal(t, maxSignatureErrorsPerPackage, strings.Count(err.Error(), fault.ErrCompileSignatureTooLarge.Error()))
	require.Contains(t, err.Error(), "(3 more not shown)")
}

func TestRejectOversizeSignaturesCountsTheReceiver(t *testing.T) {
	t.Parallel()

	parameters := strings.Repeat("int, ", maxFunctionValueArity-1) + "int"
	source := "package p\n\ntype T struct{}\n\nfunc (T) m(" + parameters + ") {}\n\nfunc f(" + parameters + ") {}\n"
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "p.go", source, 0)
	require.NoError(t, err)

	err = rejectOversizeSignatures(fileSet, []*ast.File{file})
	require.ErrorIs(t, err, fault.ErrCompileSignatureTooLarge)
	require.Equal(t, 1, strings.Count(err.Error(), fault.ErrCompileSignatureTooLarge.Error()), "only the method is too large")
	require.Contains(t, err.Error(), "p.go:5:")
}
