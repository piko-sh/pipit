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
	"slices"

	"pipit.sh/pipit/internal/symtab/typemodel"

	"pipit.sh/pipit/internal/debug"

	"pipit.sh/pipit/internal/app"

	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/stdlibindex"
)

// staticSymbols adapts a plain export table to the interpreter's symbol provider port.
type staticSymbols SymbolExports

// Exports returns the wrapped export table.
//
// Returns SymbolExports keyed by import path, then by symbol name.
func (s staticSymbols) Exports() SymbolExports {
	return SymbolExports(s)
}

// PureSurface returns the default import allowlist for the restricted and isolated tiers:
// the standard packages that compute over their inputs without touching host resources.
//
// Returns []string which is a fresh copy the caller may retain and mutate.
func PureSurface() []string {
	return policy.PureSurface()
}

// StandardLibraryPaths returns the standard-library import paths pipit can register,
// without linking their symbol tables.
//
// Returns []string which is a fresh copy the caller may retain and mutate, sorted by
// path.
func StandardLibraryPaths() []string {
	return slices.Clone(stdlibindex.ImportPaths)
}

// IsStandardLibraryPath reports whether importPath is one of those packages.
//
// Takes importPath (string) which is the full Go import path to check.
//
// Returns bool which is true for a standard-library path.
func IsStandardLibraryPath(importPath string) bool {
	return stdlibindex.Has(importPath)
}

// NewInterpreter creates an interpreter that registers no packages.
//
// Nothing is importable until a host says what interpreted code may reach. For the Go
// standard library that is one option:
//
//	pipit.NewInterpreter(stdlib.WithStandardLibrary())
//
// Takes options (...Option) which configure symbol providers, limits, dispatch mode,
// debugger attachment, environment and bytecode store wiring.
//
// Returns *Interpreter ready for Eval, Compile and Execute.
func NewInterpreter(options ...Option) *Interpreter {
	return wrapInterpreter(app.NewService(options...))
}

// NewInterpreterWithSymbols creates an interpreter registering the host packages in
// extras and nothing else.
//
// Symbols registered here shadow those of any provider passed through an option, which is
// how a host substitutes its own implementation of a standard-library package.
//
// Takes extras (SymbolExports) which are host packages to register; nil registers none.
// Takes options (...Option) which configure the interpreter.
//
// Returns *Interpreter ready for Eval, Compile and Execute.
func NewInterpreterWithSymbols(extras SymbolExports, options ...Option) *Interpreter {
	if len(extras) > 0 {
		options = append(options, app.WithSymbolProvider(staticSymbols(extras)))
	}
	service := app.NewService(options...)

	return wrapInterpreter(service)
}

// NewDebugger constructs a debugger. Attach it at construction with WithDebugger.
//
// Returns *Debugger ready to attach.
func NewDebugger() *Debugger {
	return debug.NewDebugger()
}

// RegisterIdentityTransparentFunction marks fn as a host function that compares values by
// identity rather than calling methods on them.
//
// The interpreter may wrap a value in an adapter to satisfy an interface. Comparing
// adapter pointers would never match, so registering a function here makes the
// interpreter strip adapters from arguments first. Call before the first evaluation,
// because the registry is read without a lock on the native-call path.
//
// Takes fn (any) which must be a func value.
func RegisterIdentityTransparentFunction(fn any) {
	typemodel.RegisterIdentityTransparentFunction(fn)
}

// IdentityTransparentFunctionCount reports how many functions are registered as
// identity-transparent. A host should assert a non-zero count in a test, because
// forgetting to register produces no error.
//
// Returns int which is the number of registered functions.
func IdentityTransparentFunctionCount() int {
	return typemodel.IdentityTransparentFunctionCount()
}
