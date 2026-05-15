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

package compile

import (
	"context"
	"fmt"
	"go/ast"
)

// packageCompilation is the outcome of compiling one parsed package.
type packageCompilation struct {
	// Compiler is the package compiler, holding the function table and exports.
	Compiler *Compiler

	// InitFunctionIndices lists the root's init functions in run order, the variable
	// initialiser first when the package has one.
	InitFunctionIndices []uint16
}

// CompileParsedPackage compiles one type-checked package into config.RootFunction.
//
// It registers the package-level variables, compiles every function declaration in two
// passes, then the variable initialiser. Shared by the interpreter service and the
// self-hosting lane.
//
// Takes config (CompilerConfig) which carries the type information, symbol registry,
// globals and root function the package compiles into.
// Takes files ([]*ast.File) which are the package's parsed files.
// Takes importPath (string) which labels the package in diagnostics and runtime names.
// Takes crossPackageMethods (map[string]uint16) which seeds the method table with the
// methods of packages compiled earlier, nil for a single package.
//
// Returns *packageCompilation which holds the compiler and the init order.
// Returns error when a declaration or initialiser fails to compile.
//
//revive:disable-next-line:unexported-return // package-internal type stays unexported.
func CompileParsedPackage(ctx context.Context, config CompilerConfig, files []*ast.File, importPath string, crossPackageMethods map[string]uint16) (*packageCompilation, error) {
	c := NewCompiler(ctx, config)
	copyFunctionTable(c, crossPackageMethods)

	c.RegisterPackageLevelVarsFromFiles(ctx, files)

	if err := c.TwoPassCompileFuncs(ctx, files, importPath); err != nil {
		return nil, fmt.Errorf("compiling package %s: %w", importPath, err)
	}

	variableInitialisationFunction, err := c.CompileVariableInitFunction(ctx, files)
	if err != nil {
		return nil, fmt.Errorf("compiling package %s variable init: %w", importPath, err)
	}

	initIndices := c.InitFunctionIndices()
	if variableInitialisationFunction != nil {
		varInitIndex := config.RootFunction.AppendFunction(variableInitialisationFunction)
		initIndices = append([]uint16{varInitIndex}, initIndices...)
	}
	return &packageCompilation{Compiler: c, InitFunctionIndices: initIndices}, nil
}
